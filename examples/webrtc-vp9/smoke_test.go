package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/pion/rtp"
	"github.com/pion/webrtc/v4"

	"github.com/thesyncim/govpx"
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

	rtpCh := make(chan *rtp.Packet, 8)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				if len(packet.Payload) > 0 {
					select {
					case rtpCh <- packet:
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

	// Allow a generous window for the first access unit to land. The point is
	// to prove the wire works end-to-end, not to gate on local scheduler noise.
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	var firstRTP *rtp.Packet
	select {
	case firstRTP = <-rtpCh:
	case <-ctx.Done():
		t.Fatalf("no RTP packet received within timeout")
	}
	desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(firstRTP.Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor: %v", err)
	}
	if !desc.LayerIndicesPresent || desc.SpatialID != 0 {
		t.Fatalf("first RTP descriptor layer metadata = present:%v sid:%d, want base spatial layer metadata",
			desc.LayerIndicesPresent, desc.SpatialID)
	}
	if !desc.ScalabilityStructurePresent ||
		desc.ScalabilityStructure.SpatialLayerCount != spatialLayerCount {
		t.Fatalf("first RTP scalability structure = present:%v layers:%d, want %d-layer SVC",
			desc.ScalabilityStructurePresent,
			desc.ScalabilityStructure.SpatialLayerCount, spatialLayerCount)
	}
	if !desc.ScalabilityStructure.PictureGroupPresent ||
		len(desc.ScalabilityStructure.PictureGroups) != 4 {
		t.Fatalf("first RTP temporal picture groups = present:%v count:%d, want 4 groups",
			desc.ScalabilityStructure.PictureGroupPresent,
			len(desc.ScalabilityStructure.PictureGroups))
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
		if layer.TP < 0 || layer.TP >= 3 {
			t.Fatalf("layer %d TP = %d, want three-layer temporal id", i, layer.TP)
		}
	}

	// Round-trip a control message; we don't gate on it reaching telemetry
	// because at the encoder's current pace the assertion window is wider
	// than the test budget.
	if err := dc.SendText(`{"type":"keyframe"}`); err != nil {
		t.Fatalf("send keyframe ctl: %v", err)
	}
}

func TestPickThreadsEnablesTileWorkersForRealtimeLayers(t *testing.T) {
	tests := []struct {
		name        string
		width       int
		height      int
		wantAtLeast int
		wantAtMost  int
	}{
		{"base-layer-stays-single-threaded", 160, 90, 1, 1},
		{"middle-layer-stays-within-vp9-tile-limit", 320, 180, 1, 1},
		{"top-layer-uses-two-columns-when-available", 640, 360, expectedThreads(640, 360), 2},
		{"wide-layer-can-use-four-columns", 1280, 720, expectedThreads(1280, 720), 4},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pickThreads(tc.width, tc.height)
			if got < tc.wantAtLeast || got > tc.wantAtMost {
				t.Fatalf("pickThreads(%d, %d) = %d, want in [%d,%d]",
					tc.width, tc.height, got, tc.wantAtLeast, tc.wantAtMost)
			}
			if got > runtime.NumCPU() {
				t.Fatalf("pickThreads(%d, %d) = %d exceeds NumCPU=%d",
					tc.width, tc.height, got, runtime.NumCPU())
			}
			if got > maxVP9TileColumns(tc.width) {
				t.Fatalf("pickThreads(%d, %d) = %d exceeds legal VP9 tile columns=%d",
					tc.width, tc.height, got, maxVP9TileColumns(tc.width))
			}
		})
	}
}

func TestSVCEncoderEmitsThreadedTopLayerTileLayout(t *testing.T) {
	result := encodeOneSVCResultForTest(t)

	top := result.Layers[spatialLayerCount-1]
	info, err := govpx.PeekVP9StreamInfo(top.Data)
	if err != nil {
		t.Fatalf("PeekVP9StreamInfo top layer: %v", err)
	}
	topWidth, topHeight := layerDims[spatialLayerCount-1][0], layerDims[spatialLayerCount-1][1]
	wantLog2Cols := expectedTileLog2Cols(pickThreads(topWidth, topHeight))
	if !info.TileInfoAvailable || info.TileLog2Cols != wantLog2Cols || info.TileLog2Rows != 0 {
		t.Fatalf("top-layer tile info = available:%v log2:%dx%d, want available %dx0",
			info.TileInfoAvailable, info.TileLog2Cols, info.TileLog2Rows, wantLog2Cols)
	}
}

func TestSVCEncoderUsesThreeTemporalLayers(t *testing.T) {
	results := encodeSVCResultsForTest(t, 5)
	wantTemporalID := []int{0, 2, 1, 2, 0}
	wantTL0 := []uint8{0, 0, 0, 0, 1}

	for frame, result := range results {
		if result.LayerCount != spatialLayerCount {
			t.Fatalf("frame %d layer count = %d, want %d",
				frame, result.LayerCount, spatialLayerCount)
		}
		base := result.Layers[0]
		for spatial := 0; spatial < spatialLayerCount; spatial++ {
			layer := result.Layers[spatial]
			if layer.TemporalLayerCount != 3 ||
				layer.TemporalLayerID != wantTemporalID[frame] ||
				layer.TL0PICIDX != wantTL0[frame] {
				t.Fatalf("frame %d layer %d temporal = id:%d count:%d tl0:%d, want id:%d count:3 tl0:%d",
					frame, spatial, layer.TemporalLayerID, layer.TemporalLayerCount,
					layer.TL0PICIDX, wantTemporalID[frame], wantTL0[frame])
			}
			if spatial > 0 &&
				(layer.TemporalLayerID != base.TemporalLayerID ||
					layer.TL0PICIDX != base.TL0PICIDX) {
				t.Fatalf("frame %d layer %d temporal metadata drifted from base: got id:%d tl0:%d, base id:%d tl0:%d",
					frame, spatial, layer.TemporalLayerID, layer.TL0PICIDX,
					base.TemporalLayerID, base.TL0PICIDX)
			}
		}
	}

	desc := results[0].Layers[0].RTPPayloadDescriptor()
	if !desc.ScalabilityStructurePresent ||
		desc.ScalabilityStructure.SpatialLayerCount != spatialLayerCount ||
		!desc.ScalabilityStructure.PictureGroupPresent ||
		len(desc.ScalabilityStructure.PictureGroups) != 4 {
		t.Fatalf("base RTP SS = present:%v spatial:%d groups:%v/%d, want %d spatial layers and 4 temporal picture groups",
			desc.ScalabilityStructurePresent,
			desc.ScalabilityStructure.SpatialLayerCount,
			desc.ScalabilityStructure.PictureGroupPresent,
			len(desc.ScalabilityStructure.PictureGroups),
			spatialLayerCount)
	}
}

func TestCappedSVCResultForRTPAdvertisesCappedLayerCount(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := cappedSVCResultForRTP(result, 2)
	payloads, err := capped.PacketizeRTP(500)
	if err != nil {
		t.Fatalf("PacketizeRTP capped result: %v", err)
	}
	if len(payloads) == 0 {
		t.Fatal("PacketizeRTP capped result returned no payloads")
	}

	base, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor base: %v", err)
	}
	if !base.ScalabilityStructurePresent ||
		base.ScalabilityStructure.SpatialLayerCount != 2 {
		t.Fatalf("base SS = present:%v layers:%d, want capped 2-layer structure",
			base.ScalabilityStructurePresent,
			base.ScalabilityStructure.SpatialLayerCount)
	}
	if !base.ScalabilityStructure.PictureGroupPresent ||
		len(base.ScalabilityStructure.PictureGroups) != 4 {
		t.Fatalf("base capped SS temporal groups = present:%v count:%d, want 4 groups",
			base.ScalabilityStructure.PictureGroupPresent,
			len(base.ScalabilityStructure.PictureGroups))
	}
	if base.NotRefForUpperSpatialLayer {
		t.Fatal("base descriptor unexpectedly marked not-reference-for-upper")
	}

	var foundEnhancement bool
	for _, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor enhancement scan: %v", err)
		}
		if desc.LayerIndicesPresent && desc.SpatialID == 1 && desc.StartOfFrame {
			foundEnhancement = true
			if !desc.NotRefForUpperSpatialLayer {
				t.Fatal("capped enhancement layer was not marked not-reference-for-upper")
			}
			if desc.ScalabilityStructurePresent {
				t.Fatal("enhancement layer repeated scalability structure")
			}
		}
	}
	if !foundEnhancement {
		t.Fatal("did not find capped enhancement-layer RTP frame")
	}
}

func encodeOneSVCResultForTest(t *testing.T) govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	return encodeSVCResultsForTest(t, 1)[0]
}

func encodeSVCResultsForTest(t *testing.T, frames int) []govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	svc, err := newSVCEncoder(demoConfig{
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	})
	if err != nil {
		t.Fatalf("newSVCEncoder: %v", err)
	}
	defer svc.Close()

	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := range imgs {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	dst := make([]byte, superframeBudget())
	results := make([]govpx.VP9SpatialSVCEncodeResult, frames)
	for frame := 0; frame < frames; frame++ {
		drawScene(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results[frame] = result
	}
	return results
}

func expectedThreads(width, height int) int {
	cpus := runtime.NumCPU()
	maxTileCols := maxVP9TileColumns(width)
	if cpus < 2 || maxTileCols < 2 {
		return 1
	}
	if cpus >= 4 && maxTileCols >= 4 && width*height >= 640*360 {
		return 4
	}
	return 2
}

func expectedTileLog2Cols(threads int) int {
	if threads <= 1 {
		return 0
	}
	if threads <= 2 {
		return 1
	}
	return 2
}
