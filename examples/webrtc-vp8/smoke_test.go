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
	"sync"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"

	"github.com/thesyncim/govpx"
)

func TestHandleOfferRejectsOfferWithoutVP8Receive(t *testing.T) {
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
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if !offerSupportsDemoVP8(offer.SDP, cfg) {
		t.Fatalf("test offer unexpectedly missing VP8 receive support:\n%s", offer.SDP)
	}
	offer.SDP = strings.ReplaceAll(offer.SDP, "VP8/90000", "AV1/90000")
	if offerSupportsDemoVP8(offer.SDP, cfg) {
		t.Fatalf("mutated test offer still negotiates VP8:\n%s", offer.SDP)
	}

	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s, want %d",
			resp.StatusCode, raw, http.StatusNotAcceptable)
	}
}

func TestHandleOfferRejectsOfferWithVP8ReceiverCapsBelowTopRendition(t *testing.T) {
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
	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	if !offerSupportsDemoVP8(offer.SDP, cfg) {
		t.Fatalf("test offer unexpectedly missing VP8 receive support:\n%s", offer.SDP)
	}
	maxFS, err := govpx.VP8SDPFrameSizeMacroblocks(
		cfg.Renditions[renditionCount-1].Width,
		cfg.Renditions[renditionCount-1].Height,
	)
	if err != nil {
		t.Fatalf("VP8SDPFrameSizeMacroblocks: %v", err)
	}
	offer.SDP = withVP8FmtpForTest(t, offer.SDP,
		fmt.Sprintf("max-fr=%d; max-fs=%d", cfg.FPS, maxFS-1))
	if offerSupportsDemoVP8(offer.SDP, cfg) {
		t.Fatalf("mutated test offer still allows demo top rendition:\n%s", offer.SDP)
	}

	body, err := json.Marshal(offer)
	if err != nil {
		t.Fatalf("marshal offer: %v", err)
	}
	resp, err := http.Post(ts.URL+"/offer", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /offer: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotAcceptable {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("offer status=%d body=%s, want %d",
			resp.StatusCode, raw, http.StatusNotAcceptable)
	}
}

func withVP8FmtpForTest(t *testing.T, sdp string, fmtp string) string {
	t.Helper()

	lines := strings.Split(sdp, "\n")
	payloadType := ""
	rtpmapIndex := -1
	for i, line := range lines {
		clean := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(clean), "a=rtpmap:") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(clean, "a=rtpmap:"))
		if len(fields) >= 2 && strings.EqualFold(fields[1], "VP8/90000") {
			payloadType = fields[0]
			rtpmapIndex = i
			break
		}
	}
	if payloadType == "" {
		t.Fatalf("offer SDP missing VP8 rtpmap:\n%s", sdp)
	}

	out := make([]string, 0, len(lines)+1)
	inserted := false
	for i, line := range lines {
		clean := strings.TrimSpace(line)
		if strings.HasPrefix(clean, "a=fmtp:"+payloadType+" ") {
			continue
		}
		out = append(out, line)
		if i == rtpmapIndex {
			out = append(out, "a=fmtp:"+payloadType+" "+fmtp)
			inserted = true
		}
	}
	if !inserted {
		t.Fatalf("failed to insert VP8 fmtp in offer:\n%s", sdp)
	}
	return strings.Join(out, "\n")
}

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
	rtpDescriptorHits := make([]int, renditionCount)
	rtpTemporalHits := make([]int, renditionCount)
	for i := 0; i < renditionCount; i++ {
		if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
			webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
		); err != nil {
			t.Fatalf("AddTransceiverFromKind: %v", err)
		}
	}

	var rtpMu struct {
		sync.Mutex
		done chan struct{}
		once sync.Once
	}
	rtpMu.done = make(chan struct{})
	checkRTPDone := func() {
		rtpMu.Lock()
		defer rtpMu.Unlock()
		for i := 0; i < renditionCount; i++ {
			if rtpHits[i] == 0 || rtpDescriptorHits[i] == 0 || rtpTemporalHits[i] == 0 {
				return
			}
		}
		rtpMu.once.Do(func() { close(rtpMu.done) })
	}
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		idx := -1
		for i, rc := range cfg.Renditions {
			if track.ID() == fmt.Sprintf("govpx-vp8-%s", rc.Name) {
				idx = i
				break
			}
		}
		go func() {
			for {
				pkt, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				if pkt == nil || len(pkt.Payload) == 0 || idx < 0 {
					continue
				}
				desc, _, err := govpx.ParseVP8RTPPayloadDescriptor(pkt.Payload)
				rtpMu.Lock()
				rtpHits[idx]++
				if err == nil && desc.PictureIDPresent && desc.PictureID15Bit {
					rtpDescriptorHits[idx]++
					if desc.TL0PICIDXPresent && desc.TemporalIDPresent {
						rtpTemporalHits[idx]++
					}
				}
				rtpMu.Unlock()
				checkRTPDone()
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
		t.Fatalf("missing govpx VP8 RTP descriptors for some renditions: hits=%v descriptors=%v temporal=%v",
			rtpHits, rtpDescriptorHits, rtpTemporalHits)
	}

	// Wait for at least one telemetry per rendition so DataChannel is up.
	gotTel := [renditionCount]bool{}
	gotRTPPacketTel := [renditionCount]bool{}
	for !gotTel[0] || !gotTel[1] || !gotTel[2] ||
		!gotRTPPacketTel[0] || !gotRTPPacketTel[1] || !gotRTPPacketTel[2] {
		select {
		case m := <-dcMsgCh:
			if m.ID >= 0 && m.ID < renditionCount {
				gotTel[m.ID] = true
				if m.RTPPackets > 0 {
					gotRTPPacketTel[m.ID] = true
				}
			}
		case <-ctx.Done():
			t.Fatalf("no RTP packet telemetry for all renditions: tel=%v rtp=%v",
				gotTel, gotRTPPacketTel)
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
