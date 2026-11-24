package govpx_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9DecoderRejectsMissingResidueTokens(t *testing.T) {
	packet := vp9test.SkipZeroKeyframe(t, 64, 64, false)

	d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if !errors.Is(err, govpx.ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
}

func TestVP9DecoderRejectsInvalidMultiTilePrefix(t *testing.T) {
	packet := vp9test.MultiTileStubPacket(t, 1024, 64, 1)
	tileStart, err := vp9test.TileStart(packet)
	if err != nil {
		t.Fatalf("TileStart: %v", err)
	}

	cases := []struct {
		name   string
		packet []byte
	}{
		{"truncated-prefix", packet[:tileStart+2]},
		{"oversized-prefix", func() []byte {
			corrupt := make([]byte, len(packet))
			copy(corrupt, packet)
			binary.BigEndian.PutUint32(corrupt[tileStart:tileStart+4],
				uint32(len(packet)))
			return corrupt
		}()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := govpx.NewVP9Decoder(govpx.VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			err = d.Decode(tc.packet)
			if !errors.Is(err, govpx.ErrInvalidVP9Data) {
				t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
			}
			w, h := d.LastFrameSize()
			if w != 0 || h != 0 {
				t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
			}
		})
	}
}
