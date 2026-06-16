package govpx_test

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9SpatialSVCEncodeResultPacketizeWebRTCRTP(t *testing.T) {
	results := encodeVP9WebRTCSVCTestResults(t, 2)
	result := results[0]
	const pictureID = uint16(0x8042)
	payloads, err := result.PacketizeWebRTCRTP(pictureID, 96)
	if err != nil {
		t.Fatalf("PacketizeWebRTCRTP: %v", err)
	}
	if len(payloads) <= int(result.LayerCount) {
		t.Fatalf("payload count = %d, want fragmented spatial layers", len(payloads))
	}

	var byLayer [govpx.VP9RTPMaxSpatialLayers][]govpx.RTPPayloadFragment
	var sawBaseSS bool
	for i, payload := range payloads {
		if got, want := payload.Marker, i == len(payloads)-1; got != want {
			t.Fatalf("payload %d marker = %t, want %t", i, got, want)
		}
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor[%d]: %v", i, err)
		}
		if !desc.PictureIDPresent || !desc.PictureID15Bit ||
			desc.PictureID != pictureID&govpx.VP9RTPPictureID15BitMask {
			t.Fatalf("payload %d PictureID = present:%t 15bit:%t id:%d",
				i, desc.PictureIDPresent, desc.PictureID15Bit, desc.PictureID)
		}
		if !desc.LayerIndicesPresent || desc.SpatialID >= result.LayerCount {
			t.Fatalf("payload %d descriptor = %+v, want valid spatial metadata",
				i, desc)
		}
		if desc.SpatialID == 0 && desc.StartOfFrame {
			sawBaseSS = desc.ScalabilityStructurePresent &&
				desc.ScalabilityStructure.SpatialLayerCount == int(result.LayerCount) &&
				desc.ScalabilityStructure.PictureGroupPresent &&
				len(desc.ScalabilityStructure.PictureGroups) == 2
		} else if desc.ScalabilityStructurePresent {
			t.Fatalf("payload %d repeated scalability structure", i)
		}
		byLayer[desc.SpatialID] = append(byLayer[desc.SpatialID], payload)
	}
	if !sawBaseSS {
		t.Fatal("base key payload did not carry WebRTC scalability structure")
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

	deltaPayloads, err := results[1].PacketizeWebRTCRTP(0x44, 96)
	if err != nil {
		t.Fatalf("delta PacketizeWebRTCRTP: %v", err)
	}
	for i, payload := range deltaPayloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor delta[%d]: %v", i, err)
		}
		if desc.ScalabilityStructurePresent {
			t.Fatalf("delta payload %d carried scalability structure", i)
		}
	}
	if got := govpx.NextVP9RTPPictureID(govpx.VP9RTPPictureID15BitMask); got != 0 {
		t.Fatalf("NextVP9RTPPictureID wrap = %d, want 0", got)
	}
}

func TestVP9SpatialSVCEncodeResultLimitSpatialLayersForRTP(t *testing.T) {
	result := encodeVP9WebRTCSVCTestResults(t, 1)[0]
	capped, err := result.LimitSpatialLayersForRTP(1)
	if err != nil {
		t.Fatalf("LimitSpatialLayersForRTP: %v", err)
	}
	if capped.LayerCount != 1 || capped.Data != nil ||
		capped.SizeBytes != result.Layers[0].SizeBytes {
		t.Fatalf("capped result = layers:%d data:%t size:%d, want base-only RTP view",
			capped.LayerCount, capped.Data != nil, capped.SizeBytes)
	}
	if capped.Layers[0].SpatialLayerCount != 1 ||
		!capped.Layers[0].NotRefForUpperSpatialLayer {
		t.Fatalf("capped base metadata = %+v", capped.Layers[0])
	}
	if capped.ScalabilityStructure.SpatialLayerCount != 1 ||
		capped.ScalabilityStructure.Width[1] != 0 ||
		capped.ScalabilityStructure.Height[1] != 0 {
		t.Fatalf("capped SS = %+v, want hidden layer dimensions cleared",
			capped.ScalabilityStructure)
	}
	payloads, err := capped.PacketizeWebRTCRTP(0x55, 80)
	if err != nil {
		t.Fatalf("capped PacketizeWebRTCRTP: %v", err)
	}
	for i, payload := range payloads {
		desc, _, err := govpx.ParseVP9RTPPayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParseVP9RTPPayloadDescriptor capped[%d]: %v", i, err)
		}
		if desc.SpatialID != 0 {
			t.Fatalf("capped payload %d spatial id = %d, want base only",
				i, desc.SpatialID)
		}
		if desc.StartOfFrame && (!desc.ScalabilityStructurePresent ||
			desc.ScalabilityStructure.SpatialLayerCount != 1) {
			t.Fatalf("capped base SS = %+v", desc.ScalabilityStructure)
		}
	}

	if _, err := result.LimitSpatialLayersForRTP(0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("LimitSpatialLayersForRTP(0) err = %v, want ErrInvalidConfig", err)
	}
	if _, err := result.LimitSpatialLayersForRTP(int(result.LayerCount) + 1); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("LimitSpatialLayersForRTP(over) err = %v, want ErrInvalidConfig", err)
	}
}

func TestVP9SpatialSVCEncodeResultPacketizeWebRTCRTPIntoSteadyStateNoAlloc(t *testing.T) {
	result := encodeVP9WebRTCSVCTestResults(t, 1)[0]
	const mtu = 80
	packets, payloadBytes, err := result.WebRTCRTPPacketizationSize(0x77, mtu)
	if err != nil {
		t.Fatalf("WebRTCRTPPacketizationSize: %v", err)
	}
	payloads := make([]govpx.RTPPayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	if _, _, err := result.PacketizeWebRTCRTPInto(payloads, payloadBuf, 0x77, mtu); err != nil {
		t.Fatalf("warmup PacketizeWebRTCRTPInto: %v", err)
	}

	allocs := testing.AllocsPerRun(vp9EncoderInterAllocRunsForTest, func() {
		n, used, err := result.PacketizeWebRTCRTPInto(payloads, payloadBuf,
			0x77, mtu)
		if err != nil {
			t.Fatalf("PacketizeWebRTCRTPInto: %v", err)
		}
		if n != packets || used != payloadBytes {
			t.Fatalf("PacketizeWebRTCRTPInto returned %d/%d, want %d/%d",
				n, used, packets, payloadBytes)
		}
	})
	if allocs != 0 {
		t.Fatalf("PacketizeWebRTCRTPInto allocs = %f, want 0", allocs)
	}

	if _, _, err := result.PacketizeWebRTCRTPInto(payloads[:packets-1],
		payloadBuf, 0x77, mtu); !errors.Is(err, govpx.ErrBufferTooSmall) {
		t.Fatalf("short PacketizeWebRTCRTPInto err = %v, want ErrBufferTooSmall", err)
	}
}

func encodeVP9WebRTCSVCTestResults(
	t *testing.T,
	frames int,
) []govpx.VP9SpatialSVCEncodeResult {
	t.Helper()
	temporal := govpx.TemporalScalabilityConfig{
		Enabled: true,
		Mode:    govpx.TemporalLayeringTwoLayers,
	}
	svc, err := govpx.NewVP9SpatialSVCEncoder(govpx.VP9SpatialSVCEncoderOptions{
		LayerCount:           2,
		InterLayerPrediction: true,
		Layers: [govpx.VP9MaxSpatialLayers]govpx.VP9EncoderOptions{
			{
				Width:                    32,
				Height:                   32,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        120,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
			{
				Width:                    64,
				Height:                   64,
				FPS:                      30,
				Deadline:                 govpx.DeadlineRealtime,
				CpuUsed:                  8,
				RateControlModeSet:       true,
				RateControlMode:          govpx.RateControlCBR,
				TargetBitrateKbps:        240,
				TemporalScalability:      temporal,
				ErrorResilient:           true,
				FrameParallelDecodingSet: true,
				FrameParallelDecoding:    true,
			},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}
	defer svc.Close()
	srcs := []*image.YCbCr{
		vp9test.NewYCbCr(32, 32, 90, 120, 136),
		vp9test.NewYCbCr(64, 64, 90, 120, 136),
	}
	dst := make([]byte, 1<<20)
	results := make([]govpx.VP9SpatialSVCEncodeResult, frames)
	for frame := 0; frame < frames; frame++ {
		vp9test.FillYCbCr(srcs[0], uint8(90+frame*7), 120, 136)
		vp9test.FillYCbCr(srcs[1], uint8(90+frame*7), 120, 136)
		result, err := svc.EncodeIntoWithResult(srcs, dst)
		if err != nil {
			t.Fatalf("EncodeIntoWithResult frame %d: %v", frame, err)
		}
		results[frame] = result
	}
	return results
}
