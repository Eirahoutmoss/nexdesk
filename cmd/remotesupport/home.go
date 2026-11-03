//go:build windows

package main

// Home screen: "Destek Al" (this computer's code, always ready) and
// "Destek Ver" (connect box) side by side, with the recently supported
// computers below as thumbnail tiles. The other pages live under the ☰ menu.

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/eirahoutmoss/remotesupport/client/capture"
)

const (
	ID_MENU_BUTTON  = 5901
	ID_MENU_HOME    = 5902
	ID_MENU_BOOK    = 5903
	ID_MENU_DEVICE  = 5904
	ID_MENU_NEWWIN  = 5905
	ID_MENU_THUMBS  = 5906
	ID_TILE_RENAME  = 5907
	ID_TILE_REMOVE  = 5908
	ID_TILE_CONNECT = 5909

	homeTop    = headerH + 20 // top of the two cards
	homeCardH  = 180
	homeGap    = 20
	tileW      = 180
	tileImgH   = 92 // 16:9 for the 180-16 px wide image area
	tileH      = tileImgH + 52
	tileGap    = 16
	menuBtnX   = 16
	menuBtnY   = 23
	menuBtnS   = 40
	maxRemotes = 24
)

// homeVisible: the home screen is shown (not connected, no page, and on
// the target side not sharing the screen right now).
func homeVisible(mode int, connected bool) bool {
	return !connected && (mode == 0 || (mode == 1 && !capturing.Load()))
}

// ---------------------------------------------------------------------------
// Always-ready "Destek Al" code
// ---------------------------------------------------------------------------

var (
	// homeCodeWanted: keep a code ready on the home screen. On in the first
	// window; a "Yeni Oturum" window (a technician window) turns it on only
	// when its user asks for a code.
	homeCodeWanted atomic.Bool
	// operatorBusy: this window is connecting to / supporting another
	// computer; its own code is paused meanwhile.
	operatorBusy atomic.Bool

	homeCodeMu       sync.Mutex
	homeCodeStr      string
	homeCodeErr      string
	homeCodeStarting bool
	homeCodeFailedAt time.Time
	homeSetupOnce    sync.Once
)

func setHomeCode(code string) {
	homeCodeMu.Lock()
	homeCodeStr = code
	if code != "" {
		homeCodeErr = ""
	}
	homeCodeMu.Unlock()
	setCode(formatCode(code))
	if state.hwnd != 0 {
		postMessage.Call(state.hwnd, WM_APP_RELAYOUT, 0, 0) // show/hide Kopyala, Link
	}
}

func getHomeCode() (code, errText string, starting bool) {
	homeCodeMu.Lock()
	defer homeCodeMu.Unlock()
	return homeCodeStr, homeCodeErr, homeCodeStarting
}

// homeCodeFailed records a failed attempt; the manager retries later.
func homeCodeFailed(msg string) {
	homeCodeMu.Lock()
	homeCodeErr = msg
	homeCodeFailedAt = time.Now()
	homeCodeMu.Unlock()
	codeSessionActive.Store(false)
	setHomeCode("")
	invalidateHomeCode()
}

func wantHomeCode() bool {
	if !homeCodeWanted.Load() || operatorBusy.Load() {
		return false
	}
	viewer.mu.RLock()
	active := viewer.active
	viewer.mu.RUnlock()
	return !active
}

// startHomeCodeManager keeps the home code alive: creates it, renews it when
// it expires unused, and recreates it after a session ends.
func init() {
	// Decided before the first layout: the first window keeps a code ready,
	// a "Yeni Oturum" window (technician window) only when asked.
	homeCodeWanted.Store(os.Getenv("REMOTESUPPORT_NO_DEVICE") == "")
}

func startHomeCodeManager() {
	go func() {
		defer logCrash("homeCode")
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			homeCodeTick()
		}
	}()
}

func homeCodeTick() {
	if codeSessionActive.Load() {
		// Unused code past its lifetime: get a fresh one quietly.
		if exp := codeExpiryUnix.Load(); exp > 0 && time.Now().Unix() >= exp && techCount() == 0 && !capturing.Load() && !everConnected.Load() {
			netlogf("Kod süresi doldu, yenisi oluşturuluyor")
			stopBackgroundCode()
			return
		}
		state.mu.Lock()
		mode := state.mode
		state.mu.Unlock()
		if homeVisible(mode, false) {
			invalidateHomeCode() // remaining-time label
		}
		return
	}
	if !wantHomeCode() {
		return
	}
	homeCodeMu.Lock()
	if homeCodeStarting || time.Since(homeCodeFailedAt) < 15*time.Second {
		homeCodeMu.Unlock()
		return
	}
	homeCodeStarting = true
	homeCodeMu.Unlock()
	invalidateHomeCode()
	go func() {
		defer func() {
			homeCodeMu.Lock()
			homeCodeStarting = false
			homeCodeMu.Unlock()
			invalidateHomeCode()
		}()
		// Firewall question and the local signaling port: once per run, not
		// on every renewal.
		homeSetupOnce.Do(func() {
			// The firewall question must not hold up the code: ask in parallel.
			go func() {
				defer logCrash("firewall")
				ensureFirewall()
			}()
			if !embeddedSignalingReady.Load() {
				startEmbeddedSignaling()
			}
		})
		startTarget()
	}()
}

// stopBackgroundCode ends a code session that nobody has joined (expiry,
// or the user is about to connect out from this window). No BYE: there is
// no peer.
func stopBackgroundCode() {
	if techCount() > 0 || capturing.Load() {
		return
	}
	state.mu.Lock()
	cancel, sig := state.cancel, state.sig
	state.cancel, state.sig, state.ctx = nil, nil, nil
	state.mu.Unlock()
	codeSessionActive.Store(false)
	codeExpiryUnix.Store(0)
	setHomeCode("")
	if cancel != nil {
		cancel()
	}
	if sig != nil {
		_ = sig.Close(context.Background())
	}
	invalidateHomeCode()
}

// onCodeLoopEnd runs when a code session's signaling loop ends. If that
// session is still the current one and nobody is connected, it is cleared
// so the manager can make a new code.
func onCodeLoopEnd(c any) {
	state.mu.Lock()
	current := state.sig != nil && any(state.sig) == c
	state.mu.Unlock()
	if !current || techCount() > 0 || capturing.Load() {
		return
	}
	stopBackgroundCode()
}

var homeCodeRect rect

func invalidateHomeCode() {
	if state.hwnd == 0 {
		return
	}
	r := homeCodeRect
	if r.Right <= r.Left {
		invalidateRect.Call(state.hwnd, 0, 0)
		return
	}
	invalidateRect.Call(state.hwnd, uintptr(unsafe.Pointer(&r)), 0)
}

// ---------------------------------------------------------------------------
// Layout and painting
// ---------------------------------------------------------------------------

type homeGeom struct {
	lx, rx, cw, y int // left/right card x, card width, card top
	tilesY        int
}

func computeHomeGeom(w int) homeGeom {
	contentW := w - contentX - gutter
	cw := (contentW - homeGap) / 2
	return homeGeom{lx: contentX, rx: contentX + cw + homeGap, cw: cw, y: homeTop, tilesY: homeTop + homeCardH + 26}
}

const qrSize = 100

func layoutHome(w, h int) {
	g := computeHomeGeom(w)
	setControlFont(state.codeLabel, uiCodeFont)
	code, _, _ := getHomeCode()
	showCode := code != "" && codeSessionActive.Load()
	// Left card: code box, Kopyala / Link.
	moveControl(state.codeLabel, g.lx+22, g.y+44, g.cw-44-qrSize-16, 56, showCode)
	moveControl(state.copy, g.lx+22, g.y+112, 128, 36, showCode)
	moveControl(state.shareLink, g.lx+158, g.y+112, 128, 36, showCode)
	// Secondary window without a code: a button to make one.
	setText(state.homeGet, "Kod Oluştur")
	moveControl(state.homeGet, g.lx+22, g.y+76, 200, 44, !showCode && !homeCodeWanted.Load())
	hide(state.homeGive)
	// Right card: code/id/link box + BAĞLAN, saved devices, password.
	btnW := 150
	editW := g.cw - 44 - btnW - 12
	if operatorBusy.Load() {
		setText(state.action, "BAĞLANIYOR...")
	} else {
		setText(state.action, "BAĞLAN")
	}
	moveControl(state.remoteEdit, g.rx+22, g.y+44, editW, 44, true)
	moveControl(state.action, g.rx+22+editW+12, g.y+44, btnW, 44, true)
	moveControl(state.techDev, g.rx+22, g.y+100, editW, 300, true)
	moveControl(state.techDevManage, g.rx+22+editW+12, g.y+100, btnW, 30, true)
	moveControl(state.remotePass, g.rx+22, g.y+142, editW, 28, true)
	moveControl(state.elevate, w-gutter-260, menuBtnY, 260, 40, !isElevated())
	hide(state.back)
}

func paintHome(hdc uintptr, w, h int) {
	g := computeHomeGeom(w)
	homeCodeRect = rect{Left: int32(g.lx), Top: int32(g.y), Right: int32(g.lx + g.cw), Bottom: int32(g.y + homeCardH)}

	// ---- Left card: this computer ----
	paintCard(hdc, g.lx, g.y, g.cw, homeCardH, uiPanelColor, uiBorderColor, 16)
	paintText(hdc, "BU BİLGİSAYAR — DESTEK AL", g.lx+22, g.y+16, g.cw-44, 18, uiLabelFont, uiAccentColor, txVLeft)
	code, errText, starting := getHomeCode()
	switch {
	case code != "" && codeSessionActive.Load():
		paintCard(hdc, g.lx+16, g.y+40, g.cw-32-qrSize-16, 64, uiEditColor, mixHex(uiBorderColor, uiTextColor, 0.35), 12)
		drawQR(hdc, buildJoinLink(code), g.lx+g.cw-22-qrSize, g.y+40, qrSize)
		var note string
		var col uint32 = uiMutedColor
		if n := techCount(); n > 0 {
			note, col = "● Teknisyen bağlı", uiSuccess
		} else if everConnected.Load() {
			note = "Kod bu oturum boyunca geçerli — teknisyen yeniden bağlanabilir"
		} else if exp := codeExpiryUnix.Load(); exp > 0 {
			rem := exp - time.Now().Unix()
			if rem < 0 {
				rem = 0
			}
			note = "Kodu destek verecek kişiye iletin  •  " + itoa2(int(rem/60)) + ":" + itoa2(int(rem%60)) + " sonra yenilenir"
		}
		if ic, _ := internetCodeState(); ic != "" {
			note = "İnternet kodu: " + ic + "  (Kopyala bunu kopyalar)"
		}
		paintText(hdc, note, g.lx+22, g.y+154, g.cw-44, 20, uiSmallFont, col, txEnd)
	case !homeCodeWanted.Load():
		paintText(hdc, "Bu pencere destek vermek için açıldı.", g.lx+22, g.y+42, g.cw-44, 22, uiBodyFont, uiMutedColor, txVLeft)
		paintText(hdc, "Bu bilgisayara destek alınacaksa kod oluşturun.", g.lx+22, g.y+124, g.cw-44, 20, uiSmallFont, uiFaintColor, txVLeft)
	case operatorBusy.Load():
		paintText(hdc, "Başka bir bilgisayara bağlanırken kod kapalı.", g.lx+22, g.y+60, g.cw-44, 22, uiBodyFont, uiMutedColor, txVLeft)
		paintText(hdc, "Bağlantı bitince kod kendiliğinden yenilenir.", g.lx+22, g.y+88, g.cw-44, 20, uiSmallFont, uiFaintColor, txVLeft)
	case errText != "" && !starting:
		paintText(hdc, "● Kod oluşturulamadı — birkaç saniye içinde yeniden denenecek", g.lx+22, g.y+56, g.cw-44, 22, uiBodyFont, uiDanger, txEnd)
		paintText(hdc, errText, g.lx+22, g.y+84, g.cw-44, 60, uiSmallFont, uiMutedColor, txWrap)
	default:
		paintText(hdc, "Kod hazırlanıyor…", g.lx+22, g.y+62, g.cw-44, 30, uiTitleFont, uiFaintColor, txVLeft)
	}

	// ---- Right card: connect to another computer ----
	paintCard(hdc, g.rx, g.y, g.cw, homeCardH, uiPanelColor, uiBorderColor, 16)
	paintText(hdc, "UZAK BİLGİSAYAR — DESTEK VER", g.rx+22, g.y+16, g.cw-44, 18, uiLabelFont, uiAccentColor, txVLeft)
	paintText(hdc, "kod, cihaz kimliği ya da link", g.rx+22, g.y+16, g.cw-44, 18, uiSmallFont, uiFaintColor, txVRight)
	btnW := 150
	editW := g.cw - 44 - btnW - 12
	// Frames around the input boxes: on the light theme an edit box is as
	// white as the card and could not be seen at all.
	frame := mixHex(uiBorderColor, uiTextColor, 0.35)
	paintCard(hdc, g.rx+18, g.y+40, editW+8, 52, uiEditColor, frame, 10)
	paintCard(hdc, g.rx+18, g.y+138, editW+8, 36, uiEditColor, frame, 8)
	paintText(hdc, "← cihaz parolası", g.rx+22+editW+12, g.y+142, btnW, 28, uiSmallFont, uiFaintColor, txVLeft)

	paintTiles(hdc, w, h, g)
}

func itoa2(n int) string { return fmt.Sprintf("%02d", n) }

// ---------------------------------------------------------------------------
// Recently supported computers (technician side)
// ---------------------------------------------------------------------------

type remoteEntry struct {
	Key      string `json:"key"` // "dev:<id>", "host:<name>" or "code:<code>"
	Name     string `json:"name"`
	Host     string `json:"host,omitempty"`
	DeviceID string `json:"device_id,omitempty"`
	At       int64  `json:"at"`
}

var (
	remotesMu sync.Mutex
	remotes   []remoteEntry
	// currentRemoteKey: the entry of the computer this window supports now.
	currentRemoteKey atomic.Value // string
	thumbTimer       *time.Timer
)

func appDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		return ""
	}
	return filepath.Join(base, "RemoteSupport")
}

func remotesPath() string {
	d := appDir()
	if d == "" {
		return ""
	}
	return filepath.Join(d, "remotes.json")
}

func thumbPath(key string) string {
	d := appDir()
	if d == "" {
		return ""
	}
	sum := sha1.Sum([]byte(strings.ToLower(key)))
	return filepath.Join(d, "thumbs", hex.EncodeToString(sum[:8])+".jpg")
}

func loadRemotes() {
	p := remotesPath()
	if p == "" {
		return
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var list []remoteEntry
	if json.Unmarshal(b, &list) == nil {
		remotesMu.Lock()
		remotes = list
		remotesMu.Unlock()
	}
}

func saveRemotesLocked() {
	p := remotesPath()
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	if b, err := json.MarshalIndent(remotes, "", "  "); err == nil {
		_ = writeFileAtomic(p, b, 0600)
	}
}

func remotesSnapshot() []remoteEntry {
	remotesMu.Lock()
	defer remotesMu.Unlock()
	return append([]remoteEntry(nil), remotes...)
}

// recordRemote notes the computer this window just connected to (called
// when its HELLO names it) and schedules a thumbnail.
func recordRemote(host string) {
	host = strings.TrimSpace(host)
	var devID string
	if getOpDevicePass() != "" {
		devID = strings.TrimSpace(getActiveCode())
	}
	key := ""
	switch {
	case devID != "":
		key = "dev:" + strings.ToLower(devID)
	case host != "":
		key = "host:" + strings.ToLower(host)
	default:
		return
	}
	name := host
	if name == "" {
		name = devID
	}
	remotesMu.Lock()
	var e remoteEntry
	idx := -1
	for i, r := range remotes {
		if r.Key == key {
			e, idx = r, i
			break
		}
	}
	if idx < 0 && devID != "" && host != "" {
		// Known before only by name (connected by code): carry it over.
		for i, r := range remotes {
			if r.Key == "host:"+strings.ToLower(host) {
				e, idx = r, i
				if old, now := thumbPath(r.Key), thumbPath(key); old != "" && now != "" {
					_ = os.Rename(old, now)
				}
				break
			}
		}
	}
	if idx >= 0 {
		remotes = append(remotes[:idx], remotes[idx+1:]...)
	}
	if e.Name == "" || (e.Name == e.Host && host != "") {
		e.Name = name
	}
	e.Key, e.Host, e.DeviceID, e.At = key, host, devID, time.Now().Unix()
	remotes = append([]remoteEntry{e}, remotes...)
	if len(remotes) > maxRemotes {
		for _, r := range remotes[maxRemotes:] {
			_ = os.Remove(thumbPath(r.Key))
		}
		remotes = remotes[:maxRemotes]
	}
	saveRemotesLocked()
	remotesMu.Unlock()
	currentRemoteKey.Store(key)
	// First thumbnail a few seconds in, once the screen has arrived.
	if thumbTimer != nil {
		thumbTimer.Stop()
	}
	thumbTimer = time.AfterFunc(5*time.Second, func() { saveThumbnail(key) })
}

func thumbsEnabled() bool { return !currentSettings().NoThumbnails }

// saveThumbnail stores a small picture of the current remote screen.
func saveThumbnail(key string) {
	if key == "" || !thumbsEnabled() {
		return
	}
	viewer.mu.RLock()
	w, h := viewer.width, viewer.height
	var px []byte
	if w > 0 && h > 0 && len(viewer.pixels) >= w*h*4 {
		px = append([]byte(nil), viewer.pixels[:w*h*4]...)
	}
	viewer.mu.RUnlock()
	if px == nil {
		return
	}
	img := capture.ScaleBGRA(px, w, h, tileImgH*2) // 2x for a crisp tile
	var buf bytes.Buffer
	if jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}) != nil {
		return
	}
	p := thumbPath(key)
	if p == "" {
		return
	}
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	if writeFileAtomic(p, buf.Bytes(), 0600) == nil {
		dropThumbCache(key)
	}
}

// saveCurrentThumbnail: called when a technician session ends.
func saveCurrentThumbnail() {
	if thumbTimer != nil {
		thumbTimer.Stop()
	}
	if k, _ := currentRemoteKey.Load().(string); k != "" {
		saveThumbnail(k)
	}
	currentRemoteKey.Store("")
}

func setThumbsEnabled(on bool) {
	updateSettings(func(s *appSettings) { s.NoThumbnails = !on })
	if !on {
		if d := appDir(); d != "" {
			_ = os.RemoveAll(filepath.Join(d, "thumbs"))
		}
		thumbCacheMu.Lock()
		thumbCache = map[string]*thumbImg{}
		thumbCacheMu.Unlock()
		setStatus("● Küçük resimler kapatıldı ve silindi.")
	} else {
		setStatus("● Küçük resimler açık — bir sonraki bağlantıda çekilir.")
	}
	invalidateRect.Call(state.hwnd, 0, 0)
}

// ---- decoded thumbnail cache (window thread) ----

type thumbImg struct {
	pix  []byte // BGRA, top-down
	w, h int
}

var (
	thumbCacheMu sync.Mutex
	thumbCache   = map[string]*thumbImg{}
)

func dropThumbCache(key string) {
	thumbCacheMu.Lock()
	delete(thumbCache, key)
	thumbCacheMu.Unlock()
	if state.hwnd != 0 {
		invalidateRect.Call(state.hwnd, 0, 0)
	}
}

func loadThumb(key string) *thumbImg {
	thumbCacheMu.Lock()
	t, ok := thumbCache[key]
	thumbCacheMu.Unlock()
	if ok {
		return t
	}
	t = nil
	if b, err := os.ReadFile(thumbPath(key)); err == nil {
		if img, err := jpeg.Decode(bytes.NewReader(b)); err == nil {
			bd := img.Bounds()
			ti := &thumbImg{w: bd.Dx(), h: bd.Dy(), pix: make([]byte, bd.Dx()*bd.Dy()*4)}
			for y := 0; y < ti.h; y++ {
				for x := 0; x < ti.w; x++ {
					r, g, b, _ := img.At(bd.Min.X+x, bd.Min.Y+y).RGBA()
					i := (y*ti.w + x) * 4
					ti.pix[i], ti.pix[i+1], ti.pix[i+2] = byte(b>>8), byte(g>>8), byte(r>>8)
				}
			}
			t = ti
		}
	}
	thumbCacheMu.Lock()
	thumbCache[key] = t
	thumbCacheMu.Unlock()
	return t
}

// ---- tiles ----

type tileHit struct {
	r   rect
	key string
}

var (
	tileHits     []tileHit
	thumbsToggle rect
	hoveredTile  string
)

func savedDevice(id string) (techDev, bool) {
	for _, d := range techDevices {
		if strings.EqualFold(d.ID, id) {
			return d, true
		}
	}
	return techDev{}, false
}

func paintTiles(hdc uintptr, w, h int, g homeGeom) {
	tileHits = tileHits[:0]
	y := g.tilesY
	paintText(hdc, "SON BAĞLANTILAR", contentX, y, 300, 18, uiLabelFont, uiMutedColor, txVLeft)
	lbl := "Küçük resimler: Açık"
	if !thumbsEnabled() {
		lbl = "Küçük resimler: Kapalı"
	}
	tw := 190
	thumbsToggle = rect{Left: int32(w - gutter - tw), Top: int32(y - 2), Right: int32(w - gutter), Bottom: int32(y + 20)}
	paintText(hdc, lbl, w-gutter-tw, y, tw, 18, uiSmallFont, uiAccentColor, txVRight)
	y += 28
	list := remotesSnapshot()
	bottom := h - footerH - 12
	if len(list) == 0 {
		paintText(hdc, "Henüz destek verdiğiniz bir bilgisayar yok. Bağlandığınız bilgisayarlar burada küçük resimleriyle listelenir; tıklayarak yeniden bağlanırsınız.", contentX, y+6, w-contentX-gutter, 44, uiBodyFont, uiFaintColor, txWrap)
		return
	}
	cols := (w - contentX - gutter + tileGap) / (tileW + tileGap)
	if cols < 1 {
		cols = 1
	}
	for i, e := range list {
		col, row := i%cols, i/cols
		x := contentX + col*(tileW+tileGap)
		ty := y + row*(tileH+tileGap)
		if ty+tileH > bottom {
			break
		}
		hot := e.Key == hoveredTile
		var fill, border uint32 = uiPanelColor, uiBorderColor
		if hot {
			fill, border = uiPanelHi, uiAccentColor
		}
		paintCard(hdc, x, ty, tileW, tileH, fill, border, 12)
		ix, iy, iw := x+8, ty+8, tileW-16
		if t := loadThumb(e.Key); t != nil && thumbsEnabled() {
			bi := bitmapInfo{BmiHeader: bitmapInfoHeader{BiSize: uint32(unsafe.Sizeof(bitmapInfoHeader{})), BiWidth: int32(t.w), BiHeight: -int32(t.h), BiPlanes: 1, BiBitCount: 32}}
			// Fit inside the image area, keeping the aspect ratio.
			l, tp, dw, dh := fitRect(t.w, t.h, iw, tileImgH)
			paintRoundPanel(hdc, ix, iy, iw, tileImgH, 0x000000, 6)
			setStretchMode.Call(hdc, 4)
			setBrushOrgEx.Call(hdc, 0, 0, 0)
			stretchDIBits.Call(hdc, uintptr(ix+l), uintptr(iy+tp), uintptr(dw), uintptr(dh), 0, 0, uintptr(t.w), uintptr(t.h),
				uintptr(unsafe.Pointer(&t.pix[0])), uintptr(unsafe.Pointer(&bi)), 0, 0x00CC0020)
		} else {
			paintRoundPanel(hdc, ix, iy, iw, tileImgH, uiEditColor, 6)
			paintText(hdc, "▣", ix, iy, iw, tileImgH, uiHeaderFont, uiFaintColor, txMid)
		}
		paintText(hdc, e.Name, x+10, ty+tileImgH+12, tileW-20, 20, uiButtonFont, uiTextColor, txEnd)
		sub := "Kodla bağlanılır"
		var subCol uint32 = uiFaintColor
		if e.DeviceID != "" {
			if _, ok := savedDevice(e.DeviceID); ok {
				sub, subCol = "● "+e.DeviceID, uiSuccess
			} else {
				sub = e.DeviceID
			}
		}
		paintText(hdc, sub, x+10, ty+tileImgH+32, tileW-20-74, 16, uiSmallFont, subCol, txEnd)
		paintText(hdc, relativeTime(e.At), x+10, ty+tileImgH+32, tileW-20, 16, uiSmallFont, uiFaintColor, txVRight)
		tileHits = append(tileHits, tileHit{r: rect{Left: int32(x), Top: int32(ty), Right: int32(x + tileW), Bottom: int32(ty + tileH)}, key: e.Key})
	}
}

func tileAt(x, y int) string {
	for _, t := range tileHits {
		if int32(x) >= t.r.Left && int32(x) < t.r.Right && int32(y) >= t.r.Top && int32(y) < t.r.Bottom {
			return t.key
		}
	}
	return ""
}

func remoteByKey(key string) (remoteEntry, bool) {
	for _, e := range remotesSnapshot() {
		if e.Key == key {
			return e, true
		}
	}
	return remoteEntry{}, false
}

// connectTile reconnects to a tile: a saved device connects at once; a
// computer reached by code needs its new code.
func connectTile(key string) {
	e, ok := remoteByKey(key)
	if !ok {
		return
	}
	if e.DeviceID != "" {
		if d, ok := savedDevice(e.DeviceID); ok {
			setText(state.remoteEdit, d.ID)
			setText(state.remotePass, d.Pass)
			onCommand(ID_CONNECT)
			return
		}
	}
	setText(state.remoteEdit, "")
	setText(state.remotePass, "")
	setFocus.Call(state.remoteEdit)
	setStatus("● " + e.Name + " için karşı taraftaki yeni kodu girin ve BAĞLAN'a basın.")
}

func tileMenu(key string) {
	e, ok := remoteByKey(key)
	if !ok {
		return
	}
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	add := func(id int, label string) {
		appendMenuW.Call(menu, 0, uintptr(id), uintptr(unsafe.Pointer(utf16ptr(label))))
	}
	add(ID_TILE_CONNECT, "Bağlan")
	add(ID_TILE_RENAME, "Yeniden Adlandır…")
	add(ID_TILE_REMOVE, "Listeden Kaldır")
	var pt point
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := trackPopupMenu.Call(menu, 0x0100, uintptr(pt.X), uintptr(pt.Y), 0, state.hwnd, 0)
	switch int(cmd) {
	case ID_TILE_CONNECT:
		connectTile(key)
	case ID_TILE_RENAME:
		if name, ok := promptText("Yeniden Adlandır", "Bu bilgisayar için ad:", e.Name); ok {
			if name = strings.TrimSpace(name); name != "" {
				remotesMu.Lock()
				for i := range remotes {
					if remotes[i].Key == key {
						remotes[i].Name = name
					}
				}
				saveRemotesLocked()
				remotesMu.Unlock()
				invalidateRect.Call(state.hwnd, 0, 0)
			}
		}
	case ID_TILE_REMOVE:
		remotesMu.Lock()
		for i := range remotes {
			if remotes[i].Key == key {
				remotes = append(remotes[:i], remotes[i+1:]...)
				break
			}
		}
		saveRemotesLocked()
		remotesMu.Unlock()
		_ = os.Remove(thumbPath(key))
		dropThumbCache(key)
	}
}

// ---------------------------------------------------------------------------
// ☰ menu
// ---------------------------------------------------------------------------

func inMenuButton(x, y int) bool {
	return x >= menuBtnX && x < menuBtnX+menuBtnS && y >= menuBtnY && y < menuBtnY+menuBtnS
}

func showMainMenu() {
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	page := sidebarPage.Load()
	add := func(m uintptr, id int, label string, checked bool) {
		flags := uintptr(0)
		if checked {
			flags |= 0x0008 // MF_CHECKED
		}
		appendMenuW.Call(m, flags, uintptr(id), uintptr(unsafe.Pointer(utf16ptr(label))))
	}
	add(menu, ID_MENU_HOME, "Ana Sayfa", page == 0)
	add(menu, ID_MENU_BOOK, "Bağlantılar", page == 1)
	add(menu, ID_SETTINGS, "Ayarlar", page == 2)
	add(menu, ID_MENU_DEVICE, "Cihaz Erişimi", page == 4)
	add(menu, ID_ABOUT, "Hakkında", page == 3)
	appendMenuW.Call(menu, 0x0800, 0, 0)
	add(menu, ID_MENU_NEWWIN, "Yeni Oturum Penceresi", false)
	add(menu, ID_NETLOG_OPEN, "Bağlantı Günlüğünü Aç", false)
	if sub, _, _ := createPopupMenu.Call(); sub != 0 {
		add(sub, ID_MANUAL_OP, "Davet Kodu Oluştur (destek veren)", false)
		add(sub, ID_MANUAL_AGENT, "Davet Kodunu Yapıştır (destek alan)", false)
		appendMenuW.Call(menu, 0x0010, sub, uintptr(unsafe.Pointer(utf16ptr("Sunucusuz Bağlantı"))))
	}
	// Under the button.
	pt := point{X: menuBtnX, Y: menuBtnY + menuBtnS + 4}
	clientToScreen.Call(state.hwnd, uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := trackPopupMenu.Call(menu, 0x0100, uintptr(pt.X), uintptr(pt.Y), 0, state.hwnd, 0)
	if cmd != 0 {
		onCommand(int(cmd))
	}
}

// onHomeCommand handles the menu and home-screen commands.
func onHomeCommand(id int) bool {
	switch id {
	case ID_MENU_BUTTON:
		showMainMenu()
	case ID_MENU_HOME:
		sidebarPage.Store(0)
		home()
	case ID_MENU_BOOK:
		sidebarPage.Store(1)
		showBook()
	case ID_MENU_DEVICE:
		sidebarPage.Store(4)
		showDeviceAccess()
	case ID_MENU_NEWWIN:
		launchNewSession()
	case ID_MENU_THUMBS:
		setThumbsEnabled(!thumbsEnabled())
	default:
		return false
	}
	return true
}

// homeClick handles clicks on the painted parts of the shell (☰ button,
// tiles, thumbnail toggle). Returns true when consumed.
func homeClick(x, y int, right bool) bool {
	if inMenuButton(x, y) && !right {
		showMainMenu()
		return true
	}
	state.mu.Lock()
	mode := state.mode
	state.mu.Unlock()
	viewer.mu.RLock()
	connected := viewer.active
	viewer.mu.RUnlock()
	// The logo / name in the header goes back to the home screen.
	if !right && !connected && !(mode == 1 && capturing.Load()) && x >= menuBtnX+menuBtnS+10 && x < 420 && y >= 12 && y < headerH-10 {
		home()
		return true
	}
	if !homeVisible(mode, connected) {
		return false
	}
	if !right && int32(x) >= thumbsToggle.Left && int32(x) < thumbsToggle.Right && int32(y) >= thumbsToggle.Top && int32(y) < thumbsToggle.Bottom {
		setThumbsEnabled(!thumbsEnabled())
		return true
	}
	if k := tileAt(x, y); k != "" {
		if right {
			tileMenu(k)
		} else {
			connectTile(k)
		}
		return true
	}
	return false
}

// homeHover highlights the tile under the mouse.
func homeHover(x, y int) {
	k := tileAt(x, y)
	if k == hoveredTile {
		return
	}
	hoveredTile = k
	if state.hwnd != 0 {
		invalidateRect.Call(state.hwnd, 0, 0)
	}
	if k != "" {
		tme := trackMouseEvent{CbSize: uint32(unsafe.Sizeof(trackMouseEvent{})), DwFlags: 0x02, HwndTrack: state.hwnd}
		trackMouseEventProc.Call(uintptr(unsafe.Pointer(&tme)))
	}
}

var _ image.Image // image is used through jpeg.Decode's return type

// hideHomeControls hides the home screen's child controls.
func hideHomeControls() {
	for _, h := range []uintptr{state.codeLabel, state.copy, state.shareLink, state.remoteEdit, state.remotePass,
		state.techDev, state.techDevManage, state.action, state.homeGet, state.homeGive, state.elevate} {
		hide(h)
	}
}

// showAgentMenu: "İşlemler" on the computer being supported.
func showAgentMenu() {
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	add := func(id int, label string) {
		appendMenuW.Call(menu, 0, uintptr(id), uintptr(unsafe.Pointer(utf16ptr(label))))
	}
	add(ID_FILE_SEND, "Dosya Gönder")
	add(ID_FILE_CANCEL, "Dosya Aktarımını İptal Et")
	add(ID_CLIP_SEND, "Panoyu Gönder")
	appendMenuW.Call(menu, 0x0800, 0, 0)
	sasLabel := "Güvenlik Kodunu Doğrula (SAS)"
	if sasVerified.Load() {
		sasLabel = "Güvenlik Kodu Doğrulandı ✓"
	}
	add(ID_SAS_VERIFY, sasLabel)
	add(ID_NETLOG_OPEN, "Bağlantı Günlüğünü Aç")
	var pt point
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := trackPopupMenu.Call(menu, 0x0100, uintptr(pt.X), uintptr(pt.Y), 0, state.hwnd, 0)
	if cmd != 0 {
		onCommand(int(cmd))
	}
}
