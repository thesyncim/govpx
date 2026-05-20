package govpx

import (
	"bytes"
	"testing"
)

func TestVP9RTPFacadePacketizeAssembleEncodedFrame(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	frame, err := e.Encode(newVP9CheckerYCbCrForTest(width, height, 32, 224, 96, 192))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	desc := VP9RTPPayloadDescriptor{
		PictureIDPresent:            true,
		PictureID:                   17,
		ScalabilityStructurePresent: true,
		ScalabilityStructure: VP9RTPScalabilityStructure{
			SpatialLayerCount: 1,
			ResolutionPresent: true,
			Width:             [VP9RTPMaxSpatialLayers]uint16{width},
			Height:            [VP9RTPMaxSpatialLayers]uint16{height},
		},
	}
	payloads, err := PacketizeVP9RTPFrame(desc, frame, 64)
	if err != nil {
		t.Fatalf("PacketizeVP9RTPFrame: %v", err)
	}
	if len(payloads) < 2 {
		t.Fatalf("payload count = %d, want fragmented encoded frame", len(payloads))
	}
	assembled, err := AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(assembled, frame) {
		t.Fatal("assembled RTP frame does not match encoded VP9 payload")
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(assembled); err != nil {
		t.Fatalf("Decode assembled frame: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("Decode assembled frame produced no visible output")
	}
}
