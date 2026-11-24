package main

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/pion/rtp/codecs"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

const vp9WebRTCRefFinderMaxTemporalLayersForTest = 8

func TestPlainVP9PacketizedStreamPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const width, height = 64, 64
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               defaultFPS,
		Deadline:          govpx.DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 500,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      2048,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	refFinder := newWebRTCVP9RefFinderForTest()
	pictureID := uint16(govpx.VP9RTPPictureID15BitMask - 2)
	for frame := 0; frame < 16; frame++ {
		if frame == 8 {
			encoder.ForceKeyFrame()
		}
		result, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*9), byte(220-frame*5),
			byte(96+frame*3), byte(188-frame*4)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		payloads, err := result.PacketizeWebRTCRTP(pictureID, 500)
		if err != nil {
			t.Fatalf("PacketizeWebRTCRTP frame %d: %v", frame, err)
		}
		refFinder.acceptPlainAccessUnit(t, frame, payloads, pictureID)
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
	}
}

func TestPlainVP9WebRTCPacketizerPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const width, height = 64, 64
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               defaultFPS,
		Deadline:          govpx.DeadlineRealtime,
		CpuUsed:           8,
		TargetBitrateKbps: 500,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      2048,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	packetizer := govpx.NewVP9WebRTCPacketizer(
		govpx.VP9RTPPictureID15BitMask - 2)
	refFinder := newWebRTCVP9RefFinderForTest()
	for frame := 0; frame < 16; frame++ {
		if frame == 8 {
			encoder.ForceKeyFrame()
		}
		result, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*9), byte(220-frame*5),
			byte(96+frame*3), byte(188-frame*4)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		pictureID := packetizer.PictureID()
		payloads, sent, err := packetizer.Packetize(result, 500)
		if err != nil {
			t.Fatalf("Packetize frame %d: %v", frame, err)
		}
		if !sent {
			t.Fatalf("Packetize frame %d reported unsent", frame)
		}
		refFinder.acceptPlainAccessUnit(t, frame, payloads, pictureID)
	}
}

func TestPionVP9SamplePayloaderOmitsGovpxSVCWebRTCMetadata(t *testing.T) {
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
		imgs[i] = image.NewYCbCr(
			image.Rect(0, 0, layerDims[i][0], layerDims[i][1]),
			image.YCbCrSubsampleRatio420)
	}
	drawScene(imgs, 0)
	result, err := svc.EncodeActiveLayersIntoWithResult(imgs,
		make([]byte, superframeBudget()), spatialLayerCount)
	if err != nil {
		t.Fatalf("EncodeActiveLayersIntoWithResult: %v", err)
	}
	if int(result.LayerCount) != spatialLayerCount {
		t.Fatalf("SVC layer count = %d, want %d",
			result.LayerCount, spatialLayerCount)
	}

	var pionPayloader codecs.VP9Payloader
	rawPayloads := pionPayloader.Payload(500, result.Data)
	if len(rawPayloads) == 0 {
		t.Fatal("Pion VP9Payloader returned no payloads for raw SVC access unit")
	}
	var rawPacket codecs.VP9Packet
	if _, err := rawPacket.Unmarshal(rawPayloads[0]); err != nil {
		t.Fatalf("Pion raw VP9Packet.Unmarshal: %v", err)
	}
	if rawPacket.L {
		t.Fatal("Pion raw VP9 sample payloader unexpectedly carried layer indices")
	}
	if rawPacket.V && int(rawPacket.NS)+1 == spatialLayerCount {
		t.Fatalf("Pion raw VP9 sample payloader advertised %d spatial layers; want not SVC-aware",
			int(rawPacket.NS)+1)
	}

	packetizer := govpx.NewVP9WebRTCPacketizer(0x120)
	packets, payloadBytes, err := packetizer.
		SpatialSVCWebRTCNonFlexiblePacketizationSize(result, 500)
	if err != nil {
		t.Fatalf("SpatialSVCWebRTCNonFlexiblePacketizationSize: %v", err)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	n, used, err := packetizer.PacketizeSpatialSVCWebRTCNonFlexibleInto(
		result, payloads, payloadBuf, 500)
	if err != nil {
		t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}

	var seenStart [spatialLayerCount]bool
	for i, payload := range payloads[:n] {
		var packet codecs.VP9Packet
		if _, err := packet.Unmarshal(payload.Payload); err != nil {
			t.Fatalf("govpx VP9Packet.Unmarshal[%d]: %v", i, err)
		}
		if packet.F {
			t.Fatalf("govpx payload %d used flexible mode, want non-flexible SVC",
				i)
		}
		if !packet.L {
			t.Fatalf("govpx payload %d missing layer indices", i)
		}
		if int(packet.SID) >= spatialLayerCount {
			t.Fatalf("govpx payload %d spatial id = %d, want < %d",
				i, packet.SID, spatialLayerCount)
		}
		if packet.B {
			seenStart[packet.SID] = true
			if packet.SID == 0 {
				if !packet.V || int(packet.NS)+1 != spatialLayerCount {
					t.Fatalf("govpx base start SS = V:%t layers:%d, want %d",
						packet.V, int(packet.NS)+1,
						spatialLayerCount)
				}
			} else if !packet.D {
				t.Fatalf("govpx enhancement start sid %d missing inter-layer dependency",
					packet.SID)
			}
		}
	}
	for layer := range seenStart {
		if !seenStart[layer] {
			t.Fatalf("govpx packetized access unit missing layer %d start",
				layer)
		}
	}
}

func TestPlainVP9PacketizedCBRDropsPassLibwebrtcVP9RefFinder(t *testing.T) {
	packets, sentFrames, droppedFrames := plainVP9WebRTCCBRDropStreamForTest(
		t, 48, 500, true)
	if droppedFrames == 0 {
		t.Fatal("test did not produce a VP9 CBR dropped frame")
	}
	if sentFrames != len(packets) {
		t.Fatalf("sent frames = %d, assembled packets = %d",
			sentFrames, len(packets))
	}
	if sentFrames < 8 {
		t.Fatalf("sent frames after CBR drops = %d, want at least 8",
			sentFrames)
	}
}

func TestWebRTCPacketizedSVCPassesLibwebrtcVP9RefFinder(t *testing.T) {
	steps := []struct {
		cap      int
		forceKey bool
	}{
		{cap: 3, forceKey: true},
		{cap: 3},
		{cap: 3},
		{cap: 3},
		{cap: 1},
		{cap: 1},
		{cap: 1},
		{cap: 3},
		{cap: 3},
		{cap: 2},
		{cap: 2},
		{cap: 3},
		{cap: 3, forceKey: true},
		{cap: 3},
		{cap: 3},
		{cap: 3},
	}
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
	refFinder := newWebRTCVP9RefFinderForTest()
	pictureID := uint16(govpx.VP9RTPPictureID15BitMask - 3)
	lastCap := steps[0].cap
	for frame, step := range steps {
		if frame == 0 || step.forceKey || step.cap != lastCap {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeActiveLayersIntoWithResult(imgs, dst, step.cap)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
				frame, step.cap, err)
		}
		if int(result.LayerCount) != step.cap {
			t.Fatalf("frame %d active layer count = %d, want %d",
				frame, result.LayerCount, step.cap)
		}
		payloads := packetizeWebRTCSVCResultForTest(t, result, pictureID, 500)
		refFinder.acceptAccessUnit(t, frame, result, payloads, pictureID)
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
		lastCap = step.cap
	}
}

func TestVP9WebRTCPacketizerSVCPassesLibwebrtcVP9RefFinder(t *testing.T) {
	steps := []struct {
		cap      int
		forceKey bool
	}{
		{cap: 3, forceKey: true},
		{cap: 3},
		{cap: 3},
		{cap: 3},
		{cap: 2},
		{cap: 2},
		{cap: 1},
		{cap: 1},
		{cap: 3},
		{cap: 3},
		{cap: 3, forceKey: true},
		{cap: 2},
		{cap: 2},
		{cap: 3},
		{cap: 3},
		{cap: 1},
		{cap: 1},
		{cap: 3},
		{cap: 3},
		{cap: 3},
		{cap: 3, forceKey: true},
		{cap: 2},
		{cap: 2},
		{cap: 3},
	}
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
	packetizer := govpx.NewVP9WebRTCPacketizer(govpx.VP9RTPPictureID15BitMask - 4)
	refFinder := newWebRTCVP9RefFinderForTest()
	var payloads []govpx.RTPPayloadFragment
	var payloadBuf []byte
	lastCap := steps[0].cap
	prevPictureID := packetizer.PictureID()
	sawPictureIDWrap := false
	sawFlexiblePrediction := false
	for frame, step := range steps {
		if frame == 0 || step.forceKey || step.cap != lastCap {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeActiveLayersIntoWithResult(imgs, dst, step.cap)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
				frame, step.cap, err)
		}
		if int(result.LayerCount) != step.cap {
			t.Fatalf("frame %d active layer count = %d, want %d",
				frame, result.LayerCount, step.cap)
		}

		pictureID := packetizer.PictureID()
		if frame > 0 && pictureID < prevPictureID {
			sawPictureIDWrap = true
		}
		packetCount, payloadBytes, err := packetizer.SpatialSVCWebRTCPacketizationSize(
			result, 500)
		if err != nil {
			t.Fatalf("SpatialSVCWebRTCPacketizationSize frame %d: %v",
				frame, err)
		}
		if got := packetizer.PictureID(); got != pictureID {
			t.Fatalf("size query frame %d advanced PictureID to %d, want %d",
				frame, got, pictureID)
		}
		if cap(payloads) < packetCount {
			payloads = make([]govpx.RTPPayloadFragment, packetCount)
		}
		payloads = payloads[:packetCount]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		writtenPackets, writtenBytes, err := packetizer.PacketizeSpatialSVCWebRTCInto(
			result, payloads, payloadBuf, 500)
		if err != nil {
			t.Fatalf("PacketizeSpatialSVCWebRTCInto frame %d: %v", frame, err)
		}
		if writtenPackets != packetCount || writtenBytes != payloadBytes {
			t.Fatalf("PacketizeSpatialSVCWebRTCInto frame %d returned %d/%d, want %d/%d",
				frame, writtenPackets, writtenBytes, packetCount, payloadBytes)
		}
		payloads = payloads[:writtenPackets]
		for i, payload := range payloads {
			desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
			if err != nil {
				t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
					frame, i, err)
			}
			if desc.StartOfFrame && !desc.FlexibleMode {
				t.Fatalf("frame %d packet %d used non-flexible VP9 descriptor",
					frame, i)
			}
			if desc.StartOfFrame && desc.InterPicturePredicted &&
				desc.ReferenceIndexCount > 0 {
				sawFlexiblePrediction = true
			}
		}
		refFinder.acceptAccessUnit(t, frame, result, payloads, pictureID)
		if got, want := packetizer.PictureID(),
			govpx.NextVP9RTPPictureID(pictureID); got != want {
			t.Fatalf("frame %d next PictureID = %d, want %d",
				frame, got, want)
		}
		prevPictureID = pictureID
		lastCap = step.cap
	}
	if !sawPictureIDWrap {
		t.Fatal("stateful VP9 WebRTC packetizer test did not cross PictureID wrap")
	}
	if !sawFlexiblePrediction {
		t.Fatal("stateful VP9 WebRTC packetizer test did not exercise flexible P-diff refs")
	}
}

func TestVP9WebRTCPacketizerSVCNonFlexiblePassesLibwebrtcVP9RefFinder(t *testing.T) {
	steps := []struct {
		cap      int
		forceKey bool
	}{
		{cap: 3, forceKey: true},
		{cap: 3},
		{cap: 3},
		{cap: 3},
		{cap: 2},
		{cap: 2},
		{cap: 1},
		{cap: 1},
		{cap: 3},
		{cap: 3},
		{cap: 3, forceKey: true},
		{cap: 2},
		{cap: 2},
		{cap: 3},
		{cap: 3},
		{cap: 1},
		{cap: 1},
		{cap: 3},
		{cap: 3},
	}
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
	packetizer := govpx.NewVP9WebRTCPacketizer(govpx.VP9RTPPictureID15BitMask - 4)
	refFinder := newWebRTCVP9RefFinderForTest()
	var payloads []govpx.RTPPayloadFragment
	var payloadBuf []byte
	lastCap := steps[0].cap
	prevPictureID := packetizer.PictureID()
	sawPictureIDWrap := false
	sawNonFlexibleGOF := false
	for frame, step := range steps {
		if frame == 0 || step.forceKey || step.cap != lastCap {
			forceKeyAll(svc)
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeActiveLayersIntoWithResult(imgs, dst, step.cap)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
				frame, step.cap, err)
		}
		if int(result.LayerCount) != step.cap {
			t.Fatalf("frame %d active layer count = %d, want %d",
				frame, result.LayerCount, step.cap)
		}

		pictureID := packetizer.PictureID()
		if frame > 0 && pictureID < prevPictureID {
			sawPictureIDWrap = true
		}
		packetCount, payloadBytes, err := packetizer.
			SpatialSVCWebRTCNonFlexiblePacketizationSize(result, 500)
		if err != nil {
			t.Fatalf("SpatialSVCWebRTCNonFlexiblePacketizationSize frame %d: %v",
				frame, err)
		}
		if got := packetizer.PictureID(); got != pictureID {
			t.Fatalf("size query frame %d advanced PictureID to %d, want %d",
				frame, got, pictureID)
		}
		if cap(payloads) < packetCount {
			payloads = make([]govpx.RTPPayloadFragment, packetCount)
		}
		payloads = payloads[:packetCount]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		writtenPackets, writtenBytes, err := packetizer.
			PacketizeSpatialSVCWebRTCNonFlexibleInto(result, payloads,
				payloadBuf, 500)
		if err != nil {
			t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto frame %d: %v",
				frame, err)
		}
		if writtenPackets != packetCount || writtenBytes != payloadBytes {
			t.Fatalf("PacketizeSpatialSVCWebRTCNonFlexibleInto frame %d returned %d/%d, want %d/%d",
				frame, writtenPackets, writtenBytes, packetCount, payloadBytes)
		}
		payloads = payloads[:writtenPackets]
		for i, payload := range payloads {
			desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
			if err != nil {
				t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
					frame, i, err)
			}
			if desc.StartOfFrame && desc.FlexibleMode {
				t.Fatalf("frame %d packet %d used flexible VP9 descriptor",
					frame, i)
			}
			if desc.StartOfFrame && desc.SpatialID == 0 &&
				desc.ScalabilityStructurePresent &&
				desc.ScalabilityStructure.PictureGroupPresent {
				sawNonFlexibleGOF = true
			}
		}
		refFinder.acceptAccessUnit(t, frame, result, payloads, pictureID)
		if got, want := packetizer.PictureID(),
			govpx.NextVP9RTPPictureID(pictureID); got != want {
			t.Fatalf("frame %d next PictureID = %d, want %d",
				frame, got, want)
		}
		prevPictureID = pictureID
		lastCap = step.cap
	}
	if !sawPictureIDWrap {
		t.Fatal("non-flexible stateful VP9 WebRTC packetizer test did not cross PictureID wrap")
	}
	if !sawNonFlexibleGOF {
		t.Fatal("non-flexible stateful VP9 WebRTC packetizer test did not carry GOF metadata")
	}
}

func TestVP9WebRTCPacketizerSVCDefaultKeyIntervalPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const defaultKeyFrameInterval = 128
	steps := webRTCDefaultKeyIntervalRefFinderSteps(12)
	seen := runVP9WebRTCPacketizerSVCRefFinderScenario(t,
		steps, govpx.VP9RTPPictureID15BitMask-3, -1, false,
		vp9WebRTCFlexiblePacketizerForTest)
	if !seen.sawPictureIDWrap {
		t.Fatal("stateful VP9 WebRTC ref-finder stream did not cross PictureID wrap")
	}
	if !seen.sawFlexiblePrediction {
		t.Fatal("stateful VP9 WebRTC ref-finder stream did not exercise flexible P-diff refs")
	}
	for _, frame := range []int{0, defaultKeyFrameInterval} {
		if !seen.keyFrames[frame] {
			t.Fatalf("stateful VP9 WebRTC ref-finder stream did not emit key access unit at frame %d",
				frame)
		}
	}
}

func TestVP9WebRTCPacketizerSVCRecoveryAfterKeyIntervalUnsentAccessUnitPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const defaultKeyFrameInterval = 128
	const unsentFrame = defaultKeyFrameInterval + 4
	steps := webRTCDefaultKeyIntervalRefFinderSteps(18)
	seen := runVP9WebRTCPacketizerSVCRefFinderScenario(t,
		steps, govpx.VP9RTPPictureID15BitMask-9, unsentFrame, false,
		vp9WebRTCFlexiblePacketizerForTest)
	if !seen.sawRecoveryAfterUnsent {
		t.Fatal("stateful VP9 WebRTC ref-finder stream did not emit recovery key after unsent access unit")
	}
	if !seen.sawFlexiblePrediction {
		t.Fatal("stateful VP9 WebRTC ref-finder recovery stream did not exercise flexible P-diff refs")
	}
	if !seen.keyFrames[defaultKeyFrameInterval] {
		t.Fatalf("stateful VP9 WebRTC ref-finder recovery stream did not emit key access unit at frame %d",
			defaultKeyFrameInterval)
	}
}

func TestVP9WebRTCPacketizerSVCNonFlexibleRecoveryAfterKeyIntervalUnsentAccessUnitPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const defaultKeyFrameInterval = 128
	const unsentFrame = defaultKeyFrameInterval + 4
	steps := webRTCDefaultKeyIntervalRefFinderSteps(18)
	seen := runVP9WebRTCPacketizerSVCRefFinderScenario(t,
		steps, govpx.VP9RTPPictureID15BitMask-11, unsentFrame, false,
		vp9WebRTCNonFlexiblePacketizerForTest)
	if !seen.sawRecoveryAfterUnsent {
		t.Fatal("non-flexible VP9 WebRTC ref-finder stream did not emit recovery key after unsent access unit")
	}
	if !seen.sawNonFlexibleGOF {
		t.Fatal("non-flexible VP9 WebRTC ref-finder recovery stream did not carry GOF metadata")
	}
	if !seen.keyFrames[defaultKeyFrameInterval] {
		t.Fatalf("non-flexible VP9 WebRTC ref-finder recovery stream did not emit key access unit at frame %d",
			defaultKeyFrameInterval)
	}
}

func TestVP9WebRTCPacketizerSVCRecoveryAfterPacketizedUnsentAccessUnitPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const defaultKeyFrameInterval = 128
	const unsentFrame = defaultKeyFrameInterval + 4
	steps := webRTCDefaultKeyIntervalRefFinderSteps(18)
	seen := runVP9WebRTCPacketizerSVCRefFinderScenario(t,
		steps, govpx.VP9RTPPictureID15BitMask-10, unsentFrame, true,
		vp9WebRTCFlexiblePacketizerForTest)
	if !seen.sawRecoveryAfterUnsent {
		t.Fatal("stateful VP9 WebRTC ref-finder stream did not emit recovery key after packetized unsent access unit")
	}
	if !seen.sawFlexiblePrediction {
		t.Fatal("stateful VP9 WebRTC ref-finder packetized-unsent stream did not exercise flexible P-diff refs")
	}
	if !seen.keyFrames[defaultKeyFrameInterval] {
		t.Fatalf("stateful VP9 WebRTC ref-finder packetized-unsent stream did not emit key access unit at frame %d",
			defaultKeyFrameInterval)
	}
}

func TestVP9WebRTCPacketizerSVCNonFlexibleRecoveryAfterPacketizedUnsentAccessUnitPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const defaultKeyFrameInterval = 128
	const unsentFrame = defaultKeyFrameInterval + 4
	steps := webRTCDefaultKeyIntervalRefFinderSteps(18)
	seen := runVP9WebRTCPacketizerSVCRefFinderScenario(t,
		steps, govpx.VP9RTPPictureID15BitMask-12, unsentFrame, true,
		vp9WebRTCNonFlexiblePacketizerForTest)
	if !seen.sawRecoveryAfterUnsent {
		t.Fatal("non-flexible VP9 WebRTC ref-finder stream did not emit recovery key after packetized unsent access unit")
	}
	if !seen.sawNonFlexibleGOF {
		t.Fatal("non-flexible VP9 WebRTC ref-finder packetized-unsent stream did not carry GOF metadata")
	}
	if !seen.keyFrames[defaultKeyFrameInterval] {
		t.Fatalf("non-flexible VP9 WebRTC ref-finder packetized-unsent stream did not emit key access unit at frame %d",
			defaultKeyFrameInterval)
	}
}

func TestVP9WebRTCPacketizerSVCNonFlexibleRecoveryAfterPartialWriteAccessUnitPassesLibwebrtcVP9RefFinder(t *testing.T) {
	const defaultKeyFrameInterval = 128
	const partialFrame = defaultKeyFrameInterval + 4
	steps := webRTCDefaultKeyIntervalRefFinderSteps(18)
	seen := runVP9WebRTCPacketizerSVCRefFinderScenarioWithPartialWrite(t,
		steps, govpx.VP9RTPPictureID15BitMask-13, partialFrame,
		vp9WebRTCNonFlexiblePacketizerForTest)
	if !seen.sawRecoveryAfterUnsent {
		t.Fatal("non-flexible VP9 WebRTC ref-finder stream did not emit recovery key after partial RTP write")
	}
	if !seen.sawNonFlexibleGOF {
		t.Fatal("non-flexible VP9 WebRTC ref-finder partial-write stream did not carry GOF metadata")
	}
	if !seen.keyFrames[defaultKeyFrameInterval] {
		t.Fatalf("non-flexible VP9 WebRTC ref-finder partial-write stream did not emit key access unit at frame %d",
			defaultKeyFrameInterval)
	}
}

func TestWebRTCPacketizedSVCPassesRefFinderAcrossTL0Wrap(t *testing.T) {
	svc, imgs := newSmallWebRTCSVCTestEncoder(t)
	defer svc.Close()

	dst := make([]byte, 1<<20)
	refFinder := newWebRTCVP9RefFinderForTest()
	pictureID := uint16(0x1200)
	var lastTL0 uint8
	var haveTL0 bool
	var sawWrap bool
	for frame := 0; frame < 1032; frame++ {
		drawSmallWebRTCTestFrame(imgs, frame)
		result, err := svc.EncodeIntoWithResult(imgs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		base := result.Layers[0]
		if base.TemporalLayerID == 0 {
			if haveTL0 && base.TL0PICIDX < lastTL0 {
				sawWrap = true
			}
			lastTL0 = base.TL0PICIDX
			haveTL0 = true
		}
		payloads := packetizeWebRTCSVCResultForTest(t, result, pictureID, 500)
		refFinder.acceptAccessUnit(t, frame, result, payloads, pictureID)
		pictureID = govpx.NextVP9RTPPictureID(pictureID)
	}
	if !sawWrap {
		t.Fatal("test did not cross TL0PICIDX wrap")
	}
}

type webRTCRefFinderStep struct {
	cap           int
	bitrateKbps   int
	screenMode    int
	screenModeSet bool
	forceKey      bool
}

type webRTCRefFinderScenarioSeen struct {
	keyFrames              map[int]bool
	sawFlexiblePrediction  bool
	sawNonFlexibleGOF      bool
	sawPictureIDWrap       bool
	sawRecoveryAfterUnsent bool
}

func webRTCDefaultKeyIntervalRefFinderSteps(extraFrames int) []webRTCRefFinderStep {
	const defaultKeyFrameInterval = 128
	frames := defaultKeyFrameInterval + extraFrames
	steps := make([]webRTCRefFinderStep, frames)
	for frame := range steps {
		steps[frame] = webRTCRefFinderStep{cap: spatialLayerCount}
		switch frame {
		case 17:
			steps[frame].bitrateKbps = 1200
		case 47:
			steps[frame].screenMode = 1
			steps[frame].screenModeSet = true
		case 93:
			steps[frame].bitrateKbps = 700
		case 121:
			steps[frame].screenMode = 2
			steps[frame].screenModeSet = true
		case defaultKeyFrameInterval + 7:
			steps[frame].bitrateKbps = 900
		case defaultKeyFrameInterval + 11:
			steps[frame].screenMode = 0
			steps[frame].screenModeSet = true
		}
	}
	return steps
}

func runVP9WebRTCPacketizerSVCRefFinderScenario(
	t *testing.T,
	steps []webRTCRefFinderStep,
	initialPictureID uint16,
	unsentFrame int,
	packetizedUnsent bool,
	mode vp9WebRTCTestPacketizerMode,
) webRTCRefFinderScenarioSeen {
	t.Helper()
	return runVP9WebRTCPacketizerSVCRefFinderScenarioInternal(t, steps,
		initialPictureID, unsentFrame, packetizedUnsent, -1, mode)
}

func runVP9WebRTCPacketizerSVCRefFinderScenarioWithPartialWrite(
	t *testing.T,
	steps []webRTCRefFinderStep,
	initialPictureID uint16,
	partialWriteFrame int,
	mode vp9WebRTCTestPacketizerMode,
) webRTCRefFinderScenarioSeen {
	t.Helper()
	return runVP9WebRTCPacketizerSVCRefFinderScenarioInternal(t, steps,
		initialPictureID, -1, false, partialWriteFrame, mode)
}

func runVP9WebRTCPacketizerSVCRefFinderScenarioInternal(
	t *testing.T,
	steps []webRTCRefFinderStep,
	initialPictureID uint16,
	unsentFrame int,
	packetizedUnsent bool,
	partialWriteFrame int,
	mode vp9WebRTCTestPacketizerMode,
) webRTCRefFinderScenarioSeen {
	t.Helper()
	if len(steps) == 0 {
		return webRTCRefFinderScenarioSeen{keyFrames: make(map[int]bool)}
	}
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
	packetizer := govpx.NewVP9WebRTCPacketizer(initialPictureID)
	refFinder := newWebRTCVP9RefFinderForTest()
	var payloads []govpx.RTPPayloadFragment
	var payloadBuf []byte
	lastCap := steps[0].cap
	currentBitrate := defaultBitrateKbps
	currentScreen := 0
	prevPictureID := packetizer.PictureID()
	forceNext := false
	seen := webRTCRefFinderScenarioSeen{
		keyFrames:              make(map[int]bool),
		sawRecoveryAfterUnsent: unsentFrame < 0 && partialWriteFrame < 0,
	}
	for frame, step := range steps {
		activeCap := step.cap
		if activeCap < 1 || activeCap > spatialLayerCount {
			t.Fatalf("ref-finder step %d cap = %d, want 1..%d",
				frame, activeCap, spatialLayerCount)
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
		forcedByUnsent := forceNext
		if frame > 0 && (activeCap != lastCap || step.forceKey || forceNext) {
			forceKeyAll(svc)
			forceNext = false
		}
		drawScene(imgs, frame)
		result, err := svc.EncodeActiveLayersIntoWithResult(imgs, dst, activeCap)
		if err != nil {
			t.Fatalf("EncodeActiveLayersIntoWithResult frame %d cap %d: %v",
				frame, activeCap, err)
		}
		base := result.Layers[0]
		if base.KeyFrame {
			seen.keyFrames[frame] = true
		}

		pictureID := packetizer.PictureID()
		if frame > 0 && pictureID < prevPictureID {
			seen.sawPictureIDWrap = true
		}
		packetCount, payloadBytes, err := mode.packetizationSize(
			&packetizer, result, 500)
		if err != nil {
			t.Fatalf("%s frame %d: %v", mode.packetizationSizeName(),
				frame, err)
		}
		if got := packetizer.PictureID(); got != pictureID {
			t.Fatalf("size query frame %d advanced PictureID to %d, want %d",
				frame, got, pictureID)
		}
		if frame == unsentFrame && !packetizedUnsent {
			shortPayloads := make([]govpx.RTPPayloadFragment, packetCount-1)
			if cap(payloadBuf) < payloadBytes {
				payloadBuf = make([]byte, payloadBytes)
			}
			payloadBuf = payloadBuf[:payloadBytes]
			gotPackets, gotBytes, err := mode.packetizeInto(&packetizer,
				result, shortPayloads, payloadBuf, 500)
			if !errors.Is(err, govpx.ErrBufferTooSmall) ||
				gotPackets != packetCount || gotBytes != payloadBytes {
				t.Fatalf("unsent frame %d short %s = %d/%d err:%v, want %d/%d ErrBufferTooSmall",
					frame, mode.packetizeIntoName(), gotPackets,
					gotBytes, err, packetCount, payloadBytes)
			}
			if got := packetizer.PictureID(); got != pictureID {
				t.Fatalf("unsent frame %d advanced PictureID to %d, want %d",
					frame, got, pictureID)
			}
			packetizer.MarkEncodedAccessUnitUnsent()
			if !packetizer.NeedsKeyFrame() {
				t.Fatalf("encoded unsent frame %d did not require recovery key",
					frame)
			}
			if got, want := packetizer.PictureID(),
				govpx.NextVP9RTPPictureID(pictureID); got != want {
				t.Fatalf("encoded unsent frame %d next PictureID = %d, want %d",
					frame, got, want)
			}
			forceNext = true
			lastCap = activeCap
			prevPictureID = pictureID
			continue
		}
		if cap(payloads) < packetCount {
			payloads = make([]govpx.RTPPayloadFragment, packetCount)
		}
		payloads = payloads[:packetCount]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		writtenPackets, writtenBytes, err := mode.packetizeInto(&packetizer,
			result, payloads, payloadBuf, 500)
		if err != nil {
			t.Fatalf("%s frame %d: %v", mode.packetizeIntoName(),
				frame, err)
		}
		if writtenPackets != packetCount || writtenBytes != payloadBytes {
			t.Fatalf("%s frame %d returned %d/%d, want %d/%d",
				mode.packetizeIntoName(), frame, writtenPackets,
				writtenBytes, packetCount, payloadBytes)
		}
		payloads = payloads[:writtenPackets]
		if frame == unsentFrame && packetizedUnsent {
			packetizer.MarkAccessUnitUnsent()
			if !packetizer.NeedsKeyFrame() {
				t.Fatalf("packetized unsent frame %d did not require recovery key",
					frame)
			}
			if got, want := packetizer.PictureID(),
				govpx.NextVP9RTPPictureID(pictureID); got != want {
				t.Fatalf("packetized unsent frame %d next PictureID = %d, want %d",
					frame, got, want)
			}
			forceNext = true
			lastCap = activeCap
			prevPictureID = pictureID
			continue
		}
		if frame == partialWriteFrame {
			assertWebRTCSVCPartialWritePrefixForTest(t, frame,
				payloads, mode)
			packetizer.MarkAccessUnitUnsent()
			if !packetizer.NeedsKeyFrame() {
				t.Fatalf("partial-write frame %d did not require recovery key",
					frame)
			}
			if got, want := packetizer.PictureID(),
				govpx.NextVP9RTPPictureID(pictureID); got != want {
				t.Fatalf("partial-write frame %d next PictureID = %d, want %d",
					frame, got, want)
			}
			forceNext = true
			lastCap = activeCap
			prevPictureID = pictureID
			continue
		}
		for i, payload := range payloads {
			desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
			if err != nil {
				t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
					frame, i, err)
			}
			if mode.nonFlexible() {
				if desc.StartOfFrame && desc.FlexibleMode {
					t.Fatalf("frame %d packet %d used flexible VP9 descriptor",
						frame, i)
				}
				if desc.StartOfFrame && desc.SpatialID == 0 &&
					desc.ScalabilityStructurePresent &&
					desc.ScalabilityStructure.PictureGroupPresent {
					seen.sawNonFlexibleGOF = true
				}
			} else {
				if desc.StartOfFrame && !desc.FlexibleMode {
					t.Fatalf("frame %d packet %d used non-flexible VP9 descriptor",
						frame, i)
				}
				if desc.StartOfFrame && desc.InterPicturePredicted &&
					desc.ReferenceIndexCount > 0 {
					seen.sawFlexiblePrediction = true
				}
			}
		}
		if forcedByUnsent {
			if !base.KeyFrame || base.InterPicturePredicted ||
				!base.ScalabilityStructurePresent {
				t.Fatalf("frame %d after unsent AU base = key:%t inter:%t ss:%t, want recovery key",
					frame, base.KeyFrame, base.InterPicturePredicted,
					base.ScalabilityStructurePresent)
			}
			seen.sawRecoveryAfterUnsent = true
		}
		refFinder.acceptAccessUnit(t, frame, result, payloads, pictureID)
		if got, want := packetizer.PictureID(),
			govpx.NextVP9RTPPictureID(pictureID); got != want {
			t.Fatalf("frame %d next PictureID = %d, want %d",
				frame, got, want)
		}
		prevPictureID = pictureID
		lastCap = activeCap
	}
	return seen
}

func assertWebRTCSVCPartialWritePrefixForTest(t *testing.T,
	frame int,
	payloads []govpx.RTPPayloadFragment,
	mode vp9WebRTCTestPacketizerMode,
) {
	t.Helper()
	if len(payloads) < 2 {
		t.Fatalf("frame %d payload count = %d, want multi-packet AU for partial write",
			frame, len(payloads))
	}
	payload := payloads[0]
	if payload.Marker {
		t.Fatalf("frame %d first partial-write payload has RTP marker", frame)
	}
	desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
	if err != nil {
		t.Fatalf("frame %d partial ParseVP9RTPPayloadDescriptor: %v",
			frame, err)
	}
	if !desc.StartOfFrame || !desc.PictureIDPresent ||
		!desc.LayerIndicesPresent {
		t.Fatalf("frame %d partial descriptor = %+v, want frame start with PictureID/layers",
			frame, desc)
	}
	if desc.FlexibleMode == mode.nonFlexible() {
		t.Fatalf("frame %d partial descriptor flexible = %t, want %t",
			frame, desc.FlexibleMode, !mode.nonFlexible())
	}
	var packet codecs.VP9Packet
	if _, err := packet.Unmarshal(payload.Payload); err != nil {
		t.Fatalf("frame %d partial Pion VP9Packet.Unmarshal: %v",
			frame, err)
	}
	if !packet.B {
		t.Fatalf("frame %d partial Pion payload start bit = false, want true",
			frame)
	}
}

func plainVP9WebRTCCBRDropStreamForTest(
	t *testing.T,
	frames int,
	mtu int,
	checkRefFinder bool,
) (packets [][]byte, sentFrames int, droppedFrames int) {
	t.Helper()
	const width, height = 64, 64
	encoder, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 defaultFPS,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		RateControlModeSet:  true,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   16,
		BufferSizeMs:        100,
		BufferInitialSizeMs: 10,
		BufferOptimalSizeMs: 20,
		Quantizer:           10,
		PostEncodeDrop:      true,
		TemporalScalability: govpx.TemporalScalabilityConfig{
			Enabled: true,
			Mode:    govpx.TemporalLayeringThreeLayers,
		},
		ErrorResilient:           true,
		FrameParallelDecodingSet: true,
		FrameParallelDecoding:    true,
		MaxKeyframeInterval:      128,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer encoder.Close()

	dst := make([]byte, 1<<20)
	packetizer := govpx.NewVP9WebRTCPacketizer(govpx.VP9RTPPictureID15BitMask - 2)
	refFinder := newWebRTCVP9RefFinderForTest()
	fragments := make([]govpx.RTPPayloadFragment, 0, 16)
	payloadBuf := make([]byte, 0, 4096)
	for frame := 0; frame < frames; frame++ {
		if frame != 0 && frame%17 == 0 {
			encoder.ForceKeyFrame()
		}
		result, err := encoder.EncodeIntoWithResult(vp9test.NewCheckerYCbCr(
			width, height, byte(24+frame*7), byte(224-frame*3),
			byte(96+frame*5), byte(192-frame*2)), dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		pictureID := packetizer.PictureID()
		fragmentCount, payloadBytes, sent, err := packetizer.PacketizationSize(
			result, mtu)
		if err != nil {
			t.Fatalf("PacketizationSize frame %d: %v", frame, err)
		}
		if result.Dropped {
			if sent || fragmentCount != 0 || payloadBytes != 0 {
				t.Fatalf("dropped frame %d size = packets:%d bytes:%d sent:%t",
					frame, fragmentCount, payloadBytes, sent)
			}
			droppedFrames++
			if droppedFrames == 1 {
				if err := encoder.SetPostEncodeDrop(false); err != nil {
					t.Fatalf("SetPostEncodeDrop(false): %v", err)
				}
			}
			_, _, sent, err = packetizer.PacketizeInto(result, nil, nil, mtu)
			if err != nil || sent {
				t.Fatalf("duplicate dropped PacketizeInto frame %d = sent:%t err:%v",
					frame, sent, err)
			}
			if packetizer.NeedsKeyFrame() {
				encoder.ForceKeyFrame()
			}
			continue
		}
		if !sent {
			t.Fatalf("non-dropped frame %d reported unsent size", frame)
		}
		if cap(fragments) < fragmentCount {
			fragments = make([]govpx.RTPPayloadFragment, fragmentCount)
		}
		fragments = fragments[:fragmentCount]
		if cap(payloadBuf) < payloadBytes {
			payloadBuf = make([]byte, payloadBytes)
		}
		payloadBuf = payloadBuf[:payloadBytes]
		n, used, sent, err := packetizer.PacketizeInto(result, fragments,
			payloadBuf, mtu)
		if err != nil || !sent {
			t.Fatalf("PacketizeInto frame %d = packets:%d bytes:%d sent:%t err:%v",
				frame, n, used, sent, err)
		}
		if n != fragmentCount || used != payloadBytes {
			t.Fatalf("PacketizeInto frame %d returned %d/%d, want %d/%d",
				frame, n, used, fragmentCount, payloadBytes)
		}
		payloads := fragments[:n]
		wantSS := result.KeyFrame && !result.InterPicturePredicted &&
			result.TemporalLayerID == 0
		assertPlainVP9WebRTCPionPayloadBodiesForTest(t, result, payloads,
			pictureID, width, height, wantSS)
		if checkRefFinder {
			refFinder.acceptPlainAccessUnit(t, frame, payloads, pictureID)
		}
		assembled, err := govpx.AssembleVP9RTPFrame(payloads)
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame frame %d: %v", frame, err)
		}
		if !bytes.Equal(assembled, result.Data) {
			t.Fatalf("frame %d WebRTC RTP reassembly drifted", frame)
		}
		packets = append(packets, append([]byte(nil), assembled...))
		sentFrames++
	}
	return packets, sentFrames, droppedFrames
}

func newSmallWebRTCSVCTestEncoder(t *testing.T) (
	*govpx.VP9SpatialSVCEncoder,
	[]*image.YCbCr,
) {
	t.Helper()
	dims := [3][2]int{{16, 16}, {32, 32}, {64, 64}}
	bitrates := [3]int{80, 160, 320}
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringThreeLayers,
	}
	var layers [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions
	imgs := make([]*image.YCbCr, len(dims))
	for i := range dims {
		w, h := dims[i][0], dims[i][1]
		layers[i] = govpx.VP9EncoderOptions{
			Width:                    w,
			Height:                   h,
			FPS:                      defaultFPS,
			Deadline:                 govpx.DeadlineRealtime,
			CpuUsed:                  8,
			RateControlModeSet:       true,
			RateControlMode:          govpx.RateControlCBR,
			TargetBitrateKbps:        bitrates[i],
			TemporalScalability:      temporal,
			ErrorResilient:           true,
			FrameParallelDecodingSet: true,
			FrameParallelDecoding:    true,
			MaxKeyframeInterval:      2048,
		}
		imgs[i] = image.NewYCbCr(image.Rect(0, 0, w, h),
			image.YCbCrSubsampleRatio420)
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           uint8(len(dims)),
		InterLayerPrediction: true,
		Layers:               layers,
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	return svc, imgs
}

func drawSmallWebRTCTestFrame(imgs []*image.YCbCr, frame int) {
	for layer, img := range imgs {
		for y := 0; y < img.Rect.Dy(); y++ {
			row := img.Y[y*img.YStride:]
			for x := 0; x < img.Rect.Dx(); x++ {
				row[x] = uint8(32 + (x*3+y*5+frame*7+layer*19)%192)
			}
		}
		for y := 0; y < img.Rect.Dy()/2; y++ {
			cbRow := img.Cb[y*img.CStride:]
			crRow := img.Cr[y*img.CStride:]
			for x := 0; x < img.Rect.Dx()/2; x++ {
				cbRow[x] = uint8(96 + (x*5+frame+layer*11)%64)
				crRow[x] = uint8(128 + (y*7+frame*3+layer*13)%64)
			}
		}
	}
}

type webRTCVP9RefFinderForTest struct {
	gofByTL0           map[int]*webRTCVP9GofInfoForTest
	available          map[int64]bool
	upSwitch           map[uint16]uint8
	missingFramesByTID [vp9WebRTCRefFinderMaxTemporalLayersForTest]map[uint16]bool
	lastUnwrappedTL0   int
	haveUnwrappedTL0   bool
	lastUnwrappedPicID int
	haveUnwrappedPicID bool
}

type webRTCVP9GofInfoForTest struct {
	groups        []govpx.VP9RTPPictureGroup
	pidStart      uint16
	lastPictureID uint16
}

func newWebRTCVP9RefFinderForTest() *webRTCVP9RefFinderForTest {
	f := &webRTCVP9RefFinderForTest{
		gofByTL0:  make(map[int]*webRTCVP9GofInfoForTest),
		available: make(map[int64]bool),
		upSwitch:  make(map[uint16]uint8),
	}
	for i := range f.missingFramesByTID {
		f.missingFramesByTID[i] = make(map[uint16]bool)
	}
	return f
}

func (f *webRTCVP9RefFinderForTest) acceptAccessUnit(
	t *testing.T,
	frame int,
	result govpx.VP9SpatialSVCEncodeResult,
	payloads []govpx.RTPPayloadFragment,
	pictureID uint16,
) {
	t.Helper()
	var starts []govpx.VP9RTPPayloadDescriptor
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
				frame, i, err)
		}
		if desc.StartOfFrame {
			starts = append(starts, desc)
		}
	}
	if len(starts) != int(result.LayerCount) {
		t.Fatalf("frame %d layer starts = %d, want %d",
			frame, len(starts), result.LayerCount)
	}
	for layer, desc := range starts {
		if desc.PictureID != pictureID {
			t.Fatalf("frame %d layer %d PictureID = %d, want %d",
				frame, layer, desc.PictureID, pictureID)
		}
		f.acceptFrame(t, frame, layer, desc)
	}
}

func (f *webRTCVP9RefFinderForTest) acceptPlainAccessUnit(
	t *testing.T,
	frame int,
	payloads []govpx.RTPPayloadFragment,
	pictureID uint16,
) {
	t.Helper()
	var starts []govpx.VP9RTPPayloadDescriptor
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("frame %d ParseVP9RTPPayloadDescriptor[%d]: %v",
				frame, i, err)
		}
		if desc.StartOfFrame {
			starts = append(starts, desc)
		}
	}
	if len(starts) != 1 {
		t.Fatalf("frame %d layer starts = %d, want 1", frame, len(starts))
	}
	if starts[0].PictureID != pictureID {
		t.Fatalf("frame %d PictureID = %d, want %d",
			frame, starts[0].PictureID, pictureID)
	}
	f.acceptFrame(t, frame, 0, starts[0])
}

func (f *webRTCVP9RefFinderForTest) acceptFrame(
	t *testing.T,
	frame int,
	layer int,
	desc govpx.VP9RTPPayloadDescriptor,
) {
	t.Helper()
	if !desc.LayerIndicesPresent {
		t.Fatalf("frame %d layer %d missing VP9 layer indices", frame, layer)
	}
	if int(desc.TemporalID) >= vp9WebRTCRefFinderMaxTemporalLayersForTest {
		t.Fatalf("frame %d layer %d temporal id = %d",
			frame, layer, desc.TemporalID)
	}
	tl0 := f.unwrapTL0(desc.TL0PICIDX)
	if desc.PictureIDPresent {
		_ = f.unwrapPictureID(desc.PictureID)
	}
	if desc.FlexibleMode {
		if desc.ScalabilityStructurePresent &&
			desc.ScalabilityStructure.PictureGroupPresent {
			t.Fatalf("frame %d layer %d flexible SS unexpectedly carried GOF",
				frame, layer)
		}
		if desc.InterPicturePredicted {
			if desc.ReferenceIndexCount == 0 {
				t.Fatalf("frame %d layer %d carried P=1 without flexible refs",
					frame, layer)
			}
			for i := 0; i < desc.ReferenceIndexCount; i++ {
				refPictureID := vp9WebRTCPictureIDSub(desc.PictureID,
					desc.ReferenceIndices[i])
				f.requireAvailable(t, frame, layer, refPictureID,
					desc.SpatialID, "flex")
			}
		}
		if desc.InterLayerDependency {
			if desc.SpatialID == 0 {
				t.Fatalf("frame %d layer %d base layer has inter-layer dependency",
					frame, layer)
			}
			f.requireAvailable(t, frame, layer, desc.PictureID,
				desc.SpatialID-1, "inter-layer")
		}
		f.markAvailable(desc.PictureID, desc.SpatialID)
		return
	}
	isBaseKey := !desc.InterPicturePredicted && desc.SpatialID == 0 &&
		!desc.InterLayerDependency
	var info *webRTCVP9GofInfoForTest
	if desc.ScalabilityStructurePresent && desc.TemporalID == 0 {
		info = newWebRTCVP9GofInfoForTest(desc.ScalabilityStructure,
			desc.PictureID)
		f.gofByTL0[tl0] = info
		if isBaseKey {
			f.frameReceived(desc.PictureID, info)
			f.markAvailable(desc.PictureID, desc.SpatialID)
			return
		}
	} else {
		if isBaseKey {
			t.Fatalf("frame %d layer %d keyframe reached receiver without SS",
				frame, layer)
		}
		lookupTL0 := tl0
		if desc.TemporalID == 0 && !desc.InterLayerDependency {
			lookupTL0 = tl0 - 1
		}
		info = f.gofByTL0[lookupTL0]
		if info == nil {
			t.Fatalf("frame %d layer %d missing GOF info for TL0 %d",
				frame, layer, lookupTL0)
		}
		if desc.TemporalID == 0 {
			info = &webRTCVP9GofInfoForTest{
				groups:        info.groups,
				pidStart:      desc.PictureID,
				lastPictureID: desc.PictureID,
			}
			f.gofByTL0[tl0] = info
		}
	}
	f.frameReceived(desc.PictureID, info)
	gofIdx := f.gofIndex(desc.PictureID, info)
	if f.missingRequiredFrame(desc.PictureID, info, gofIdx) {
		t.Fatalf("frame %d layer %d would be stashed by libwebrtc VP9 ref finder",
			frame, layer)
	}
	if desc.SwitchingUpPoint {
		f.upSwitch[desc.PictureID] = desc.TemporalID
	}
	if desc.InterPicturePredicted {
		group := info.groups[gofIdx]
		for i := 0; i < group.ReferenceIndexCount; i++ {
			refPictureID := vp9WebRTCPictureIDSub(desc.PictureID,
				group.ReferenceIndices[i])
			if f.upSwitchInInterval(desc.PictureID, desc.TemporalID,
				refPictureID) {
				continue
			}
			f.requireAvailable(t, frame, layer, refPictureID,
				desc.SpatialID, "GOF")
		}
	}
	if desc.InterLayerDependency {
		if desc.SpatialID == 0 {
			t.Fatalf("frame %d layer %d base layer has inter-layer dependency",
				frame, layer)
		}
		f.requireAvailable(t, frame, layer, desc.PictureID,
			desc.SpatialID-1, "inter-layer")
	}
	f.markAvailable(desc.PictureID, desc.SpatialID)
}

func newWebRTCVP9GofInfoForTest(
	ss govpx.VP9RTPScalabilityStructure,
	pictureID uint16,
) *webRTCVP9GofInfoForTest {
	groups := ss.PictureGroups
	if !ss.PictureGroupPresent || len(groups) == 0 {
		groups = []govpx.VP9RTPPictureGroup{{TemporalID: 0}}
	}
	copied := append([]govpx.VP9RTPPictureGroup(nil), groups...)
	return &webRTCVP9GofInfoForTest{
		groups:        copied,
		pidStart:      pictureID,
		lastPictureID: pictureID,
	}
}

func (f *webRTCVP9RefFinderForTest) frameReceived(
	pictureID uint16,
	info *webRTCVP9GofInfoForTest,
) {
	if vp9WebRTCPictureIDAheadOf(pictureID, info.lastPictureID) {
		gofIdx := f.gofIndex(info.lastPictureID, info)
		next := govpx.NextVP9RTPPictureID(info.lastPictureID)
		for next != pictureID {
			gofIdx = (gofIdx + 1) % len(info.groups)
			tid := info.groups[gofIdx].TemporalID
			if int(tid) < len(f.missingFramesByTID) {
				f.missingFramesByTID[tid][next] = true
			}
			next = govpx.NextVP9RTPPictureID(next)
		}
		info.lastPictureID = pictureID
		return
	}
	gofIdx := f.gofIndex(pictureID, info)
	tid := info.groups[gofIdx].TemporalID
	if int(tid) < len(f.missingFramesByTID) {
		delete(f.missingFramesByTID[tid], pictureID)
	}
}

func (f *webRTCVP9RefFinderForTest) missingRequiredFrame(
	pictureID uint16,
	info *webRTCVP9GofInfoForTest,
	gofIdx int,
) bool {
	group := info.groups[gofIdx]
	for i := 0; i < group.ReferenceIndexCount; i++ {
		refPictureID := vp9WebRTCPictureIDSub(pictureID,
			group.ReferenceIndices[i])
		for tid := uint8(0); tid < group.TemporalID; tid++ {
			for missing := range f.missingFramesByTID[tid] {
				if vp9WebRTCPictureIDAheadOf(missing, refPictureID) &&
					vp9WebRTCPictureIDAheadOf(pictureID, missing) {
					return true
				}
			}
		}
	}
	return false
}

func (f *webRTCVP9RefFinderForTest) upSwitchInInterval(
	pictureID uint16,
	temporalID uint8,
	refPictureID uint16,
) bool {
	for upSwitchID, upSwitchTemporalID := range f.upSwitch {
		if vp9WebRTCPictureIDAheadOf(upSwitchID, refPictureID) &&
			vp9WebRTCPictureIDAheadOf(pictureID, upSwitchID) &&
			upSwitchTemporalID < temporalID {
			return true
		}
	}
	return false
}

func (f *webRTCVP9RefFinderForTest) gofIndex(
	pictureID uint16,
	info *webRTCVP9GofInfoForTest,
) int {
	return vp9WebRTCPictureIDForwardDiff(info.pidStart, pictureID) %
		len(info.groups)
}

func (f *webRTCVP9RefFinderForTest) requireAvailable(
	t *testing.T,
	frame int,
	layer int,
	pictureID uint16,
	spatialID uint8,
	reason string,
) {
	t.Helper()
	if !f.available[vp9WebRTCFrameIDForTest(pictureID, spatialID)] {
		t.Fatalf("frame %d layer %d missing %s reference pid=%d sid=%d",
			frame, layer, reason, pictureID, spatialID)
	}
}

func (f *webRTCVP9RefFinderForTest) markAvailable(
	pictureID uint16,
	spatialID uint8,
) {
	f.available[vp9WebRTCFrameIDForTest(pictureID, spatialID)] = true
}

func (f *webRTCVP9RefFinderForTest) unwrapTL0(v uint8) int {
	if !f.haveUnwrappedTL0 {
		f.lastUnwrappedTL0 = int(v)
		f.haveUnwrappedTL0 = true
		return f.lastUnwrappedTL0
	}
	f.lastUnwrappedTL0 = vp9WebRTCUnwrap8ForTest(f.lastUnwrappedTL0, v)
	return f.lastUnwrappedTL0
}

func (f *webRTCVP9RefFinderForTest) unwrapPictureID(v uint16) int {
	if !f.haveUnwrappedPicID {
		f.lastUnwrappedPicID = int(v)
		f.haveUnwrappedPicID = true
		return f.lastUnwrappedPicID
	}
	f.lastUnwrappedPicID = vp9WebRTCUnwrap15ForTest(f.lastUnwrappedPicID, v)
	return f.lastUnwrappedPicID
}

func vp9WebRTCFrameIDForTest(pictureID uint16, spatialID uint8) int64 {
	return int64(pictureID)*govpx.VP9RTPMaxSpatialLayers + int64(spatialID)
}

func vp9WebRTCPictureIDSub(pictureID uint16, diff uint8) uint16 {
	mod := int(govpx.VP9RTPPictureID15BitMask) + 1
	return uint16((int(pictureID) - int(diff) + mod) % mod)
}

func vp9WebRTCPictureIDForwardDiff(from uint16, to uint16) int {
	mod := int(govpx.VP9RTPPictureID15BitMask) + 1
	return (int(to) - int(from) + mod) % mod
}

func vp9WebRTCPictureIDAheadOf(a uint16, b uint16) bool {
	diff := vp9WebRTCPictureIDForwardDiff(b, a)
	return diff > 0 && diff < (int(govpx.VP9RTPPictureID15BitMask)+1)/2
}

func vp9WebRTCUnwrap8ForTest(prev int, value uint8) int {
	return vp9WebRTCUnwrapModuloForTest(prev, int(value), 256)
}

func vp9WebRTCUnwrap15ForTest(prev int, value uint16) int {
	return vp9WebRTCUnwrapModuloForTest(prev, int(value),
		int(govpx.VP9RTPPictureID15BitMask)+1)
}

func vp9WebRTCUnwrapModuloForTest(prev int, value int, mod int) int {
	base := prev - positiveModForTest(prev, mod)
	best := base + value
	for _, candidate := range []int{best - mod, best + mod} {
		if absIntForTest(candidate-prev) < absIntForTest(best-prev) {
			best = candidate
		}
	}
	return best
}

func positiveModForTest(v int, mod int) int {
	r := v % mod
	if r < 0 {
		r += mod
	}
	return r
}

func absIntForTest(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
