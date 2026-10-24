//go:build windows

package main

// Several technicians on one target (protocol v3, "multi"). Each technician
// gets its own WebRTC peer and screen stream; the target keeps a registry so
// the user can see who is connected, who is typing, and drop anyone.
//
// state.peer on the target always points at the most recently active
// technician's peer, so older single-peer code paths (file send, etc.) keep
// working; broadcasts (control messages, chat) go to every technician.

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	clientsignaling "github.com/eirahoutmoss/remotesupport/client/signaling"
	webrtcpeer "github.com/eirahoutmoss/remotesupport/client/webrtc"
	signaling "github.com/eirahoutmoss/remotesupport/server/signaling"
	"github.com/pion/webrtc/v4"
)

const (
	ID_TECHS          = 5400
	ID_TECH_KICK_ALL  = 5401
	ID_TECH_KICK_BASE = 5410 // + index, max techMenuMax
	techMenuMax       = signaling.MaxOperators
)

type techConn struct {
	key   string
	id    string // operator id from the server ("" on a legacy server)
	name  string
	ip    string
	since time.Time
	sig   *clientsignaling.Client

	// guarded by techMu
	peer      *webrtcpeer.Peer
	cancel    context.CancelFunc
	lastInput time.Time
	connected bool
}

var (
	techMu       sync.Mutex
	techs        = map[string]*techConn{}
	techCtlKey   string // key of the technician who last sent input
	techMenuKeys []string
)

func techKey(c *clientsignaling.Client, id string) string { return fmt.Sprintf("%p|%s", c, id) }

func techDisplayName(t *techConn) string {
	n := signaling.CleanName(t.name)
	if n == "" {
		n = "Teknisyen"
	}
	return n
}

func techAdd(c *clientsignaling.Client, id, name, ip string) *techConn {
	techMu.Lock()
	defer techMu.Unlock()
	k := techKey(c, id)
	if t := techs[k]; t != nil {
		if name != "" {
			t.name = name
		}
		return t
	}
	t := &techConn{key: k, id: id, name: name, ip: ip, since: time.Now(), sig: c}
	techs[k] = t
	return t
}

func techGet(c *clientsignaling.Client, id string) *techConn {
	techMu.Lock()
	defer techMu.Unlock()
	return techs[techKey(c, id)]
}

// techSnapshot returns technicians ordered by join time.
func techSnapshot() []*techConn {
	techMu.Lock()
	out := make([]*techConn, 0, len(techs))
	for _, t := range techs {
		out = append(out, t)
	}
	techMu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].since.Before(out[j].since) })
	return out
}

// techCount reports technicians with an established peer.
func techCount() int {
	n := 0
	for _, t := range techSnapshot() {
		techMu.Lock()
		if t.connected {
			n++
		}
		techMu.Unlock()
	}
	return n
}

// techOwns reports whether p is the current peer of some technician.
func techOwns(p *webrtcpeer.Peer) *techConn {
	techMu.Lock()
	defer techMu.Unlock()
	for _, t := range techs {
		if t.peer == p {
			return t
		}
	}
	return nil
}

// techPeers returns the live peers of all technicians.
func techPeers() []*webrtcpeer.Peer {
	techMu.Lock()
	defer techMu.Unlock()
	var out []*webrtcpeer.Peer
	for _, t := range techs {
		if t.peer != nil && t.peer.ConnectionState() == webrtc.PeerConnectionStateConnected {
			out = append(out, t.peer)
		}
	}
	return out
}

// ctlPeers is where control messages go: every technician on the target,
// the single peer on the operator side.
func ctlPeers() []*webrtcpeer.Peer {
	if state.mode == 1 {
		if ps := techPeers(); len(ps) > 0 {
			return ps
		}
	}
	if p := currentPeer(); p != nil {
		return []*webrtcpeer.Peer{p}
	}
	return nil
}

// techBuildPeer (re)creates the WebRTC peer for one technician. A second
// offer from the same technician (auto-reconnect) replaces the old peer.
func techBuildPeer(ctx context.Context, c *clientsignaling.Client, t *techConn) (*webrtcpeer.Peer, error) {
	peer, err := newPeerTo(ctx, c, t.id)
	if err != nil {
		return nil, err
	}
	peer.SetControlHandler(func(msg string) {
		defer logCrash("control")
		if strings.HasPrefix(msg, "INPUT:") {
			techNoteInput(t)
		}
		handleRemoteInput(peer, msg)
	})
	peer.SetFileHandler(func(d []byte) { defer logCrash("file"); handleRemoteFile(d) })

	pctx, cancel := context.WithCancel(ctx)
	techMu.Lock()
	old, oldCancel := t.peer, t.cancel
	t.peer, t.cancel, t.connected = peer, cancel, false
	techMu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if old != nil {
		_ = old.Close()
	}
	state.mu.Lock()
	state.peer = peer
	if state.mode != 1 { // device mode: a session is starting, show it
		state.mode = 1
	}
	state.mu.Unlock()
	startCapture(peer, pctx)
	go watchPeer(peer)
	return peer, nil
}

// techConnected is called by watchPeer when a technician's peer is up.
func techConnected(p *webrtcpeer.Peer) {
	t := techOwns(p)
	if t == nil {
		return
	}
	techMu.Lock()
	first := !t.connected
	t.connected = true
	techMu.Unlock()
	if first {
		auditLog("Teknisyen bağlandı: " + describeRequester(t.name, t.ip))
	}
	techChanged()
}

// techPeerDropped handles a technician's peer failing. It returns true when
// other technicians are still connected (the session as a whole lives on).
func techPeerDropped(p *webrtcpeer.Peer) bool {
	t := techOwns(p)
	if t != nil {
		techMu.Lock()
		t.connected = false
		techMu.Unlock()
		netlogf("Teknisyen bağlantısı koptu: %s", techDisplayName(t))
	}
	techChanged()
	return techCount() > 0
}

// techRemove drops a technician locally (left, kicked or rejected).
func techRemove(c *clientsignaling.Client, id, why string) {
	techMu.Lock()
	k := techKey(c, id)
	t := techs[k]
	delete(techs, k)
	if techCtlKey == k {
		techCtlKey = ""
	}
	var p *webrtcpeer.Peer
	var cancel context.CancelFunc
	if t != nil {
		p, cancel = t.peer, t.cancel
	}
	techMu.Unlock()
	if t == nil {
		return
	}
	if cancel != nil {
		cancel()
	}
	if p != nil {
		_ = p.Close()
	}
	if why != "" {
		auditLog("Teknisyen " + why + ": " + describeRequester(t.name, t.ip))
		netlogf("Teknisyen %s: %s", why, techDisplayName(t))
	}
	techRepointState()
	techChanged()
}

// techDropAll removes every technician that came through signaling client c
// (nil: all of them).
func techDropAll(c *clientsignaling.Client) {
	for _, t := range techSnapshot() {
		if c == nil || t.sig == c {
			techRemove(t.sig, t.id, "")
		}
	}
}

// techRepointState keeps state.peer on a live technician after a removal.
func techRepointState() {
	var best *webrtcpeer.Peer
	techMu.Lock()
	if t := techs[techCtlKey]; t != nil && t.peer != nil {
		best = t.peer
	}
	if best == nil {
		var newest time.Time
		for _, t := range techs {
			if t.peer != nil && t.since.After(newest) {
				best, newest = t.peer, t.since
			}
		}
	}
	techMu.Unlock()
	state.mu.Lock()
	if state.mode == 1 {
		state.peer = best
	}
	state.mu.Unlock()
}

// techNoteInput records who is operating the mouse/keyboard; when the hand
// changes everyone is told, so two people don't type over each other.
func techNoteInput(t *techConn) {
	techMu.Lock()
	t.lastInput = time.Now()
	changed := techCtlKey != t.key
	techCtlKey = t.key
	p := t.peer
	n := len(techs)
	techMu.Unlock()
	if !changed {
		return
	}
	state.mu.Lock()
	state.peer = p
	state.mu.Unlock()
	if n > 1 {
		name := techDisplayName(t)
		setStatus("● Kontrol: " + name + "  •  " + techSummary())
		broadcastCtl("INPUTBY:" + name)
	}
}

// techSummary is a one-line "who is here" text for the status bar.
func techSummary() string {
	var names []string
	for _, t := range techSnapshot() {
		techMu.Lock()
		on := t.connected
		techMu.Unlock()
		if on {
			names = append(names, techDisplayName(t))
		}
	}
	switch len(names) {
	case 0:
		return "bağlı teknisyen yok"
	case 1:
		return "Bağlı: " + names[0]
	}
	return fmt.Sprintf("%d teknisyen bağlı: %s", len(names), strings.Join(names, ", "))
}

type rosterEntry struct {
	Name  string `json:"name"`
	IP    string `json:"ip,omitempty"`
	Since int64  `json:"since"`
	Ctl   bool   `json:"ctl,omitempty"`
}

// techChanged refreshes the target UI and tells every technician who else
// is in the session.
func techChanged() {
	var roster []rosterEntry
	techMu.Lock()
	ctl := techCtlKey
	techMu.Unlock()
	for _, t := range techSnapshot() {
		techMu.Lock()
		on := t.connected
		techMu.Unlock()
		if on {
			roster = append(roster, rosterEntry{Name: techDisplayName(t), IP: t.ip, Since: t.since.Unix(), Ctl: t.key == ctl})
		}
	}
	if b, err := json.Marshal(roster); err == nil {
		broadcastCtl("ROSTER:" + string(b))
	}
	if state.mode == 1 && len(roster) > 0 {
		setStatus("● " + techSummary())
	}
	if state.techsBtn != 0 {
		setText(state.techsBtn, fmt.Sprintf("Teknisyenler (%d) ▾", len(roster)))
	}
	if state.hwnd != 0 {
		postMessage.Call(state.hwnd, WM_APP_RELAYOUT, 0, 0)
	}
}

func broadcastCtl(msg string) {
	for _, p := range ctlPeers() {
		_ = p.SendControlText(msg)
	}
}

// kickTech drops one technician (server-side kick on v3, local close otherwise).
func kickTech(t *techConn) {
	if t == nil {
		return
	}
	if t.sig != nil && t.sig.Multi() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = t.sig.Kick(ctx, t.id)
		cancel()
		techRemove(t.sig, t.id, "çıkarıldı")
		return
	}
	// Legacy single-operator session: ending it is the only way.
	stopSession()
	home()
}

func kickAllTechs() {
	ts := techSnapshot()
	if len(ts) == 0 {
		return
	}
	if len(ts) == 1 || !ts[0].sig.Multi() {
		stopSession()
		home()
		return
	}
	for _, t := range ts {
		kickTech(t)
	}
	setStatus("● Tüm teknisyenlerin bağlantısı kesildi.")
}

// showTechsMenu lists connected technicians with a "Kes" item for each.
func showTechsMenu() {
	menu, _, _ := createPopupMenu.Call()
	if menu == 0 {
		return
	}
	defer destroyMenu.Call(menu)
	add := func(flags, id uintptr, label string) {
		var p uintptr
		if label != "" {
			p = uintptr(unsafe.Pointer(utf16ptr(label)))
		}
		appendMenuW.Call(menu, flags, id, p)
	}
	ts := techSnapshot()
	techMu.Lock()
	ctl := techCtlKey
	techMenuKeys = techMenuKeys[:0]
	techMu.Unlock()
	if len(ts) == 0 {
		add(0x0001, 0, "Bağlı teknisyen yok") // MF_GRAYED
	}
	for i, t := range ts {
		if i >= techMenuMax {
			break
		}
		techMu.Lock()
		on, last := t.connected, t.lastInput
		techMu.Unlock()
		label := fmt.Sprintf("Kes:  %s  (%s, %s)", techDisplayName(t), t.ip, shortDur(time.Since(t.since)))
		if t.key == ctl {
			label += "  ← kontrol"
		} else if !last.IsZero() {
			label += "  son müdahale " + shortDur(time.Since(last)) + " önce"
		}
		if !on {
			label += "  [bağlanıyor]"
		}
		techMu.Lock()
		techMenuKeys = append(techMenuKeys, t.key)
		techMu.Unlock()
		add(0, uintptr(ID_TECH_KICK_BASE+i), label)
	}
	if len(ts) > 1 {
		add(0x0800, 0, "") // MF_SEPARATOR
		add(0, ID_TECH_KICK_ALL, "Tümünün bağlantısını kes")
	}
	var pt point
	getCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	cmd, _, _ := trackPopupMenu.Call(menu, 0x0100, uintptr(pt.X), uintptr(pt.Y), 0, state.hwnd, 0)
	if cmd != 0 {
		onTechsCommand(int(cmd))
	}
}

func onTechsCommand(id int) bool {
	switch {
	case id == ID_TECHS:
		showTechsMenu()
	case id == ID_TECH_KICK_ALL:
		go kickAllTechs()
	case id >= ID_TECH_KICK_BASE && id < ID_TECH_KICK_BASE+techMenuMax:
		techMu.Lock()
		var t *techConn
		if i := id - ID_TECH_KICK_BASE; i < len(techMenuKeys) {
			t = techs[techMenuKeys[i]]
		}
		techMu.Unlock()
		if t != nil {
			go kickTech(t)
		}
	default:
		return false
	}
	return true
}

func shortDur(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d sn", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d dk", int(d.Minutes()))
	}
	return fmt.Sprintf("%d sa %d dk", int(d.Hours()), int(d.Minutes())%60)
}

// ---- operator side: who else is in the session ----

var (
	rosterMu   sync.Mutex
	rosterText string
)

// handleRosterMsg consumes ROSTER:/INPUTBY: on the operator side.
func handleRosterMsg(raw string) bool {
	switch {
	case strings.HasPrefix(raw, "ROSTER:"):
		var r []rosterEntry
		if json.Unmarshal([]byte(strings.TrimPrefix(raw, "ROSTER:")), &r) != nil {
			return true
		}
		me := signaling.CleanName(operatorName())
		var others []string
		for _, e := range r {
			if e.Name != me {
				others = append(others, e.Name)
			}
		}
		txt := ""
		if len(others) > 0 {
			txt = "Oturumda ayrıca: " + strings.Join(others, ", ")
		}
		rosterMu.Lock()
		changed := txt != rosterText
		rosterText = txt
		rosterMu.Unlock()
		if changed && txt != "" {
			setStatus("● " + txt)
		}
		return true
	case strings.HasPrefix(raw, "INPUTBY:"):
		who := signaling.CleanName(strings.TrimPrefix(raw, "INPUTBY:"))
		if who != "" && who != signaling.CleanName(operatorName()) {
			setStatus("● Fare/klavye şu an " + who + " tarafından kullanılıyor")
		}
		return true
	}
	return false
}
