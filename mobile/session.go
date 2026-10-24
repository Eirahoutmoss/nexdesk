// Package nexdesk is the Go core of the NexDesk Android technician app,
// exported with `gomobile bind`. It speaks the same signaling/WebRTC protocol
// as the Windows app (it is one more operator in a v3 multi-technician
// session) and leaves decoding and drawing of frames to the platform.
//
// gomobile rules apply: exported API uses only int/float64/bool/string/[]byte,
// pointers to exported structs, and callback interfaces.
package nexdesk

import (
	"context"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eirahoutmoss/remotesupport/client/screen"
	clientsignaling "github.com/eirahoutmoss/remotesupport/client/signaling"
	webrtcpeer "github.com/eirahoutmoss/remotesupport/client/webrtc"
	signaling "github.com/eirahoutmoss/remotesupport/server/signaling"
	"github.com/eirahoutmoss/remotesupport/shared/joinlink"
	"github.com/eirahoutmoss/remotesupport/shared/protocol"
	"github.com/eirahoutmoss/remotesupport/shared/tiles"
	"github.com/pion/ice/v4"
	"github.com/pion/webrtc/v4"
)

//go:embed assets/server-defaults*.json
var defaultsFS embed.FS

type serverDefaults struct {
	SignalingURL string `json:"signaling_url"`
	TurnURL      string `json:"turn_url"`
	TurnUser     string `json:"turn_user"`
	TurnPass     string `json:"turn_pass"`
}

func builtin() serverDefaults {
	var d serverDefaults
	for _, n := range []string{"assets/server-defaults.local.json", "assets/server-defaults.json"} {
		if b, err := defaultsFS.ReadFile(n); err == nil && json.Unmarshal(b, &d) == nil {
			return d
		}
	}
	return d
}

// DefaultSignalingURL is the server built into this APK ("" if none).
func DefaultSignalingURL() string { return firstURL(builtin().SignalingURL) }

func firstURL(s string) string {
	for _, u := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		return u
	}
	return ""
}

// Frame is one received screen frame. JPEG is handed over as-is (platform
// decoders ignore the tile segment). TileSize == 0 means a full frame of
// Width×Height; otherwise JPEG is an atlas whose tile i (Cols per row) is
// drawn at Tiles[2i]*TileSize, Tiles[2i+1]*TileSize. Tiles is packed as
// big-endian u16 pairs; use TileX/TileY to read it.
type Frame struct {
	Monitor  int
	Width    int
	Height   int
	TileSize int
	Cols     int
	Count    int
	JPEG     []byte
	Tiles    []byte
}

func (f *Frame) TileX(i int) int { return int(binary.BigEndian.Uint16(f.Tiles[4*i:])) }
func (f *Frame) TileY(i int) int { return int(binary.BigEndian.Uint16(f.Tiles[4*i+2:])) }

// Listener receives session events (called from Go goroutines; the app must
// hop to its UI thread).
type Listener interface {
	OnStatus(text string)
	OnConnected(path, sas string)
	OnFrame(f *Frame)
	OnRoster(text string)
	OnInputBy(name string)
	OnChat(text string)
	OnMonitors(count int)
	OnClosed(reason string)
	// OnDeviceSaved delivers the device id + password after the remote user
	// approved "save this computer"; the app stores them encrypted.
	OnDeviceSaved(id, pass string)
}

// Session is one connection to one remote computer.
type Session struct {
	l       Listener
	sigURL  string
	turnURL string
	turnU   string
	turnP   string
	name    string
	app     string

	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	sig     *clientsignaling.Client
	peer    *webrtcpeer.Peer
	turn    *signaling.ICECreds
	code    string
	stopped bool
	ever    bool
	recon   bool
	linkWS  string // signaling URL from a scanned QR / link (this connect only)

	statMu    sync.Mutex
	frames    int64
	bytes     int64
	statAt    time.Time
	statFrame int64
	statBytes int64
}

func NewSession(l Listener) *Session { return &Session{l: l} }

// Configure sets server and identity. Empty server fields use the built-in
// defaults of this build.
func (s *Session) Configure(signalingURL, turnURL, turnUser, turnPass, name, appVersion string) {
	d := builtin()
	if strings.TrimSpace(signalingURL) == "" {
		signalingURL = d.SignalingURL
	}
	if strings.TrimSpace(turnURL) == "" {
		turnURL, turnUser, turnPass = d.TurnURL, d.TurnUser, d.TurnPass
	}
	s.sigURL, s.turnURL, s.turnU, s.turnP = firstURL(signalingURL), turnURL, turnUser, turnPass
	s.name, s.app = strings.TrimSpace(name), appVersion
}

// ConnectCode joins a one-time support code (asynchronous; watch the listener).
func (s *Session) ConnectCode(code string) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	s.linkWS = ""
	go s.run(code, "", "")
}

// ConnectLink joins from a scanned QR code, a nexdesk://join link or an
// internet code. Returns false (and does nothing) when s is not one of them.
func (s *Session) ConnectLink(link string) bool {
	code, ws := joinlink.Parse(link)
	if code == "" {
		return false
	}
	s.linkWS = ws
	go s.run(code, "", "")
	return true
}

// LinkCode returns the session code inside a QR/link ("" if none).
func LinkCode(link string) string {
	code, _ := joinlink.Parse(link)
	return code
}

// ConnectDevice joins a registered device by id + password.
func (s *Session) ConnectDevice(id, pass string) {
	go s.run(strings.TrimSpace(id), strings.TrimSpace(id), pass)
}

func (s *Session) run(code, deviceID, pass string) {
	defer func() {
		if r := recover(); r != nil {
			s.l.OnClosed(fmt.Sprintf("İç hata: %v", r))
		}
	}()
	sigURL := s.sigURL
	if s.linkWS != "" {
		sigURL = s.linkWS
	}
	if sigURL == "" {
		s.l.OnClosed("Sunucu adresi tanımlı değil (Ayarlar).")
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.ctx, s.cancel, s.code, s.stopped, s.ever = ctx, cancel, code, false, false
	s.mu.Unlock()
	defer cancel()

	s.l.OnStatus("Sunucuya bağlanılıyor…")
	dctx, dcancel := context.WithTimeout(ctx, 15*time.Second)
	c, err := clientsignaling.Dial(dctx, sigURL)
	if err != nil {
		dcancel()
		s.l.OnClosed(errorText(err))
		return
	}
	c.App, c.Name = "android/"+s.app, s.name
	if deviceID != "" {
		err = c.JoinDevice(dctx, deviceID, pass)
	} else {
		err = c.Join(dctx, code)
	}
	dcancel()
	if err != nil {
		_ = c.Close(context.Background())
		s.l.OnClosed(errorText(err))
		return
	}
	s.mu.Lock()
	s.sig = c
	s.mu.Unlock()
	defer c.Close(context.Background())
	if deviceID == "" {
		s.l.OnStatus("Karşı tarafın onayı bekleniyor…")
	} else {
		s.l.OnStatus("Cihaza bağlanılıyor…")
	}

	for {
		msg, err := c.Read(ctx)
		if err != nil {
			if s.isStopped() {
				return
			}
			if s.everConnected() && s.peerAlive() {
				// Media is peer-to-peer; keep going until the peer drops.
				s.l.OnStatus("Sunucu bağlantısı koptu; oturum sürüyor.")
				s.waitPeerEnd(ctx)
			}
			s.l.OnClosed("Bağlantı kapandı.")
			return
		}
		switch msg.Type {
		case "approved":
			if msg.ICE != nil {
				s.mu.Lock()
				s.turn = msg.ICE
				s.mu.Unlock()
			}
			if err := s.negotiate(ctx, c); err != nil {
				s.l.OnClosed("WebRTC başlatılamadı: " + err.Error())
				return
			}
			s.l.OnStatus("Güvenli bağlantı kuruluyor…")
		case "signal":
			if p := s.currentPeer(); p != nil {
				_ = p.AddSignal(webrtcpeer.Signal{Kind: msg.Kind, Payload: msg.Payload})
			}
		case "rejected":
			s.l.OnClosed("Karşı taraf isteği reddetti.")
			return
		case "closed":
			if msg.Reason == "kicked" {
				s.l.OnClosed("Karşı taraf bağlantınızı kesti.")
			} else {
				s.l.OnClosed("Oturum sonlandırıldı.")
			}
			return
		case "error":
			if msg.Reason != "" && !strings.HasPrefix(msg.Reason, "approval_") {
				s.l.OnStatus("Sunucu: " + msg.Reason)
			}
		}
	}
}

func (s *Session) iceServers() []webrtc.ICEServer {
	ice := []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302", "stun:stun1.l.google.com:19302"}}}
	s.mu.Lock()
	t := s.turn
	s.mu.Unlock()
	var urls []string
	user, pass := s.turnU, s.turnP
	if t != nil && len(t.URLs) > 0 {
		urls, user, pass = t.URLs, t.Username, t.Credential
	} else {
		urls = strings.FieldsFunc(s.turnURL, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	}
	if len(urls) > 0 {
		srv := webrtc.ICEServer{URLs: urls}
		if user != "" {
			srv.Username, srv.Credential, srv.CredentialType = user, pass, webrtc.ICECredentialTypePassword
		}
		ice = append(ice, srv)
	}
	return ice
}

// negotiate builds a fresh peer and sends the offer (first connect and
// auto-reconnect both use it).
func (s *Session) negotiate(ctx context.Context, c *clientsignaling.Client) error {
	var se webrtc.SettingEngine
	se.SetSCTPMaxReceiveBufferSize(8 << 20)
	se.SetICEMulticastDNSMode(ice.MulticastDNSModeDisabled) // mDNS is unreliable on phones
	peer, err := webrtcpeer.NewWithSettings(true, webrtc.Configuration{ICEServers: s.iceServers()}, se, func(sctx context.Context, sig webrtcpeer.Signal) error {
		return c.Signal(sctx, sig.Kind, sig.Payload)
	})
	if err != nil {
		return err
	}
	peer.SetScreenHandler(s.onFrame)
	peer.SetControlHandler(s.onControl)
	peer.SetFileHandler(func([]byte) {})
	for _, f := range []func() error{peer.CreateControl, peer.CreateFile, peer.CreateScreen} {
		if err := f(); err != nil {
			_ = peer.Close()
			return err
		}
	}
	offer, err := peer.Offer(ctx)
	if err != nil {
		_ = peer.Close()
		return err
	}
	s.mu.Lock()
	old := s.peer
	s.peer = peer
	s.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	if err := c.Signal(ctx, offer.Kind, offer.Payload); err != nil {
		return err
	}
	go s.watch(ctx, peer)
	return nil
}

const disconnectGrace = 6 * time.Second

func (s *Session) watch(ctx context.Context, peer *webrtcpeer.Peer) {
	t := time.NewTicker(250 * time.Millisecond)
	defer t.Stop()
	announced := false
	var discSince time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if s.currentPeer() != peer {
			return // replaced by a reconnect
		}
		st := peer.ConnectionState()
		if st != webrtc.PeerConnectionStateDisconnected && !discSince.IsZero() {
			discSince = time.Time{}
			if st == webrtc.PeerConnectionStateConnected {
				s.l.OnStatus("Bağlantı toparlandı.")
			}
		}
		switch st {
		case webrtc.PeerConnectionStateConnected:
			if announced {
				continue
			}
			announced = true
			s.mu.Lock()
			s.ever = true
			c, code := s.sig, s.code
			s.mu.Unlock()
			if c != nil {
				c.KeepAlive(ctx, 25*time.Second)
			}
			go func() {
				for i := 0; i < 20; i++ {
					if peer.SendControlText("HELLO:"+s.helloName()) == nil {
						return
					}
					time.Sleep(150 * time.Millisecond)
				}
			}()
			sas, _ := peer.FingerprintSAS(code)
			s.l.OnConnected(peer.PathKind(), sas)
		case webrtc.PeerConnectionStateDisconnected:
			if discSince.IsZero() {
				discSince = time.Now()
				if announced {
					s.l.OnStatus("Bağlantı zayıfladı — toparlanması bekleniyor…")
				}
				continue
			}
			if time.Since(discSince) < disconnectGrace {
				continue
			}
			fallthrough
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			if s.isStopped() {
				return
			}
			if !announced {
				s.l.OnClosed("Güvenli bağlantı kurulamadı (ağ engeli olabilir).")
				s.Disconnect()
				return
			}
			go s.reconnect(ctx)
			return
		}
	}
}

func (s *Session) reconnect(ctx context.Context) {
	s.mu.Lock()
	if s.recon {
		s.mu.Unlock()
		return
	}
	s.recon = true
	c := s.sig
	s.mu.Unlock()
	defer func() { s.mu.Lock(); s.recon = false; s.mu.Unlock() }()
	for attempt := 1; attempt <= 6 && c != nil; attempt++ {
		if s.isStopped() || ctx.Err() != nil {
			return
		}
		s.l.OnStatus(fmt.Sprintf("Yeniden bağlanılıyor… (%d/6)", attempt))
		time.Sleep(time.Duration(attempt) * time.Second)
		if err := s.negotiate(ctx, c); err != nil {
			continue
		}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			if p := s.currentPeer(); p != nil && p.ConnectionState() == webrtc.PeerConnectionStateConnected {
				return
			}
			time.Sleep(300 * time.Millisecond)
		}
	}
	s.l.OnClosed("Yeniden bağlanılamadı.")
	s.Disconnect()
}

func (s *Session) helloName() string {
	if s.name != "" {
		return s.name + " (Android)"
	}
	return "Android"
}

func (s *Session) onFrame(fr screen.Frame) {
	s.statMu.Lock()
	s.frames++
	s.bytes += int64(len(fr.JPEG))
	s.statMu.Unlock()
	f := &Frame{Monitor: int(fr.Monitor), Width: int(fr.Width), Height: int(fr.Height), JPEG: fr.JPEG}
	if m, ok := tiles.Parse(fr.JPEG); ok {
		f.TileSize, f.Cols, f.Count = m.Size, m.Cols, len(m.Tiles)
		f.Tiles = make([]byte, 0, 4*len(m.Tiles))
		for _, t := range m.Tiles {
			f.Tiles = binary.BigEndian.AppendUint16(f.Tiles, uint16(t.X))
			f.Tiles = binary.BigEndian.AppendUint16(f.Tiles, uint16(t.Y))
		}
	}
	s.l.OnFrame(f)
}

func (s *Session) onControl(raw string) {
	switch {
	case strings.HasPrefix(raw, "ROSTER:"):
		var r []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(raw, "ROSTER:")), &r) != nil {
			return
		}
		me := signaling.CleanName(s.name)
		var others []string
		for _, e := range r {
			if e.Name != me {
				others = append(others, e.Name)
			}
		}
		s.l.OnRoster(strings.Join(others, ", "))
	case strings.HasPrefix(raw, "INPUTBY:"):
		who := signaling.CleanName(strings.TrimPrefix(raw, "INPUTBY:"))
		if who == signaling.CleanName(s.name) {
			who = ""
		}
		s.l.OnInputBy(who)
	case strings.HasPrefix(raw, "CHAT:"):
		s.l.OnChat(strings.TrimPrefix(raw, "CHAT:"))
	case strings.HasPrefix(raw, "MONITORS:"):
		var m []json.RawMessage
		if json.Unmarshal([]byte(strings.TrimPrefix(raw, "MONITORS:")), &m) == nil {
			s.l.OnMonitors(len(m))
		}
	case strings.HasPrefix(raw, "REGDEV_OK:"):
		id, pass, _ := strings.Cut(strings.TrimPrefix(raw, "REGDEV_OK:"), "|")
		if id = strings.TrimSpace(id); id != "" && pass != "" {
			s.l.OnDeviceSaved(id, pass)
			s.l.OnStatus("Bilgisayar kaydedildi: " + id)
		}
	case raw == "REGDEV_NO":
		s.l.OnStatus("Karşı taraf kalıcı kaydı onaylamadı.")
	case raw == "PING":
		if p := s.currentPeer(); p != nil {
			_ = p.SendControlText("PONG")
		}
	}
}

func (s *Session) send(ev protocol.InputEvent) {
	if ev.Validate() != nil {
		return
	}
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	if p := s.currentPeer(); p != nil {
		_ = p.SendControlText("INPUT:" + string(b))
	}
}

// Mouse sends move/down/up/wheel; x,y are 0..1 of the remote screen,
// button 0 left / 1 middle / 2 right, delta 120 per wheel notch.
func (s *Session) Mouse(kind string, x, y float64, button, delta int) {
	x, y = clamp01(x), clamp01(y)
	s.send(protocol.InputEvent{T: kind, X: x, Y: y, B: button, D: delta})
}

// Key sends keydown/keyup with a Windows virtual-key code.
func (s *Session) Key(kind string, vk int) { s.send(protocol.InputEvent{T: kind, K: vk}) }

// Tap presses and releases a Windows virtual-key code.
func (s *Session) Tap(vk int) {
	s.Key("keydown", vk)
	s.Key("keyup", vk)
}

// Combo presses modifiers+key together, e.g. Combo("17,18,46") = Ctrl+Alt+Del
// (the remote secure desktop may still refuse that one).
func (s *Session) Combo(vks string) {
	var codes []int
	for _, p := range strings.Split(vks, ",") {
		var v int
		if _, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &v); err == nil {
			codes = append(codes, v)
		}
	}
	for _, v := range codes {
		s.Key("keydown", v)
	}
	for i := len(codes) - 1; i >= 0; i-- {
		s.Key("keyup", codes[i])
	}
}

// Text types Unicode text on the remote computer (layout independent).
func (s *Session) Text(text string) {
	r := []rune(text)
	for len(r) > 0 {
		n := min(len(r), protocol.MaxTextRunes)
		s.send(protocol.InputEvent{T: "text", S: string(r[:n])})
		r = r[n:]
	}
}

// RequestSaveDevice asks the remote user to allow permanent access
// (id + password, no code next time). The answer arrives as OnDeviceSaved or
// a status line. Returns false when the control channel is not ready.
func (s *Session) RequestSaveDevice() bool {
	p := s.currentPeer()
	if p == nil {
		return false
	}
	if p.SendControlText("REGDEV_REQ:"+s.helloName()) != nil {
		return false
	}
	s.l.OnStatus("Karşı taraftan kalıcı kayıt izni isteniyor — onayı bekleyin…")
	return true
}

// Chat sends a chat line to the remote user.
func (s *Session) Chat(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if len(text) > 2000 {
		text = text[:2000]
	}
	if p := s.currentPeer(); p != nil {
		_ = p.SendControlText("CHAT:" + text)
	}
}

// SelectMonitor switches the streamed monitor (shared by everyone in the session).
func (s *Session) SelectMonitor(i int) {
	if p := s.currentPeer(); p != nil {
		_ = p.SendControlText(fmt.Sprintf("MONITOR:%d", i))
	}
}

// RequestKeyframe asks for a full frame (e.g. after the app was paused).
func (s *Session) RequestKeyframe() {
	if p := s.currentPeer(); p != nil {
		_ = p.SendControlText("KEYFRAME")
	}
}

// Disconnect ends this technician's part of the session.
func (s *Session) Disconnect() {
	s.mu.Lock()
	s.stopped = true
	p, c, cancel := s.peer, s.sig, s.cancel
	s.peer, s.sig = nil, nil
	s.mu.Unlock()
	if p != nil {
		_ = p.Close()
	}
	if c != nil {
		_ = c.Close(context.Background())
	}
	if cancel != nil {
		cancel()
	}
}

// Stats is a one-line connection summary for the info panel:
// path, round trip, frame rate and incoming screen data rate.
func (s *Session) Stats() string {
	p := s.currentPeer()
	if p == nil {
		return "Bağlı değil"
	}
	path := map[string]string{"direct": "doğrudan", "relay": "sunucu aktarmalı (TURN)"}[p.PathKind()]
	if path == "" {
		path = "belirleniyor"
	}
	parts := []string{"Yol: " + path}
	if rtt := p.RoundTrip(); rtt > 0 {
		parts = append(parts, fmt.Sprintf("Gecikme: %d ms", rtt.Milliseconds()))
	}
	s.statMu.Lock()
	now := time.Now()
	if !s.statAt.IsZero() {
		if dt := now.Sub(s.statAt).Seconds(); dt > 0.2 {
			fps := float64(s.frames-s.statFrame) / dt
			kbs := float64(s.bytes-s.statBytes) / dt / 1024
			parts = append(parts, fmt.Sprintf("Kare: %.0f/sn", fps), fmt.Sprintf("Veri: %.0f KB/sn", kbs))
		}
	}
	s.statAt, s.statFrame, s.statBytes = now, s.frames, s.bytes
	s.statMu.Unlock()
	return strings.Join(parts, "  •  ")
}

func (s *Session) currentPeer() *webrtcpeer.Peer {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.peer
}

func (s *Session) isStopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Session) everConnected() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ever
}

func (s *Session) peerAlive() bool {
	p := s.currentPeer()
	if p == nil {
		return false
	}
	st := p.ConnectionState()
	return st == webrtc.PeerConnectionStateConnected || st == webrtc.PeerConnectionStateDisconnected
}

func (s *Session) waitPeerEnd(ctx context.Context) {
	for ctx.Err() == nil && s.peerAlive() && !s.isStopped() {
		time.Sleep(500 * time.Millisecond)
	}
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func errorText(err error) string {
	e := err.Error()
	switch {
	case errors.Is(err, clientsignaling.ErrPinMismatch) || strings.Contains(e, "pin ile eşleşmiyor"):
		return "Sunucu kimliği doğrulanamadı (sertifika pini uyuşmuyor). Bağlanmayın, yöneticinize haber verin."
	case strings.Contains(e, "invalid_or_expired_code"):
		return "Kod bulunamadı ya da süresi doldu. Karşı tarafta yeni kod oluşturun. (Cihazsa: çevrimdışı olabilir.)"
	case strings.Contains(e, "device_auth_failed"):
		return "Cihaz kimliği ya da parolası hatalı."
	case strings.Contains(e, "device_busy"):
		return "Cihaz başka bir oturumda (karşı tarafta eski NexDesk sürümü)."
	case strings.Contains(e, "session_full"):
		return "Oturumdaki teknisyen sayısı üst sınırda."
	case strings.Contains(e, "rate_limited"):
		return "Çok fazla hatalı deneme — 1 dakika bekleyin."
	case strings.Contains(e, "client_outdated"):
		return "Uygulama bu sunucu için eski; güncelleyin."
	}
	return "Bağlanılamadı: " + e
}
