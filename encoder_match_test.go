package govpx

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

func TestSourceMatchesReferenceUsesVisiblePlanes(t *testing.T) {
	const (
		width  = 17
		height = 17
	)
	ref, err := vp8common.NewFrameBuffer(width, height, 32, 32)
	if err != nil {
		t.Fatalf("NewFrameBuffer returned error: %v", err)
	}
	src := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, 24*height),
		U:       make([]byte, 16*((height+1)>>1)),
		V:       make([]byte, 16*((height+1)>>1)),
		YStride: 24,
		UStride: 16,
		VStride: 16,
	}
	fillMatchingVisiblePlanes(src, &ref.Img)
	for i := range src.Y {
		if i%src.YStride >= width {
			src.Y[i] = 0xee
		}
	}

	if !sourceMatchesReference(src, &ref.Img) {
		t.Fatalf("sourceMatchesReference = false, want true for matching visible samples with different strides")
	}
	src.V[3*src.VStride+2] ^= 1
	if sourceMatchesReference(src, &ref.Img) {
		t.Fatalf("sourceMatchesReference = true after visible chroma mismatch, want false")
	}
}

func fillMatchingVisiblePlanes(src Image, ref *vp8common.Image) {
	for row := 0; row < src.Height; row++ {
		for col := 0; col < src.Width; col++ {
			v := byte(13 + row*3 + col*5)
			src.Y[row*src.YStride+col] = v
			ref.Y[row*ref.YStride+col] = v
		}
	}
	uvWidth := (src.Width + 1) >> 1
	uvHeight := (src.Height + 1) >> 1
	for row := range uvHeight {
		for col := range uvWidth {
			u := byte(40 + row*7 + col*3)
			v := byte(170 - row*4 + col*2)
			src.U[row*src.UStride+col] = u
			src.V[row*src.VStride+col] = v
			ref.U[row*ref.UStride+col] = u
			ref.V[row*ref.VStride+col] = v
		}
	}
}
