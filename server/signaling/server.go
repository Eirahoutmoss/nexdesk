package signaling

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	defaultSessionTTL  = 5 * time.Minute
	defaultMaxSessions = 1024
	defaultWriteTTL    = 5 * time.Second
	defaultCleanupTick = 1 * time.Second
	defaultRateWindow  = 1 * time.Minute
	defaultJoinFails   = 8
	defaultCreates     = 20 // code creations per IP per rate window
	defaultTURNTTL     = 12 * time.Hour
	maxMessageBytes    = 256 << 10
	maxNameRunes       = 64
)

// ProtocolVersion is the signaling protocol spoken by this package. Clients
// send their own version in "v"; a server that predates versioning simply
// ignores the field, and a client that predates it sends 0.
//
//	v2: version/feature negotiation, operator identity in peer_joined,
//	    per-session TURN credentials ("turncred").
//	v3: several operators per session ("multi"): target-bound messages carry
//	    the operator's id in "peer"; peer_left/kick; server keepalive pings.
const ProtocolVersion = 3

// FeatureMulti means a v3 target may serve several operators at once.
const FeatureMulti = "multi"

// MaxOperators caps operators per session (abuse guard, not a licence limit).
const MaxOperators = 32

const (
	pingInterval = 30 * time.Second
	pingTimeout  = 15 * time.Second
)

// FeatureTURNCred means the server hands out short-lived TURN credentials
// after approval: an "ice" message to the target, and ICE in "approved".
const FeatureTURNCred = "turncred"

type Role string

const (
	RoleTarget   Role = "target"
	RoleOperator Role = "operator"
)

type Message struct {
	Type      string          `json:"type"`
	Code      string          `json:"code,omitempty"`
	Role      Role            `json:"role,omitempty"`
	Approved  bool            `json:"approved,omitempty"`
	Kind      string          `json:"kind,omitempty"` // offer, answer, candidate
	Payload   json.RawMessage `json:"payload,omitempty"`
	ExpiresIn int             `json:"expires_in,omitempty"`
	Reason    string          `json:"reason,omitempty"`

	// v2 fields — ignored by older peers.
	V        int       `json:"v,omitempty"`        // protocol version of the sender
	App      string    `json:"app,omitempty"`      // sender application version, for logs
	Name     string    `json:"name,omitempty"`     // operator's self-declared name (join, peer_joined)
	From     string    `json:"from,omitempty"`     // operator IP as seen by the server (peer_joined)
	Features []string  `json:"features,omitempty"` // server capabilities (created, joined)
	ICE      *ICECreds `json:"ice,omitempty"`      // TURN credentials (ice, approved)

	// Version advisories (created, joined). Informational only — the server
	// never rejects by app version (that is MinClientVersion, by protocol v).
	MinApp    string `json:"min_app,omitempty"`    // oldest app version still supported
	LatestApp string `json:"latest_app,omitempty"` // newest app version available

	// Device access (attended technician support by stable device id + password).
	DeviceID string `json:"device_id,omitempty"` // create: register/announce; join: target device
	Pass     string `json:"pass,omitempty"`      // device password (device create, operator join)
	Owner    string `json:"owner,omitempty"`     // device create: the device's private owner secret

	// v3: operator id on every target-bound/target-sent per-operator message
	// (peer_joined, ice, signal, approve, peer_left, kick).
	Peer string `json:"peer,omitempty"`
}

// ICECreds is a short-lived TURN credential in coturn "use-auth-secret"
// (TURN REST API) form: username "<expiry-unix>:<id>", password
// base64(HMAC-SHA1(secret, username)).
type ICECreds struct {
	URLs       []string `json:"urls"`
	Username   string   `json:"username"`
	Credential string   `json:"credential"`
	ExpiresAt  int64    `json:"expires_at"`
}

// Server is a small rendezvous/signaling service. It never carries session
// data. After approval it only forwards SDP/ICE signaling messages.
type Server struct {
	mu          sync.Mutex
	sessions    map[string]*session
	maxSessions int
	ttl         time.Duration
	writeTTL    time.Duration
	cleanupStop chan struct{}
	cleanupOnce sync.Once
	limits      map[string]*rateLimit
	creates     map[string]*rateLimit
	minClientV  int
	minApp      string
	latestApp   string
	turnURLs    []string
	turnSecret  []byte
	turnTTL     time.Duration
	logf        func(format string, a ...any)

	devMu      sync.Mutex
	devices    map[string]devReg // "DEV-<ID>" -> password record
	deviceFile string            // optional JSON persistence path
}

// devReg is one device's salted password hash.
type devReg struct {
	Salt []byte `json:"salt"`
	Hash []byte `json:"hash"`
	// Owner is the salted hash of a secret only the device itself holds
	// (generated on first registration). It lets the device change its own
	// password; knowing the device id is not enough to take it over.
	Owner []byte `json:"owner,omitempty"`
}

type session struct {
	code       string
	createdAt  time.Time
	target     *peer
	ops        map[string]*opPeer // operators by id
	opOrder    []string           // join order (legacy single-operator lookups)
	used       bool               // legacy: code consumed at first join
	approved   bool               // at least one operator was approved
	persistent bool               // device session: never TTL-expires, re-created by the device
}

// multi reports whether the target speaks v3 and can serve several operators.
func (sess *session) multi() bool { return sess.target != nil && sess.target.v >= 3 }

// op finds an operator: by id for v3 targets, the only one for legacy targets.
func (sess *session) op(id string) *opPeer {
	if o := sess.ops[id]; o != nil {
		return o
	}
	if (!sess.multi() || id == "") && len(sess.opOrder) == 1 {
		return sess.ops[sess.opOrder[0]] // legacy target, or a v3 target omitting the id with one operator
	}
	return nil
}

func (sess *session) removeOp(id string) *opPeer {
	o := sess.ops[id]
	delete(sess.ops, id)
	for i, x := range sess.opOrder {
		if x == id {
			sess.opOrder = append(sess.opOrder[:i], sess.opOrder[i+1:]...)
			break
		}
	}
	return o
}

type peer struct {
	conn *websocket.Conn
	role Role
	v    int
}

type opPeer struct {
	peer
	id       string
	approved bool
}

type rateLimit struct {
	windowStart time.Time
	failures    int
}

func NewServer() *Server {
	s := &Server{
		sessions:    make(map[string]*session),
		maxSessions: defaultMaxSessions,
		ttl:         defaultSessionTTL,
		writeTTL:    defaultWriteTTL,
		limits:      make(map[string]*rateLimit),
		creates:     make(map[string]*rateLimit),
		devices:     make(map[string]devReg),
		cleanupStop: make(chan struct{}),
		turnTTL:     defaultTURNTTL,
		logf:        func(string, ...any) {},
	}
	go s.cleanupLoop()
	return s
}

func (s *Server) SetSessionTTL(ttl time.Duration) {
	if ttl > 0 {
		s.mu.Lock()
		s.ttl = ttl
		s.mu.Unlock()
	}
}

func (s *Server) SetMaxSessions(n int) {
	if n > 0 {
		s.mu.Lock()
		s.maxSessions = n
		s.mu.Unlock()
	}
}

// SetMinClientVersion rejects clients whose protocol version is below n with
// the error reason "client_outdated". 0 (default) accepts every client.
func (s *Server) SetMinClientVersion(n int) {
	s.mu.Lock()
	s.minClientV = n
	s.mu.Unlock()
}

// SetAppVersions advertises version advisories to clients: min is the oldest
// app version still supported, latest the newest available. Both are purely
// informational (the client decides what to show); empty values advertise
// nothing. This lets clients nudge users to update before MinClientVersion is
// ever raised to actually reject them.
func (s *Server) SetAppVersions(min, latest string) {
	s.mu.Lock()
	s.minApp, s.latestApp = strings.TrimSpace(min), strings.TrimSpace(latest)
	s.mu.Unlock()
}

// appVers returns the configured advisories under lock.
func (s *Server) appVers() (min, latest string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.minApp, s.latestApp
}

// SetTURN enables per-session TURN credentials. secret must match coturn's
// static-auth-secret; ttl <= 0 keeps the default.
func (s *Server) SetTURN(urls []string, secret string, ttl time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.turnURLs = nil
	for _, u := range urls {
		if u = strings.TrimSpace(u); u != "" {
			s.turnURLs = append(s.turnURLs, u)
		}
	}
	s.turnSecret = []byte(secret)
	if ttl > 0 {
		s.turnTTL = ttl
	}
}

// SetLogger receives one line per notable event (create, join, reject…).
func (s *Server) SetLogger(f func(format string, a ...any)) {
	if f != nil {
		s.mu.Lock()
		s.logf = f
		s.mu.Unlock()
	}
}

func (s *Server) features() []string {
	f := []string{FeatureMulti}
	if len(s.turnSecret) > 0 && len(s.turnURLs) > 0 {
		f = append(f, FeatureTURNCred)
	}
	return f
}

// turnCreds issues a credential; nil when TURN is not configured.
func (s *Server) turnCreds() *ICECreds {
	s.mu.Lock()
	urls, secret, ttl := append([]string(nil), s.turnURLs...), s.turnSecret, s.turnTTL
	s.mu.Unlock()
	if len(secret) == 0 || len(urls) == 0 {
		return nil
	}
	return MakeTURNCreds(urls, secret, ttl, time.Now())
}

// MakeTURNCreds builds a coturn use-auth-secret credential valid until now+ttl.
func MakeTURNCreds(urls []string, secret []byte, ttl time.Duration, now time.Time) *ICECreds {
	id := make([]byte, 4)
	_, _ = rand.Read(id)
	exp := now.Add(ttl).Unix()
	user := strconv.FormatInt(exp, 10) + ":nd" + hex.EncodeToString(id)
	m := hmac.New(sha1.New, secret)
	m.Write([]byte(user))
	return &ICECreds{URLs: urls, Username: user, Credential: base64.StdEncoding.EncodeToString(m.Sum(nil)), ExpiresAt: exp}
}

// CleanName makes a self-declared display name safe to show in a dialog:
// no control characters (no injected line breaks), at most 64 runes.
func CleanName(n string) string {
	var b strings.Builder
	count := 0
	for _, r := range strings.TrimSpace(n) {
		if unicode.IsControl(r) || unicode.Is(unicode.Bidi_Control, r) {
			r = ' '
		}
		if count >= maxNameRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return strings.TrimSpace(b.String())
}

// Close stops background cleanup. It is intended for tests and graceful
// shutdown of an embedding application.
func (s *Server) Close() {
	s.cleanupOnce.Do(func() { close(s.cleanupStop) })
	s.mu.Lock()
	peers := make([]*peer, 0, len(s.sessions)*2)
	for code, sess := range s.sessions {
		delete(s.sessions, code)
		if sess.target != nil {
			peers = append(peers, sess.target)
		}
		for _, o := range sess.ops {
			peers = append(peers, &o.peer)
		}
	}
	s.mu.Unlock()
	for _, p := range peers {
		_ = p.conn.Close(websocket.StatusNormalClosure, "server shutdown")
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", s.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	return mux
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	// Non-trickle SDP carries every candidate; the 32 KiB default can be hit
	// on machines with many adapters, which closes the socket mid-handshake.
	conn.SetReadLimit(maxMessageBytes)

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var first Message
	if err := wsjson.Read(ctx, conn, &first); err != nil {
		return
	}

	ip := remoteIP(r.RemoteAddr)
	if first.V >= 3 {
		// v3 clients keep reading while dialogs are open, so a missing pong
		// really means a dead link (sleeping laptop, NAT drop). Older
		// clients block in the approval dialog and would be dropped falsely.
		pctx, pcancel := context.WithCancel(r.Context())
		defer pcancel()
		go pingLoop(pctx, conn)
	}
	s.mu.Lock()
	minV, logf := s.minClientV, s.logf
	s.mu.Unlock()
	if (first.Type == "create" || first.Type == "join") && first.V < minV {
		logf("reddedildi: eski istemci ip=%s v=%d app=%q", ip, first.V, first.App)
		_ = s.write(conn, Message{Type: "error", Reason: "client_outdated", V: ProtocolVersion})
		return
	}
	switch first.Type {
	case "create":
		if s.createRateLimited(ip) {
			logf("create sınırı: ip=%s", ip)
			_ = s.write(conn, Message{Type: "error", Reason: "rate_limited"})
			return
		}
		s.handleCreate(r.Context(), conn, first, ip)
	case "join":
		s.handleJoin(r.Context(), conn, strings.ToUpper(strings.TrimSpace(first.Code)), ip, first)
	default:
		_ = s.write(conn, Message{Type: "error", Reason: "expected create or join"})
	}
}

func (s *Server) handleCreate(ctx context.Context, conn *websocket.Conn, first Message, ip string) {
	if first.DeviceID != "" {
		s.handleDeviceCreate(ctx, conn, first, ip)
		return
	}
	s.mu.Lock()
	if len(s.sessions) >= s.maxSessions {
		s.mu.Unlock()
		_ = s.write(conn, Message{Type: "error", Reason: "server_capacity"})
		return
	}
	var code string
	for {
		code = newCode()
		if _, ok := s.sessions[code]; !ok {
			break
		}
	}
	ttl := s.ttl
	sess := &session{code: code, createdAt: time.Now(), target: &peer{conn: conn, role: RoleTarget, v: first.V}}
	s.sessions[code] = sess
	feats, logf := s.features(), s.logf
	s.mu.Unlock()
	logf("kod oluşturuldu: ip=%s v=%d app=%q", ip, first.V, first.App)

	minApp, latestApp := s.appVers()
	if err := s.write(conn, Message{Type: "created", Code: code, Role: RoleTarget, ExpiresIn: int(ttl.Seconds()), V: ProtocolVersion, Features: feats, MinApp: minApp, LatestApp: latestApp}); err != nil {
		s.removeOwned(code, conn)
		return
	}

	s.targetReadLoop(ctx, conn, code)
}

// targetReadLoop handles the target side of a session (code or device) after it
// is created: approvals and signal/close forwarding.
func (s *Server) targetReadLoop(ctx context.Context, conn *websocket.Conn, code string) {
	for {
		var msg Message
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			s.removeOwned(code, conn)
			return
		}
		switch msg.Type {
		case "approve":
			s.handleApprove(conn, code, msg.Peer, msg.Approved, msg.Reason)
		case "signal", "close":
			s.forward(code, RoleTarget, "", msg)
		case "kick":
			s.kick(code, msg.Peer)
		default:
			_ = s.write(conn, Message{Type: "error", Reason: "invalid_target_message"})
		}
	}
}

func deviceKey(id string) string { return "DEV-" + strings.ToUpper(strings.TrimSpace(id)) }

// handleDeviceCreate registers (first contact) or authenticates a device by
// password, then keeps a persistent, non-expiring session the device re-creates
// after each support session, so a technician can connect by device id +
// password whenever the device is online.
func (s *Server) handleDeviceCreate(ctx context.Context, conn *websocket.Conn, first Message, ip string) {
	key := deviceKey(first.DeviceID)
	if err := s.authDeviceOwner(key, first.Pass, first.Owner); err != nil {
		s.logf("cihaz kaydı reddedildi: %s ip=%s (%v)", key, ip, err)
		_ = s.write(conn, Message{Type: "error", Reason: err.Error()})
		return
	}
	s.mu.Lock()
	if old := s.sessions[key]; old != nil { // device reconnected: drop the stale one
		delete(s.sessions, key)
		go s.closePeers(old)
	}
	sess := &session{code: key, createdAt: time.Now(), target: &peer{conn: conn, role: RoleTarget, v: first.V}, persistent: true}
	s.sessions[key] = sess
	feats, logf := s.features(), s.logf
	s.mu.Unlock()
	logf("cihaz çevrimiçi: %s ip=%s v=%d", key, ip, first.V)

	minApp, latestApp := s.appVers()
	if err := s.write(conn, Message{Type: "created", Code: first.DeviceID, Role: RoleTarget, V: ProtocolVersion, Features: feats, MinApp: minApp, LatestApp: latestApp}); err != nil {
		s.removeOwned(key, conn)
		return
	}
	s.targetReadLoop(ctx, conn, key)
}

// authDevice registers a new device password (register=true) or verifies an
// existing one. A known device always requires the correct password.
func (s *Server) authDevice(key, pass string, register bool) error {
	if register {
		return s.authDeviceOwner(key, pass, "")
	}
	s.devMu.Lock()
	defer s.devMu.Unlock()
	reg, ok := s.devices[key]
	if !ok {
		return errors.New("unknown_device")
	}
	if subtle.ConstantTimeCompare(reg.Hash, hashPass(reg.Salt, pass)) != 1 {
		return errors.New("device_auth_failed")
	}
	return nil
}

// authDeviceOwner registers or re-announces a device. A known device must
// present its password — or, to change the password, the owner secret it
// registered with. A device that registered before owner secrets existed
// adopts one the first time it presents the right password.
func (s *Server) authDeviceOwner(key, pass, owner string) error {
	s.devMu.Lock()
	defer s.devMu.Unlock()
	if len(pass) < 4 {
		return errors.New("weak_or_missing_password")
	}
	reg, ok := s.devices[key]
	if !ok {
		salt := make([]byte, 16)
		if _, err := rand.Read(salt); err != nil {
			return errors.New("server_error")
		}
		reg = devReg{Salt: salt, Hash: hashPass(salt, pass)}
		if owner != "" {
			reg.Owner = hashPass(salt, "owner|"+owner)
		}
		s.devices[key] = reg
		s.saveDevicesLocked()
		return nil
	}
	passOK := subtle.ConstantTimeCompare(reg.Hash, hashPass(reg.Salt, pass)) == 1
	ownerOK := owner != "" && reg.Owner != nil &&
		subtle.ConstantTimeCompare(reg.Owner, hashPass(reg.Salt, "owner|"+owner)) == 1
	switch {
	case passOK && owner != "" && reg.Owner == nil:
		reg.Owner = hashPass(reg.Salt, "owner|"+owner) // adopt
	case passOK:
		return nil
	case ownerOK:
		reg.Hash = hashPass(reg.Salt, pass) // the device changed its password
		s.logf("cihaz parolası değişti (sahip anahtarıyla): %s", key)
	default:
		return errors.New("device_auth_failed")
	}
	s.devices[key] = reg
	s.saveDevicesLocked()
	return nil
}

func hashPass(salt []byte, pass string) []byte {
	h := sha256.New()
	h.Write(salt)
	h.Write([]byte("nexdesk-dev/v1|"))
	h.Write([]byte(pass))
	return h.Sum(nil)
}

// SetDeviceStore enables JSON persistence of the device registry and loads it.
func (s *Server) SetDeviceStore(path string) {
	s.devMu.Lock()
	defer s.devMu.Unlock()
	s.deviceFile = path
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	m := map[string]devReg{}
	if json.Unmarshal(b, &m) == nil {
		s.devices = m
	}
}

func (s *Server) saveDevicesLocked() {
	if s.deviceFile == "" {
		return
	}
	if b, err := json.Marshal(s.devices); err == nil {
		tmp := s.deviceFile + ".tmp"
		if os.WriteFile(tmp, b, 0600) == nil {
			_ = os.Rename(tmp, s.deviceFile)
		}
	}
}

func (s *Server) handleJoin(ctx context.Context, conn *websocket.Conn, code, ip string, first Message) {
	device := first.DeviceID != ""
	if device {
		code = deviceKey(first.DeviceID)
	}
	if code == "" {
		_ = s.write(conn, Message{Type: "error", Reason: "invalid_code"})
		return
	}
	if s.joinRateLimited(ip) {
		_ = s.write(conn, Message{Type: "error", Reason: "rate_limited"})
		return
	}
	if device {
		// Verify the device password before revealing whether it is online; a
		// bad password counts as a join failure for the rate limiter.
		if err := s.authDevice(code, first.Pass, false); err != nil {
			s.recordJoinFailure(ip, time.Now())
			s.logf("cihaz parolası reddedildi: %s ip=%s", code, ip)
			_ = s.write(conn, Message{Type: "error", Reason: "device_auth_failed"})
			return
		}
	}

	s.mu.Lock()
	sess := s.sessions[code]
	now := time.Now()
	logf := s.logf
	reason := ""
	switch {
	case sess == nil || sess.target == nil:
		reason = "invalid_or_expired_code"
	case !sess.persistent && !sess.approved && len(sess.ops) == 0 && now.Sub(sess.createdAt) > s.ttl:
		reason = "invalid_or_expired_code"
	case !sess.multi() && (sess.used || len(sess.ops) > 0):
		// Legacy target: one operator per session; a code is single-use.
		if sess.persistent {
			reason = "device_busy"
		} else {
			reason = "invalid_or_expired_code"
		}
	case len(sess.ops) >= MaxOperators:
		reason = "session_full"
	}
	if reason != "" {
		s.mu.Unlock()
		if reason == "invalid_or_expired_code" {
			s.recordJoinFailure(ip, now)
			logf("geçersiz kod denemesi: ip=%s", ip)
		}
		_ = s.write(conn, Message{Type: "error", Reason: reason})
		return
	}
	id := newPeerID()
	o := &opPeer{peer: peer{conn: conn, role: RoleOperator, v: first.V}, id: id}
	if sess.ops == nil {
		sess.ops = map[string]*opPeer{}
	}
	sess.ops[id] = o
	sess.opOrder = append(sess.opOrder, id)
	sess.used = true
	multi := sess.multi()
	target := sess.target.conn
	feats := s.features()
	s.mu.Unlock()
	name := CleanName(first.Name)
	logf("koda katılındı: ip=%s v=%d app=%q ad=%q teknisyen=%s", ip, first.V, first.App, name, id)

	minApp, latestApp := s.appVers()
	if err := s.write(conn, Message{Type: "joined", Code: code, Role: RoleOperator, V: ProtocolVersion, Features: feats, MinApp: minApp, LatestApp: latestApp, Peer: id}); err != nil {
		s.operatorGone(code, id, conn)
		return
	}
	pj := Message{Type: "peer_joined", Code: code, Name: name, From: ip, V: first.V}
	if multi {
		pj.Peer = id
	}
	if err := s.write(target, pj); err != nil {
		s.operatorGone(code, id, conn)
		return
	}

	for {
		var msg Message
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			s.operatorGone(code, id, conn)
			return
		}
		switch msg.Type {
		case "signal":
			s.forward(code, RoleOperator, id, msg)
		case "close":
			if multi {
				s.operatorGone(code, id, conn) // v3: leaving ends only this operator
				return
			}
			s.forward(code, RoleOperator, id, msg)
		default:
			_ = s.write(conn, Message{Type: "error", Reason: "invalid_operator_message"})
		}
	}
}

// operatorGone removes one operator. For a v3 target the session lives on
// and the target is told (peer_left); a legacy session ends as before.
func (s *Server) operatorGone(code, id string, conn *websocket.Conn) {
	s.mu.Lock()
	sess := s.sessions[code]
	if sess == nil || sess.ops[id] == nil || sess.ops[id].conn != conn {
		s.mu.Unlock()
		_ = conn.Close(websocket.StatusNormalClosure, "")
		return
	}
	if !sess.multi() {
		s.mu.Unlock()
		s.removeOwned(code, conn)
		return
	}
	sess.removeOp(id)
	target := sess.target.conn
	s.mu.Unlock()
	_ = conn.Close(websocket.StatusNormalClosure, "")
	_ = s.write(target, Message{Type: "peer_left", Peer: id})
	s.logf("teknisyen ayrıldı: %s teknisyen=%s", code, id)
}

// kick lets a v3 target drop one operator.
func (s *Server) kick(code, id string) {
	s.mu.Lock()
	sess := s.sessions[code]
	var o *opPeer
	if sess != nil && sess.multi() {
		o = sess.removeOp(id)
	}
	s.mu.Unlock()
	if o == nil {
		return
	}
	_ = s.write(o.conn, Message{Type: "closed", Reason: "kicked"})
	go o.conn.Close(websocket.StatusNormalClosure, "kicked")
	s.logf("teknisyen çıkarıldı: %s teknisyen=%s", code, id)
}

func (s *Server) handleApprove(conn *websocket.Conn, code, id string, ok bool, reason string) {
	s.mu.Lock()
	sess := s.sessions[code]
	var o *opPeer
	if sess != nil {
		o = sess.op(id)
	}
	if o == nil {
		s.mu.Unlock()
		_ = s.write(conn, Message{Type: "error", Reason: "approval_not_available"})
		return
	}
	if o.approved {
		s.mu.Unlock()
		_ = s.write(conn, Message{Type: "error", Reason: "approval_already_decided"})
		return
	}
	multi := sess.multi()
	if !ok {
		target := sess.target.conn
		if multi {
			sess.removeOp(o.id)
		} else {
			delete(s.sessions, code)
		}
		s.mu.Unlock()
		_ = s.write(o.conn, Message{Type: "rejected", Reason: reason})
		go o.conn.Close(websocket.StatusPolicyViolation, "rejected") // never block the target's loop on a close handshake
		if !multi {
			_ = target.Close(websocket.StatusPolicyViolation, "rejected")
		}
		return
	}
	o.approved = true
	sess.approved = true
	opV, tgV := o.v, sess.target.v
	target := sess.target.conn
	s.mu.Unlock()
	var creds *ICECreds
	if opV >= 2 || tgV >= 2 {
		creds = s.turnCreds()
	}
	// The target learns its credential before the operator is told to send
	// an offer, so the "ice" message always precedes the first signal.
	if creds != nil && tgV >= 2 {
		im := Message{Type: "ice", ICE: creds}
		if multi {
			im.Peer = o.id
		}
		_ = s.write(target, im)
	}
	approved := Message{Type: "approved"}
	if opV >= 2 {
		approved.ICE = creds
	}
	_ = s.write(o.conn, approved)
}

// forward relays signal/close between the target and one operator (opID:
// sender for operator messages; msg.Peer selects the receiver for target
// messages, legacy targets have a single operator).
func (s *Server) forward(code string, from Role, opID string, msg Message) {
	s.mu.Lock()
	sess := s.sessions[code]
	if sess == nil {
		s.mu.Unlock()
		return
	}
	if from == RoleTarget && msg.Type == "close" {
		delete(s.sessions, code)
		var dsts []*websocket.Conn
		for _, o := range sess.ops {
			if o.approved {
				dsts = append(dsts, o.conn)
			}
		}
		s.mu.Unlock()
		for _, d := range dsts {
			_ = s.write(d, msg)
		}
		s.closePeers(sess)
		return
	}
	var o *opPeer
	if from == RoleTarget {
		o = sess.op(msg.Peer)
	} else {
		o = sess.ops[opID]
	}
	if o == nil {
		s.mu.Unlock()
		return
	}
	if !o.approved {
		src := sess.target.conn
		if from == RoleOperator {
			src = o.conn
		}
		s.mu.Unlock()
		_ = s.write(src, Message{Type: "error", Reason: "approval_required"})
		return
	}
	var dst *websocket.Conn
	closeSession := false
	if from == RoleTarget {
		dst = o.conn
		msg.Peer = ""
	} else {
		dst = sess.target.conn
		if sess.multi() {
			msg.Peer = o.id
		}
		if msg.Type == "close" { // legacy operator close ends the session
			closeSession = true
			delete(s.sessions, code)
		}
	}
	s.mu.Unlock()

	if dst != nil {
		_ = s.write(dst, msg)
	}
	if closeSession {
		s.closePeers(sess)
	}
}

// removeOwned removes the session only while conn still belongs to it. A
// re-registered device replaces its session under the same key; when the old
// socket's read loop then fails it must not tear down the new session.
func (s *Server) removeOwned(code string, conn *websocket.Conn) {
	s.mu.Lock()
	sess := s.sessions[code]
	if sess == nil || !sess.owns(conn) {
		s.mu.Unlock()
		return
	}
	delete(s.sessions, code)
	s.mu.Unlock()
	s.closePeers(sess)
}

func (sess *session) owns(conn *websocket.Conn) bool {
	if sess.target != nil && sess.target.conn == conn {
		return true
	}
	for _, o := range sess.ops {
		if o.conn == conn {
			return true
		}
	}
	return false
}

func (s *Server) remove(code string) {
	s.mu.Lock()
	sess := s.sessions[code]
	delete(s.sessions, code)
	s.mu.Unlock()
	if sess != nil {
		s.closePeers(sess)
	}
}

// closePeers closes every socket of a session that is no longer in the map
// (callers delete it first, so sess.ops is not mutated concurrently).
func (s *Server) closePeers(sess *session) {
	s.mu.Lock()
	conns := make([]*websocket.Conn, 0, 1+len(sess.ops))
	if sess.target != nil && sess.target.conn != nil {
		conns = append(conns, sess.target.conn)
	}
	for _, o := range sess.ops {
		if o.conn != nil {
			conns = append(conns, o.conn)
		}
	}
	s.mu.Unlock()
	for _, c := range conns {
		go c.Close(websocket.StatusNormalClosure, "session closed") // close handshakes must not stall callers
	}
}

// pingLoop drops a connection that stops answering pings.
func pingLoop(ctx context.Context, conn *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	misses := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := conn.Ping(pctx)
			cancel()
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				misses++
				if misses >= 2 {
					_ = conn.Close(websocket.StatusGoingAway, "ping timeout")
					return
				}
				continue
			}
			misses = 0
		}
	}
}

func newPeerID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Server) write(conn *websocket.Conn, msg Message) error {
	if conn == nil {
		return errors.New("signaling: nil connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), s.writeTTL)
	defer cancel()
	return wsjson.Write(ctx, conn, msg)
}

func (s *Server) cleanupLoop() {
	t := time.NewTicker(defaultCleanupTick)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			s.cleanupExpired()
		case <-s.cleanupStop:
			return
		}
	}
}

func (s *Server) cleanupExpired() {
	now := time.Now()
	var expired []*session
	s.mu.Lock()
	for code, sess := range s.sessions {
		if !sess.persistent && !sess.approved && len(sess.ops) == 0 && now.Sub(sess.createdAt) > s.ttl {
			delete(s.sessions, code)
			expired = append(expired, sess)
		}
	}
	// Keep the rate-limit maps bounded.
	for _, m := range []map[string]*rateLimit{s.limits, s.creates} {
		for ip, limit := range m {
			if now.Sub(limit.windowStart) >= defaultRateWindow {
				delete(m, ip)
			}
		}
	}
	s.mu.Unlock()
	for _, sess := range expired {
		s.closePeers(sess)
	}
}

func (s *Server) joinRateLimited(ip string) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := s.limits[ip]
	if limit == nil || now.Sub(limit.windowStart) >= defaultRateWindow {
		s.limits[ip] = &rateLimit{windowStart: now}
		return false
	}
	return limit.failures >= defaultJoinFails
}

// createRateLimited counts this create and reports whether the IP is over the
// per-minute limit. Loopback (the embedded LAN server's own UI) is exempt.
func (s *Server) createRateLimited(ip string) bool {
	if p := net.ParseIP(ip); p != nil && p.IsLoopback() {
		return false
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := s.creates[ip]
	if limit == nil || now.Sub(limit.windowStart) >= defaultRateWindow {
		s.creates[ip] = &rateLimit{windowStart: now, failures: 1}
		return false
	}
	limit.failures++
	return limit.failures > defaultCreates
}

func (s *Server) recordJoinFailure(ip string, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := s.limits[ip]
	if limit == nil || now.Sub(limit.windowStart) >= defaultRateWindow {
		s.limits[ip] = &rateLimit{windowStart: now, failures: 1}
		return
	}
	limit.failures++
}

func remoteIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err == nil && host != "" {
		return host
	}
	return addr
}

func newCode() string {
	// 10-digit code (10 billion combinations) — 6 digits was too weak for
	// use over the internet. Single-use + short TTL + explicit consent still apply.
	n, err := rand.Int(rand.Reader, big.NewInt(10_000_000_000))
	if err != nil {
		panic(fmt.Sprintf("signaling: crypto/rand failed: %v", err))
	}
	return fmt.Sprintf("%010d", n.Int64())
}

var ErrNotApproved = errors.New("signaling: session not approved")
