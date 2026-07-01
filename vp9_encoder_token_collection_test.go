package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderCountTokenCollectionBuildsEOSBLists(t *testing.T) {
	const width, height = 64, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 65536)
	keySrc := vp9test.NewCheckerYCbCr(width, height, 32, 224, 128, 128)
	interSrc := vp9test.NewCheckerYCbCr(width, height, 48, 208, 128, 128)

	if _, err := e.EncodeInto(keySrc, dst); err != nil {
		t.Fatalf("key EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "keyframe row 0", 0, false)
	assertVP9CountTokenList(t, e, "keyframe row 1", 1, false)

	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "inter row 0", 0, false)
	assertVP9CountTokenList(t, e, "inter row 1", 1, false)

	if _, err := e.EncodeInto(interSrc, dst); err != nil {
		t.Fatalf("inter skip EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "inter repeat row 0", 0, false)
	assertVP9CountTokenList(t, e, "inter repeat row 1", 1, false)
}

func TestVP9EncoderCountTokenCollectionTerminatesNoResidueLeaves(t *testing.T) {
	const width, height = 64, 128
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	dst := make([]byte, 65536)
	src := vp9test.NewYCbCr(width, height, 128, 128, 128)

	if _, err := e.EncodeInto(src, dst); err != nil {
		t.Fatalf("flat key EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "flat key row 0", 0, true)
	assertVP9CountTokenList(t, e, "flat key row 1", 1, true)

	if _, err := e.EncodeInto(src, dst); err != nil {
		t.Fatalf("flat inter EncodeInto: %v", err)
	}
	assertVP9CountTokenList(t, e, "flat inter row 0", 0, true)
	assertVP9CountTokenList(t, e, "flat inter row 1", 1, true)
}

func assertVP9CountTokenList(t *testing.T, e *VP9Encoder, label string,
	tileSBRow int, allowOnlyEOSB bool,
) {
	t.Helper()
	if e.vp9TokenFrame.Used <= 0 {
		t.Fatalf("%s token frame used = %d, want tokens", label, e.vp9TokenFrame.Used)
	}
	list, ok := e.vp9CountTokenListForTileSBRow(0, 0, tileSBRow)
	if !ok {
		t.Fatalf("%s token list missing", label)
	}
	tokens, ok := e.vp9TokenFrame.TokensForList(list)
	if !ok {
		t.Fatalf("%s token list slice rejected: %+v", label, list)
	}
	if !vp9TokenListHasOnlyEOSBTerminatedLeaves(tokens) {
		t.Fatalf("%s token list is not EOSB terminated", label)
	}
	if got := vp9TokenListEOSBCount(tokens); got == 0 {
		t.Fatalf("%s token list EOSB count = 0, want at least one leaf", label)
	}
	if allowOnlyEOSB && len(tokens) != vp9TokenListEOSBCount(tokens) {
		t.Fatalf("%s token list contains coefficient tokens in skip fixture: len=%d eosb=%d",
			label, len(tokens), vp9TokenListEOSBCount(tokens))
	}
}
