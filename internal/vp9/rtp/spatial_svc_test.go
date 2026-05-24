package rtp

import (
	"bytes"
	"errors"
	"testing"

	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
	"github.com/thesyncim/govpx/internal/vpx/rtp"
)

func TestPacketizeSpatialSVCFrameIntoOrdersLayerFrames(t *testing.T) {
	layers := testSpatialSVCLayerFrames()
	const mtu = 14

	packets, payloadBytes, err := SpatialSVCFramePacketizationSize(layers, mtu)
	if err != nil {
		t.Fatalf("SpatialSVCFramePacketizationSize: %v", err)
	}
	if packets <= len(layers) {
		t.Fatalf("packets = %d, want fragmented spatial layers", packets)
	}

	shortDst := make([]rtp.PayloadFragment, packets-1)
	payloadBuf := make([]byte, payloadBytes)
	gotPackets, gotBytes, err := PacketizeSpatialSVCFrameInto(shortDst,
		payloadBuf, layers, mtu)
	if !errors.Is(err, vpxerrors.ErrBufferTooSmall) {
		t.Fatalf("short PacketizeSpatialSVCFrameInto error = %v, want ErrBufferTooSmall", err)
	}
	if gotPackets != packets || gotBytes != payloadBytes {
		t.Fatalf("short PacketizeSpatialSVCFrameInto need = %d/%d, want %d/%d",
			gotPackets, gotBytes, packets, payloadBytes)
	}

	payloads := make([]rtp.PayloadFragment, packets)
	n, used, err := PacketizeSpatialSVCFrameInto(payloads, payloadBuf, layers, mtu)
	if err != nil {
		t.Fatalf("PacketizeSpatialSVCFrameInto: %v", err)
	}
	if n != packets || used != payloadBytes {
		t.Fatalf("PacketizeSpatialSVCFrameInto returned %d/%d, want %d/%d",
			n, used, packets, payloadBytes)
	}

	var byLayer [2][]rtp.PayloadFragment
	var seen [2]int
	prevSpatialID := uint8(0)
	for i, payload := range payloads {
		if len(payload.Payload) > mtu {
			t.Fatalf("payload %d length = %d, exceeds mtu %d",
				i, len(payload.Payload), mtu)
		}
		desc, _, err := ParsePayloadDescriptor(payload.Payload)
		if err != nil {
			t.Fatalf("ParsePayloadDescriptor[%d]: %v", i, err)
		}
		if desc.SpatialID < prevSpatialID {
			t.Fatalf("payload %d spatial id = %d after %d",
				i, desc.SpatialID, prevSpatialID)
		}
		prevSpatialID = desc.SpatialID
		if desc.SpatialID >= uint8(len(layers)) {
			t.Fatalf("payload %d spatial id = %d, want < %d",
				i, desc.SpatialID, len(layers))
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
	for i := range layers {
		if len(byLayer[i]) == 0 {
			t.Fatalf("layer %d had no payloads", i)
		}
		for j := range byLayer[i] {
			wantMarker := j == len(byLayer[i])-1
			if byLayer[i][j].Marker != wantMarker {
				t.Fatalf("layer %d payload %d marker = %v, want %v",
					i, j, byLayer[i][j].Marker, wantMarker)
			}
		}
		assembled, err := AssembleFrame(byLayer[i])
		if err != nil {
			t.Fatalf("AssembleFrame layer %d: %v", i, err)
		}
		if !bytes.Equal(assembled, layers[i].Frame) {
			t.Fatalf("assembled layer %d = % x, want % x",
				i, assembled, layers[i].Frame)
		}
	}

	allocPayloads, err := PacketizeSpatialSVCFrame(layers, mtu)
	if err != nil {
		t.Fatalf("PacketizeSpatialSVCFrame: %v", err)
	}
	if len(allocPayloads) != packets {
		t.Fatalf("PacketizeSpatialSVCFrame payloads = %d, want %d",
			len(allocPayloads), packets)
	}
}

func TestPacketizeSpatialSVCFrameIntoAllocatesZero(t *testing.T) {
	layers := testSpatialSVCLayerFrames()
	const mtu = 14
	packets, payloadBytes, err := SpatialSVCFramePacketizationSize(layers, mtu)
	if err != nil {
		t.Fatalf("SpatialSVCFramePacketizationSize: %v", err)
	}
	payloads := make([]rtp.PayloadFragment, packets)
	payloadBuf := make([]byte, payloadBytes)
	if _, _, err := PacketizeSpatialSVCFrameInto(payloads, payloadBuf, layers, mtu); err != nil {
		t.Fatalf("warmup PacketizeSpatialSVCFrameInto: %v", err)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		n, used, err := PacketizeSpatialSVCFrameInto(payloads, payloadBuf,
			layers, mtu)
		if err != nil {
			t.Fatalf("PacketizeSpatialSVCFrameInto: %v", err)
		}
		if n != packets || used != payloadBytes {
			t.Fatalf("PacketizeSpatialSVCFrameInto returned %d/%d, want %d/%d",
				n, used, packets, payloadBytes)
		}
	})
	if allocs != 0 {
		t.Fatalf("PacketizeSpatialSVCFrameInto allocs = %f, want 0", allocs)
	}
}

func TestSpatialSVCFramePacketizationSizeRejectsEmptyAccessUnit(t *testing.T) {
	if _, _, err := SpatialSVCFramePacketizationSize(nil, 1200); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("SpatialSVCFramePacketizationSize(nil) error = %v, want ErrInvalidConfig", err)
	}
}

func testSpatialSVCLayerFrames() []SpatialSVCLayerFrame {
	return []SpatialSVCLayerFrame{
		{
			Descriptor: PayloadDescriptor{
				LayerIndicesPresent:         true,
				SpatialID:                   0,
				ScalabilityStructurePresent: true,
				ScalabilityStructure: ScalabilityStructure{
					SpatialLayerCount: 2,
					ResolutionPresent: true,
					Width:             [MaxSpatialLayers]uint16{32, 64},
					Height:            [MaxSpatialLayers]uint16{18, 36},
				},
			},
			Frame: []byte{0x82, 0x49, 0x83, 0x11, 0x22, 0x33, 0x44, 0x55,
				0x66, 0x77, 0x88, 0x99},
		},
		{
			Descriptor: PayloadDescriptor{
				LayerIndicesPresent:        true,
				SpatialID:                  1,
				InterLayerDependency:       true,
				NotRefForUpperSpatialLayer: true,
			},
			Frame: []byte{0x84, 0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60,
				0x70, 0x80, 0x90},
		},
	}
}
