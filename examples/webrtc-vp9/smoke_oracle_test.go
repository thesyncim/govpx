//go:build govpx_oracle_trace

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"hash/fnv"
	"image"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/pion/rtcp"
	"github.com/pion/rtp"
	"github.com/pion/rtp/codecs"
	"github.com/pion/webrtc/v4"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestWebRTCEndToEndReceivedSVCStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	cfg := demoConfig{
		Addr:        ":0",
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	}
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

	rtpCh := make(chan *rtp.Packet, 512)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				if len(packet.Payload) == 0 {
					continue
				}
				copyPacket := *packet
				copyPacket.Payload = append([]byte(nil), packet.Payload...)
				select {
				case rtpCh <- &copyPacket:
				default:
				}
			}
		}()
	})

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
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
	resp, err := http.Post(ts.URL+"/offer", "application/json",
		bytes.NewReader(body))
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
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	const frames = 6
	packets := make([][]byte, 0, frames)
	var prevAU []*rtp.Packet
	var prevDesc govpx.VP9RTPPayloadDescriptor
	for frame := 0; frame < frames; frame++ {
		au := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
		first, _, err := govpx.ParseVP9RTPPayloadDescriptor(au[0].Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor live frame %d: %v",
				frame, err)
		}
		desc := assertWebRTCRTPAccessUnitForTest(t, au, spatialLayerCount,
			first.ScalabilityStructurePresent)
		if frame > 0 {
			prevLastSeq := prevAU[0].SequenceNumber + uint16(len(prevAU)-1)
			if got, want := au[0].SequenceNumber, prevLastSeq+1; got != want {
				t.Fatalf("live RTP frame %d first sequence = %d, want %d",
					frame, got, want)
			}
			assertRTPMediaTimestampAdvancedForTest(t, "live RTP frame",
				prevAU[0].Timestamp, au[0].Timestamp, frames)
			if got, want := desc.PictureID,
				govpx.NextVP9RTPPictureID(prevDesc.PictureID); got != want {
				t.Fatalf("live RTP frame %d PictureID = %d, want %d",
					frame, got, want)
			}
		}
		packets = append(packets,
			reassembleWebRTCRTPAccessUnitForOracle(t, au, spatialLayerCount))
		prevAU = au
		prevDesc = desc
	}

	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := frames * layerDims[layer][0] * layerDims[layer][1] * 3 / 2
		if len(raw) != want {
			t.Fatalf("live WebRTC vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCEndToEndRuntimeControlsDecodeWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	cfg := demoConfig{
		Addr:        ":0",
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	}
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

	rtpCh := make(chan *rtp.Packet, 1024)
	pc.OnTrack(func(track *webrtc.TrackRemote, _ *webrtc.RTPReceiver) {
		go func() {
			for {
				packet, _, err := track.ReadRTP()
				if err != nil {
					return
				}
				if len(packet.Payload) == 0 {
					continue
				}
				copyPacket := *packet
				copyPacket.Payload = append([]byte(nil), packet.Payload...)
				select {
				case rtpCh <- &copyPacket:
				default:
				}
			}
		}()
	})

	if _, err := pc.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo,
		webrtc.RTPTransceiverInit{Direction: webrtc.RTPTransceiverDirectionRecvonly},
	); err != nil {
		t.Fatalf("AddTransceiverFromKind: %v", err)
	}
	dcOpen := make(chan struct{})
	var dcOpenOnce sync.Once
	dc, err := pc.CreateDataChannel("demo", nil)
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}
	dc.OnOpen(func() {
		dcOpenOnce.Do(func() { close(dcOpen) })
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
	resp, err := http.Post(ts.URL+"/offer", "application/json",
		bytes.NewReader(body))
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
	if err := pc.SetRemoteDescription(answer); err != nil {
		t.Fatalf("SetRemoteDescription: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	select {
	case <-dcOpen:
	case <-ctx.Done():
		t.Fatalf("DataChannel did not open before timeout")
	}

	state := liveWebRTCRTPOracleState{}
	firstDesc, firstLayers := state.read(t, ctx, rtpCh)
	if firstLayers != spatialLayerCount || !firstDesc.ScalabilityStructurePresent {
		t.Fatalf("first live AU = layers:%d ss:%t, want %d-layer key AU",
			firstLayers, firstDesc.ScalabilityStructurePresent,
			spatialLayerCount)
	}
	for range 2 {
		state.read(t, ctx, rtpCh)
	}

	sendWebRTCControlForOracle(t, dc, `{"type":"bitrate","kbps":1200}`)
	sendWebRTCControlForOracle(t, dc, `{"type":"screen","mode":1}`)
	for range 2 {
		state.read(t, ctx, rtpCh)
	}

	sendWebRTCControlForOracle(t, dc, `{"type":"spatial","cap":2}`)
	state.readUntilKeyForLayers(t, ctx, rtpCh, 2)
	sendWebRTCControlForOracle(t, dc, `{"type":"spatial","cap":1}`)
	state.readUntilKeyForLayers(t, ctx, rtpCh, 1)
	sendWebRTCControlForOracle(t, dc, `{"type":"spatial","cap":3}`)
	state.readUntilKeyForLayers(t, ctx, rtpCh, 3)

	if err := pc.WriteRTCP([]rtcp.Packet{
		&rtcp.PictureLossIndication{MediaSSRC: state.prevAU[0].SSRC},
	}); err != nil {
		t.Fatalf("send PLI: %v", err)
	}
	state.readUntilKeyForLayers(t, ctx, rtpCh, 3)

	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], state.packets...)
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(state.caps, layer)
		if len(raw) != want {
			t.Fatalf("live controls vpxdec layer %d raw size = %d, want %d (caps=%v)",
				layer, len(raw), want, state.caps)
		}
		assertVpxdecLayerOutputVariesForCaps(t, "live controls",
			raw, state.caps, layer)
	}
}

func TestWebRTCPacketizedSVCStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets := encodeWebRTCPacketizedAccessUnitsForOracle(t,
		[]int{3, 3, 3, 3})
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := len(packets) * layerDims[layer][0] * layerDims[layer][1] * 3 / 2
		if len(raw) != want {
			t.Fatalf("vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCForcedKeyStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	packets := encodeWebRTCPacketizedAccessUnitsForOracle(t,
		[]int{3, 3, 3, 3, 3}, 2)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := len(packets) * layerDims[layer][0] * layerDims[layer][1] * 3 / 2
		if len(raw) != want {
			t.Fatalf("forced-key vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCPeriodicKeyStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const keyInterval = 4
	caps := []int{3, 3, 3, 3, 3, 3, 3, 3, 3}
	packets := encodeWebRTCPacketizedPeriodicKeyAccessUnitsForOracle(t,
		caps, keyInterval)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)

	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(caps, layer)
		if len(raw) != want {
			t.Fatalf("periodic-key vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCCapRecoveryStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	caps := []int{3, 3, 2, 2, 1, 3, 3, 2, 3}
	packets := encodeWebRTCPacketizedAccessUnitsForOracle(t, caps)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)

	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(caps, layer)
		if len(raw) != want {
			t.Fatalf("cap-recovery vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCRuntimeControlStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	steps := []webRTCSVCOracleStep{
		{cap: 3},
		{cap: 3, bitrateKbps: 1200},
		{cap: 3, screenMode: 1, screenModeSet: true},
		{cap: 2},
		{cap: 2, bitrateKbps: 500},
		{cap: 3, forceKey: true},
		{cap: 3, screenMode: 2, screenModeSet: true},
		{cap: 1},
		{cap: 3, bitrateKbps: 900, screenMode: 0, screenModeSet: true},
	}
	packets := encodeWebRTCPacketizedRuntimeAccessUnitsForOracle(t, steps)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)

	caps := make([]int, len(steps))
	for i, step := range steps {
		caps[i] = step.cap
	}
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(caps, layer)
		if len(raw) != want {
			t.Fatalf("runtime-control vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
	}
}

func TestWebRTCPacketizedSVCLongNoLossControlStreamDecodesWithVpxdec(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const frames = 48
	steps := make([]webRTCSVCOracleStep, frames)
	for frame := range steps {
		cap := 3
		switch {
		case frame >= 9 && frame < 15:
			cap = 2
		case frame >= 24 && frame < 30:
			cap = 1
		case frame >= 36 && frame < 42:
			cap = 2
		}
		steps[frame] = webRTCSVCOracleStep{cap: cap}
		switch frame {
		case 5:
			steps[frame].bitrateKbps = 1200
		case 13:
			steps[frame].screenMode = 1
			steps[frame].screenModeSet = true
		case 20:
			steps[frame].bitrateKbps = 500
		case 31:
			steps[frame].screenMode = 2
			steps[frame].screenModeSet = true
		case 39:
			steps[frame].bitrateKbps = 900
			steps[frame].screenMode = 0
			steps[frame].screenModeSet = true
		}
		if frame != 0 && frame%11 == 0 {
			steps[frame].forceKey = true
		}
	}
	packets := encodeWebRTCPacketizedRuntimeAccessUnitsForOracleStartingAtPictureID(t,
		steps, govpx.VP9RTPPictureID15BitMask-2)
	ivf := vp9test.BuildVP9IVF(layerDims[spatialLayerCount-1][0],
		layerDims[spatialLayerCount-1][1], packets...)

	caps := make([]int, len(steps))
	for i, step := range steps {
		caps[i] = step.cap
	}
	for layer := 0; layer < spatialLayerCount; layer++ {
		raw := vp9test.VpxdecI420WithOptions(t, ivf, vp9test.VpxdecOptions{
			SVCSpatialLayerSet: true,
			SVCSpatialLayer:    layer,
		})
		want := capRecoveryVpxdecBytesForLayer(caps, layer)
		if len(raw) != want {
			t.Fatalf("long no-loss vpxdec layer %d raw size = %d, want %d",
				layer, len(raw), want)
		}
		assertVpxdecLayerOutputVariesForCaps(t, "long no-loss",
			raw, caps, layer)
	}
}

func assertVpxdecLayerOutputVariesForCaps(
	t *testing.T,
	label string,
	raw []byte,
	caps []int,
	layer int,
) {
	t.Helper()
	type layerSeen struct {
		count    int
		distinct map[uint64]bool
	}
	var seen [spatialLayerCount]layerSeen
	off := 0
	for frame, cap := range caps {
		if cap <= 0 || cap > spatialLayerCount {
			t.Fatalf("%s layer %d frame %d cap = %d, want 1..%d",
				label, layer, frame, cap, spatialLayerCount)
		}
		outputLayer := layer
		if outputLayer >= cap {
			outputLayer = cap - 1
		}
		frameBytes := layerDims[outputLayer][0] *
			layerDims[outputLayer][1] * 3 / 2
		if len(raw)-off < frameBytes {
			t.Fatalf("%s layer %d frame %d raw remainder = %d, want %d",
				label, layer, frame, len(raw)-off, frameBytes)
		}
		h := fnv.New64a()
		_, _ = h.Write(raw[off : off+frameBytes])
		sig := h.Sum64()
		if seen[outputLayer].distinct == nil {
			seen[outputLayer].distinct = make(map[uint64]bool)
		}
		seen[outputLayer].count++
		seen[outputLayer].distinct[sig] = true
		off += frameBytes
	}
	if off != len(raw) {
		t.Fatalf("%s layer %d consumed %d decoded bytes, raw has %d",
			label, layer, off, len(raw))
	}
	for outputLayer, s := range seen {
		if s.count >= 2 && len(s.distinct) < 2 {
			t.Fatalf("%s layer %d effective output layer %d produced %d identical decoded frames",
				label, layer, outputLayer, s.count)
		}
	}
}

func capRecoveryVpxdecBytesForLayer(caps []int, layer int) int {
	total := 0
	for _, cap := range caps {
		if cap <= 0 {
			continue
		}
		outputLayer := layer
		if outputLayer >= cap {
			outputLayer = cap - 1
		}
		total += layerDims[outputLayer][0] * layerDims[outputLayer][1] * 3 / 2
	}
	return total
}

type webRTCSVCOracleStep struct {
	cap           int
	bitrateKbps   int
	screenMode    int
	screenModeSet bool
	forceKey      bool
}

func encodeWebRTCPacketizedAccessUnitsForOracle(t *testing.T, caps []int, forceKeyFrames ...int) [][]byte {
	t.Helper()
	steps := make([]webRTCSVCOracleStep, len(caps))
	for frame, cap := range caps {
		steps[frame] = webRTCSVCOracleStep{cap: cap}
	}
	for _, frame := range forceKeyFrames {
		if frame >= 0 && frame < len(steps) {
			steps[frame].forceKey = true
		}
	}
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracle(t, steps)
}

func encodeWebRTCPacketizedPeriodicKeyAccessUnitsForOracle(t *testing.T, caps []int, keyInterval int) [][]byte {
	t.Helper()
	steps := make([]webRTCSVCOracleStep, len(caps))
	for frame, cap := range caps {
		steps[frame] = webRTCSVCOracleStep{cap: cap}
	}
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracleWithHooks(t,
		steps,
		func(svc *govpx.VP9SpatialSVCEncoder) {
			if err := svc.SetLayerKeyFrameInterval(0, keyInterval); err != nil {
				t.Fatalf("SetLayerKeyFrameInterval: %v", err)
			}
		},
		func(frame int, result govpx.VP9SpatialSVCEncodeResult) {
			wantBaseKey := frame == 0 || frame%keyInterval == 0
			if result.Layers[0].KeyFrame != wantBaseKey {
				t.Fatalf("frame %d base key = %t, want %t",
					frame, result.Layers[0].KeyFrame, wantBaseKey)
			}
			if !wantBaseKey {
				return
			}
			for spatial := 1; spatial < int(result.LayerCount); spatial++ {
				layer := result.Layers[spatial]
				if layer.KeyFrame || !layer.ShowFrame {
					t.Fatalf("frame %d layer %d = key:%t show:%t, want visible inter-layer refresh",
						frame, spatial, layer.KeyFrame, layer.ShowFrame)
				}
			}
		})
}

func encodeWebRTCPacketizedRuntimeAccessUnitsForOracle(t *testing.T, steps []webRTCSVCOracleStep) [][]byte {
	t.Helper()
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracleWithHooks(t,
		steps, nil, nil)
}

func encodeWebRTCPacketizedRuntimeAccessUnitsForOracleStartingAtPictureID(
	t *testing.T,
	steps []webRTCSVCOracleStep,
	pictureID uint16,
) [][]byte {
	t.Helper()
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracleInternal(t,
		steps, nil, nil, pictureID)
}

func encodeWebRTCPacketizedRuntimeAccessUnitsForOracleWithHooks(
	t *testing.T,
	steps []webRTCSVCOracleStep,
	configure func(*govpx.VP9SpatialSVCEncoder),
	inspect func(int, govpx.VP9SpatialSVCEncodeResult),
) [][]byte {
	t.Helper()
	return encodeWebRTCPacketizedRuntimeAccessUnitsForOracleInternal(t,
		steps, configure, inspect, 0x100)
}

func encodeWebRTCPacketizedRuntimeAccessUnitsForOracleInternal(
	t *testing.T,
	steps []webRTCSVCOracleStep,
	configure func(*govpx.VP9SpatialSVCEncoder),
	inspect func(int, govpx.VP9SpatialSVCEncodeResult),
	pictureID uint16,
) [][]byte {
	t.Helper()
	if len(steps) == 0 {
		return nil
	}
	svc, err := newSVCEncoder(demoConfig{
		FPS:         defaultFPS,
		BitrateKbps: defaultBitrateKbps,
	})
	if err != nil {
		t.Fatalf("newSVCEncoder: %v", err)
	}
	defer svc.Close()
	if configure != nil {
		configure(svc)
	}

	imgs := make([]*image.YCbCr, spatialLayerCount)
	for i := range imgs {
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	dst := make([]byte, superframeBudget())
	packets := make([][]byte, len(steps))
	lastCap := steps[0].cap
	currentBitrate := defaultBitrateKbps
	currentScreen := 0
	for frame, step := range steps {
		cap := step.cap
		if cap < 1 || cap > spatialLayerCount {
			t.Fatalf("oracle step %d cap = %d, want 1..%d",
				frame, cap, spatialLayerCount)
		}
		if step.bitrateKbps != 0 && step.bitrateKbps != currentBitrate {
			if err := applyBitrate(svc, step.bitrateKbps); err != nil {
				t.Fatalf("applyBitrate frame %d: %v", frame, err)
			}
			currentBitrate = step.bitrateKbps
		}
		if step.screenModeSet && step.screenMode != currentScreen {
			if err := applyScreenMode(svc, step.screenMode); err != nil {
				t.Fatalf("applyScreenMode frame %d: %v", frame, err)
			}
			currentScreen = step.screenMode
		}
		if frame > 0 && (cap != lastCap || step.forceKey) {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeActiveLayersIntoWithResult(imgs, dst, cap)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
				frame, cap, err)
		}
		if inspect != nil {
			inspect(frame, result)
		}
		payloads := packetizeWebRTCSVCResultForTest(t, result, pictureID, 500)
		pionPacket := reassembleWebRTCSVCResultWithPionForOracle(t,
			result, payloads)
		govpxPacket := reassembleWebRTCSVCResultForTest(t, result, payloads,
			pictureID)
		if !bytes.Equal(pionPacket, govpxPacket) {
			t.Fatalf("frame %d Pion RTP reassembly differed from govpx reassembly",
				frame)
		}
		packets[frame] = append([]byte(nil),
			pionPacket...)
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
		lastCap = cap
	}
	return packets
}

func reassembleWebRTCSVCResultWithPionForOracle(
	t *testing.T,
	result govpx.VP9SpatialSVCEncodeResult,
	payloads []govpx.RTPPayloadFragment,
) []byte {
	t.Helper()
	count := int(result.LayerCount)
	var frames [govpx.VP9MaxSpatialLayers][]byte
	var sawStart [govpx.VP9MaxSpatialLayers]bool
	var sawEnd [govpx.VP9MaxSpatialLayers]bool

	for i, payload := range payloads {
		var packet codecs.VP9Packet
		fragment, err := packet.Unmarshal(payload.Payload)
		if err != nil {
			t.Fatalf("Pion VP9Packet.Unmarshal[%d]: %v", i, err)
		}
		if !packet.I || !packet.L {
			t.Fatalf("Pion VP9 packet %d = I:%t L:%t, want PictureID and layer metadata",
				i, packet.I, packet.L)
		}
		if packet.F {
			t.Fatalf("Pion VP9 packet %d used flexible mode; WebRTC SVC path expects non-flexible mode", i)
		}
		layerID := int(packet.SID)
		if layerID >= count {
			t.Fatalf("Pion VP9 packet %d spatial id = %d, want < %d",
				i, layerID, count)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("Pion VP9 packet %d RTP marker = %t, want %t",
				i, got, want)
		}
		if packet.B {
			if sawStart[layerID] {
				t.Fatalf("Pion VP9 packet %d repeated layer %d start",
					i, layerID)
			}
			sawStart[layerID] = true
		} else if !sawStart[layerID] {
			t.Fatalf("Pion VP9 packet %d layer %d fragment arrived before start",
				i, layerID)
		}
		if packet.E {
			sawEnd[layerID] = true
		}
		frames[layerID] = append(frames[layerID], fragment...)
	}

	for layerID := 0; layerID < count; layerID++ {
		if !sawStart[layerID] || !sawEnd[layerID] {
			t.Fatalf("Pion VP9 layer %d start/end = %t/%t, want true/true",
				layerID, sawStart[layerID], sawEnd[layerID])
		}
		if !bytes.Equal(frames[layerID], result.Layers[layerID].Data) {
			t.Fatalf("Pion VP9 reassembled layer %d does not match encoded layer",
				layerID)
		}
	}
	need, err := govpx.VP9SuperframeSize(frames[:count]...)
	if err != nil {
		t.Fatalf("Pion VP9SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := govpx.PackVP9SuperframeInto(packet, frames[:count]...)
	if err != nil {
		t.Fatalf("Pion PackVP9SuperframeInto: %v", err)
	}
	return packet[:n]
}

type liveWebRTCRTPOracleState struct {
	packets  [][]byte
	caps     []int
	prevAU   []*rtp.Packet
	prevDesc govpx.VP9RTPPayloadDescriptor
}

func (s *liveWebRTCRTPOracleState) read(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
) (govpx.VP9RTPPayloadDescriptor, int) {
	t.Helper()
	au := readVP9RTPAccessUnitForTest(t, ctx, rtpCh)
	layers := rtpAccessUnitSpatialLayerCountForTest(t, au)
	first, _, err := govpx.ParseVP9RTPPayloadDescriptor(au[0].Payload)
	if err != nil {
		t.Fatalf("ParseVP9RTPPayloadDescriptor live controls: %v", err)
	}
	desc := assertWebRTCRTPAccessUnitForTest(t, au, layers,
		first.ScalabilityStructurePresent)
	if len(s.prevAU) > 0 {
		prevLastSeq := s.prevAU[0].SequenceNumber +
			uint16(len(s.prevAU)-1)
		if got, want := au[0].SequenceNumber, prevLastSeq+1; got != want {
			t.Fatalf("live controls RTP first sequence = %d, want %d",
				got, want)
		}
		assertRTPMediaTimestampAdvancedForTest(t, "live controls RTP",
			s.prevAU[0].Timestamp, au[0].Timestamp, defaultFPS)
		if got, want := desc.PictureID,
			govpx.NextVP9RTPPictureID(s.prevDesc.PictureID); got != want {
			t.Fatalf("live controls PictureID = %d, want %d",
				got, want)
		}
	}
	s.packets = append(s.packets,
		reassembleWebRTCRTPAccessUnitForOracle(t, au, layers))
	s.caps = append(s.caps, layers)
	s.prevAU = au
	s.prevDesc = desc
	return desc, layers
}

func (s *liveWebRTCRTPOracleState) readUntilKeyForLayers(
	t *testing.T,
	ctx context.Context,
	rtpCh <-chan *rtp.Packet,
	wantLayers int,
) govpx.VP9RTPPayloadDescriptor {
	t.Helper()
	for attempt := 0; attempt < 2*defaultFPS; attempt++ {
		desc, layers := s.read(t, ctx, rtpCh)
		if layers == wantLayers &&
			desc.ScalabilityStructurePresent &&
			desc.ScalabilityStructure.SpatialLayerCount == wantLayers &&
			!desc.InterPicturePredicted {
			return desc
		}
	}
	t.Fatalf("did not receive %d-layer key AU within %d frames",
		wantLayers, 2*defaultFPS)
	return govpx.VP9RTPPayloadDescriptor{}
}

func sendWebRTCControlForOracle(
	t *testing.T,
	dc *webrtc.DataChannel,
	payload string,
) {
	t.Helper()
	if err := dc.SendText(payload); err != nil {
		t.Fatalf("send control %s: %v", payload, err)
	}
}

func reassembleWebRTCRTPAccessUnitForOracle(
	t *testing.T,
	packets []*rtp.Packet,
	spatialLayers int,
) []byte {
	t.Helper()
	if spatialLayers <= 0 || spatialLayers > govpx.VP9RTPMaxSpatialLayers {
		t.Fatalf("spatialLayers = %d, want 1..%d",
			spatialLayers, govpx.VP9RTPMaxSpatialLayers)
	}
	var frames [govpx.VP9RTPMaxSpatialLayers][]byte
	var sawStart [govpx.VP9RTPMaxSpatialLayers]bool
	var sawEnd [govpx.VP9RTPMaxSpatialLayers]bool
	for i, rtpPacket := range packets {
		var packet codecs.VP9Packet
		fragment, err := packet.Unmarshal(rtpPacket.Payload)
		if err != nil {
			t.Fatalf("Pion live VP9Packet.Unmarshal[%d]: %v", i, err)
		}
		if !packet.I || !packet.L {
			t.Fatalf("Pion live VP9 packet %d = I:%t L:%t, want PictureID and layer metadata",
				i, packet.I, packet.L)
		}
		layerID := int(packet.SID)
		if layerID >= spatialLayers {
			t.Fatalf("Pion live VP9 packet %d spatial id = %d, want < %d",
				i, layerID, spatialLayers)
		}
		if packet.B {
			if sawStart[layerID] {
				t.Fatalf("Pion live VP9 packet %d repeated layer %d start",
					i, layerID)
			}
			sawStart[layerID] = true
		} else if !sawStart[layerID] {
			t.Fatalf("Pion live VP9 packet %d layer %d fragment before start",
				i, layerID)
		}
		if packet.E {
			sawEnd[layerID] = true
		}
		frames[layerID] = append(frames[layerID], fragment...)
	}
	for layerID := 0; layerID < spatialLayers; layerID++ {
		if !sawStart[layerID] || !sawEnd[layerID] {
			t.Fatalf("Pion live VP9 layer %d start/end = %t/%t, want true/true",
				layerID, sawStart[layerID], sawEnd[layerID])
		}
	}
	need, err := govpx.VP9SuperframeSize(frames[:spatialLayers]...)
	if err != nil {
		t.Fatalf("live VP9SuperframeSize: %v", err)
	}
	packet := make([]byte, need)
	n, err := govpx.PackVP9SuperframeInto(packet, frames[:spatialLayers]...)
	if err != nil {
		t.Fatalf("live PackVP9SuperframeInto: %v", err)
	}
	return packet[:n]
}
