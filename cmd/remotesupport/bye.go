//go:build windows

package main

import (
	"sync/atomic"
	"time"

	webrtcpeer "github.com/eirahoutmoss/remotesupport/client/webrtc"
)

// Ending a session on purpose ("Bağlantıyı Kes", closing the window, a kick)
// sends "BYE" over the control channel. The other side then ends at once
// instead of treating the silence as a network drop: no reconnect attempts
// on the technician's side, no "waiting for the technician" on the target.
// Peers that never say BYE (older builds, a crash, a real network loss)
// still get the old wait-and-reconnect behaviour.

var (
	// skipBye suppresses the BYE of the next stopSession (handover: the
	// target stays connected to the new technician; or we are answering a
	// BYE ourselves).
	skipBye atomic.Bool
)

// sendBye tells every connected peer that we are leaving and gives the
// message a moment to leave before the peers are closed.
func sendBye(peers []*webrtcpeer.Peer) {
	sent := false
	for _, p := range peers {
		if p != nil && p.SendControlText("BYE") == nil {
			sent = true
		}
	}
	if sent {
		time.Sleep(250 * time.Millisecond)
	}
}

// onPeerBye runs on the control-channel goroutine when the other side says
// BYE. UI work is handed to the window thread.
func onPeerBye(peer *webrtcpeer.Peer) {
	if state.mode == 1 { // target: one technician left
		t := techOwns(peer)
		if t == nil {
			return
		}
		go func() {
			defer logCrash("bye")
			netlogf("Teknisyen bağlantıyı kesti: %s", techDisplayName(t))
			techRemove(t.sig, t.id, "bağlantıyı kesti")
			if techCount() == 0 && state.hwnd != 0 {
				postMessage.Call(state.hwnd, WM_APP_PEER_BYE, 0, 0)
			}
		}()
		return
	}
	// Technician: the supported computer ended the session. Mark it stopped
	// right away so the closing peer does not start a reconnect.
	if userStopped.Load() {
		return
	}
	userStopped.Store(true)
	netlogf("Karşı taraf bağlantıyı kesti")
	if state.hwnd != 0 {
		postMessage.Call(state.hwnd, WM_APP_PEER_BYE, 1, 0)
	}
}

// endByPeer closes the local session after the other side's BYE (window
// thread). technician: we were giving support.
func endByPeer(technician bool) {
	if !technician && techCount() > 0 {
		return // another technician joined meanwhile
	}
	skipBye.Store(true)
	stopSession()
	home()
	if technician {
		setStatus("● Karşı taraf bağlantıyı kesti — oturum sona erdi.")
	} else {
		setStatus("● Destek veren bağlantıyı kesti — oturum sona erdi.")
	}
}
