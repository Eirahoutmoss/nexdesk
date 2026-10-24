package signaling

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestDeviceRegisterAndTechnicianConnect(t *testing.T) {
	s := NewServer()
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	// Device registers (first contact sets the password).
	dev := dialServer(t, ts)
	defer dev.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, dev, Message{Type: "create", V: ProtocolVersion, DeviceID: "PC-01", Pass: "gizli123"})
	if got := readMessage(t, dev); got.Type != "created" || got.Code != "PC-01" {
		t.Fatalf("device create: %+v", got)
	}

	// Technician connects with the right id + password.
	op := dialServer(t, ts)
	defer op.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, op, Message{Type: "join", V: ProtocolVersion, DeviceID: "pc-01", Pass: "gizli123"}) // id case-insensitive
	if got := readMessage(t, op); got.Type != "joined" {
		t.Fatalf("technician join: %+v", got)
	}
	if pj := readMessage(t, dev); pj.Type != "peer_joined" {
		t.Fatalf("device peer_joined: %+v", pj)
	}
}

func TestDeviceWrongPasswordRejected(t *testing.T) {
	s := NewServer()
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	dev := dialServer(t, ts)
	defer dev.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, dev, Message{Type: "create", DeviceID: "PC-02", Pass: "dogruparola"})
	readMessage(t, dev)

	op := dialServer(t, ts)
	defer op.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, op, Message{Type: "join", DeviceID: "PC-02", Pass: "yanlis"})
	if got := readMessage(t, op); got.Type != "error" || got.Reason != "device_auth_failed" {
		t.Fatalf("expected device_auth_failed, got %+v", got)
	}
}

func TestDeviceReRegisterNeedsPassword(t *testing.T) {
	s := NewServer()
	defer s.Close()
	// First registration sets it.
	if err := s.authDevice("DEV-X", "ilkparola", true); err != nil {
		t.Fatal(err)
	}
	// A second machine claiming the same id with a wrong password is rejected.
	if err := s.authDevice("DEV-X", "baska", true); err == nil {
		t.Fatal("takeover with wrong password allowed")
	}
	// Correct password re-auths fine.
	if err := s.authDevice("DEV-X", "ilkparola", true); err != nil {
		t.Fatal(err)
	}
	// Too-short password on first contact is refused.
	if err := s.authDevice("DEV-Y", "ab", true); err == nil {
		t.Fatal("weak password accepted")
	}
}

// The owner secret lets a device change its password; without it a new
// password is refused (someone who only knows the id cannot take over).
func TestDeviceOwnerChangesPassword(t *testing.T) {
	s := NewServer()
	defer s.Close()
	if err := s.authDeviceOwner("DEV-O", "eskiparola", "sahip-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.authDeviceOwner("DEV-O", "yeniparola", "baskasi"); err == nil {
		t.Fatal("wrong owner changed the password")
	}
	if err := s.authDeviceOwner("DEV-O", "yeniparola", ""); err == nil {
		t.Fatal("no owner changed the password")
	}
	if err := s.authDeviceOwner("DEV-O", "yeniparola", "sahip-1"); err != nil {
		t.Fatalf("owner could not change password: %v", err)
	}
	if err := s.authDevice("DEV-O", "yeniparola", false); err != nil {
		t.Fatalf("technician with new password: %v", err)
	}
	if err := s.authDevice("DEV-O", "eskiparola", false); err == nil {
		t.Fatal("old password still accepted")
	}
	// legacy registration (no owner) adopts one with the right password
	if err := s.authDeviceOwner("DEV-L", "parola1", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.authDeviceOwner("DEV-L", "parola1", "sahip-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.authDeviceOwner("DEV-L", "parola2", "sahip-2"); err != nil {
		t.Fatalf("adopted owner could not change password: %v", err)
	}
}

func TestDevicePersistentSessionNotExpired(t *testing.T) {
	s := NewServer()
	defer s.Close()
	s.SetSessionTTL(20 * 1e6) // 20ms
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	dev := dialServer(t, ts)
	defer dev.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, dev, Message{Type: "create", DeviceID: "PC-03", Pass: "parolam12"})
	readMessage(t, dev)
	// Give cleanup a few ticks; a persistent device session must survive.
	op := dialServer(t, ts)
	defer op.Close(websocket.StatusNormalClosure, "")
	// small wait via a round-trip
	writeMessage(t, op, Message{Type: "join", DeviceID: "PC-03", Pass: "parolam12"})
	if got := readMessage(t, op); got.Type != "joined" {
		t.Fatalf("persistent device expired? got %+v", got)
	}
}

// A device that re-registers replaces its session; the old socket's teardown
// must not remove the new session (it used to, causing a re-register loop).
func TestDeviceReRegisterKeepsNewSession(t *testing.T) {
	s := NewServer()
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	old := dialServer(t, ts)
	writeMessage(t, old, Message{Type: "create", V: ProtocolVersion, DeviceID: "PC-02", Pass: "gizli123"})
	if got := readMessage(t, old); got.Type != "created" {
		t.Fatalf("first create: %+v", got)
	}
	dev := dialServer(t, ts)
	defer dev.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, dev, Message{Type: "create", V: ProtocolVersion, DeviceID: "PC-02", Pass: "gizli123"})
	if got := readMessage(t, dev); got.Type != "created" {
		t.Fatalf("second create: %+v", got)
	}
	old.Close(websocket.StatusNormalClosure, "")
	time.Sleep(300 * time.Millisecond) // let the old read loop exit

	op := dialServer(t, ts)
	defer op.Close(websocket.StatusNormalClosure, "")
	writeMessage(t, op, Message{Type: "join", V: ProtocolVersion, DeviceID: "PC-02", Pass: "gizli123"})
	if got := readMessage(t, op); got.Type != "joined" {
		t.Fatalf("join after re-register: %+v", got)
	}
	if pj := readMessage(t, dev); pj.Type != "peer_joined" {
		t.Fatalf("new device socket peer_joined: %+v", pj)
	}
}
