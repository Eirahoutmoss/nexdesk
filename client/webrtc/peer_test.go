package webrtcpeer

import (
	"context"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

func TestPionLoopbackPingPong(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connectedA := make(chan struct{}, 1)
	connectedB := make(chan struct{}, 1)
	pong := make(chan struct{}, 1)

	a, err := New(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := New(true, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	a.pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateConnected {
			select {
			case connectedA <- struct{}{}:
			default:
			}
		}
	})
	b.pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateConnected {
			select {
			case connectedB <- struct{}{}:
			default:
			}
		}
	})
	b.SetControlHandler(func(msg string) {
		if msg == "PING" {
			_ = b.SendControlText("PONG")
		}
	})
	a.SetControlHandler(func(msg string) {
		if msg == "PONG" {
			select {
			case pong <- struct{}{}:
			default:
			}
		}
	})
	if err := a.CreateControl(); err != nil {
		t.Fatal(err)
	}

	offer, err := a.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := b.AcceptOffer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.AddSignal(answer); err != nil {
		t.Fatal(err)
	}

	select {
	case <-connectedA:
	case <-ctx.Done():
		t.Fatal("operator peer did not connect")
	}
	select {
	case <-connectedB:
	case <-ctx.Done():
		t.Fatal("target peer did not connect")
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := a.SendPing(); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("control channel did not open")
		}
		time.Sleep(10 * time.Millisecond)
	}
	select {
	case <-pong:
	case <-ctx.Done():
		t.Fatal("PING/PONG did not complete")
	}
}

func TestCandidateIPs(t *testing.T) {
	sdp := "a=candidate:1 1 udp 2130706431 10.2.2.9 5000 typ host\r\n" +
		"a=candidate:2 1 udp 1694498815 195.175.55.10 6000 typ srflx raddr 10.2.2.9 rport 5000\r\n" +
		"a=candidate:3 1 udp 16777215 140.238.143.54 49160 typ relay raddr 0.0.0.0 rport 0\r\n" +
		"a=candidate:4 1 udp 2130706431 fe80::1 5001 typ host\r\n"
	s, h := candidateIPs(sdp)
	if !s["195.175.55.10"] || len(s) != 1 || !h["10.2.2.9"] || len(h) != 1 {
		t.Fatalf("srflx=%v host=%v", s, h)
	}
	if prefix16("10.2.33.119") != "10.2" {
		t.Fatal("prefix16")
	}
}
