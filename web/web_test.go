package web

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServesApp(t *testing.T) {
	ts := httptest.NewServer(Handler())
	defer ts.Close()
	for _, p := range []string{"/", "/app.js", "/style.css", "/manifest.webmanifest", "/sw.js", "/icon-192.png"} {
		resp, err := ts.Client().Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || len(b) == 0 {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
		if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "default-src 'self'") {
			t.Fatalf("%s: no CSP", p)
		}
	}
}
