//go:build govpx_oracle_trace

package govpx_test

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9oracle"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestVP9EncoderVpxdecOracleMatchesOddIntegerMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 128, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.ShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 7, 0)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatches16x8InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 32, 8
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.ShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, 0)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitXShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, -8)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert32x32InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 32, 32
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitXShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, -8)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesVert16x16InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 16, 16
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitXShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 4, -4)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesHorz64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.SplitYShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride, 8, -8)
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func TestVP9EncoderVpxdecOracleMatchesSplit64x64InterMotion(t *testing.T) {
	vp9test.RequireVpxdec(t)

	const width, height = 64, 64
	key, inter := encodeVP9IntegerMotionPair(t, width, height,
		func(ref govpx.Image) *image.YCbCr {
			return vp9test.QuadrantShiftedI420(ref.Width, ref.Height,
				ref.Y, ref.U, ref.V, ref.YStride, ref.UStride, ref.VStride,
				image.Point{X: 8}, image.Point{X: -8},
				image.Point{Y: 8}, image.Point{Y: -8})
		})

	vp9oracle.AssertEncoderVpxdecI420Match(t, width, height, key, inter)
}

func encodeVP9IntegerMotionPair(t testing.TB, width, height int,
	makeInter func(govpx.Image) *image.YCbCr,
) ([]byte, []byte) {
	t.Helper()
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: width, Height: height})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer e.Close()
	key, err := e.Encode(vp9test.NewMotionYCbCr(width, height))
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	ref := vp9oracle.DecodeLastVisibleFrame(t, key)
	inter, err := e.Encode(makeInter(ref))
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	return key, inter
}
