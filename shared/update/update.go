// Package update finds, downloads and verifies NexDesk releases published on
// GitHub. A release must carry the executable and a detached Ed25519
// signature (<exe>.sig). The signature covers the file name, the version and
// the SHA-256 of the file, so an older signed build cannot be served as a
// newer one and a compromised GitHub account alone cannot push an update.
package update

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// DefaultRepo is the GitHub repository releases are read from.
const DefaultRepo = "Eirahoutmoss/NexDesk"

// MaxExeSize bounds the download so a bad release cannot fill the disk.
const MaxExeSize = 200 << 20

// Release describes the newest published release.
type Release struct {
	Version string // "1.2.1" (tag without a leading v)
	PageURL string // release page, for manual download
	Notes   string
	ExeURL  string
	SigURL  string
	ExeSize int64
}

type ghRelease struct {
	TagName    string `json:"tag_name"`
	HTMLURL    string `json:"html_url"`
	Body       string `json:"body"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
	Assets     []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
		Size int64  `json:"size"`
	} `json:"assets"`
}

// Latest asks GitHub for the newest non-draft, non-prerelease release.
// apiBase is normally "https://api.github.com" (overridable for tests).
func Latest(ctx context.Context, hc *http.Client, apiBase, repo, exeName string) (*Release, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiBase, "/")+"/repos/"+repo+"/releases/latest", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "NexDesk-updater")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, ErrNoRelease
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: GitHub yanıtı %s", resp.Status)
	}
	var gr ghRelease
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&gr); err != nil {
		return nil, err
	}
	return fromGitHub(gr, exeName)
}

// ErrNoRelease means the repository has no published release yet.
var ErrNoRelease = errors.New("update: yayımlanmış sürüm yok")

func fromGitHub(gr ghRelease, exeName string) (*Release, error) {
	if gr.Draft || gr.Prerelease {
		return nil, ErrNoRelease
	}
	r := &Release{Version: strings.TrimPrefix(strings.TrimSpace(gr.TagName), "v"), PageURL: gr.HTMLURL, Notes: gr.Body}
	for _, a := range gr.Assets {
		switch {
		case strings.EqualFold(a.Name, exeName):
			r.ExeURL, r.ExeSize = a.URL, a.Size
		case strings.EqualFold(a.Name, exeName+".sig"):
			r.SigURL = a.URL
		}
	}
	if r.Version == "" {
		return nil, errors.New("update: sürüm etiketi boş")
	}
	if r.ExeURL == "" || r.SigURL == "" {
		return r, fmt.Errorf("update: sürüm %s içinde %s veya imzası yok", r.Version, exeName)
	}
	return r, nil
}

// Download fetches url, refusing bodies larger than max bytes.
func Download(ctx context.Context, hc *http.Client, url string, max int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NexDesk-updater")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("update: indirme yanıtı %s", resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, errors.New("update: dosya beklenenden büyük")
	}
	return b, nil
}

// Message is the byte string that gets signed for one release file.
func Message(name, version string, data []byte) []byte {
	sum := sha256.Sum256(data)
	return []byte("nexdesk-update-v1|" + strings.ToLower(name) + "|" + strings.TrimPrefix(version, "v") + "|" + hex.EncodeToString(sum[:]))
}

// Sign returns the base64 signature text stored in <exe>.sig.
func Sign(priv ed25519.PrivateKey, name, version string, data []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, Message(name, version, data)))
}

// Verify checks a .sig file's content against the downloaded file.
func Verify(pub ed25519.PublicKey, name, version string, data, sigText []byte) error {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return errors.New("update: imza dosyası bozuk")
	}
	if !ed25519.Verify(pub, Message(name, version, data), sig) {
		return errors.New("update: imza doğrulanamadı — dosya kurulmadı")
	}
	return nil
}

// ParsePublicKey decodes a base64 Ed25519 public key; "" yields nil, nil.
func ParsePublicKey(s string) (ed25519.PublicKey, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil, errors.New("update: genel anahtar geçersiz")
	}
	return ed25519.PublicKey(b), nil
}

// Newer reports whether version a is newer than b (dotted numeric).
func Newer(a, b string) bool { return cmp(parse(a), parse(b)) > 0 }

func parse(v string) []int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		out = append(out, n)
	}
	return out
}

func cmp(a, b []int) int {
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y int
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}
