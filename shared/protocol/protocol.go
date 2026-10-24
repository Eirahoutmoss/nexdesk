// Package protocol defines the one message type still shared across the app:
// InputEvent, the mouse/keyboard event the operator sends to the agent inside
// the encrypted control channel.
//
// Earlier revisions carried a whole plaintext control layer and a byte-tagged
// session layer here. Both were superseded — control messages are now text
// frames handled in cmd/remotesupport, and screen frames by client/screen — so
// only the input event, which both ends still share, remains.
package protocol

import (
	"fmt"
	"unicode"
	"unicode/utf8"
)

// MaxTextRunes bounds one "text" event (soft keyboards send words/paste).
const MaxTextRunes = 256

// InputEvent is a mouse or keyboard event. X/Y are normalized (0..1) relative
// to the streamed monitor so the operator needs no pixel math.
type InputEvent struct {
	T string  `json:"t"` // move, down, up, wheel, keydown, keyup, text
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
	B int     `json:"b,omitempty"` // 0 left, 1 middle, 2 right
	D int     `json:"d,omitempty"` // wheel delta (120 per notch)
	K int     `json:"k,omitempty"` // Windows virtual-key code
	S string  `json:"s,omitempty"` // text: Unicode characters to type (mobile keyboards)
}

// Validate rejects malformed or out-of-range input events before they reach
// the input injector.
func (e InputEvent) Validate() error {
	switch e.T {
	case "move", "down", "up", "wheel":
		if e.X < 0 || e.X > 1 || e.Y < 0 || e.Y > 1 {
			return fmt.Errorf("protocol: coordinates out of range")
		}
		if e.B < 0 || e.B > 2 {
			return fmt.Errorf("protocol: invalid button %d", e.B)
		}
		if e.D < -1200 || e.D > 1200 {
			return fmt.Errorf("protocol: invalid wheel delta %d", e.D)
		}
	case "keydown", "keyup":
		if e.K < 1 || e.K > 254 {
			return fmt.Errorf("protocol: invalid key code %d", e.K)
		}
	case "text":
		n := utf8.RuneCountInString(e.S)
		if n == 0 || n > MaxTextRunes || !utf8.ValidString(e.S) {
			return fmt.Errorf("protocol: invalid text length")
		}
		for _, r := range e.S {
			if unicode.IsControl(r) && r != '\n' && r != '\t' {
				return fmt.Errorf("protocol: control character in text")
			}
		}
	default:
		return fmt.Errorf("protocol: unknown input type %q", e.T)
	}
	return nil
}
