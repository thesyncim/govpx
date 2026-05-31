//go:build govpx_oracle_trace

package govpx_test

import (
	"image"
	"testing"

	govpx "github.com/thesyncim/govpx"
)

func encodeVP9OracleFramesWithGovpx(t testing.TB, opts govpx.VP9EncoderOptions,
	sources []*image.YCbCr, flags []govpx.EncodeFlags,
) [][]byte {
	t.Helper()
	if len(sources) == 0 {
		t.Fatal("encodeVP9OracleFramesWithGovpx: no sources")
	}
	width := sources[0].Rect.Dx()
	height := sources[0].Rect.Dy()
	opts.Width = width
	opts.Height = height
	enc, err := govpx.NewVP9Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	defer enc.Close()

	out := make([][]byte, 0, len(sources))
	for i, src := range sources {
		var f govpx.EncodeFlags
		if i < len(flags) {
			f = flags[i]
		}
		packet, err := enc.EncodeWithFlags(src, f)
		if err != nil {
			t.Fatalf("VP9 EncodeWithFlags frame %d: %v", i, err)
		}
		out = append(out, append([]byte(nil), packet...))
	}
	return out
}

func newVP9OracleImage(width int, height int) govpx.Image {
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
	return govpx.Image{
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

func packVP9OracleI420(img *govpx.Image) []byte {
	out := make([]byte, 0, vp9OracleI420FrameSize(img.Width, img.Height))
	return appendVP9OracleI420(out, img)
}

func appendVP9OracleI420(out []byte, img *govpx.Image) []byte {
	w := img.Width
	h := img.Height
	uvW := (w + 1) >> 1
	uvH := (h + 1) >> 1
	for y := 0; y < h; y++ {
		out = append(out, img.Y[y*img.YStride:y*img.YStride+w]...)
	}
	for y := 0; y < uvH; y++ {
		out = append(out, img.U[y*img.UStride:y*img.UStride+uvW]...)
	}
	for y := 0; y < uvH; y++ {
		out = append(out, img.V[y*img.VStride:y*img.VStride+uvW]...)
	}
	return out
}

func vp9OracleI420FrameSize(width int, height int) int {
	if width <= 0 || height <= 0 {
		return 0
	}
	uvWidth, uvHeight := (width+1)>>1, (height+1)>>1
	return width*height + 2*uvWidth*uvHeight
}
