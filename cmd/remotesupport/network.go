//go:build windows

package main

import (
	"context"
	"image"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eirahoutmoss/remotesupport/client/capture"
	"github.com/eirahoutmoss/remotesupport/client/screen"
	webrtcpeer "github.com/eirahoutmoss/remotesupport/client/webrtc"
	server "github.com/eirahoutmoss/remotesupport/server/signaling"
	"github.com/eirahoutmoss/remotesupport/shared/landisc"
	"golang.org/x/sys/windows"
)

func startCapture(peer *webrtcpeer.Peer, ctx context.Context) {
	go func() {
		defer logCrash("capture")
		counted := false
		defer func() {
			// Several technicians each have a stream; the session-wide state
			// (capturing flag, privacy curtain) ends with the last one.
			n := activeStreams.Load()
			if counted {
				n = activeStreams.Add(-1)
			}
			tilesByPeer.Delete(peer)
			if n > 0 {
				return
			}
			capturing.Store(false)
			clearPrivacy()
			if state.hwnd != 0 {
				invalidateRect.Call(state.hwnd, 0, 0)
			}
		}()
		if err := peer.WaitScreen(ctx); err != nil {
			if ctx.Err() == nil {
				setStatus("● Ekran kanalı bekleniyor: " + err.Error())
			}
			return
		}
		setStatus("● Ekran kanalı hazır. Ekran aktarılıyor...")
		// ~15 fps cap. Frames are produced only when the screen channel has
		// drained (ScreenIdle), and only changed 64x64 tiles are sent.
		ticker := time.NewTicker(66 * time.Millisecond)
		defer ticker.Stop()
		enc := &tileEncoder{}
		activeTiles.Store(enc)
		tilesByPeer.Store(peer, enc)
		var seq uint64
		var cong congestion
		var capFail captureFailure
		var busySince time.Time
		var busyLogged, encLogged, bigLogged bool
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if fraudHold.Load() {
					continue // fraud shield: stop streaming until the user decides
				}
				busy := !peer.ScreenIdle()
				cong.tick(peer, busy)
				if busy {
					if busySince.IsZero() {
						busySince = time.Now()
					} else if !busyLogged && !counted && time.Since(busySince) > 15*time.Second {
						busyLogged = true
						netlogf("Ekran kanalı 15 sn'dir dolu, ilk kare gönderilemedi (kanal açık mı / veri akıyor mu?)")
					}
					continue // link still busy with the previous frame
				}
				busySince = time.Time{}
				monitor := int(activeMonitor.Load())
				maxHeight := int(captureMaxHeight.Load())
				cfg := currentSettings()
				quality := cfg.Quality
				if quality == 0 {
					quality = 88
				}
				quality, maxHeight = adaptParams(quality, maxHeight)
				img, err := sharedCapture(monitor, maxHeight)
				if err != nil {
					// Ctrl+Alt+Del / UAC / lock screen switch to the secure desktop,
					// where capture fails. Keep retrying instead of ending the stream,
					// but tell a real failure (wrong session, blocked by security
					// software) apart from it, log it and tell the technician.
					capFail.fail(peer, monitor, err)
					enc.reset()
					continue
				}
				capFail.ok(peer)
				secureDesktopState(peer, false)
				payload, skip, err := enc.encode(img, monitor, quality)
				if err != nil {
					if !encLogged {
						encLogged = true
						netlogf("Ekran kodlanamadı: %v", err)
					}
					continue
				}
				if skip {
					continue
				}
				if len(payload) > screen.MaxPayloadSize {
					if !bigLogged {
						bigLogged = true
						netlogf("Ekran karesi çok büyük (%d KB > %d KB), atlandı", len(payload)/1024, screen.MaxPayloadSize/1024)
					}
					enc.reset()
					continue
				}
				seq++
				err = peer.SendScreenFrame(screen.Frame{
					Monitor: uint16(monitor),
					Seq:     seq,
					Width:   uint32(img.Bounds().Dx()),
					Height:  uint32(img.Bounds().Dy()),
					JPEG:    payload,
				})
				if err != nil {
					netlogf("Ekran gönderim hatası: %v", err)
					return
				}
				// Bir kare fiilen bu makineden çıktı: agent artık görülüyor.
				if !counted {
					counted = true
					activeStreams.Add(1)
					netlogf("İlk ekran karesi gönderildi: monitör %d, %dx%d, %d KB", monitor+1, img.Bounds().Dx(), img.Bounds().Dy(), len(payload)/1024)
				}
				if capturing.CompareAndSwap(false, true) && state.hwnd != 0 {
					invalidateRect.Call(state.hwnd, 0, 0)
				}
			}
		}
	}()
}

var (
	activeStreams atomic.Int32 // screen streams that have sent at least one frame
	tilesByPeer   sync.Map     // *webrtcpeer.Peer -> *tileEncoder

	capMu    sync.Mutex
	capImg   *image.RGBA
	capKey   [2]int
	capAt    time.Time
	capErr   error
	capReuse = 50 * time.Millisecond
)

// sharedCapture grabs the screen once for all technicians' streams: a frame
// captured within capReuse for the same monitor/size is handed out again.
// Encoders only read the image, so sharing it is safe.
func sharedCapture(monitor, maxHeight int) (*image.RGBA, error) {
	capMu.Lock()
	defer capMu.Unlock()
	k := [2]int{monitor, maxHeight}
	if k == capKey && time.Since(capAt) < capReuse && (capImg != nil || capErr != nil) {
		return capImg, capErr
	}
	img, err := capture.CaptureMonitorImage(monitor, maxHeight)
	capImg, capErr, capKey, capAt = img, err, k, time.Now()
	return img, err
}

func startEmbeddedSignaling() {
	// Bind the port ourselves so we KNOW we own it. The old probe only checked
	// whether *something* answered on :8091, so a leftover/old RemoteSupport
	// instance holding the port made us use ITS (6-digit) signaling. Now we use
	// our own 10-digit server when we can bind, and warn clearly when we cannot.
	ln, err := net.Listen("tcp", "0.0.0.0:8091")
	if err != nil {
		log.Printf("Embedded signaling could not bind :8091: %v", err)
		setStatus("● 8091 portu meşgul: açık/eski bir NexDesk örneği olabilir. Onu kapatıp yeniden başlatın.")
		return
	}
	s := server.NewServer()
	embeddedSignalingReady.Store(true)
	go func() {
		if e := http.Serve(ln, s.Handler()); e != nil {
			log.Printf("Embedded signaling stopped: %v", e)
			embeddedSignalingReady.Store(false)
		}
	}()
	go landisc.Serve(log.Printf)
}

func signalingEndpoint() string {
	if v := strings.TrimSpace(os.Getenv("REMOTESUPPORT_SIGNALING_URL")); v != "" {
		return v
	}
	cfg := currentSettings()
	if v := pickSignaling(cfg.SignalingURL); v != "" {
		return v
	}
	if v := builtinSignaling(); v != "" {
		return v
	}
	if embeddedSignalingReady.Load() {
		return "ws://127.0.0.1:8091/v1/ws"
	}
	if v := discoverSignalingEndpoint(1200 * time.Millisecond); v != "" {
		return v
	}
	return defaultSignalingURL
}

func discoverSignalingEndpoint(timeout time.Duration) string {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
	if err != nil {
		return ""
	}
	defer conn.Close()
	if raw, err := conn.SyscallConn(); err == nil {
		_ = raw.Control(func(fd uintptr) {
			_ = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_BROADCAST, 1)
		})
	}
	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	msg := []byte(landisc.Request)
	for _, bcast := range discoveryBroadcasts() {
		_, _ = conn.WriteToUDP(msg, &net.UDPAddr{IP: bcast, Port: landisc.DiscoveryPort})
	}
	buf := make([]byte, 256)
	for time.Now().Before(deadline) {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return ""
		}
		line := strings.TrimSpace(string(buf[:n]))
		if strings.HasPrefix(line, landisc.ResponsePrefix) {
			u := strings.TrimSpace(strings.TrimPrefix(line, landisc.ResponsePrefix))
			if parsed, err := url.Parse(u); err == nil && parsed.Scheme == "ws" && parsed.Host != "" {
				return u
			}
		}
	}
	return ""
}

func discoveryBroadcasts() []net.IP {
	result := []net.IP{net.IPv4bcast}
	ifaces, err := net.Interfaces()
	if err != nil {
		return result
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			mask := ipnet.Mask
			if ip == nil || len(mask) != 4 {
				continue
			}
			b := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				b[i] = ip[i] | ^mask[i]
			}
			result = append(result, b)
		}
	}
	return result
}

// captureFailure tracks one stream's capture errors: each distinct error is
// logged once with a session/desktop diagnosis, the secure desktop keeps its
// own notice, and any other failure lasting a few seconds is reported to the
// technician instead of leaving them with an empty screen.
type captureFailure struct {
	since    time.Time
	lastErr  string
	reported bool
}

const captureFailReportAfter = 3 * time.Second

func (f *captureFailure) fail(peer *webrtcpeer.Peer, monitor int, err error) {
	d := capture.Diagnose()
	if f.since.IsZero() {
		f.since = time.Now()
	}
	if msg := err.Error(); msg != f.lastErr {
		f.lastErr = msg
		netlogf("Ekran yakalanamadı (monitör %d): %v [%s]", monitor+1, err, d)
	}
	if d.SecureDesktop() {
		secureDesktopState(peer, true)
		return
	}
	if f.reported || time.Since(f.since) < captureFailReportAfter {
		return
	}
	f.reported = true
	reason := captureFailText(d, err)
	netlogf("Ekran yakalama başarısız, teknisyene bildirildi: %s", reason)
	setStatus("● Ekran alınamıyor: " + reason)
	_ = peer.SendControlText("CAPTURE_FAIL:" + reason)
}

func (f *captureFailure) ok(peer *webrtcpeer.Peer) {
	if f.since.IsZero() {
		return
	}
	netlogf("Ekran yakalama düzeldi (%s sonra)", time.Since(f.since).Round(time.Second))
	if f.reported {
		_ = peer.SendControlText("CAPTURE_FAIL:0")
		setStatus("● Ekran paylaşımı devam ediyor.")
	}
	*f = captureFailure{}
}

func captureFailText(d capture.Diagnosis, err error) string {
	if d.OutsideConsole() {
		return "NexDesk bilgisayarın ekrandaki oturumunda çalışmıyor (Uzak Masaüstü ile bağlanılmış, kullanıcı değiştirilmiş ya da program hizmet olarak başlatılmış olabilir). Ekrandaki oturumda açın."
	}
	return "Windows ekran görüntüsü vermiyor (" + err.Error() + "). Bir güvenlik / veri sızıntısı önleme yazılımı engelliyor olabilir."
}
