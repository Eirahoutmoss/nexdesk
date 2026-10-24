package nexdesk

import (
	"context"
	"encoding/json"
	"image"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eirahoutmoss/remotesupport/client/screen"
	clientsignaling "github.com/eirahoutmoss/remotesupport/client/signaling"
	webrtcpeer "github.com/eirahoutmoss/remotesupport/client/webrtc"
	server "github.com/eirahoutmoss/remotesupport/server/signaling"
	"github.com/eirahoutmoss/remotesupport/shared/protocol"
	"github.com/eirahoutmoss/remotesupport/shared/tiles"
	"github.com/pion/webrtc/v4"
)

type rec struct {
	mu        sync.Mutex
	connected chan struct{}
	frames    chan *Frame
	closed    chan string
	roster    chan string
}

func newRec() *rec {
	return &rec{connected: make(chan struct{}, 1), frames: make(chan *Frame, 8), closed: make(chan string, 1), roster: make(chan string, 4)}
}
func (r *rec) OnStatus(string)            {}
func (r *rec) OnConnected(string, string) { r.connected <- struct{}{} }
func (r *rec) OnFrame(f *Frame)           { r.frames <- f }
func (r *rec) OnRoster(t string)          { r.roster <- t }
func (r *rec) OnInputBy(string)           {}
func (r *rec) OnChat(string)              {}
func (r *rec) OnMonitors(int)             {}
func (r *rec) OnClosed(reason string)     { r.closed <- reason }

// The mobile core joins a code like the desktop operator, receives a delta
// frame with tile metadata, and its text input reaches the target intact.
func TestMobileSessionEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s := server.NewServer()
	defer s.Close()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	url := "ws" + ts.URL[len("http"):] + "/v1/ws"

	target, err := clientsignaling.Dial(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close(context.Background())
	code, _, err := target.Create(ctx)
	if err != nil {
		t.Fatal(err)
	}
	inputs := make(chan protocol.InputEvent, 4)
	var tp *webrtcpeer.Peer
	go func() {
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
				p, _ := webrtcpeer.NewWithConfig(true, webrtc.Configuration{}, func(c context.Context, sig webrtcpeer.Signal) error {
					return target.SignalTo(c, id, sig.Kind, sig.Payload)
				})
				tp = p
				p.SetControlHandler(func(m string) {
					var ev protocol.InputEvent
					if len(m) > 6 && m[:6] == "INPUT:" && json.Unmarshal([]byte(m[6:]), &ev) == nil {
						inputs <- ev
					}
				})
				ans, _ := p.AcceptOffer(ctx, webrtcpeer.Signal{Kind: msg.Kind, Payload: msg.Payload})
				_ = target.SignalTo(ctx, id, ans.Kind, ans.Payload)
				go func() {
					if p.WaitScreen(ctx) != nil {
						return
					}
					jp := tiles.WithMeta([]byte{0xFF, 0xD8, 1, 2, 0xFF, 0xD9}, tiles.Cols, []image.Point{{3, 4}})
					_ = p.SendScreenFrame(screen.Frame{Monitor: 0, Seq: 1, Width: 640, Height: 480, JPEG: jp})
				}()
			}
		}
	}()

	r := newRec()
	ms := NewSession(r)
	ms.Configure(url, "", "", "", "Telefon", "1.4.0")
	ms.ConnectCode(code)
	defer ms.Disconnect()
	select {
	case <-r.connected:
	case why := <-r.closed:
		t.Fatal(why)
	case <-ctx.Done():
		t.Fatal("not connected")
	}
	select {
	case f := <-r.frames:
		if f.Width != 640 || f.TileSize != tiles.Size || f.Count != 1 || f.TileX(0) != 3 || f.TileY(0) != 4 {
			t.Fatalf("frame %+v", f)
		}
	case <-ctx.Done():
		t.Fatal("no frame")
	}
	for i := 0; i < 50; i++ { // control channel may open just after "connected"
		ms.Text("İğ ş\n")
		select {
		case ev := <-inputs:
			if ev.T != "text" || ev.S != "İğ ş\n" {
				t.Fatalf("input %+v", ev)
			}
			if tp != nil {
				tp.Close()
			}
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	t.Fatal("text input never arrived")
}
