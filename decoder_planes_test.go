package govpx

import (
	"bytes"
	"testing"
)

type capturedFramePlanes struct {
	width  int
	height int
	y      []byte
	u      []byte
	v      []byte
}

type testFrameDecoder interface {
	Decode([]byte) error
	NextFrame() (Image, bool)
}

func decodeFramesForTest(t testing.TB, codec string, d testFrameDecoder,
	packets [][]byte, want int,
) []capturedFramePlanes {
	t.Helper()
	out := make([]capturedFramePlanes, 0, want)
	for i, packet := range packets {
		if err := d.Decode(packet); err != nil {
			t.Fatalf("%s Decode[%d]: %v", codec, i, err)
		}
		img, ok := d.NextFrame()
		if !ok {
			continue
		}
		out = append(out, captureDecodedPlanes(img))
	}
	return out
}

func decodeOneFrameForTest(t testing.TB, codec string, d testFrameDecoder,
	packet []byte,
) capturedFramePlanes {
	t.Helper()
	if err := d.Decode(packet); err != nil {
		t.Fatalf("%s Decode: %v", codec, err)
	}
	img, ok := d.NextFrame()
	if !ok {
		t.Fatalf("%s Decode produced no visible frame", codec)
	}
	return captureDecodedPlanes(img)
}

func captureDecodedPlanes(img Image) capturedFramePlanes {
	yWidth := img.Width
	yHeight := img.Height
	uvWidth := (img.Width + 1) >> 1
	uvHeight := (img.Height + 1) >> 1

	y := make([]byte, yWidth*yHeight)
	for row := range yHeight {
		copy(y[row*yWidth:(row+1)*yWidth], img.Y[row*img.YStride:row*img.YStride+yWidth])
	}
	u := make([]byte, uvWidth*uvHeight)
	for row := range uvHeight {
		copy(u[row*uvWidth:(row+1)*uvWidth], img.U[row*img.UStride:row*img.UStride+uvWidth])
	}
	v := make([]byte, uvWidth*uvHeight)
	for row := range uvHeight {
		copy(v[row*uvWidth:(row+1)*uvWidth], img.V[row*img.VStride:row*img.VStride+uvWidth])
	}
	return capturedFramePlanes{width: yWidth, height: yHeight, y: y, u: u, v: v}
}

func sameCapturedFramePlanes(a capturedFramePlanes, b capturedFramePlanes) bool {
	return a.width == b.width &&
		a.height == b.height &&
		bytes.Equal(a.y, b.y) &&
		bytes.Equal(a.u, b.u) &&
		bytes.Equal(a.v, b.v)
}
