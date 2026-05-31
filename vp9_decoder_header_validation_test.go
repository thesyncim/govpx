package govpx

import (
	"errors"
	"testing"
)

func TestVP9DecoderRejectsMissingResidueTokens(t *testing.T) {
	packet := vp9SkipZeroKeyframeForTest(t, 64, 64, false)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	err = d.Decode(packet)
	if !errors.Is(err, ErrInvalidVP9Data) {
		t.Fatalf("Decode err = %v, want ErrInvalidVP9Data", err)
	}
	w, h := d.LastFrameSize()
	if w != 0 || h != 0 {
		t.Fatalf("LastFrameSize() = (%d, %d), want (0, 0)", w, h)
	}
}

// TestVP9DecoderDecodeSteadyStateAlloc keeps the public header +
// tile/residual parse and intra reconstruct output path allocation-free after
// construction.
