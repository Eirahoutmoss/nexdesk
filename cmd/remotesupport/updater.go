//go:build windows

package main

// Self-update from GitHub releases. A release carries NexDesk.exe and
// NexDesk.exe.sig (made by cmd/nd-sign); the signature is checked against the
// public key embedded at build time before anything is replaced. The running
// exe is renamed to *.old (Windows allows renaming a running image), the new
// file takes its place, the new version is started and this one closes.

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/eirahoutmoss/remotesupport/shared/update"
	"github.com/pion/webrtc/v4"
)

//go:embed assets/update-pubkey.txt
var updatePubKey string

const (
	updateExeName      = "NexDesk.exe"
	updateAPIBase      = "https://api.github.com"
	updateCheckEvery   = 12 * time.Hour
	updateStartupDelay = 10 * time.Second
)

var (
	updateBusy      atomic.Bool
	updateAskedMu   sync.Mutex
	updateAskedVers string // version already offered automatically this run
)

func updateHTTP(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout} // DefaultTransport: honours system proxy env
}

// latestRelease returns the newest GitHub release. A release that lacks the
// exe/signature is returned together with an error (manual download only).
func latestRelease() (*update.Release, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return update.Latest(ctx, updateHTTP(15*time.Second), updateAPIBase, update.DefaultRepo, updateExeName)
}

// startUpdateWatcher runs a quiet check shortly after start and then
// periodically. It only prompts when no session is active.
func startUpdateWatcher() {
	go cleanupOldExe()
	go func() {
		defer logCrash("updateWatcher")
		time.Sleep(updateStartupDelay)
		for {
			if rel, err := latestRelease(); rel != nil {
				offerUpdate(rel, false)
				repaintAbout()
			} else if err != nil && !errors.Is(err, update.ErrNoRelease) {
				netlogf("Otomatik güncelleme kontrolü: %v", err)
			}
			time.Sleep(updateCheckEvery)
		}
	}()
}

// offerUpdate updates the status line and, when a newer version exists, asks
// the user. manual=true comes from the button and always asks; automatic
// checks ask once per version and never during a session.
func offerUpdate(rel *update.Release, manual bool) {
	if rel == nil {
		return
	}
	if !update.Newer(rel.Version, appVersion) {
		setUpdateStatus("Güncel (" + appVersion + ")")
		return
	}
	setUpdateStatus("Yeni sürüm mevcut: " + rel.Version)
	netlogf("Yeni sürüm bulundu: %s (sizde %s)", rel.Version, appVersion)
	if !manual {
		updateAskedMu.Lock()
		asked := updateAskedVers == rel.Version
		updateAskedVers = rel.Version
		updateAskedMu.Unlock()
		if asked || sessionActive() {
			return
		}
	}
	if rel.ExeURL == "" || rel.SigURL == "" || updateKey() == nil {
		if updAsk(fmt.Sprintf("Yeni sürüm mevcut: %s (sizde %s).\n\nBu sürüm otomatik kurulamıyor. İndirme sayfası açılsın mı?", rel.Version, appVersion), false) {
			openURL(rel.PageURL)
		}
		return
	}
	notes := strings.TrimSpace(rel.Notes)
	if len([]rune(notes)) > 600 {
		notes = string([]rune(notes)[:600]) + "…"
	}
	if notes != "" {
		notes = "\n\nYenilikler:\n" + notes
	}
	if updAsk(fmt.Sprintf("Yeni sürüm mevcut: %s (sizde %s).%s\n\nŞimdi indirilip kurulsun mu? Program kısa süreliğine kapanıp yeniden açılacak.", rel.Version, appVersion, notes), false) {
		go installUpdate(rel)
	}
}

func updateKey() []byte {
	k, err := update.ParsePublicKey(updatePubKey)
	if err != nil {
		netlogf("Güncelleme anahtarı geçersiz: %v", err)
		return nil
	}
	return k
}

// sessionActive reports a live WebRTC session (either role).
func sessionActive() bool {
	state.mu.Lock()
	p := state.peer
	state.mu.Unlock()
	return (p != nil && p.ConnectionState() == webrtc.PeerConnectionStateConnected) || len(techPeers()) > 0
}

func installUpdate(rel *update.Release) {
	defer logCrash("installUpdate")
	if !updateBusy.CompareAndSwap(false, true) {
		return
	}
	defer updateBusy.Store(false)
	if sessionActive() {
		showUpdateError("Etkin bir bağlantı varken güncelleme yapılamaz. Oturumu bitirip tekrar deneyin.")
		return
	}
	fail := func(msg string, err error) {
		netlogf("Güncelleme başarısız: %s: %v", msg, err)
		setUpdateStatus("Güncelleme başarısız: " + msg)
		repaintAbout()
		if updAsk("Güncelleme kurulamadı: "+msg+"\n\nİndirme sayfası açılsın mı?", true) {
			openURL(rel.PageURL)
		}
	}
	setUpdateStatus("İndiriliyor: " + rel.Version + "…")
	setStatus("● Güncelleme indiriliyor (" + rel.Version + ")…")
	repaintAbout()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	hc := updateHTTP(10 * time.Minute)
	sig, err := update.Download(ctx, hc, rel.SigURL, 4096)
	if err != nil {
		fail("imza indirilemedi", err)
		return
	}
	data, err := update.Download(ctx, hc, rel.ExeURL, update.MaxExeSize)
	if err != nil {
		fail("dosya indirilemedi", err)
		return
	}
	if err := update.Verify(updateKey(), updateExeName, rel.Version, data, sig); err != nil {
		fail("imza doğrulanamadı", err)
		return
	}
	exe, err := os.Executable()
	if err != nil {
		fail("program yolu bulunamadı", err)
		return
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	if err := replaceExe(exe, data); err != nil {
		fail("program dosyası değiştirilemedi (klasöre yazma izni yok olabilir; yönetici olarak çalıştırmayı deneyin)", err)
		return
	}
	netlogf("Güncelleme kuruldu: %s → %s", appVersion, rel.Version)
	auditLog("Güncelleme kuruldu: " + rel.Version)
	r, _, _ := shellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16ptr("open"))), uintptr(unsafe.Pointer(utf16ptr(exe))), 0,
		uintptr(unsafe.Pointer(utf16ptr(filepath.Dir(exe)))), 1)
	if r <= 32 {
		setUpdateStatus("Kuruldu — programı yeniden başlatın.")
		setStatus("● Güncelleme kuruldu. Programı kapatıp yeniden açın.")
		repaintAbout()
		return
	}
	postMessage.Call(state.hwnd, 0x0010, 0, 0) // WM_CLOSE: normal shutdown path
}

// replaceExe swaps the running executable for data, rolling back on failure.
func replaceExe(exe string, data []byte) error {
	tmp := exe + ".new"
	old := exe + ".old"
	_ = os.Remove(tmp)
	if err := os.WriteFile(tmp, data, 0755); err != nil {
		return err
	}
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Rename(old, exe) // roll back
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// cleanupOldExe removes the previous version left by an update; the old
// process may still be shutting down, so it retries for a while.
func cleanupOldExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	old := exe + ".old"
	for i := 0; i < 30; i++ {
		if _, err := os.Stat(old); os.IsNotExist(err) {
			return
		}
		if os.Remove(old) == nil {
			return
		}
		time.Sleep(time.Second)
	}
}

func updAsk(msg string, warn bool) bool {
	var icon uintptr = 0x00000020 // MB_ICONQUESTION
	if warn {
		icon = 0x00000030
	}
	r, _, _ := messageBox.Call(state.hwnd, uintptr(unsafe.Pointer(utf16ptr(msg))),
		uintptr(unsafe.Pointer(utf16ptr("Güncelleme"))), 0x00000004|icon|0x00010000)
	return r == 6 // IDYES
}

func showUpdateError(msg string) {
	messageBox.Call(state.hwnd, uintptr(unsafe.Pointer(utf16ptr(msg))),
		uintptr(unsafe.Pointer(utf16ptr("Güncelleme"))), 0x00000030)
}

func openURL(u string) {
	if !strings.HasPrefix(u, "https://") {
		return
	}
	shellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16ptr("open"))), uintptr(unsafe.Pointer(utf16ptr(u))), 0, 0, 1)
}
