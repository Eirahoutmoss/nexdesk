package integration

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eirahoutmoss/remotesupport/client/signaling"
	webrtcpeer "github.com/eirahoutmoss/remotesupport/client/webrtc"
	server "github.com/eirahoutmoss/remotesupport/server/signaling"
	"github.com/pion/webrtc/v4"
)

// Two technicians join the same code and both get their own WebRTC session
// with the one target; signals are routed by operator id.
func TestTwoOperatorsOneTarget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := server.NewServer()
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	target, err := clientsignaling.Dial(ctx, wsURL(ts.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(context.Background())
	code, _, err := target.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !target.Multi() {
		t.Fatal("server should advertise multi")
	}

	cfg := webrtc.Configuration{}
	var mu sync.Mutex
	tpeers := map[string]*webrtcpeer.Peer{}
	go func() { // target loop: approve everyone, one peer per operator id
		for {
			msg, err := target.Read(ctx)
			if err != nil {
				return
			}
			switch msg.Type {
			case "peer_joined":
				_ = target.ApprovePeer(ctx, msg.Peer, true, "")
			case "signal":
				if msg.Kind != "offer" {
					continue
				}
				id := msg.Peer
				p, err := webrtcpeer.NewWithConfig(true, cfg, func(c context.Context, sig webrtcpeer.Signal) error {
					return target.SignalTo(c, id, sig.Kind, sig.Payload)
				})
				if err != nil {
					t.Error(err)
					return
				}
				p.SetControlHandler(func(m string) {
					if m == "PING" {
						_ = p.SendControlText("PONG:" + id)
					}
				})
				mu.Lock()
				tpeers[id] = p
				mu.Unlock()
				ans, err := p.AcceptOffer(ctx, webrtcpeer.Signal{Kind: msg.Kind, Payload: msg.Payload})
				if err != nil {
					t.Error(err)
					return
				}
				_ = target.SignalTo(ctx, id, ans.Kind, ans.Payload)
			}
		}
	}()

	run := func(name string) <-chan string {
		got := make(chan string, 1)
		op, err := clientsignaling.Dial(ctx, wsURL(ts.URL))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { op.Close(context.Background()) })
		op.Name = name
		if err := op.Join(ctx, code); err != nil {
			t.Fatalf("%s join: %v", name, err)
		}
		p, err := webrtcpeer.NewWithConfig(true, cfg, func(c context.Context, sig webrtcpeer.Signal) error {
			return op.Signal(c, sig.Kind, sig.Payload)
		})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { p.Close() })
		p.SetControlHandler(func(m string) { got <- m })
		if err := p.CreateControl(); err != nil {
			t.Fatal(err)
		}
		go func() {
			for {
				msg, err := op.Read(ctx)
				if err != nil {
					return
				}
				switch msg.Type {
				case "approved":
					offer, err := p.Offer(ctx)
					if err != nil {
						t.Error(err)
						return
					}
					_ = op.Signal(ctx, offer.Kind, offer.Payload)
				case "signal":
					_ = p.AddSignal(webrtcpeer.Signal{Kind: msg.Kind, Payload: msg.Payload})
				}
			}
		}()
		go func() {
			for ctx.Err() == nil {
				if p.ConnectionState() == webrtc.PeerConnectionStateConnected {
					time.Sleep(200 * time.Millisecond)
					_ = p.SendControlText("PING")
					return
				}
				time.Sleep(50 * time.Millisecond)
			}
		}()
		return got
	}
	a := run("Ahmet")
	b := run("Mehmet")
	var ra, rb string
	for ra == "" || rb == "" {
		select {
		case ra = <-a:
		case rb = <-b:
		case <-ctx.Done():
			t.Fatalf("timeout: a=%q b=%q", ra, rb)
		}
	}
	if ra == rb {
		t.Fatalf("both operators answered by the same target peer: %q", ra)
	}
	mu.Lock()
	n := len(tpeers)
	for _, p := range tpeers {
		p.Close()
	}
	mu.Unlock()
	if n != 2 {
		t.Fatalf("target peers = %d", n)
	}
}
