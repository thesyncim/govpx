package govpx_test

import (
	"bytes"
	"errors"
	"image"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

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
