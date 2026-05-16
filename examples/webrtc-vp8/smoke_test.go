package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
)

// TestSuperDemoEndToEnd boots the demo HTTP server, opens a pion peer
// that mirrors the browser handshake, then exercises every public
// runtime control through the DataChannel and asserts the telemetry
// surface reflects each change.
//
// Each rendition has its own VP8 encoder; the test verifies all three
// produce RTP and per-rendition telemetry, then validates every control
// path: bitrate, screen mode, denoiser, temporal cap, force keyframe,
// pause/resume, ROI install, ROI clear, and the global active-map
// toggle. A control is considered effective once a subsequent
// telemetry message reports the new value.
func TestSuperDemoEndToEnd(t *testing.T) {
	cfg := demoConfig{Addr: ":0", FPS: 30, Renditions: defaultRenditions}
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

	rtpHits := make([]int, renditionCount)
	for i := 0; i < renditionCount; i++ {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
		); err != nil {
			t.Fatalf("AddTransceiverFromKind: %v", err)
		}
	}

	var rtpMu = struct{ done chan struct{} }{done: make(chan struct{})}
	closedOnce := false
	closeOnce := func() {
		if !closedOnce {
			closedOnce = true
			close(rtpMu.done)
		}
	}
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		idx := -1
		go func() {
			buf := make([]byte, 1500)
			for {
				n, _, err := track.Read(buf)
				if err != nil {
					return
				}
				if n > 0 {
					if idx < 0 {
						// Identify the rendition by the first track we
						// hit that doesn't yet have packets.
						for i := range rtpHits {
							if rtpHits[i] == 0 {
								idx = i
								break
							}
						}
						if idx < 0 {
							return
						}
					}
					rtpHits[idx]++
					if rtpHits[0] > 0 && rtpHits[1] > 0 && rtpHits[2] > 0 {
						closeOnce()
					}
				}
			}
		}()
	})

	dcMsgCh := make(chan telemetryMessage, 256)
	dc, err := pc.CreateDataChannel("demo", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		var m telemetryMessage
		if err := json.Unmarshal(msg.Data, &m); err != nil {
			return
		}
		select {
		case dcMsgCh <- m:
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
	if !strings.Contains(strings.ToUpper(answer.SDP), "VP8") {
		t.Fatalf("answer SDP missing VP8:\n%s", answer.SDP)
	}
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Wait for all three renditions to deliver RTP.
	select {
	case <-rtpMu.done:
	case <-ctx.Done():
		t.Fatalf("missing RTP for some renditions: %v", rtpHits)
	}

	// Wait for at least one telemetry per rendition so DataChannel is up.
	gotTel := [renditionCount]bool{}
	for !gotTel[0] || !gotTel[1] || !gotTel[2] {
		select {
		case m := <-dcMsgCh:
			if m.ID >= 0 && m.ID < renditionCount {
				gotTel[m.ID] = true
			}
		case <-ctx.Done():
			t.Fatalf("no telemetry for all renditions: %v", gotTel)
		}
	}

	// helper that waits for telemetry on `id` matching pred.
	waitFor := func(id int, name string, pred func(telemetryMessage) bool) {
		t.Helper()
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case m := <-dcMsgCh:
				if m.ID == id && pred(m) {
					return
				}
			case <-ctx.Done():
				t.Fatalf("[id=%d %s] context done", id, name)
			}
		}
		t.Fatalf("[id=%d %s] telemetry never matched", id, name)
	}

	// -- per-rendition controls --
	for id := 0; id < renditionCount; id++ {
		id := id
		t.Run(fmt.Sprintf("rendition_%d", id), func(t *testing.T) {
			send := func(obj any) {
				raw, _ := json.Marshal(obj)
				if err := dc.SendText(string(raw)); err != nil {
					t.Fatalf("send: %v", err)
				}
			}

			send(map[string]any{"type": "bitrate", "id": id, "kbps": 500 + id*100})
			waitFor(id, "bitrate", func(m telemetryMessage) bool {
				return m.TargetKbps == 500+id*100
			})

			send(map[string]any{"type": "screen", "id": id, "mode": 1})
			waitFor(id, "screen mode=1", func(m telemetryMessage) bool {
				return m.Screen == 1
			})
			send(map[string]any{"type": "screen", "id": id, "mode": 0})
			waitFor(id, "screen mode=0", func(m telemetryMessage) bool {
				return m.Screen == 0
			})

			send(map[string]any{"type": "denoise", "id": id, "level": 2})
			waitFor(id, "denoise=2", func(m telemetryMessage) bool {
				return m.Denoise == 2
			})
			send(map[string]any{"type": "denoise", "id": id, "level": 0})
			waitFor(id, "denoise=0", func(m telemetryMessage) bool {
				return m.Denoise == 0
			})

			send(map[string]any{"type": "temporal", "id": id, "cap": 0})
			waitFor(id, "temporal cap=0", func(m telemetryMessage) bool {
				return m.Suppressed || m.TP == 0
			})
			send(map[string]any{"type": "temporal", "id": id, "cap": 2})
			waitFor(id, "temporal cap restored", func(m telemetryMessage) bool {
				return !m.Suppressed
			})

			send(map[string]any{"type": "roi", "id": id, "u": 0.5, "v": 0.5})
			waitFor(id, "roi installed", func(m telemetryMessage) bool {
				return m.ROI
			})
			send(map[string]any{"type": "roi-clear"})
			waitFor(id, "roi cleared", func(m telemetryMessage) bool {
				return !m.ROI
			})

			send(map[string]any{"type": "keyframe", "id": id})
			waitFor(id, "keyframe", func(m telemetryMessage) bool {
				return m.KF
			})

			// Exercise the full min<->max round-trip the browser UI
			// offers so a regression in validDim or in the encoder's
			// big-jump resize path fails the suite.
			for _, dim := range [][2]int{
				{160, 90}, {1920, 1088}, {160, 90}, {640, 360},
			} {
				send(map[string]any{"type": "resize", "id": id, "w": dim[0], "h": dim[1]})
				waitFor(id, fmt.Sprintf("resize %dx%d", dim[0], dim[1]),
					func(m telemetryMessage) bool {
						return m.Width == dim[0] && m.Height == dim[1]
					})
			}

			send(map[string]any{"type": "pause", "id": id, "paused": true})
			// Drain telemetry briefly to verify the rendition stops
			// producing fresh frames during pause.
			frameBefore := -1
			deadline := time.Now().Add(2 * time.Second)
			for time.Now().Before(deadline) {
				select {
				case m := <-dcMsgCh:
					if m.ID == id {
						if frameBefore < 0 {
							frameBefore = m.Frame
						} else if m.Frame > frameBefore+5 {
							// still ticking → pause didn't take
							t.Fatalf("pause did not stop frame counter: before=%d now=%d",
								frameBefore, m.Frame)
						}
					}
				case <-ctx.Done():
					t.Fatalf("context done during pause check")
				}
			}
			send(map[string]any{"type": "pause", "id": id, "paused": false})
		})
	}

	// global active-map toggle: applies to every encoder; assert telemetry
	// keeps flowing after the toggle (encoder didn't choke).
	t.Run("active_map_toggle", func(t *testing.T) {
		raw, _ := json.Marshal(map[string]any{"type": "activemap", "enabled": true})
		if err := dc.SendText(string(raw)); err != nil {
			t.Fatalf("send activemap: %v", err)
		}
		deadline := time.Now().Add(5 * time.Second)
		seenAfter := 0
		for time.Now().Before(deadline) {
			select {
			case <-dcMsgCh:
				seenAfter++
				if seenAfter >= 10 {
					raw, _ := json.Marshal(map[string]any{"type": "activemap", "enabled": false})
					if err := dc.SendText(string(raw)); err != nil {
						t.Fatalf("send activemap off: %v", err)
					}
					return
				}
			case <-ctx.Done():
				t.Fatalf("activemap: context done")
			}
		}
		t.Fatalf("activemap: telemetry stopped after toggle")
	})
}
