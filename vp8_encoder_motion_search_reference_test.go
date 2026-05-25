package govpx

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
	"testing"
)

func TestSelectInterFrameReferenceMotionVectorChoosesLowestCostReference(t *testing.T) {
	src := testImage(16, 16)
	fillImage(src, 40, 90, 170)
	last := testVP8Frame(t, 16, 16, 220, 90, 170)
	golden := testVP8Frame(t, 16, 16, 40, 90, 170)
	alt := testVP8Frame(t, 16, 16, 80, 90, 170)
	for row := range 16 {
		for col := range 16 {
			v := byte(32 + ((row*17 + col*11) & 127))
			src.Y[row*src.YStride+col] = v
			golden.Img.Y[row*golden.Img.YStride+col] = v
			last.Img.Y[row*last.Img.YStride+col] = byte(200 - ((row*7 + col*19) & 63))
			alt.Img.Y[row*alt.Img.YStride+col] = byte(96 + ((row*5 + col*3) & 63))
		}
	}
	last.ExtendBorders()
	golden.ExtendBorders()
	alt.ExtendBorders()
	refs := [...]interAnalysisReference{
		{Frame: vp8common.LastFrame, Img: &last.Img},
		{Frame: vp8common.GoldenFrame, Img: &golden.Img},
		{Frame: vp8common.AltRefFrame, Img: &alt.Img},
	}
	source := sourceImageFromPublic(src)

	ref, mv := selectInterFrameReferenceMotionVector(source, refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.GoldenFrame || mv != (vp8enc.MotionVector{}) {
		t.Fatalf("selection = %v %+v, want golden zero MV", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorUsesLibvpxHexCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte(17 + ((row*19 + col*11) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+2)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 16}) {
		t.Fatalf("selection = %v %+v, want last row +16 from libvpx hex ring", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsFullPixelCandidate(t *testing.T) {
	src := testImage(32, 32)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte(21 + ((row*23 + col*7) & 127))
		}
	}

	last := testVP8Frame(t, 32, 32, 220, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+3)*last.Img.YStride+col] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 24}) {
		t.Fatalf("selection = %v %+v, want last row +24 after exhaustive search", ref.Frame, mv)
	}
}

func TestSelectInterFrameReferenceMotionVectorFindsExhaustiveCornerCandidate(t *testing.T) {
	src := testImage(64, 64)
	fillImage(src, 13, 90, 170)
	for row := range 16 {
		for col := range 16 {
			src.Y[row*src.YStride+col] = byte(31 + ((row*29 + col*5) & 127))
		}
	}

	last := testVP8Frame(t, 64, 64, 220, 90, 170)
	for row := range 16 {
		for col := range 16 {
			last.Img.Y[(row+4)*last.Img.YStride+col+4] = src.Y[row*src.YStride+col]
		}
	}
	refs := [...]interAnalysisReference{{Frame: vp8common.LastFrame, Img: &last.Img}}

	ref, mv := selectInterFrameReferenceMotionVector(sourceImageFromPublic(src), refs[:], len(refs), 0, 0, 1, 1, nil, nil, nil, testInterSearchQIndex, &vp8tables.DefaultMVContext)

	if ref.Frame != vp8common.LastFrame || mv != (vp8enc.MotionVector{Row: 32, Col: 32}) {
		t.Fatalf("selection = %v %+v, want last +32,+32 exhaustive candidate", ref.Frame, mv)
	}
}
