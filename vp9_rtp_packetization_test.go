package govpx_test

import (
	"bytes"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9RTPPacketizeAssembleEncodedFrame(t *testing.T) {
	const width, height = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	frame, err := e.Encode(vp9test.NewCheckerYCbCr(width, height, 32, 224, 96, 192))
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	desc := govpx.VP9RTPPayloadDescriptor{
		PictureIDPresent:            true,
		PictureID:                   17,
		ScalabilityStructurePresent: true,
		ScalabilityStructure: govpx.VP9RTPScalabilityStructure{
			SpatialLayerCount: 1,
			ResolutionPresent: true,
			Width:             [govpx.VP9RTPMaxSpatialLayers]uint16{width},
			Height:            [govpx.VP9RTPMaxSpatialLayers]uint16{height},
		},
	}
	payloads, err := govpx.PacketizeVP9RTPFrame(desc, frame, 64)
	if err != nil {
		t.Fatalf("govpx.PacketizeVP9RTPFrame: %v", err)
	}
	if len(payloads) < 2 {
		t.Fatalf("payload count = %d, want fragmented encoded frame", len(payloads))
	}
	assembled, err := govpx.AssembleVP9RTPFrame(payloads)
	if err != nil {
		t.Fatalf("govpx.AssembleVP9RTPFrame: %v", err)
	}
	if !bytes.Equal(assembled, frame) {
		t.Fatal("assembled RTP frame does not match encoded VP9 payload")
	}
	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("govpx.NewVP9Decoder: %v", err)
	}
	if err := d.Decode(assembled); err != nil {
		t.Fatalf("Decode assembled frame: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("Decode assembled frame produced no visible output")
	}
}
