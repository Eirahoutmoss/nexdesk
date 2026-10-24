package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignVerify(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	data := []byte("exe-bytes")
	sig := Sign(priv, "NexDesk.exe", "1.3.0", data)
	if err := Verify(pub, "NexDesk.exe", "v1.3.0", data, []byte(sig+"\n")); err != nil {
		t.Fatal(err)
	}
	if Verify(pub, "NexDesk.exe", "1.3.1", data, []byte(sig)) == nil {
		t.Fatal("signature must be bound to the version")
	}
	if Verify(pub, "NexDesk.exe", "1.3.0", []byte("tampered"), []byte(sig)) == nil {
		t.Fatal("tampered file accepted")
	}
	k, err := ParsePublicKey(base64.StdEncoding.EncodeToString(pub))
	if err != nil || !k.Equal(pub) {
		t.Fatal("public key round trip")
	}
}

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{{"1.2.1", "1.2.0", true}, {"v1.10.0", "1.9.9", true}, {"1.2.0", "1.2.0", false}, {"1.2", "1.2.0", false}, {"1.1.9", "1.2.0", false}}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%s,%s)=%v", c.a, c.b, got)
		}
	}
}

func TestLatest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/releases/latest" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"tag_name":"v1.3.0","html_url":"https://x/rel","assets":[{"name":"NexDesk.exe","browser_download_url":"https://x/e","size":5},{"name":"NexDesk.exe.sig","browser_download_url":"https://x/s"}]}`))
	}))
	defer ts.Close()
	r, err := Latest(context.Background(), ts.Client(), ts.URL, "o/r", "NexDesk.exe")
	if err != nil || r.Version != "1.3.0" || r.ExeURL != "https://x/e" || r.SigURL != "https://x/s" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := Latest(context.Background(), ts.Client(), ts.URL, "o/none", "NexDesk.exe"); err != ErrNoRelease {
		t.Fatalf("missing repo: %v", err)
	}
}
