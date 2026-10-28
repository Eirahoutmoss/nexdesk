package capture

import (
	"fmt"
	"strings"
)

// Diagnosis describes why screen capture may be failing on this machine.
type Diagnosis struct {
	Session        uint32 // Windows session this process runs in
	ConsoleSession uint32 // session attached to the physical screen (0xFFFFFFFF: none)
	Desktop        string // current input desktop ("Default", "Winlogon", ...)
	DesktopErr     error  // the input desktop could not be opened
}

// Diagnose inspects the session and input desktop. Cheap; call it only when
// a capture fails.
func Diagnose() Diagnosis { return diagnose() }

// OutsideConsole reports that this process does not run in the session shown
// on the physical screen (Remote Desktop, switched user, service).
func (d Diagnosis) OutsideConsole() bool {
	return d.ConsoleSession != 0xFFFFFFFF && d.Session != d.ConsoleSession
}

// SecureDesktop reports Ctrl+Alt+Del / UAC / lock screen: the input desktop
// is not the user's "Default" desktop, or cannot be opened at all from a
// process that does run in the console session.
func (d Diagnosis) SecureDesktop() bool {
	if d.OutsideConsole() {
		return false
	}
	if d.DesktopErr != nil {
		return true
	}
	return d.Desktop != "" && !strings.EqualFold(d.Desktop, "Default")
}

func (d Diagnosis) String() string {
	desk := d.Desktop
	if d.DesktopErr != nil {
		desk = "açılamadı (" + d.DesktopErr.Error() + ")"
	}
	console := fmt.Sprint(d.ConsoleSession)
	if d.ConsoleSession == 0xFFFFFFFF {
		console = "yok"
	}
	return fmt.Sprintf("oturum=%d konsol=%s masaüstü=%s", d.Session, console, desk)
}
