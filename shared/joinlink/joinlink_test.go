package joinlink

import (
	"net"
	"testing"
)

func TestParse(t *testing.T) {
	ws := "wss://140.238.143.54:8443/v1/ws#pin=av3ksnpgP8LbmOo2nQZwqRpD0JuaM4Q_9yDmEO4Wpaw"
	code, got := Parse(Build("1234 567 890", ws))
	if code != "1234567890" || got != ws {
		t.Fatalf("link: %q %q", code, got)
	}
	if c, w := Parse(" 123-456-789 "); c != "123456789" || w != "" {
		t.Fatalf("plain: %q %q", c, w)
	}
	if c, _ := Parse("nexdesk://join?code=12&ws=http://x"); c != "" {
		t.Fatalf("short code accepted: %q", c)
	}
	if _, w := Parse("nexdesk://join?code=1234567&ws=http://evil"); w != "" {
		t.Fatalf("non-ws scheme accepted: %q", w)
	}
	ic, ok := EncodeInternetCode(net.ParseIP("46.1.148.182"), 8091, "0123456789")
	if !ok {
		t.Fatal("encode")
	}
	if c, w := Parse(ic); c != "0123456789" || w != "ws://46.1.148.182:8091/v1/ws" {
		t.Fatalf("internet code: %q %q", c, w)
	}
}
