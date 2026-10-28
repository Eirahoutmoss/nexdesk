//go:build windows

package capture

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procOpenInputDesktop          = user32.NewProc("OpenInputDesktop")
	procCloseDesktop              = user32.NewProc("CloseDesktop")
	procGetUserObjectInformationW = user32.NewProc("GetUserObjectInformationW")
)

// inputDesktopName returns the name of the desktop that currently receives
// user input ("Default" normally, "Winlogon" for Ctrl+Alt+Del / UAC / lock).
// A process outside the active session, or one not allowed to look at the
// secure desktop, gets an error instead.
func inputDesktopName() (string, error) {
	const desktopReadObjects = 0x0001
	h, _, err := procOpenInputDesktop.Call(0, 0, desktopReadObjects)
	if h == 0 {
		return "", fmt.Errorf("OpenInputDesktop: %w", err)
	}
	defer procCloseDesktop.Call(h)
	var buf [128]uint16
	var need uint32
	r, _, err := procGetUserObjectInformationW.Call(h, 2 /* UOI_NAME */, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2), uintptr(unsafe.Pointer(&need)))
	if r == 0 {
		return "", fmt.Errorf("GetUserObjectInformation: %w", err)
	}
	return windows.UTF16ToString(buf[:]), nil
}

func sessionIDs() (own, console uint32) {
	_ = windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &own)
	return own, windows.WTSGetActiveConsoleSessionId()
}

func diagnose() Diagnosis {
	var d Diagnosis
	d.Session, d.ConsoleSession = sessionIDs()
	d.Desktop, d.DesktopErr = inputDesktopName()
	return d
}
