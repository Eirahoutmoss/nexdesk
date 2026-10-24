package signaling

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/coder/websocket"
)

// joinAndApprove joins op to code, lets the target approve it, and returns
// the operator id the target saw.
func joinAndApprove(t *testing.T, target, op *websocket.Conn, code, name string) string {
	writeMessage(t, op, Message{Type: "join", Code: code, V: ProtocolVersion, Name: name})
	if got := readMessage(t, op); got.Type != "joined" {
		t.Fatalf("%s join: %+v", name, got)
	}
	pj := readMessage(t, target)
	if pj.Type != "peer_joined" || pj.Peer == "" || pj.Name != name {
		t.Fatalf("%s peer_joined: %+v", name, pj)
	}
	writeMessage(t, target, Message{Type: "approve", Approved: true, Peer: pj.Peer})
	if got := readMessage(t, op); got.Type != "approved" {
		t.Fatalf("%s approved: %+v", name, got)
	}
	return pj.Peer
}

func TestMultipleOperatorsRoutedByPeer(t *testing.T) {
	s := NewServer()
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	target := dialServer(t, ts)
	defer target.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, target, Message{Type: "create", V: ProtocolVersion})
	code := readMessage(t, target).Code

	a := dialServer(t, ts)
	defer a.Close(websocket.StatusNormalClosure, "")
	b := dialServer(t, ts)
	defer b.Close(websocket.StatusNormalClosure, "")
	idA := joinAndApprove(t, target, a, code, "Ahmet")
	idB := joinAndApprove(t, target, b, code, "Mehmet") // same code, still valid
	if idA == idB {
		t.Fatal("operator ids must differ")
	}

	// operator -> target carries the sender id
	writeMessage(t, b, Message{Type: "signal", Kind: "offer", Payload: json.RawMessage(`{"x":1}`)})
	if got := readMessage(t, target); got.Type != "signal" || got.Peer != idB {
		t.Fatalf("target got %+v", got)
	}
	// target -> one operator only
	writeMessage(t, target, Message{Type: "signal", Kind: "answer", Payload: json.RawMessage(`{}`), Peer: idA})
	if got := readMessage(t, a); got.Type != "signal" || got.Kind != "answer" {
		t.Fatalf("A got %+v", got)
	}

	// kick B: B is told and dropped, session survives for A
	writeMessage(t, target, Message{Type: "kick", Peer: idB})
	if got := readMessage(t, b); got.Type != "closed" || got.Reason != "kicked" {
		t.Fatalf("B kick: %+v", got)
	}
	// A leaves: target gets peer_left, session stays open for new joins
	a.Close(websocket.StatusNormalClosure, "")
	if got := readMessage(t, target); got.Type != "peer_left" || got.Peer != idA {
		t.Fatalf("peer_left: %+v", got)
	}
	c := dialServer(t, ts)
	defer c.Close(websocket.StatusNormalClosure, "")
	joinAndApprove(t, target, c, code, "Ali")
}

func TestRejectOneOperatorKeepsSession(t *testing.T) {
	s := NewServer()
	s.SetLogger(t.Logf)
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	target := dialServer(t, ts)
	defer target.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, target, Message{Type: "create", V: ProtocolVersion})
	code := readMessage(t, target).Code

	a := dialServer(t, ts)
	defer a.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, a, Message{Type: "join", Code: code, V: ProtocolVersion})
	readMessage(t, a)
	pj := readMessage(t, target)
	writeMessage(t, target, Message{Type: "approve", Approved: false, Peer: pj.Peer})
	if got := readMessage(t, a); got.Type != "rejected" {
		t.Fatalf("reject: %+v", got)
	}
	b := dialServer(t, ts)
	defer b.Close(websocket.StatusNormalClosure, "")
	joinAndApprove(t, target, b, code, "Mehmet")
}

func TestLegacyDeviceBusy(t *testing.T) {
	s := NewServer()
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	dev := dialServer(t, ts)
	defer dev.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, dev, Message{Type: "create", V: 2, DeviceID: "PC-9", Pass: "gizli123"})
	readMessage(t, dev)
	a := dialServer(t, ts)
	defer a.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, a, Message{Type: "join", V: ProtocolVersion, DeviceID: "PC-9", Pass: "gizli123"})
	if got := readMessage(t, a); got.Type != "joined" {
		t.Fatalf("first: %+v", got)
	}
	if pj := readMessage(t, dev); pj.Peer != "" {
		t.Fatalf("legacy target must not get peer ids: %+v", pj)
	}
	b := dialServer(t, ts)
	defer b.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, b, Message{Type: "join", V: ProtocolVersion, DeviceID: "PC-9", Pass: "gizli123"})
	if got := readMessage(t, b); got.Type != "error" || got.Reason != "device_busy" {
		t.Fatalf("second: %+v", got)
	}
}
