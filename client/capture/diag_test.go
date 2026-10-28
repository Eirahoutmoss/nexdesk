package capture

import (
	"errors"
	"testing"
)

func TestDiagnosis(t *testing.T) {
	cases := []struct {
		d               Diagnosis
		outside, secure bool
	}{
		{Diagnosis{Session: 1, ConsoleSession: 1, Desktop: "Default"}, false, false},
		{Diagnosis{Session: 1, ConsoleSession: 1, Desktop: "Winlogon"}, false, true},
		{Diagnosis{Session: 1, ConsoleSession: 1, DesktopErr: errors.New("access denied")}, false, true},
		{Diagnosis{Session: 2, ConsoleSession: 1, DesktopErr: errors.New("x")}, true, false},
		{Diagnosis{Session: 2, ConsoleSession: 0xFFFFFFFF, Desktop: "Default"}, false, false},
	}
	for i, c := range cases {
		if got := c.d.OutsideConsole(); got != c.outside {
			t.Errorf("%d: OutsideConsole=%v want %v", i, got, c.outside)
		}
		if got := c.d.SecureDesktop(); got != c.secure {
			t.Errorf("%d: SecureDesktop=%v want %v", i, got, c.secure)
		}
		if c.d.String() == "" {
			t.Errorf("%d: empty String", i)
		}
	}
}
