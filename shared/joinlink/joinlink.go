// Package joinlink parses what a technician may receive to join a session:
// a plain numeric code, a "nexdesk://join?code=…&ws=…" link (QR code, shared
// link) or a 20-character "internet code" that embeds the host's public
// IPv4:port. Shared by the Windows app and the Android app.
package joinlink

import (
	"encoding/binary"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// NormalizeCode strips separators; "" unless 6–14 digits remain.
func NormalizeCode(s string) string {
	s = strings.NewReplacer(" ", "", "-", "", ".", "", " ", "").Replace(strings.TrimSpace(s))
	if len(s) < 6 || len(s) > 14 {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

// Parse returns the session code and an optional signaling URL override.
// code == "" means the input was not recognised.
func Parse(s string) (code, ws string) {
	s = strings.TrimSpace(s)
	if ws, code, ok := DecodeInternetCode(s); ok {
		return code, ws
	}
	if strings.HasPrefix(strings.ToLower(s), "nexdesk://") {
		if u, err := url.Parse(s); err == nil {
			q := u.Query()
			ws := strings.TrimSpace(q.Get("ws"))
			if ws != "" && !strings.HasPrefix(ws, "ws://") && !strings.HasPrefix(ws, "wss://") {
				ws = ""
			}
			// url.Query drops the #pin fragment of a nested URL only when it
			// is not escaped; Encode() escapes it, so it survives here.
			return NormalizeCode(q.Get("code")), ws
		}
	}
	return NormalizeCode(s), ""
}

// Build makes the link a QR code / share button carries.
func Build(code, ws string) string {
	v := url.Values{}
	if ws != "" {
		v.Set("ws", ws)
	}
	v.Set("code", strings.ReplaceAll(strings.TrimSpace(code), " ", ""))
	return "nexdesk://join?" + v.Encode()
}

func checksum(b []byte) byte {
	var c byte = 0xA5
	for i, x := range b {
		c = c*31 + x + byte(i)
	}
	return c
}

// EncodeInternetCode packs IPv4, port and the numeric session code.
func EncodeInternetCode(ip net.IP, port int, code string) (string, bool) {
	ip4 := ip.To4()
	n, err := strconv.ParseUint(code, 10, 64)
	if ip4 == nil || err != nil || n >= 1<<40 || port <= 0 || port > 65535 {
		return "", false
	}
	b := make([]byte, 12)
	copy(b, ip4)
	binary.BigEndian.PutUint16(b[4:], uint16(port))
	for i := 0; i < 5; i++ {
		b[6+i] = byte(n >> (8 * (4 - i)))
	}
	b[11] = checksum(b[:11])
	var sb strings.Builder
	var acc uint32
	bits := 0
	for _, x := range b {
		acc = acc<<8 | uint32(x)
		bits += 8
		for bits >= 5 {
			bits -= 5
			sb.WriteByte(crockford[(acc>>bits)&31])
		}
	}
	if bits > 0 {
		sb.WriteByte(crockford[(acc<<(5-bits))&31])
	}
	s := sb.String() // 20 chars
	return s[0:5] + "-" + s[5:10] + "-" + s[10:15] + "-" + s[15:20], true
}

// DecodeInternetCode reverses EncodeInternetCode; tolerant of case, spaces,
// dashes and the usual O/0, I/L/1 confusions.
func DecodeInternetCode(s string) (ws, code string, ok bool) {
	s = strings.ToUpper(s)
	s = strings.NewReplacer("-", "", " ", "", "O", "0", "I", "1", "L", "1").Replace(s)
	if len(s) != 20 {
		return "", "", false
	}
	var acc uint32
	bits := 0
	var b []byte
	for _, r := range s {
		v := strings.IndexRune(crockford, r)
		if v < 0 {
			return "", "", false
		}
		acc = acc<<5 | uint32(v)
		bits += 5
		if bits >= 8 {
			bits -= 8
			b = append(b, byte(acc>>bits))
		}
	}
	if len(b) < 12 || checksum(b[:11]) != b[11] {
		return "", "", false
	}
	ip := net.IPv4(b[0], b[1], b[2], b[3])
	port := int(binary.BigEndian.Uint16(b[4:6]))
	var n uint64
	for i := 0; i < 5; i++ {
		n = n<<8 | uint64(b[6+i])
	}
	return fmt.Sprintf("ws://%s/v1/ws", net.JoinHostPort(ip.String(), strconv.Itoa(port))), fmt.Sprintf("%010d", n), true
}
