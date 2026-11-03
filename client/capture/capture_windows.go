//go:build windows

package capture

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	smxvirtualscreen  = 76
	smyvirtualscreen  = 77
	smcxvirtualscreen = 78
	smcyvirtualscreen = 79
	smcxscreen        = 0
	smcyscreen        = 1
	srccopy           = 0x00CC0020
	dibRGBColors      = 0
	biRGB             = 0
)

type rect struct{ Left, Top, Right, Bottom int32 }
type monitorInfo struct {
	CbSize    uint32
	RcMonitor rect
	RcWork    rect
	DwFlags   uint32
}
type bitmapInfoHeader struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}
type bitmapInfo struct {
	BmiHeader bitmapInfoHeader
	BmiColors [1]uint32
}

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	gdi32                   = windows.NewLazySystemDLL("gdi32.dll")
	procEnumDisplayMonitors = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW     = user32.NewProc("GetMonitorInfoW")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procBitBlt              = gdi32.NewProc("BitBlt")
	procCreateDIBSection    = gdi32.NewProc("CreateDIBSection")
	procGdiFlush            = gdi32.NewProc("GdiFlush")
	procGetCursorInfo       = user32.NewProc("GetCursorInfo")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procGetIconInfo         = user32.NewProc("GetIconInfo")
	procDrawIconEx          = user32.NewProc("DrawIconEx")
	procSetThreadDpiCtx     = user32.NewProc("SetThreadDpiAwarenessContext")
)

// DrawCursor paints the mouse pointer into captured frames (on by default) so
// the operator sees where the remote cursor is.
var DrawCursor atomic.Bool

// Grayscale drops colour from captured frames; used by adaptive quality on
// very poor links.
var Grayscale atomic.Bool

func init() { DrawCursor.Store(true) }

type cursorInfo struct {
	CbSize uint32
	Flags  uint32
	Cursor uintptr
	X, Y   int32
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

// drawCursor overlays the current pointer onto memdc, whose origin is the
// monitor's top-left corner at (originX, originY) in virtual-screen space.
func drawCursor(memdc uintptr, originX, originY int) {
	ci := cursorInfo{CbSize: uint32(unsafe.Sizeof(cursorInfo{}))}
	if r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 {
		return
	}
	// In a Remote Desktop session (and when input is injected) Windows often
	// reports the pointer as hidden, so the technician saw it only on clicks.
	// The technician always needs to see where the pointer is: fall back to
	// the standard arrow when the cursor is reported hidden or has no handle.
	if ci.Flags&1 == 0 || ci.Cursor == 0 {
		arrow, _, _ := procLoadCursorW.Call(0, 32512) // IDC_ARROW (shared, not freed)
		if arrow == 0 {
			return
		}
		ci.Cursor = arrow
	}
	var ii iconInfo
	hx, hy := int32(0), int32(0)
	if r, _, _ := procGetIconInfo.Call(ci.Cursor, uintptr(unsafe.Pointer(&ii))); r != 0 {
		hx, hy = int32(ii.XHotspot), int32(ii.YHotspot)
		if ii.HbmMask != 0 {
			procDeleteObject.Call(ii.HbmMask)
		}
		if ii.HbmColor != 0 {
			procDeleteObject.Call(ii.HbmColor)
		}
	}
	x := ci.X - hx - int32(originX)
	y := ci.Y - hy - int32(originY)
	procDrawIconEx.Call(memdc, uintptr(x), uintptr(y), ci.Cursor, 0, 0, 0, 0, 0x0003) // DI_NORMAL
}

var captureMu sync.Mutex

// EnumDisplayMonitors needs a C callback. syscall/windows callbacks are NEVER
// freed and the process has a hard cap (~2000). listMonitors runs on every
// captured frame (~10 fps), so creating a fresh callback each call exhausted
// the cap in ~1-2 minutes and crashed the agent. Create it exactly once and
// reuse it; the enum result is passed through package state under enumMu.
var (
	enumMu       sync.Mutex
	enumMonitors []Monitor
	enumCbOnce   sync.Once
	enumCb       uintptr
)

func monitorEnumProc(hmon, hdc, r, lparam uintptr) uintptr {
	var mi monitorInfo
	mi.CbSize = uint32(unsafe.Sizeof(mi))
	ret, _, _ := procGetMonitorInfoW.Call(hmon, uintptr(unsafe.Pointer(&mi)))
	if ret == 0 {
		return 1
	}
	enumMonitors = append(enumMonitors, Monitor{
		Index:   len(enumMonitors),
		X:       int(mi.RcMonitor.Left),
		Y:       int(mi.RcMonitor.Top),
		Width:   int(mi.RcMonitor.Right - mi.RcMonitor.Left),
		Height:  int(mi.RcMonitor.Bottom - mi.RcMonitor.Top),
		Primary: mi.DwFlags&1 != 0,
	})
	return 1
}

func listMonitors() ([]Monitor, error) {
	enumMu.Lock()
	defer enumMu.Unlock()
	enumCbOnce.Do(func() { enumCb = windows.NewCallback(monitorEnumProc) })
	enumMonitors = enumMonitors[:0]
	ret, _, err := procEnumDisplayMonitors.Call(0, 0, enumCb, 0)
	if ret == 0 {
		if err != windows.ERROR_SUCCESS {
			return nil, err
		}
		return nil, errors.New("capture: EnumDisplayMonitors failed")
	}
	return append([]Monitor(nil), enumMonitors...), nil
}

func captureMonitor(index int, quality int) (Frame, error) {
	return captureMonitorScaled(index, quality, 0)
}

func captureMonitorScaled(index int, quality int, maxHeight int) (Frame, error) {
	img, err := captureMonitorImage(index, maxHeight)
	if err != nil {
		return Frame{}, err
	}
	var out bytes.Buffer
	if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: ValidateQuality(quality)}); err != nil {
		return Frame{}, err
	}
	return Frame{Width: img.Bounds().Dx(), Height: img.Bounds().Dy(), JPEG: out.Bytes()}, nil
}

// captureMonitorImage grabs one display (cursor drawn, optional grayscale)
// and downscales it to maxHeight when set.
func captureMonitorImage(index int, maxHeight int) (*image.RGBA, error) {
	captureMu.Lock()
	defer captureMu.Unlock()
	// NexDesk itself is DPI-unaware, so on a 125/150 % display Windows hands
	// it scaled (logical) monitor sizes and BitBlt grabbed only the top-left
	// part of the real screen. Capture in a per-monitor-aware thread context
	// to work in physical pixels.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if procSetThreadDpiCtx.Find() == nil {
		prev, _, _ := procSetThreadDpiCtx.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 (-4)
		if prev != 0 {
			defer procSetThreadDpiCtx.Call(prev)
		}
	}

	monitors, err := listMonitors()
	if err != nil {
		return nil, err
	}
	if index < 0 || index >= len(monitors) {
		return nil, fmt.Errorf("capture: monitor index %d out of range", index)
	}
	m := monitors[index]

	screen, _, err := procGetDC.Call(0)
	if screen == 0 {
		return nil, fmt.Errorf("capture: GetDC: %w", err)
	}
	defer procReleaseDC.Call(0, screen)

	memdc, _, err := procCreateCompatibleDC.Call(screen)
	if memdc == 0 {
		return nil, fmt.Errorf("capture: CreateCompatibleDC: %w", err)
	}
	defer procDeleteDC.Call(memdc)

	// Blit straight into a top-down 32-bit DIB section and read its memory.
	// (The old CreateCompatibleBitmap + GetDIBits path called GetDIBits while
	// the bitmap was still selected into the DC, which the API forbids; some
	// drivers, e.g. Remote Desktop's, then returned 0 rows with no error.)
	// The DIB is kept between frames: reallocating ~8 MB per frame cost time
	// and garbage for nothing.
	bits, err := dibFor(screen, m.Width, m.Height)
	if err != nil {
		return nil, err
	}
	old, _, _ := procSelectObject.Call(memdc, dib.bmp)
	if old == 0 {
		return nil, errors.New("capture: SelectObject failed")
	}
	ok, _, err := procBitBlt.Call(memdc, 0, 0, uintptr(m.Width), uintptr(m.Height), screen, uintptr(int32(m.X)), uintptr(int32(m.Y)), srccopy)
	if ok == 0 {
		procSelectObject.Call(memdc, old)
		return nil, fmt.Errorf("capture: BitBlt: %w", err)
	}
	if DrawCursor.Load() {
		drawCursor(memdc, m.X, m.Y)
	}
	procGdiFlush.Call()
	procSelectObject.Call(memdc, old)
	raw := unsafe.Slice((*byte)(bits), m.Width*m.Height*4)

	// Convert BGRA -> RGBA and scale in one pass: when the technician's
	// window is smaller than this screen there is no full-size copy at all.
	return scaleBGRA(raw, m.Width, m.Height, maxHeight, Grayscale.Load()), nil
}

// dib is the reusable capture bitmap (guarded by captureMu).
var dib struct {
	bmp  uintptr
	bits unsafe.Pointer
	w, h int
}

func dibFor(screen uintptr, w, h int) (unsafe.Pointer, error) {
	if dib.bmp != 0 && dib.w == w && dib.h == h {
		return dib.bits, nil
	}
	if dib.bmp != 0 {
		procDeleteObject.Call(dib.bmp)
		dib.bmp, dib.bits = 0, nil
	}
	hdr := bitmapInfoHeader{BiSize: uint32(unsafe.Sizeof(bitmapInfoHeader{})), BiWidth: int32(w), BiHeight: -int32(h), BiPlanes: 1, BiBitCount: 32, BiCompression: biRGB}
	bmi := bitmapInfo{BmiHeader: hdr}
	var bits unsafe.Pointer
	bmp, _, err := procCreateDIBSection.Call(screen, uintptr(unsafe.Pointer(&bmi)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return nil, fmt.Errorf("capture: CreateDIBSection: %w", err)
	}
	dib.bmp, dib.bits, dib.w, dib.h = bmp, bits, w, h
	return bits, nil
}

var _ = procGetSystemMetrics
var _ = smxvirtualscreen
var _ = smyvirtualscreen
var _ = smcxvirtualscreen
var _ = smcyvirtualscreen
var _ = smcxscreen
var _ = smcyscreen
