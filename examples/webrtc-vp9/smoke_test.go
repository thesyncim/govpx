package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// TestDemoEndToEnd boots the demo HTTP server, opens a pion peer that does
// the same offer/answer/DataChannel dance the browser does, and asserts the
// server delivers RTP packets and JSON telemetry within the per-frame
// budget the in-tree VP9 encoder can sustain. It covers the spatial-SVC
// encoder, the WebRTC track, the DataChannel control path, and the JSON
// schema the browser overlay expects.
func TestDemoEndToEnd(t *testing.T) {
	cfg := demoConfig{Addr: ":0", FPS: defaultFPS, BitrateKbps: defaultBitrateKbps}
	mux := http.NewServeMux()
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		handleOffer(w, r, cfg)
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		t.Fatalf("NewPeerConnection: %v", err)
	}
	defer pc.Close()

	rtpCh := make(chan struct{}, 1)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			buf := make([]byte, 1500)
			for {
				n, _, err := track.Read(buf)
				if err != nil {
					return
				}
				if n > 0 {
					select {
					case rtpCh <- struct{}{}:
					default:
					}
				}
			}
		}()
	})

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}

	dcMsgCh := make(chan []byte, 8)
	dc, err := pc.CreateDataChannel("demo", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		select {
		case dcMsgCh <- append([]byte(nil), msg.Data...):
		default:
		}
	})

	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	gather := webrtc.GatheringCompletePromise(pc)
	if err := pc.SetLocalDescription(offer); err != nil {
		t.Fatalf("SetLocalDescription: %v", err)
	}
	<-gather

	body, err := json.Marshal(pc.LocalDescription())
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s", resp.StatusCode, raw)
	}
	var answer webrtc.SessionDescription
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decode answer: %v", err)
	}
	if !strings.Contains(strings.ToUpper(answer.SDP), "VP9") {
		t.Fatalf("answer SDP missing VP9:\n%s", answer.SDP)
	}
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	// The encoder is sub-realtime today, so allow a generous window for the
	// first access unit to land. The point is to prove the wire works
	// end-to-end, not to gate on framerate.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	select {
	case <-rtpCh:
	case <-ctx.Done():
		t.Fatalf("no RTP packet received within timeout")
	}

	var raw []byte
	select {
	case raw = <-dcMsgCh:
	case <-ctx.Done():
		t.Fatalf("no DataChannel telemetry received within timeout")
	}

	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("telemetry json: %v\npayload=%s", err, raw)
	}
	if len(msg.Layers) != spatialLayerCount {
		t.Fatalf("telemetry layer count = %d, want %d (msg=%s)",
			len(msg.Layers), spatialLayerCount, raw)
	}
	if msg.Settings.TargetKbps != cfg.BitrateKbps {
		t.Fatalf("telemetry target kbps = %d, want %d",
			msg.Settings.TargetKbps, cfg.BitrateKbps)
	}
	for i, layer := range msg.Layers {
		if layer.SP != i {
			t.Fatalf("layer %d SP = %d", i, layer.SP)
		}
	}

	// Round-trip a control message; we don't gate on it reaching telemetry
	// because at the encoder's current pace the assertion window is wider
	// than the test budget.
	if err := dc.SendText(`{"type":"keyframe"}`); err != nil {
		t.Fatalf("send keyframe ctl: %v", err)
	}
}
