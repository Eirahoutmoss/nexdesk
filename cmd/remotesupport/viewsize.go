//go:build windows

package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/eirahoutmoss/remotesupport/client/capture"
	webrtcpeer "github.com/eirahoutmoss/remotesupport/client/webrtc"
)

// Viewer-sized streaming. The technician's window is usually smaller than
// the supported screen; sending the full screen only for the viewer to
// shrink it wasted capture, JPEG encode/decode and bandwidth on both sides
// and blurred small text. The operator reports its viewer size in physical
// pixels ("VIEWSIZE:w,h") and the target streams at that size (never larger
// than the real screen). Clients that never send it get the full screen.

// viewSizes: *webrtcpeer.Peer -> [2]int (target side).
var viewSizes sync.Map

func setViewSize(peer *webrtcpeer.Peer, w, h int) {
	if w <= 0 || h <= 0 || w > 16384 || h > 16384 {
		viewSizes.Delete(peer) // 0,0: full resolution
		return
	}
	viewSizes.Store(peer, [2]int{w, h})
}

// viewLimitedHeight lowers maxHeight (0 = no limit) to what the peer's
// viewer can show for the given monitor.
func viewLimitedHeight(peer *webrtcpeer.Peer, monitor, maxHeight int) int {
	v, ok := viewSizes.Load(peer)
	if !ok {
		return maxHeight
	}
	vs := v.([2]int)
	mons, err := capture.ListMonitors()
	if err != nil || monitor < 0 || monitor >= len(mons) {
		return maxHeight
	}
	m := mons[monitor]
	if m.Width <= 0 || m.Height <= 0 {
		return maxHeight
	}
	scale := min(float64(vs[0])/float64(m.Width), float64(vs[1])/float64(m.Height))
	if scale >= 0.98 {
		return maxHeight // the viewer shows (nearly) every pixel
	}
	h := int(float64(m.Height)*scale + 0.999)
	h = (h + 7) &^ 7 // steps of 8 px: small window moves don't restart the stream
	if h < 360 {
		h = 360
	}
	if h >= m.Height {
		return maxHeight
	}
	if maxHeight == 0 || h < maxHeight {
		return h
	}
	return maxHeight
}

// ---- operator side ----

var (
	viewSizeMu    sync.Mutex
	viewSizeTimer *time.Timer
	viewSizeLast  string
	viewSizeTries int
)

// reportViewSize tells the target the viewer's size, debounced so a window
// drag sends one message at the end.
func reportViewSize() {
	viewSizeMu.Lock()
	defer viewSizeMu.Unlock()
	if viewSizeTimer != nil {
		viewSizeTimer.Stop()
	}
	viewSizeTimer = time.AfterFunc(300*time.Millisecond, sendViewSize)
}

func sendViewSize() {
	if state.mode == 1 || viewer.hwnd == 0 {
		return
	}
	var rc rect
	getClientRect.Call(viewer.hwnd, uintptr(unsafe.Pointer(&rc)))
	w, h := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	if w < 50 || h < 50 {
		return
	}
	s := viewerPhysicalScale()
	msg := fmt.Sprintf("VIEWSIZE:%d,%d", int(float64(w)*s+0.5), int(float64(h)*s+0.5))
	state.mu.Lock()
	peer := state.peer
	state.mu.Unlock()
	if peer == nil {
		return
	}
	viewSizeMu.Lock()
	same := msg == viewSizeLast
	viewSizeLast = msg
	viewSizeMu.Unlock()
	if same {
		return
	}
	if peer.SendControlText(msg) != nil {
		// Control channel not open yet (right after connect): try again.
		viewSizeMu.Lock()
		viewSizeLast = ""
		viewSizeTries++
		retry := viewSizeTries <= 20
		viewSizeMu.Unlock()
		if retry {
			reportViewSize()
		}
		return
	}
	viewSizeMu.Lock()
	viewSizeTries = 0
	viewSizeMu.Unlock()
}

// resetViewSize forgets the last sent size (new connection).
func resetViewSize() {
	viewSizeMu.Lock()
	viewSizeLast = ""
	viewSizeTries = 0
	viewSizeMu.Unlock()
}

var (
	shcoreDLL             = syscall.NewLazyDLL("shcore.dll")
	procGetDpiForMonitor  = shcoreDLL.NewProc("GetDpiForMonitor")
	procMonitorFromWindow = user32.NewProc("MonitorFromWindow")
	procSetThreadDpiAwCtx = user32.NewProc("SetThreadDpiAwarenessContext")
)

// viewerPhysicalScale is the real display scaling of the monitor showing the
// viewer (1.0 at 100 %, 1.5 at 150 % ...). NexDesk's window is DPI-unaware,
// so Windows reports its size in logical pixels and stretches it; asking in
// a per-monitor-aware thread context gives the real DPI.
func viewerPhysicalScale() float64 {
	if procGetDpiForMonitor.Find() != nil || procSetThreadDpiAwCtx.Find() != nil || viewer.hwnd == 0 {
		return 1
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	prev, _, _ := procSetThreadDpiAwCtx.Call(^uintptr(3)) // PER_MONITOR_AWARE_V2
	if prev != 0 {
		defer procSetThreadDpiAwCtx.Call(prev)
	}
	mon, _, _ := procMonitorFromWindow.Call(viewer.hwnd, 2) // MONITOR_DEFAULTTONEAREST
	if mon == 0 {
		return 1
	}
	var dx, dy uint32
	if r, _, _ := procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r != 0 || dx < 96 {
		return 1
	}
	return float64(dx) / 96
}
