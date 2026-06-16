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

	"github.com/pion/rtcp"
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

	rtpCh := make(chan *rtp.Packet, 256)
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

	firstAU := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
	firstDesc := assertWebRTCRTPAccessUnitForTest(t, firstAU,
		spatialLayerCount, true)
	secondAU := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
	secondDesc := assertWebRTCRTPAccessUnitForTest(t, secondAU,
		spatialLayerCount, false)
	if got, want := secondAU[0].SequenceNumber, firstAU[0].SequenceNumber+uint16(len(firstAU)); got != want {
		t.Fatalf("second RTP access unit first sequence = %d, want %d",
			got, want)
	}
	if got, want := secondAU[0].Timestamp-firstAU[0].Timestamp,
		uint32(rtpClockHz/defaultFPS); got != want {
		t.Fatalf("second RTP access unit timestamp step = %d, want %d",
			got, want)
	}
	if got, want := secondDesc.PictureID, govpx.NextVP9RTPPictureID(firstDesc.PictureID); got != want {
		t.Fatalf("second RTP picture ID = %d, want %d", got, want)
	}
	if err := pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: firstAU[0].SSRC},
	}); err != nil {
		t.Fatalf("send PLI: %v", err)
	}
	pliAU, pliDesc := readVP9RTPKeyAccessUnitAfterFeedbackForTest(t, ctx, rtpCh,
		secondAU, secondDesc)
	if pliDesc.InterPicturePredicted {
		t.Fatal("PLI response base descriptor kept inter-picture prediction")
	}
	if !pliDesc.ScalabilityStructurePresent {
		t.Fatal("PLI response did not carry key access-unit scalability structure")
	}
	if err := dc.SendText(`{"type":"spatial","cap":1}`); err != nil {
		t.Fatalf("send spatial cap ctl: %v", err)
	}
	capDesc := readVP9RTPAccessUnitWithActiveSpatialLayersForTest(t, ctx,
		rtpCh, pliAU, pliDesc, 1)
	if capDesc.InterPicturePredicted {
		t.Fatal("spatial-cap response base descriptor kept inter-picture prediction")
	}
	if !capDesc.ScalabilityStructurePresent ||
		capDesc.ScalabilityStructure.SpatialLayerCount != 1 {
		t.Fatalf("spatial-cap response SS = present:%t layers:%d, want active base-only key",
			capDesc.ScalabilityStructurePresent,
			capDesc.ScalabilityStructure.SpatialLayerCount)
	}
	if capDesc.ScalabilityStructure.Width[1] != 0 ||
		capDesc.ScalabilityStructure.Height[1] != 0 {
		t.Fatalf("spatial-cap response leaked hidden dimensions = %dx%d",
			capDesc.ScalabilityStructure.Width[1],
			capDesc.ScalabilityStructure.Height[1])
	}

	desc := firstDesc
	if !desc.LayerIndicesPresent || desc.SpatialID != 0 {
		t.Fatalf("first RTP descriptor layer metadata = present:%v sid:%d, want base spatial layer metadata",
			desc.LayerIndicesPresent, desc.SpatialID)
	}
	if !desc.PictureIDPresent || !desc.PictureID15Bit {
		t.Fatalf("first RTP picture ID = present:%v 15bit:%v, want 15-bit PictureID",
			desc.PictureIDPresent, desc.PictureID15Bit)
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

func readVP9RTPAccessUnitForTest(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
) []*rtp.Packet {
	t.Helper()
	var out []*rtp.Packet
	for {
		select {
		case packet := <-rtpCh:
			if packet == nil || len(packet.Payload) == 0 {
				continue
			}
			desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packet.Payload)
			if err != nil {
				t.Fatalf("ParseVP9RTPPayloadDescriptor while reading AU: %v", err)
			}
			if len(out) == 0 &&
				(!desc.LayerIndicesPresent || desc.SpatialID != 0 || !desc.StartOfFrame) {
				continue
			}
			copyPacket := *packet
			copyPacket.Payload = append([]byte(nil), packet.Payload...)
			out = append(out, &copyPacket)
			if packet.Marker {
				return out
			}
		case <-ctx.Done():
			t.Fatalf("no complete RTP access unit received within timeout")
		}
	}
}

func readVP9RTPKeyAccessUnitAfterFeedbackForTest(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
	prevAU []*rtp.Packet,
	prevDesc govpx.VP9RTPPayloadDescriptor,
) ([]*rtp.Packet, govpx.VP9RTPPayloadDescriptor) {
	t.Helper()
	if len(prevAU) == 0 {
		t.Fatal("feedback wait started without previous RTP access unit")
	}
	const maxAccessUnits = defaultFPS
	for attempt := 0; attempt < maxAccessUnits; attempt++ {
		au := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
		first, _, err := govpx.ParseVP9RTPPayloadDescriptor(au[0].Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor feedback AU: %v", err)
		}
		desc := assertWebRTCRTPAccessUnitForTest(t, au, spatialLayerCount,
			first.ScalabilityStructurePresent)
		prevLastSeq := prevAU[0].SequenceNumber + uint16(len(prevAU)-1)
		if got, want := au[0].SequenceNumber, prevLastSeq+1; got != want {
			t.Fatalf("feedback RTP access unit first sequence = %d, want %d",
				got, want)
		}
		if got, want := au[0].Timestamp-prevAU[0].Timestamp,
			uint32(rtpClockHz/defaultFPS); got != want {
			t.Fatalf("feedback RTP timestamp step = %d, want %d", got, want)
		}
		if got, want := desc.PictureID, govpx.NextVP9RTPPictureID(prevDesc.PictureID); got != want {
			t.Fatalf("feedback RTP picture ID = %d, want %d", got, want)
		}
		if desc.ScalabilityStructurePresent {
			return au, desc
		}
		prevAU = au
		prevDesc = desc
	}
	t.Fatalf("receiver feedback did not produce a key RTP access unit within %d frames",
		maxAccessUnits)
	return nil, govpx.VP9RTPPayloadDescriptor{}
}

func readVP9RTPAccessUnitWithActiveSpatialLayersForTest(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
	prevAU []*rtp.Packet,
	prevDesc govpx.VP9RTPPayloadDescriptor,
	wantLayers int,
) govpx.VP9RTPPayloadDescriptor {
	t.Helper()
	if len(prevAU) == 0 {
		t.Fatal("spatial-layer wait started without previous RTP access unit")
	}
	const maxAccessUnits = defaultFPS
	for attempt := 0; attempt < maxAccessUnits; attempt++ {
		au := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
		activeLayers := rtpAccessUnitSpatialLayerCountForTest(t, au)
		first, _, err := govpx.ParseVP9RTPPayloadDescriptor(au[0].Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor spatial-cap AU: %v", err)
		}
		desc := assertWebRTCRTPAccessUnitForTest(t, au, activeLayers,
			first.ScalabilityStructurePresent)
		prevLastSeq := prevAU[0].SequenceNumber + uint16(len(prevAU)-1)
		if got, want := au[0].SequenceNumber, prevLastSeq+1; got != want {
			t.Fatalf("spatial-cap RTP access unit first sequence = %d, want %d",
				got, want)
		}
		if got, want := au[0].Timestamp-prevAU[0].Timestamp,
			uint32(rtpClockHz/defaultFPS); got != want {
			t.Fatalf("spatial-cap RTP timestamp step = %d, want %d", got, want)
		}
		if got, want := desc.PictureID, govpx.NextVP9RTPPictureID(prevDesc.PictureID); got != want {
			t.Fatalf("spatial-cap RTP picture ID = %d, want %d", got, want)
		}
		if desc.ScalabilityStructurePresent &&
			desc.ScalabilityStructure.SpatialLayerCount == wantLayers {
			return desc
		}
		prevAU = au
		prevDesc = desc
	}
	t.Fatalf("spatial-cap control did not produce an active %d-layer key RTP access unit within %d frames",
		wantLayers, maxAccessUnits)
	return govpx.VP9RTPPayloadDescriptor{}
}

func rtpAccessUnitSpatialLayerCountForTest(
	t *testing.T,
	packets []*rtp.Packet,
) int {
	t.Helper()
	maxLayer := -1
	for i, packet := range packets {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packet.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor layer-count[%d]: %v", i, err)
		}
		if !desc.LayerIndicesPresent {
			t.Fatalf("RTP packet %d missing VP9 layer metadata", i)
		}
		if int(desc.SpatialID) > maxLayer {
			maxLayer = int(desc.SpatialID)
		}
	}
	if maxLayer < 0 {
		t.Fatal("RTP access unit had no spatial layers")
	}
	return maxLayer + 1
}

func assertWebRTCRTPAccessUnitForTest(
	t *testing.T,
	packets []*rtp.Packet,
	spatialLayers int,
	wantSS bool,
) govpx.VP9RTPPayloadDescriptor {
	t.Helper()
	if len(packets) == 0 {
		t.Fatal("empty RTP access unit")
	}
	firstSeq := packets[0].SequenceNumber
	timestamp := packets[0].Timestamp
	var firstDesc govpx.VP9RTPPayloadDescriptor
	var pictureID uint16
	var seenPictureID bool
	var seenLayerStart [govpx.VP9RTPMaxSpatialLayers]bool
	for i, packet := range packets {
		if got, want := packet.SequenceNumber, firstSeq+uint16(i); got != want {
			t.Fatalf("RTP packet %d sequence = %d, want %d", i, got, want)
		}
		if packet.Timestamp != timestamp {
			t.Fatalf("RTP packet %d timestamp = %d, want AU timestamp %d",
				i, packet.Timestamp, timestamp)
		}
		if got, want := packet.Marker, i == len(packets)-1; got != want {
			t.Fatalf("RTP packet %d marker = %t, want %t", i, got, want)
		}
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packet.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if i == 0 {
			firstDesc = desc
			if !desc.StartOfFrame || desc.SpatialID != 0 {
				t.Fatalf("first RTP packet descriptor = start:%t sid:%d, want base layer start",
					desc.StartOfFrame, desc.SpatialID)
			}
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit {
			t.Fatalf("RTP packet %d PictureID = present:%t 15bit:%t, want 15-bit",
				i, desc.PictureIDPresent, desc.PictureID15Bit)
		}
		if !seenPictureID {
			pictureID = desc.PictureID
			seenPictureID = true
		} else if desc.PictureID != pictureID {
			t.Fatalf("RTP packet %d PictureID = %d, want AU PictureID %d",
				i, desc.PictureID, pictureID)
		}
		if !desc.LayerIndicesPresent || int(desc.SpatialID) >= spatialLayers {
			t.Fatalf("RTP packet %d layer metadata = present:%t sid:%d, want sid < %d",
				i, desc.LayerIndicesPresent, desc.SpatialID, spatialLayers)
		}
		if desc.StartOfFrame {
			seenLayerStart[desc.SpatialID] = true
			if desc.SpatialID == 0 && wantSS {
				if !desc.ScalabilityStructurePresent ||
					desc.ScalabilityStructure.SpatialLayerCount != spatialLayers {
					t.Fatalf("base RTP SS = present:%t layers:%d, want %d active layers",
						desc.ScalabilityStructurePresent,
						desc.ScalabilityStructure.SpatialLayerCount,
						spatialLayers)
				}
			} else if desc.ScalabilityStructurePresent {
				t.Fatalf("RTP packet %d layer %d repeated scalability structure",
					i, desc.SpatialID)
			}
			if desc.SpatialID > 0 && !desc.InterLayerDependency {
				t.Fatalf("RTP packet %d enhancement layer %d missing inter-layer dependency",
					i, desc.SpatialID)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("RTP packet %d repeated scalability structure on non-start fragment", i)
		}
	}
	for layer := 0; layer < spatialLayers; layer++ {
		if !seenLayerStart[layer] {
			t.Fatalf("RTP access unit missing spatial layer %d start", layer)
		}
	}
	lastDesc, _, err := govpx.ParseVP9RTPPayloadDescriptor(packets[len(packets)-1].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor[last]: %v", err)
	}
	if int(lastDesc.SpatialID) != spatialLayers-1 || !lastDesc.EndOfFrame {
		t.Fatalf("last RTP descriptor = sid:%d end:%t, want top layer end",
			lastDesc.SpatialID, lastDesc.EndOfFrame)
	}
	return firstDesc
}

func TestSplitBitrateTreatsBitrateAsTotalBudget(t *testing.T) {
	got := splitBitrate(defaultBitrateKbps, layerSplitPct)
	want := [spatialLayerCount]int{96, 288, 416}
	if got != want {
		t.Fatalf("splitBitrate(%d) = %v, want %v",
			defaultBitrateKbps, got, want)
	}

	total := 0
	for i, kbps := range got {
		if kbps < minLayerBitrateKbps {
			t.Fatalf("layer %d bitrate = %d, want at least %d",
				i, kbps, minLayerBitrateKbps)
		}
		total += kbps
	}
	if total != defaultBitrateKbps {
		t.Fatalf("split total = %d, want %d", total, defaultBitrateKbps)
	}
}

func TestRTCPRequestsKeyFrameOnlyForPLIAndFIR(t *testing.T) {
	rr := marshalRTCPForTest(t, &rtcp.ReceiverReport{SSRC: 1})
	if rtcpRequestsKeyFrame(rr) {
		t.Fatal("receiver report unexpectedly requested keyframe")
	}

	pli := marshalRTCPForTest(t, &rtcp.PictureLossIndication{
		SenderSSRC: 1,
		MediaSSRC:  2,
	})
	if !rtcpRequestsKeyFrame(pli) {
		t.Fatal("PLI did not request keyframe")
	}

	fir := marshalRTCPForTest(t, &rtcp.FullIntraRequest{
		SenderSSRC: 1,
		MediaSSRC:  2,
		FIR: []rtcp.FIREntry{{
			SSRC:           2,
			SequenceNumber: 1,
		}},
	})
	if !rtcpRequestsKeyFrame(fir) {
		t.Fatal("FIR did not request keyframe")
	}

	compound, err := rtcp.Marshal([]rtcp.Packet{
		&rtcp.ReceiverReport{SSRC: 1},
		&rtcp.PictureLossIndication{SenderSSRC: 1, MediaSSRC: 2},
	})
	if err != nil {
		t.Fatalf("marshal compound RTCP: %v", err)
	}
	if !rtcpRequestsKeyFrame(compound) {
		t.Fatal("compound RTCP with PLI did not request keyframe")
	}

	if rtcpRequestsKeyFrame([]byte{0x80}) {
		t.Fatal("malformed RTCP unexpectedly requested keyframe")
	}
}

func TestRTPClockOffsetAvoidsNonDivisorFPSDrift(t *testing.T) {
	const fps = 29
	naiveAfterOneSecond := uint64(fps) * uint64(rtpClockHz/fps)
	if naiveAfterOneSecond == rtpClockHz {
		t.Fatal("test setup expected integer-division RTP clock drift")
	}
	if got := rtpClockOffset(uint64(fps), fps); got != rtpClockHz {
		t.Fatalf("rtpClockOffset(%d frames @ %dfps) = %d, want %d",
			fps, fps, got, rtpClockHz)
	}

	var sawLongStep bool
	for frame := uint64(1); frame <= fps; frame++ {
		prev := rtpClockOffset(frame-1, fps)
		next := rtpClockOffset(frame, fps)
		step := next - prev
		if step != uint64(rtpClockHz/fps) && step != uint64(rtpClockHz/fps+1) {
			t.Fatalf("rtp clock step %d = %d, want %d or %d",
				frame, step, rtpClockHz/fps, rtpClockHz/fps+1)
		}
		if step == uint64(rtpClockHz/fps+1) {
			sawLongStep = true
		}
	}
	if !sawLongStep {
		t.Fatal("rtp clock never compensated for fractional frame duration")
	}
}

func TestWaitICEGatheringComplete(t *testing.T) {
	done := make(chan struct{})
	close(done)
	if !waitICEGatheringComplete(done, time.Second) {
		t.Fatal("closed gathering channel reported timeout")
	}
	if !waitICEGatheringComplete(done, 0) {
		t.Fatal("closed gathering channel with no timeout reported timeout")
	}
	if waitICEGatheringComplete(nil, time.Second) {
		t.Fatal("nil gathering channel reported complete")
	}

	open := make(chan struct{})
	start := time.Now()
	if waitICEGatheringComplete(open, time.Millisecond) {
		t.Fatal("open gathering channel reported complete")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout helper took %s, want bounded wait", elapsed)
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

func TestSVCLayerOptionsEnableRowMTForThreadedLayers(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
	}{
		{"base-layer", 160, 90},
		{"middle-layer", 320, 180},
		{"top-layer", 640, 360},
		{"wide-layer", 1280, 720},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := newSVCLayerOptions(tc.width, tc.height, defaultFPS, 700)
			wantThreads := pickThreads(tc.width, tc.height)
			if opts.Threads != wantThreads {
				t.Fatalf("Threads = %d, want %d", opts.Threads, wantThreads)
			}
			if opts.CpuUsed != 8 {
				t.Fatalf("CpuUsed = %d, want 8", opts.CpuUsed)
			}
			if opts.RowMT != (wantThreads > 1) {
				t.Fatalf("RowMT = %t for %d threads, want %t",
					opts.RowMT, wantThreads, wantThreads > 1)
			}
			if !opts.ErrorResilient ||
				!opts.FrameParallelDecodingSet ||
				!opts.FrameParallelDecoding {
				t.Fatalf("VP9 resilience flags = err:%t fp-set:%t fp:%t, want true/true/true",
					opts.ErrorResilient,
					opts.FrameParallelDecodingSet,
					opts.FrameParallelDecoding)
			}
		})
	}
}

func TestWebRTCPacketizedSVCDecodeContinuityAndCapRecovery(t *testing.T) {
	svc, err := newSVCEncoder(demoConfig{
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	})
	if err != nil {
		t.Fatalf("newSVCEncoder: %v", err)
	}
	defer svc.Close()

	var decoders [spatialLayerCount]*govpx.VP9Decoder
	for layer := 0; layer < spatialLayerCount; layer++ {
		decoders[layer], err = govpx.NewVP9Decoder(govpx.VP9DecoderOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    uint8(layer),
		})
		if err != nil {
			t.Fatalf("NewVP9Decoder layer %d: %v", layer, err)
		}
		defer decoders[layer].Close()
	}

	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := range imgs {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	dst := make([]byte, superframeBudget())
	caps := []int{3, 3, 2, 2, 1, 3, 3, 2, 3}
	pictureID := uint16(0x7ffc)
	lastCap := caps[0]
	for frame, cap := range caps {
		if frame > 0 && cap != lastCap {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		if frame > 0 && cap != lastCap {
			base := result.Layers[0]
			if !base.KeyFrame || base.InterPicturePredicted {
				t.Fatalf("frame %d cap %d->%d base = key:%t inter:%t, want key/non-predicted",
					frame, lastCap, cap, base.KeyFrame,
					base.InterPicturePredicted)
			}
			for spatial := 1; spatial < spatialLayerCount; spatial++ {
				if result.Layers[spatial].KeyFrame ||
					!result.Layers[spatial].ShowFrame {
					t.Fatalf("frame %d cap %d->%d layer %d = key:%t show:%t, want visible inter-layer refresh",
						frame, lastCap, cap, spatial,
						result.Layers[spatial].KeyFrame,
						result.Layers[spatial].ShowFrame)
				}
			}
		}
		rtpResult := limitSVCResultForRTPForTest(t, result, cap)
		payloads := packetizeWebRTCSVCResultForTest(t, rtpResult, pictureID, 500)
		packet := reassembleWebRTCSVCResultForTest(t, rtpResult, payloads, pictureID)
		for layer := 0; layer < cap; layer++ {
			assertWebRTCSVCDecoderOutputForTest(t, decoders[layer],
				packet, frame, layer, layerDims[layer][0], layerDims[layer][1])
		}
		lastCap = cap
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
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

func TestForceKeyAllRefreshesEverySpatialLayer(t *testing.T) {
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
	for frame := 0; frame < 2; frame++ {
		drawScene(imgs, frame)
		if _, err := svc.EncodeIntoWithResult(imgs, dst); err != nil {
			t.Fatalf("warm EncodeIntoWithResult frame %d: %v", frame, err)
		}
	}

	forceKeyAll(svc)
	if !svc.IsKeyFrameNext() {
		t.Fatal("ForceKeyFrame request was not armed")
	}
	drawScene(imgs, 2)
	result, err := svc.EncodeIntoWithResult(imgs, dst)
	if err != nil {
		t.Fatalf("forced EncodeIntoWithResult: %v", err)
	}
	base := result.Layers[0]
	if !base.KeyFrame || base.InterPicturePredicted {
		t.Fatalf("base forced result = key:%t inter-pred:%t, want key/non-predicted",
			base.KeyFrame, base.InterPicturePredicted)
	}
	for spatial := 1; spatial < spatialLayerCount; spatial++ {
		layer := result.Layers[spatial]
		if layer.KeyFrame || !layer.ShowFrame {
			t.Fatalf("layer %d forced result = key:%t show:%t, want visible inter-layer refresh",
				spatial, layer.KeyFrame, layer.ShowFrame)
		}
	}
	if svc.IsKeyFrameNext() {
		t.Fatal("ForceKeyFrame request remained armed after encode")
	}
}

func TestPacketizeSVCResultForWebRTCAddsPictureID(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	const pictureID = uint16(0x1234)
	payloads := packetizeWebRTCSVCResultForTest(t, result, pictureID, 500)
	if len(payloads) == 0 {
		t.Fatal("PacketizeWebRTCRTPInto returned no payloads")
	}

	var sawBaseSS bool
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID {
			t.Fatalf("payload %d PictureID = present:%v 15bit:%v id:%d, want %d",
				i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID, pictureID)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %v, want %v", i, got, want)
		}
		if desc.SpatialID == 0 && desc.StartOfFrame {
			sawBaseSS = desc.ScalabilityStructurePresent &&
				desc.ScalabilityStructure.SpatialLayerCount == spatialLayerCount
		}
	}
	if !sawBaseSS {
		t.Fatal("base WebRTC packet did not carry full scalability structure")
	}
	if got := govpx.NextVP9RTPPictureID(govpx.VP9RTPPictureID15BitMask); got != 0 {
		t.Fatalf("NextVP9RTPPictureID wrap = %d, want 0", got)
	}
}

func TestPacketizeSVCResultForWebRTCSignalsSSOnBaseKeyOnly(t *testing.T) {
	results := encodeSVCResultsForTest(t, 2)
	keyPayloads := packetizeWebRTCSVCResultForTest(t, results[0], 0x60, 500)
	if !webRTCSVCBaseStartHasSSForTest(t, keyPayloads) {
		t.Fatal("base key RTP frame did not signal scalability structure")
	}

	deltaPayloads := packetizeWebRTCSVCResultForTest(t, results[1], 0x61, 500)
	for i, payload := range deltaPayloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.ScalabilityStructurePresent {
			t.Fatalf("delta payload %d repeated scalability structure", i)
		}
	}
}

func TestPacketizeCappedSVCResultForWebRTCSignalsActiveScalabilityStructure(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 2)
	payloads := packetizeWebRTCSVCResultForTest(t, capped, 0x55, 500)

	base, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor base: %v", err)
	}
	if !base.PictureIDPresent || base.PictureID != 0x55 {
		t.Fatalf("base PictureID = present:%v id:%d, want 0x55",
			base.PictureIDPresent, base.PictureID)
	}
	if !base.ScalabilityStructurePresent ||
		base.ScalabilityStructure.SpatialLayerCount != 2 {
		t.Fatalf("base SS = present:%v layers:%d, want active 2-layer structure",
			base.ScalabilityStructurePresent,
			base.ScalabilityStructure.SpatialLayerCount)
	}
	if base.ScalabilityStructure.Width[2] != 0 ||
		base.ScalabilityStructure.Height[2] != 0 {
		t.Fatalf("base SS leaked hidden layer dimensions = %dx%d",
			base.ScalabilityStructure.Width[2],
			base.ScalabilityStructure.Height[2])
	}
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.SpatialID >= 2 {
			t.Fatalf("payload %d spatial id = %d, want capped layers < 2",
				i, desc.SpatialID)
		}
	}
}

func TestCappedSVCResultForRTPKeepsActiveScalabilityStructure(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 2)
	wantSize := result.Layers[0].SizeBytes + result.Layers[1].SizeBytes
	if capped.SizeBytes != wantSize || capped.LayerCount != 2 {
		t.Fatalf("capped result accounting = size:%d layers:%d, want %d/2",
			capped.SizeBytes, capped.LayerCount, wantSize)
	}
	payloads := packetizeWebRTCSVCResultForTest(t, capped, 0x56, 500)
	if len(payloads) == 0 {
		t.Fatal("capped WebRTC packetizer returned no payloads")
	}

	base, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor base: %v", err)
	}
	if !base.ScalabilityStructurePresent ||
		base.ScalabilityStructure.SpatialLayerCount != 2 {
		t.Fatalf("base SS = present:%v layers:%d, want active 2-layer structure",
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
	for spatial := 0; spatial < int(capped.LayerCount); spatial++ {
		if capped.Layers[spatial].SpatialLayerCount != capped.LayerCount {
			t.Fatalf("layer %d SpatialLayerCount = %d, want capped count %d",
				spatial, capped.Layers[spatial].SpatialLayerCount,
				capped.LayerCount)
		}
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

func TestCappedSVCResultForRTPSingleLayerSignalsBaseOnly(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 1)
	payloads := packetizeWebRTCSVCResultForTest(t, capped, 0x57, 500)

	base, _, err := govpx.ParseVP9RTPPayloadDescriptor(payloads[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor base: %v", err)
	}
	if !base.ScalabilityStructurePresent ||
		base.ScalabilityStructure.SpatialLayerCount != 1 {
		t.Fatalf("base-only SS = present:%v layers:%d, want one active layer",
			base.ScalabilityStructurePresent,
			base.ScalabilityStructure.SpatialLayerCount)
	}
	if base.ScalabilityStructure.Width[1] != 0 ||
		base.ScalabilityStructure.Height[1] != 0 {
		t.Fatalf("base-only SS leaked hidden layer dimensions = %dx%d",
			base.ScalabilityStructure.Width[1],
			base.ScalabilityStructure.Height[1])
	}
	if !base.NotRefForUpperSpatialLayer {
		t.Fatal("base-only descriptor was not marked not-reference-for-upper")
	}
	if capped.Layers[0].SpatialLayerCount != 1 {
		t.Fatalf("base-only SpatialLayerCount = %d, want 1",
			capped.Layers[0].SpatialLayerCount)
	}
}

func webRTCSVCBaseStartHasSSForTest(
	t *testing.T,
	payloads []govpx.RTPPayloadFragment,
) bool {
	t.Helper()
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.SpatialID == 0 && desc.StartOfFrame {
			return desc.ScalabilityStructurePresent
		}
	}
	return false
}

func TestCappedTelemetryReportsTransmittedLayers(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 2)

	tracker := newStatsTracker()
	tracker.observe(capped, time.Now())
	raw, err := tracker.snapshot(capped, defaultBitrateKbps, 0, 0)
	if err != nil {
		t.Fatalf("snapshot capped telemetry: %v", err)
	}
	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode capped telemetry: %v\npayload=%s", err, raw)
	}
	if len(msg.Layers) != 2 {
		t.Fatalf("capped telemetry layer count = %d, want 2", len(msg.Layers))
	}
	if msg.Totals.Bytes != capped.SizeBytes {
		t.Fatalf("capped telemetry bytes = %d, want %d",
			msg.Totals.Bytes, capped.SizeBytes)
	}
	if msg.Layers[1].SP != 1 {
		t.Fatalf("top transmitted telemetry SP = %d, want 1", msg.Layers[1].SP)
	}
}

func TestStatsTrackerClearsHiddenLayerWindows(t *testing.T) {
	result := encodeOneSVCResultForTest(t)
	capped := limitSVCResultForRTPForTest(t, result, 1)
	tracker := newStatsTracker()
	start := time.Now()

	tracker.observe(result, start.Add(time.Second))
	if tracker.windowed[1].lastKBPS == 0 || tracker.windowed[2].lastKBPS == 0 {
		t.Fatalf("full-layer warmup kbps = %.2f/%.2f, want non-zero",
			tracker.windowed[1].lastKBPS, tracker.windowed[2].lastKBPS)
	}

	tracker.observe(capped, start.Add(1500*time.Millisecond))
	if tracker.windowed[1].lastKBPS != 0 || tracker.windowed[2].lastKBPS != 0 {
		t.Fatalf("hidden-layer kbps after cap = %.2f/%.2f, want zero",
			tracker.windowed[1].lastKBPS, tracker.windowed[2].lastKBPS)
	}

	tracker.observe(result, start.Add(1600*time.Millisecond))
	raw, err := tracker.snapshot(result, defaultBitrateKbps, 0, 0)
	if err != nil {
		t.Fatalf("snapshot restored telemetry: %v", err)
	}
	var msg telemetryMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatalf("decode restored telemetry: %v\npayload=%s", err, raw)
	}
	if len(msg.Layers) != spatialLayerCount {
		t.Fatalf("restored telemetry layer count = %d, want %d",
			len(msg.Layers), spatialLayerCount)
	}
	if msg.Layers[1].KbpsR != 0 || msg.Layers[2].KbpsR != 0 {
		t.Fatalf("restored hidden-layer stale kbps = %.2f/%.2f, want zero until fresh window",
			msg.Layers[1].KbpsR, msg.Layers[2].KbpsR)
	}
}

func limitSVCResultForRTPForTest(
	t *testing.T,
	result govpx.VP9SpatialSVCEncodeResult,
	layerCount int,
) govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	limited, err := result.LimitSpatialLayersForRTP(layerCount)
	if err != nil {
		t.Fatalf("LimitSpatialLayersForRTP(%d): %v", layerCount, err)
	}
	return limited
}

func webRTCSVCShouldSignalScalabilityStructureForTest(
	layer govpx.VP9EncodeResult,
	result govpx.VP9SpatialSVCEncodeResult,
) bool {
	if !layer.KeyFrame || layer.InterPicturePredicted ||
		layer.TemporalLayerID != 0 {
		return false
	}
	ss := result.ScalabilityStructure
	if ss.SpatialLayerCount != 0 || ss.ResolutionPresent ||
		ss.PictureGroupPresent || len(ss.PictureGroups) != 0 {
		return true
	}
	for i := range ss.Width {
		if ss.Width[i] != 0 || ss.Height[i] != 0 {
			return true
		}
	}
	return false
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

func packetizeWebRTCSVCResultForTest(t *testing.T, result govpx.VP9SpatialSVCEncodeResult,
	pictureID uint16, mtu int,
) []govpx.RTPPayloadFragment {
	t.Helper()
	packets, payloadBytes, err := result.WebRTCRTPPacketizationSize(pictureID, mtu)
	if err != nil {
		t.Fatalf("WebRTCRTPPacketizationSize: %v", err)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, used, err := result.PacketizeWebRTCRTPInto(payloads, payloadBuf,
		pictureID, mtu)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTPInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeWebRTCRTPInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	return payloads[:n]
}

func reassembleWebRTCSVCResultForTest(t *testing.T,
	result govpx.VP9SpatialSVCEncodeResult,
	payloads []govpx.RTPPayloadFragment,
	pictureID uint16,
) []byte {
	t.Helper()
	count := int(result.LayerCount)
	var byLayer [govpx.VP9MaxSpatialLayers][]govpx.RTPPayloadFragment
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID {
			t.Fatalf("payload %d PictureID = present:%t 15bit:%t id:%d, want %d",
				i, desc.PictureIDPresent, desc.PictureID15Bit,
				desc.PictureID, pictureID)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %t, want %t", i, got, want)
		}
		if !desc.LayerIndicesPresent || int(desc.SpatialID) >= count {
			t.Fatalf("payload %d descriptor = %+v, want spatial layer < %d",
				i, desc, count)
		}
		layerID := int(desc.SpatialID)
		wantLayer := result.Layers[layerID]
		if int(desc.TemporalID) != wantLayer.TemporalLayerID ||
			desc.TL0PICIDX != wantLayer.TL0PICIDX ||
			desc.SwitchingUpPoint != wantLayer.TemporalLayerSync ||
			desc.InterPicturePredicted != wantLayer.InterPicturePredicted ||
			desc.InterLayerDependency != wantLayer.InterLayerDependency ||
			desc.NotRefForUpperSpatialLayer != wantLayer.NotRefForUpperSpatialLayer {
			t.Fatalf("payload %d layer %d descriptor = tid:%d tl0:%d sync:%t p:%t dep:%t n:%t, want tid:%d tl0:%d sync:%t p:%t dep:%t n:%t",
				i, layerID, desc.TemporalID, desc.TL0PICIDX,
				desc.SwitchingUpPoint, desc.InterPicturePredicted,
				desc.InterLayerDependency,
				desc.NotRefForUpperSpatialLayer,
				wantLayer.TemporalLayerID, wantLayer.TL0PICIDX,
				wantLayer.TemporalLayerSync, wantLayer.InterPicturePredicted,
				wantLayer.InterLayerDependency,
				wantLayer.NotRefForUpperSpatialLayer)
		}
		if layerID == 0 && desc.StartOfFrame {
			if webRTCSVCShouldSignalScalabilityStructureForTest(wantLayer, result) {
				wantSpatialLayers := count
				if result.ScalabilityStructure.SpatialLayerCount != 0 {
					wantSpatialLayers = result.ScalabilityStructure.SpatialLayerCount
				}
				if !desc.ScalabilityStructurePresent ||
					desc.ScalabilityStructure.SpatialLayerCount != wantSpatialLayers {
					t.Fatalf("base payload %d SS = present:%t layers:%d, want %d",
						i, desc.ScalabilityStructurePresent,
						desc.ScalabilityStructure.SpatialLayerCount,
						wantSpatialLayers)
				}
			} else if desc.ScalabilityStructurePresent {
				t.Fatalf("base delta payload %d repeated scalability structure",
					i)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d layer %d repeated scalability structure",
				i, layerID)
		}
		byLayer[layerID] = append(byLayer[layerID], payload)
	}

	var frames [govpx.VP9MaxSpatialLayers][]byte
	for layerID := 0; layerID < count; layerID++ {
		assembled, err := govpx.AssembleVP9RTPFrame(byLayer[layerID])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame layer %d: %v", layerID, err)
		}
		if !bytes.Equal(assembled, result.Layers[layerID].Data) {
			t.Fatalf("assembled RTP layer %d does not match encoded layer",
				layerID)
		}
		frames[layerID] = assembled
	}
	need, err := govpx.VP9SuperframeSize(frames[:count]...)
	if err != nil {
		t.Fatalf("VP9SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := govpx.PackVP9SuperframeInto(packet, frames[:count]...)
	if err != nil {
		t.Fatalf("PackVP9SuperframeInto: %v", err)
	}
	return packet[:n]
}

func assertWebRTCSVCDecoderOutputForTest(
	t *testing.T,
	decoder *govpx.VP9Decoder,
	packet []byte,
	frame int,
	layer int,
	wantWidth int,
	wantHeight int,
) {
	t.Helper()
	if err := decoder.Decode(packet); err != nil {
		t.Fatalf("Decode frame %d layer %d: %v", frame, layer, err)
	}
	img, ok := decoder.NextFrame()
	if !ok {
		t.Fatalf("Decode frame %d layer %d produced no visible frame",
			frame, layer)
	}
	if img.Width != wantWidth || img.Height != wantHeight {
		t.Fatalf("Decode frame %d layer %d image = %dx%d, want %dx%d",
			frame, layer, img.Width, img.Height, wantWidth, wantHeight)
	}
	info, ok := decoder.LastFrameInfo()
	if !ok || !info.ShowFrame || info.Corrupted ||
		info.Width != wantWidth || info.Height != wantHeight {
		t.Fatalf("Decode frame %d layer %d info = %+v ok=%t, want clean %dx%d",
			frame, layer, info, ok, wantWidth, wantHeight)
	}
}

func marshalRTCPForTest(t *testing.T, packet rtcp.Packet) []byte {
	t.Helper()
	raw, err := packet.Marshal()
	if err != nil {
		t.Fatalf("marshal %T: %v", packet, err)
	}
	return raw
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
