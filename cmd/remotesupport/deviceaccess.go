//go:build windows

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	clientsignaling "github.com/eirahoutmoss/remotesupport/client/signaling"
)

// deviceModeActive is set while this machine is serving technician access.
var deviceModeActive atomic.Bool

// deviceModeGen tokens the running loop. startDeviceMode launches a loop with a
// token; stopDeviceMode (and any restart) advances it so the old loop exits.
// This guarantees exactly one announce loop — multiple concurrent loops were
// registering in a tight cycle, overwriting each other on the server and
// tripping its create rate-limiter, which left the device unreachable.
var deviceModeGen atomic.Int64

// effectiveDeviceID is the configured id, or a generated default
// (hostname + random suffix) so two PCs with the same name don't collide.
func effectiveDeviceID() string {
	cfg := currentSettings()
	if id := strings.TrimSpace(cfg.DeviceID); id != "" {
		return id
	}
	if id := strings.TrimSpace(cfg.DeviceAutoID); id != "" {
		return id
	}
	return regenerateAutoDeviceID()
}

// usingAutoDeviceID reports whether the id in use is the generated default.
func usingAutoDeviceID() bool { return strings.TrimSpace(currentSettings().DeviceID) == "" }

// regenerateAutoDeviceID makes and stores a new default id.
func regenerateAutoDeviceID() string {
	h, _ := os.Hostname()
	h = strings.ToUpper(strings.TrimSpace(h))
	if h == "" {
		h = "PC"
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	id := h + "-" + string(b)
	settingsMu.Lock()
	appCfg.DeviceAutoID = id
	settingsMu.Unlock()
	saveSettings()
	return id
}

// startDeviceMode registers this machine with the server (device id + password)
// and waits for technician connections, re-registering after each session. It
// is idempotent: if a loop is already running it does nothing, so the app
// launch, REGDEV, and the settings page can all call it safely.
func startDeviceMode() {
	cfg := currentSettings()
	if !cfg.DeviceAccessOn {
		return
	}
	// Escape hatch for single-machine testing: a second instance launched with
	// REMOTESUPPORT_NO_DEVICE set will act purely as technician and not announce
	// the shared device id, so the two instances don't evict each other.
	if strings.TrimSpace(os.Getenv("REMOTESUPPORT_NO_DEVICE")) != "" {
		netlogf("Cihaz erişimi REMOTESUPPORT_NO_DEVICE ile bu örnekte kapalı")
		return
	}
	id := effectiveDeviceID()
	pass := cfg.DevicePass
	if id == "" || len(pass) < 4 {
		netlogf("Cihaz erişimi açık ama kimlik/parola eksik — devre dışı")
		return
	}
	// One NexDesk process per Windows session announces the device; a second
	// window works as a technician window only.
	if !acquireDeviceRole() {
		netlogf("Cihaz erişimi başka bir NexDesk penceresinde etkin — bu pencere teknisyen modunda")
		setStatus("● Cihaz erişimi başka bir NexDesk penceresinde zaten etkin; bu pencere yalnızca destek vermek için kullanılabilir.")
		return
	}
	// One loop only. CAS fails if a loop is already active.
	if !deviceModeActive.CompareAndSwap(false, true) {
		return
	}
	ensureFirewall()
	startEmbeddedSignaling()
	gen := deviceModeGen.Add(1)
	if !codeSessionActive.Load() {
		setActiveCode(id)
	}
	// Run in the background: the app stays on whatever screen the user is on
	// (Home on launch). A connecting technician triggers the approval dialog
	// and then the live session view; status reflects it meanwhile.
	setStatus("● Cihaz erişimi arka planda etkin — teknisyen kimlik ve parola ile bağlanabilir.")
	go deviceModeLoop(id, pass, gen)
}

func deviceModeLoop(id, pass string, gen int64) {
	defer logCrash("deviceMode")
	// Release the active flag only if this loop is still the current one; a
	// stop or restart that advanced the generation owns the flag instead.
	defer func() {
		if deviceModeGen.Load() == gen {
			deviceModeActive.Store(false)
		}
	}()
	const baseBackoff = 3 * time.Second
	const maxBackoff = 30 * time.Second
	backoff := baseBackoff
	for {
		if deviceModeGen.Load() != gen {
			return // superseded by stop or a newer start
		}
		ep := signalingEndpoint()
		ctx, cancel := context.WithCancel(context.Background())
		c, err := clientsignaling.Dial(ctx, ep)
		if err != nil {
			netlogf("Cihaz erişimi: sunucuya bağlanılamadı (%s): %v", ep, err)
			cancel()
			sleepGen(backoff, gen)
			backoff = nextBackoff(backoff, maxBackoff)
			continue
		}
		c.App = appVersion
		c.DeviceOwner = ensureDeviceOwner()
		if err := c.CreateDevice(ctx, id, pass); err != nil {
			_ = c.Close(context.Background())
			cancel()
			netlogf("Cihaz kaydı başarısız: %v", err)
			if strings.Contains(err.Error(), "device_id_taken") && usingAutoDeviceID() {
				old := id
				id = regenerateAutoDeviceID()
				netlogf("Cihaz kimliği %s başka bir bilgisayarda kullanılıyor — yeni kimlik: %s", old, id)
				if !codeSessionActive.Load() {
					setActiveCode(id)
				}
				sleepGen(2*time.Second, gen)
				continue
			}
			if strings.Contains(err.Error(), "device_id_taken") {
				setDeviceError("Bu cihaz kimliği başka bir bilgisayara ait. Ayarlardan farklı bir kimlik girin (ya da kutuyu boşaltın, otomatik kimlik üretilsin).")
			} else if strings.Contains(err.Error(), "device_auth_failed") {
				setDeviceError("Bu cihaz kimliği sunucuda başka bir parolayla kayıtlı. Eski parolayı girin, farklı bir cihaz kimliği seçin ya da sunucu yöneticisi eski kaydı silsin.")
			} else {
				setDeviceError("Sunucuya kaydolunamadı: " + err.Error())
			}
			// rate_limited means we (or a prior build's duplicate loop) hit the
			// server too fast; wait well clear of its window before retrying.
			wait := 15 * time.Second
			if strings.Contains(err.Error(), "rate_limited") {
				wait = 20 * time.Second
			}
			sleepGen(wait, gen)
			continue
		}
		netlogf("Cihaz erişimi çevrimiçi: %s (sunucu=%s)", id, ep)
		setDeviceError("")
		// The device link keeps its own context and socket: a "Destek Al"
		// code session (state.ctx/state.sig) can run next to it, and ending
		// that session must not take the device offline.
		deviceLinkMu.Lock()
		deviceLinkSig, deviceLinkCancel = c, cancel
		deviceLinkMu.Unlock()
		if !capturing.Load() {
			setStatus("● Cihaz erişimi etkin — bağlantı bekleniyor (kimlik: " + id + ").")
		}
		// Serve one technician session (blocks until the signaling link drops),
		// then re-register so the device stays reachable.
		c.KeepAlive(ctx, signalingKeepAlive) // long idle waits behind NAT/proxies
		start := time.Now()
		lost := targetLoopMode(ctx, c, true)
		_ = c.Close(context.Background())
		if lost {
			// Signaling dropped but media is peer-to-peer: cancelling ctx now
			// would stop capture and kill a live session. Wait for it to end.
			waitSessionEnd(gen)
		}
		techDropAll(c)
		cancel()
		if deviceModeGen.Load() != gen {
			return
		}
		// A link that dropped almost at once was most likely evicted by another
		// registration of the same device id (a duplicate instance, or a restart
		// race). Grow the backoff so two instances don't ping-pong every few
		// seconds; a link that lasted was a real session, so reset to base.
		if time.Since(start) < 5*time.Second {
			netlogf("Cihaz oturumu hemen düştü — %s sonra tekrar denenecek (aynı kimlikle başka bir örnek olabilir)", backoff)
			sleepGen(backoff, gen)
			backoff = nextBackoff(backoff, maxBackoff)
		} else {
			backoff = baseBackoff
			sleepGen(backoff, gen)
		}
	}
}

// waitSessionEnd blocks while the current WebRTC session is still alive.
func waitSessionEnd(gen int64) {
	logged := false
	for deviceModeGen.Load() == gen {
		if len(techPeers()) == 0 {
			return
		}
		if !logged {
			netlogf("Cihaz erişimi: sinyal kanalı koptu, etkin oturum bitene kadar bekleniyor")
			logged = true
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// deviceError is shown on the device-access screens while registration
// fails ("" = registered / trying normally).
var deviceError atomic.Value

func setDeviceError(s string) {
	if old, _ := deviceError.Load().(string); old == s {
		return
	}
	deviceError.Store(s)
	if state.hwnd != 0 {
		invalidateRect.Call(state.hwnd, 0, 0)
	}
}

func getDeviceError() string { s, _ := deviceError.Load().(string); return s }

// ensureDeviceOwner returns this machine's owner secret, creating it once.
func ensureDeviceOwner() string {
	cfg := currentSettings()
	if len(cfg.DeviceOwner) >= 32 {
		return cfg.DeviceOwner
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	cfg.DeviceOwner = hex.EncodeToString(b)
	settingsMu.Lock()
	appCfg.DeviceOwner = cfg.DeviceOwner
	settingsMu.Unlock()
	saveSettings()
	return cfg.DeviceOwner
}

var (
	deviceLinkMu     sync.Mutex
	deviceLinkSig    *clientsignaling.Client
	deviceLinkCancel context.CancelFunc
)

func nextBackoff(cur, max time.Duration) time.Duration {
	if n := cur * 2; n < max {
		return n
	}
	return max
}

// sleepGen waits up to d, waking early if this loop was superseded.
func sleepGen(d time.Duration, gen int64) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if deviceModeGen.Load() != gen {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// stopDeviceMode ends background device access immediately (Settings → Kapalı).
func stopDeviceMode() {
	if !deviceModeActive.Load() {
		return
	}
	deviceModeGen.Add(1) // supersede the running loop
	deviceModeActive.Store(false)
	setDeviceError("")
	releaseDeviceRole()
	deviceLinkMu.Lock()
	c, cancel := deviceLinkSig, deviceLinkCancel
	deviceLinkSig, deviceLinkCancel = nil, nil
	deviceLinkMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if c != nil {
		_ = c.Close(context.Background())
	}
	netlogf("Cihaz erişimi kapatıldı")
}
