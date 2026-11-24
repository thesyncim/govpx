//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"testing"
)

func TestVP9EncoderVpxdecOracleMatchesQuarterPelMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := predictedVP9ReferenceYCbCrForTest(t,
		e.refFrames[0].img, vp9dec.MV{Col: 58})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesEighthPelMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := predictedVP9ReferenceYCbCrForTest(t,
		e.refFrames[0].img, vp9dec.MV{Col: 57})
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}

	assertVP9EncoderVpxdecI420Match(t, width, height, key, inter)
}

// TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame covers the first
// non-intra inter tile shape the public decoder now parses: one
// LAST/ZeroMv skipped block referencing the prior keyframe.
func TestVP9EncoderVpxdecOracleAcceptsInterSkipFrame(t *testing.T) {
	vp9test.RequireVpxdec(t)

	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	inter := vp9InterSkipFrameForTest(t, 64, 64)
	vp9test.VpxdecAccepts(t, "the inter skip frame", 64, 64, key, inter)
}
