package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestSelectInterFrameSplitSubsetMotionModeTrialsReusableLabels(t *testing.T) {
	src, ref := splitMotionSourceAndReference(t)
	for row := range 32 {
		copy(src.Y[row*src.YStride:row*src.YStride+32], ref.Img.Y[row*ref.Img.YStride:row*ref.Img.YStride+32])
	}
	ref.ExtendBorders()

	mode := vp8enc.InterFrameMacroblockMode{
		RefFrame:  vp8common.LastFrame,
		Mode:      vp8common.SplitMV,
		Partition: 2,
	}
	left := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.NewMV, MV: vp8enc.MotionVector{Col: 16}}
	above := vp8enc.InterFrameMacroblockMode{RefFrame: vp8common.LastFrame, Mode: vp8common.ZeroMV}
	width, height := splitMotionPartitionBlockSize(int(mode.Partition))

	mv, bMode := selectInterFrameSplitSubsetMotionMode(sourceImageFromPublic(src), &ref.Img, 0, 0, &mode, 0, width, height, vp8enc.MotionVector{}, testInterSearchQIndex, &left, &above)

	if mv != (vp8enc.MotionVector{}) || bMode != vp8common.Above4x4 {
		t.Fatalf("subset candidate = %+v/%v, want ABOVE4X4 zero-MV reuse", mv, bMode)
	}
}

func TestSelectInterFrameReferenceMotionVectorPrefersCheaperMotionOnTie(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 32, 32, 40, 90, 170)
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	_, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if mv != (vp8enc.MotionVector{}) {
		t.Fatalf("mv = %+v, want zero MV for equal-SAD candidates", mv)
	}
}

func TestPredictSplitMotionBlock4x4UsesCodedClamp(t *testing.T) {
	ref := testVP8Frame(t, 5, 5, 0, 90, 170)
	for row := 0; row < ref.Img.CodedHeight; row++ {
		for col := 0; col < ref.Img.CodedWidth; col++ {
			ref.Img.Y[row*ref.Img.YStride+col] = 200
		}
		ref.Img.Y[row*ref.Img.YStride+4] = byte(40 + row)
	}
	ref.ExtendBorders()

	var out [16]byte
	if !predictSplitMotionBlock4x4(&ref.Img, 0, 0, 1, vp8enc.MotionVector{}, &out) {
		t.Fatalf("predictSplitMotionBlock4x4 returned false")
	}
	for row := range 4 {
		for col := range 4 {
			want := byte(200)
			if col == 0 {
				want = byte(40 + row)
			}
			if got := out[row*4+col]; got != want {
				t.Fatalf("fullpel coded-clamp out[%d,%d] = %d, want %d", row, col, got, want)
			}
		}
	}
}
