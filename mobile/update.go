package nexdesk

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/eirahoutmoss/remotesupport/shared/update"
)

// ApkName is the release asset the Android app updates itself from.
const ApkName = "NexDesk.apk"

// UpdateInfo describes a newer release; nil from CheckUpdate means up to date.
type UpdateInfo struct {
	Version string
	Notes   string
	PageURL string
	apkURL  string
	sigURL  string
}

// CheckUpdate asks GitHub for the newest release. It returns nil (and no
// error) when currentVersion is already the newest or the release has no
// signed APK.
func CheckUpdate(currentVersion string) (*UpdateInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rel, err := update.Latest(ctx, &http.Client{Timeout: 15 * time.Second}, "https://api.github.com", update.DefaultRepo, ApkName)
	if err == update.ErrNoRelease {
		return nil, nil
	}
	if rel == nil {
		return nil, err
	}
	if !update.Newer(rel.Version, currentVersion) || rel.ExeURL == "" || rel.SigURL == "" {
		return nil, nil
	}
	return &UpdateInfo{Version: rel.Version, Notes: rel.Notes, PageURL: rel.PageURL, apkURL: rel.ExeURL, sigURL: rel.SigURL}, nil
}

// DownloadUpdate fetches the APK, checks its Ed25519 signature against the
// key built into this app, and writes it to destPath only if it verifies.
func DownloadUpdate(u *UpdateInfo, destPath string) error {
	key, err := update.ParsePublicKey(update.DefaultPublicKey)
	if err != nil {
		return err
	}
	if key == nil {
		return errNoKey
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	hc := &http.Client{Timeout: 10 * time.Minute}
	sig, err := update.Download(ctx, hc, u.sigURL, 4096)
	if err != nil {
		return err
	}
	data, err := update.Download(ctx, hc, u.apkURL, update.MaxExeSize)
	if err != nil {
		return err
	}
	if err := update.Verify(key, ApkName, u.Version, data, sig); err != nil {
		return err
	}
	tmp := destPath + ".part"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, destPath)
}

type keyErr string

func (e keyErr) Error() string { return string(e) }

const errNoKey = keyErr("güncelleme anahtarı bu derlemede yok")
