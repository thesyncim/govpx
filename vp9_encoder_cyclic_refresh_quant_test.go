package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	vp9enc "github.com/thesyncim/govpx/internal/vp9/encoder"
	"testing"
)

func TestVP9CyclicRefreshSegmentIDBoostedMirrorsLibvpx(t *testing.T) {
	if vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBase) {
		t.Fatalf("base segment must not be boosted")
	}
	if !vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBoost1) {
		t.Fatalf("BOOST1 must be boosted")
	}
	if !vp9enc.CyclicRefreshSegmentIDBoosted(vp9enc.CyclicRefreshSegmentBoost2) {
		t.Fatalf("BOOST2 must be boosted")
	}
	if vp9enc.CyclicRefreshSegmentIDBoosted(7) {
		t.Fatalf("non-CR segments must not be boosted")
	}
}

// TestVP9EncoderConstantKeyframeProducesParseableBitstream: the constant
// source-backed keyframe path emits oracle-shaped Block32x32 / Tx16 DC
// skip leaves whose every layer parses cleanly through the decoder.

func TestVP9EncoderDefaultQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	img := vp9test.NewCheckerYCbCr(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if got := int(h.Quant.BaseQindex); got != vp9DefaultBaseQIndex {
		t.Fatalf("BaseQindex = %d, want pinned default %d",
			got, vp9DefaultBaseQIndex)
	}
}

func TestVP9EncoderDefaultInterQuantizerUsesPinnedCQBaseQIndex(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64})
	src := vp9test.NewYCbCr(64, 64, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	var keyBR vp9dec.BitReader
	keyBR.Init(key)
	keyHeader, err := vp9dec.ReadUncompressedHeader(&keyBR, nil, nil)
	if err != nil {
		t.Fatalf("ReadUncompressedHeader keyframe: %v", err)
	}
	var interBR vp9dec.BitReader
	interBR.Init(inter)
	interHeader, err := vp9dec.ReadUncompressedHeader(&interBR, &keyHeader,
		func(uint8) (uint32, uint32) { return 64, 64 })
	if err != nil {
		t.Fatalf("ReadUncompressedHeader inter: %v", err)
	}
	if got := int(interHeader.Quant.BaseQindex); got != vp9DefaultInterBaseQIndex {
		t.Fatalf("inter BaseQindex = %d, want pinned default %d",
			got, vp9DefaultInterBaseQIndex)
	}
}

func TestVP9EncoderPublicFixedQuantizerControlsQIndex(t *testing.T) {
	const width, height = 64, 64
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        width,
		Height:       height,
		MinQuantizer: 20,
		MaxQuantizer: 20,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)
	key, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	inter, err := e.Encode(src)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	wantQIndex := vp9enc.PublicQuantizerToQIndex(20)
	keyHeader, _ := vp9test.ParseHeader(t, key)
	if got := int(keyHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("key BaseQindex = %d, want %d", got, wantQIndex)
	}
	interHeader, _ := vp9test.ParseHeader(t, inter)
	if got := int(interHeader.Quant.BaseQindex); got != wantQIndex {
		t.Fatalf("inter BaseQindex = %d, want %d", got, wantQIndex)
	}
}

func TestVP9EncoderExplicitQuantizerOverridesDefault(t *testing.T) {
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:     64,
		Height:    64,
		Quantizer: 1,
	})
	img := vp9test.NewCheckerYCbCr(64, 64, 32, 224, 128, 128)
	packet, err := e.Encode(img)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	h, _ := vp9test.ParseHeader(t, packet)
	if h.Quant.BaseQindex != 1 {
		t.Fatalf("BaseQindex = %d, want explicit qindex 1", h.Quant.BaseQindex)
	}
}
