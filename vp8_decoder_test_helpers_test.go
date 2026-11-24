package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vpx/buffers"
)

func assertCodedBordersExtended(t *testing.T, img *vp8common.Image) {
	t.Helper()

	codedUVWidth := (img.CodedWidth + 1) >> 1
	codedUVHeight := (img.CodedHeight + 1) >> 1

	yRightEdge := img.Y[img.CodedWidth-1]
	if got := img.Y[img.CodedWidth]; got != yRightEdge {
		t.Fatalf("first Y right border = %d, want coded edge %d", got, yRightEdge)
	}
	yBottomEdge := img.Y[(img.CodedHeight-1)*img.YStride+img.CodedWidth-1]
	if got := img.YFull[img.YOrigin+img.CodedHeight*img.YStride+img.CodedWidth-1]; got != yBottomEdge {
		t.Fatalf("first Y bottom border = %d, want coded edge %d", got, yBottomEdge)
	}

	uRightEdge := img.U[codedUVWidth-1]
	if got := img.U[codedUVWidth]; got != uRightEdge {
		t.Fatalf("first U right border = %d, want coded edge %d", got, uRightEdge)
	}
	uBottomEdge := img.U[(codedUVHeight-1)*img.UStride+codedUVWidth-1]
	if got := img.UFull[img.UOrigin+codedUVHeight*img.UStride+codedUVWidth-1]; got != uBottomEdge {
		t.Fatalf("first U bottom border = %d, want coded edge %d", got, uBottomEdge)
	}

	vRightEdge := img.V[codedUVWidth-1]
	if got := img.V[codedUVWidth]; got != vRightEdge {
		t.Fatalf("first V right border = %d, want coded edge %d", got, vRightEdge)
	}
	vBottomEdge := img.V[(codedUVHeight-1)*img.VStride+codedUVWidth-1]
	if got := img.VFull[img.VOrigin+codedUVHeight*img.VStride+codedUVWidth-1]; got != vBottomEdge {
		t.Fatalf("first V bottom border = %d, want coded edge %d", got, vBottomEdge)
	}
}

func fillVP8Image(img *vp8common.Image, value byte) {
	for i := range img.Y {
		img.Y[i] = value
	}
	for i := range img.U {
		img.U[i] = value
	}
	for i := range img.V {
		img.V[i] = value
	}
}

func newTestImage(width int, height int) Image {
	uvWidth, uvHeight := buffers.Chroma420Dimensions(width, height)
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}

func publicImageEqualVP8(got Image, want *vp8common.Image) bool {
	if want == nil || got.Width != want.Width || got.Height != want.Height {
		return false
	}
	uvWidth, uvHeight := buffers.Chroma420Dimensions(want.Width, want.Height)
	return testutil.PlaneEqual(got.Y, got.YStride, want.Y, want.YStride, want.Width, want.Height) &&
		testutil.PlaneEqual(got.U, got.UStride, want.U, want.UStride, uvWidth, uvHeight) &&
		testutil.PlaneEqual(got.V, got.VStride, want.V, want.VStride, uvWidth, uvHeight)
}
