package codec_test

import (
	"bytes"
	"errors"
	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"testing"
)

func TestVP9SpatialSVCEncoderLayerRuntimeControlSettersNoAlloc(t *testing.T) {
	cbrLayer := func(width, height, kbps int) govpx.VP9EncoderOptions {
		return govpx.VP9EncoderOptions{
			Width:               width,
			Height:              height,
			RateControlModeSet:  true,
			RateControlMode:     govpx.RateControlCBR,
			TargetBitrateKbps:   kbps,
			MinQuantizer:        4,
			MaxQuantizer:        56,
			MaxKeyframeInterval: 128,
			Threads:             2,
		}
	}
	vbrLayer := func(width, height, kbps int) govpx.VP9EncoderOptions {
		opts := cbrLayer(width, height, kbps)
		opts.RateControlMode = govpx.RateControlVBR
		return opts
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			cbrLayer(32, 32, 300),
			vbrLayer(64, 64, 700),
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	stats := finalizedVP9TwoPassStatsForTest(100, 120, 90, 110)
	activeOut := make([]uint8, 4)

	tests := []struct {
		name string
		fn   func() error
	}{
		{name: "SetInterLayerPrediction", fn: func() error { return svc.SetInterLayerPrediction(true) }},
		{name: "SetLayerCQLevel", fn: func() error { return svc.SetLayerCQLevel(1, 24) }},
		{name: "SetLayerAQMode", fn: func() error { return svc.SetLayerAQMode(1, govpx.VP9AQComplexity) }},
		{name: "SetLayerAutoAltRefFalse", fn: func() error { return svc.SetLayerAutoAltRef(0, false) }},
		{name: "SetLayerFrameParallelDecoding", fn: func() error {
			return svc.SetLayerFrameParallelDecoding(0, false)
		}},
		{name: "SetLayerFrameParallelEncoderThreads", fn: func() error {
			return svc.SetLayerFrameParallelEncoderThreads(0, 1)
		}},
		{name: "SetLayerFrameDropAllowed", fn: func() error { return svc.SetLayerFrameDropAllowed(0, false) }},
		{name: "SetLayerRateControlBuffer", fn: func() error { return svc.SetLayerRateControlBuffer(0, 320, 160, 240) }},
		{name: "SetLayerPostEncodeDrop", fn: func() error { return svc.SetLayerPostEncodeDrop(0, false) }},
		{name: "SetLayerDisableOvershootMaxQCBR", fn: func() error {
			return svc.SetLayerDisableOvershootMaxQCBR(0, true)
		}},
		{name: "SetLayerMaxIntraBitratePct", fn: func() error { return svc.SetLayerMaxIntraBitratePct(0, 180) }},
		{name: "SetLayerMaxInterBitratePct", fn: func() error { return svc.SetLayerMaxInterBitratePct(1, 220) }},
		{name: "SetLayerGFCBRBoostPct", fn: func() error { return svc.SetLayerGFCBRBoostPct(0, 45) }},
		{name: "SetLayerRealtimeTarget", fn: func() error {
			return svc.SetLayerRealtimeTarget(0, govpx.RealtimeTarget{
				BitrateKbps:  360,
				FPS:          24,
				Width:        32,
				Height:       32,
				MinQuantizer: 6,
				MaxQuantizer: 54,
				FrameDrop:    govpx.RealtimeFrameDropDisabled,
			})
		}},
		{name: "SetLayerTwoPassStats", fn: func() error { return svc.SetLayerTwoPassStats(1, stats) }},
		{name: "SetLayerDeadline", fn: func() error { return svc.SetLayerDeadline(0, govpx.DeadlineRealtime) }},
		{name: "SetLayerCPUUsed", fn: func() error { return svc.SetLayerCPUUsed(0, 4) }},
		{name: "SetLayerTuning", fn: func() error { return svc.SetLayerTuning(0, govpx.TuneSSIM) }},
		{name: "SetLayerColorSpace", fn: func() error { return svc.SetLayerColorSpace(0, govpx.VP9ColorSpaceBT709) }},
		{name: "SetLayerColorRange", fn: func() error { return svc.SetLayerColorRange(0, govpx.VP9ColorRangeFull) }},
		{name: "SetLayerRenderSize", fn: func() error { return svc.SetLayerRenderSize(0, 30, 28) }},
		{name: "SetLayerTargetLevel", fn: func() error { return svc.SetLayerTargetLevel(0, 31) }},
		{name: "SetLayerDisableLoopfilter", fn: func() error {
			return svc.SetLayerDisableLoopfilter(0, govpx.VP9LoopfilterDisableInter)
		}},
		{name: "SetLayerLossless", fn: func() error { return svc.SetLayerLossless(0, true) }},
		{name: "SetLayerScreenContentMode", fn: func() error { return svc.SetLayerScreenContentMode(0, 1) }},
		{name: "SetLayerNoiseSensitivityZero", fn: func() error { return svc.SetLayerNoiseSensitivity(0, 0) }},
		{name: "SetLayerSharpness", fn: func() error { return svc.SetLayerSharpness(0, 4) }},
		{name: "SetLayerStaticThreshold", fn: func() error { return svc.SetLayerStaticThreshold(0, 512) }},
		{name: "SetLayerKeyFrameInterval", fn: func() error { return svc.SetLayerKeyFrameInterval(0, 7) }},
		{name: "SetLayerKeyFrameIntervalRange", fn: func() error {
			return svc.SetLayerKeyFrameIntervalRange(0, 1, 7)
		}},
		{name: "SetLayerAdaptiveKeyFrames", fn: func() error {
			return svc.SetLayerAdaptiveKeyFrames(0, true)
		}},
		{name: "SetLayerRTCExternalRateControl", fn: func() error {
			return svc.SetLayerRTCExternalRateControl(0, true)
		}},
		{name: "SetLayerRowMT", fn: func() error { return svc.SetLayerRowMT(0, true) }},
		{name: "SetLayerARNR", fn: func() error { return svc.SetLayerARNR(1, 3, 4, 2) }},
		{name: "SetLayerMinGFInterval", fn: func() error { return svc.SetLayerMinGFInterval(1, 4) }},
		{name: "SetLayerMaxGFInterval", fn: func() error { return svc.SetLayerMaxGFInterval(1, 12) }},
		{name: "SetLayerFramePeriodicBoost", fn: func() error { return svc.SetLayerFramePeriodicBoost(1, true) }},
		{name: "SetLayerAltRefAQ", fn: func() error { return svc.SetLayerAltRefAQ(1, true) }},
		{name: "SetLayerEnableKeyFrameFiltering", fn: func() error {
			return svc.SetLayerEnableKeyFrameFiltering(1, true)
		}},
		{name: "SetLayerEnableTPLFalse", fn: func() error { return svc.SetLayerEnableTPL(0, false) }},
		{name: "SetLayerNextFrameQIndex", fn: func() error { return svc.SetLayerNextFrameQIndex(0, 96) }},
		{name: "SetLayerDeltaQUV", fn: func() error { return svc.SetLayerDeltaQUV(1, 4) }},
		{name: "GetLayerActiveMap", fn: func() error { return svc.GetLayerActiveMap(0, activeOut, 2, 2) }},
	}
	for _, tc := range tests {
		allocs := testing.AllocsPerRun(100, func() {
			if err := tc.fn(); err != nil {
				t.Fatalf("%s returned error: %v", tc.name, err)
			}
		})
		if allocs != 0 {
			t.Fatalf("%s allocations = %v, want 0", tc.name, allocs)
		}
	}
}

func TestVP9SpatialSVCEncoderSteadyStateNoAlloc(t *testing.T) {
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{Width: 32, Height: 32},
			{Width: 64, Height: 64},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 80, 128, 128),
		vp9test.NewYCbCr(64, 64, 80, 128, 128),
	}
	dst := make([]byte, 1<<20)
	for i := range 3 {
		if _, err := svc.EncodeIntoWithResult(srcs, dst); err != nil {
			t.Fatalf("warmup EncodeIntoWithResult %d: %v", i, err)
		}
	}
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRunsForTest, func() {
		if _, err := svc.EncodeIntoWithResult(srcs, dst); err != nil {
			t.Fatalf("alloc run EncodeIntoWithResult: %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 spatial SVC steady state allocs = %f, want 0", allocs)
	}
}

func TestVP9SpatialSVCEncodeResultPacketizeRTP(t *testing.T) {
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{Width: 32, Height: 32, Lossless: true},
			{Width: 64, Height: 64, Lossless: true},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult([]*image.YCbCr{
		vp9test.NewCheckerYCbCr(32, 32, 40, 200, 120, 136),
		vp9test.NewCheckerYCbCr(64, 64, 40, 200, 120, 136),
	}, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}

	const mtu = 96
	packets, payloadBytes, err := result.RTPPacketizationSize(mtu)
	if err != nil {
		t.Fatalf("RTPPacketizationSize: %v", err)
	}
	if packets <= int(result.LayerCount) || payloadBytes <= len(result.Layers[0].Data)+len(result.Layers[1].Data) {
		t.Fatalf("packetization size = packets:%d bytes:%d, want fragmented payloads",
			packets, payloadBytes)
	}
	shortPackets := make([]govpx.RTPPayloadFragment, packets-1)
	payloadBuf := make([]byte, payloadBytes)
	gotPackets, gotBytes, err := result.PacketizeRTPInto(shortPackets,
		payloadBuf, mtu)
	if !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("short PacketizeRTPInto err = %v, want ErrBufferTooSmall", err)
	}
	if gotPackets != packets || gotBytes != payloadBytes {
		t.Fatalf("short PacketizeRTPInto need = %d/%d, want %d/%d",
			gotPackets, gotBytes, packets, payloadBytes)
	}

	payloads := make([]govpx.RTPPayloadFragment, packets)
	n, used, err := result.PacketizeRTPInto(payloads, payloadBuf, mtu)
	if err != nil {
		t.Fatalf("PacketizeRTPInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeRTPInto returned = %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}
	var byLayer [govpx.VP9MaxSpatialLayers][]govpx.RTPPayloadFragment
	var seen [govpx.VP9MaxSpatialLayers]int
	prevSpatial := uint8(0)
	for i, payload := range payloads {
		if len(payload.Payload) > mtu {
			t.Fatalf("payload %d length = %d, exceeds mtu %d",
				i, len(payload.Payload), mtu)
		}
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %v, want %v", i, got, want)
		}
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if desc.SpatialID < prevSpatial {
			t.Fatalf("payload %d spatial id = %d after %d",
				i, desc.SpatialID, prevSpatial)
		}
		prevSpatial = desc.SpatialID
		if desc.SpatialID >= result.LayerCount {
			t.Fatalf("payload %d spatial id = %d, want < %d",
				i, desc.SpatialID, result.LayerCount)
		}
		layerID := int(desc.SpatialID)
		seen[layerID]++
		if layerID == 0 && seen[layerID] == 1 {
			if !desc.ScalabilityStructurePresent ||
				desc.ScalabilityStructure.SpatialLayerCount != 2 ||
				desc.ScalabilityStructure.Width[0] != 32 ||
				desc.ScalabilityStructure.Width[1] != 64 {
				t.Fatalf("base first descriptor = %+v", desc)
			}
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d layer %d repeated scalability structure",
				i, layerID)
		}
		if layerID == 1 && !desc.InterLayerDependency {
			t.Fatalf("enhancement payload %d missing inter-layer dependency", i)
		}
		byLayer[layerID] = append(byLayer[layerID], payload)
	}
	for layerID := 0; layerID < int(result.LayerCount); layerID++ {
		assembled, err := govpx.AssembleVP9RTPFrame(byLayer[layerID])
		if err != nil {
			t.Fatalf("AssembleVP9RTPFrame layer %d: %v", layerID, err)
		}
		if !bytes.Equal(assembled, result.Layers[layerID].Data) {
			t.Fatalf("assembled layer %d does not match encoded layer", layerID)
		}
	}

	allocPayloads, err := result.PacketizeRTP(mtu)
	if err != nil {
		t.Fatalf("PacketizeRTP: %v", err)
	}
	if len(allocPayloads) != packets {
		t.Fatalf("PacketizeRTP payloads = %d, want %d", len(allocPayloads), packets)
	}
}

func TestVP9SpatialSVCRTPPacketizeSteadyStateNoAlloc(t *testing.T) {
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{Width: 32, Height: 32},
			{Width: 64, Height: 64},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 80, 128, 128),
		vp9test.NewYCbCr(64, 64, 80, 128, 128),
	}
	dst := make([]byte, 1<<20)
	result, err := svc.EncodeIntoWithResult(srcs, dst)
	if err != nil {
		t.Fatalf("EncodeIntoWithResult: %v", err)
	}
	const mtu = 80
	packets, payloadBytes, err := result.RTPPacketizationSize(mtu)
	if err != nil {
		t.Fatalf("RTPPacketizationSize: %v", err)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	if _, _, err := result.PacketizeRTPInto(payloads, payloadBuf, mtu); err != nil {
		t.Fatalf("warmup PacketizeRTPInto: %v", err)
	}
	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRunsForTest, func() {
		n, used, err := result.PacketizeRTPInto(payloads, payloadBuf, mtu)
		if err != nil {
			t.Fatalf("alloc run PacketizeRTPInto: %v", err)
		}
		if n != packets || used != payloadBytes {
			t.Fatalf("alloc run PacketizeRTPInto returned %d/%d, want %d/%d",
				n, used, packets, payloadBytes)
		}
	})
	if allocs != 0 {
		t.Fatalf("VP9 spatial SVC RTP packetize allocs = %f, want 0", allocs)
	}
}
