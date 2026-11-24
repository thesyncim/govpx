package govpx_test

import (
	"errors"
	"testing"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9EncoderRejectsInvalidRenderSize(t *testing.T) {
	cases := []struct {
		name string
		w, h int
	}{
		{"only-width", 1280, 0},
		{"only-height", 0, 720},
		{"negative-width", -1, 720},
		{"negative-height", 1280, -1},
		{"too-large-width", 1<<16 + 1, 720},
		{"too-large-height", 1280, 1<<16 + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := govpx.VP9EncoderOptions{
				Width:        64,
				Height:       64,
				FPS:          30,
				RenderWidth:  tc.w,
				RenderHeight: tc.h,
			}
			if _, err := govpx.NewVP9Encoder(opts); !errors.Is(err, govpx.ErrInvalidConfig) {
				t.Fatalf("RenderSize=(%d, %d) err = %v, want govpx.ErrInvalidConfig",
					tc.w, tc.h, err)
			}
		})
	}
}

func TestVP9EncoderRenderSizeAppliesToHeader(t *testing.T) {
	const codedW, codedH = 64, 64
	const renderW, renderH = 320, 240
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:        codedW,
		Height:       codedH,
		FPS:          30,
		RenderWidth:  renderW,
		RenderHeight: renderH,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(codedW, codedH, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.Render.Width != renderW || hdr.Render.Height != renderH {
		t.Fatalf("Render = (%d, %d), want (%d, %d)",
			hdr.Render.Width, hdr.Render.Height, renderW, renderH)
	}
	if hdr.Width != codedW || hdr.Height != codedH {
		t.Fatalf("coded = (%d, %d), want (%d, %d)",
			hdr.Width, hdr.Height, codedW, codedH)
	}
}

func TestVP9EncoderRenderSizeDefaultInheritsCoded(t *testing.T) {
	const codedW, codedH = 64, 64
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{
		Width:  codedW,
		Height: codedH,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(codedW, codedH, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	if hdr.Render.Width != codedW || hdr.Render.Height != codedH {
		t.Fatalf("default Render = (%d, %d), want coded (%d, %d)",
			hdr.Render.Width, hdr.Render.Height, codedW, codedH)
	}
}

func TestVP9EncoderRuntimeRenderSizeAppliesToHeader(t *testing.T) {
	e, err := govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	if err := e.SetRenderSize(640, 480); err != nil {
		t.Fatalf("SetRenderSize(640, 480): %v", err)
	}
	hdr := encodeVP9HeaderForRenderSizeTest(t, e)
	if hdr.Render.Width != 640 || hdr.Render.Height != 480 {
		t.Fatalf("Render = (%d, %d), want (640, 480)",
			hdr.Render.Width, hdr.Render.Height)
	}

	e, err = govpx.NewVP9Encoder(govpx.VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("govpx.NewVP9Encoder: %v", err)
	}
	if err := e.SetRenderSize(0, 0); err != nil {
		t.Fatalf("SetRenderSize(0, 0): %v", err)
	}
	hdr = encodeVP9HeaderForRenderSizeTest(t, e)
	if hdr.Render.Width != 64 || hdr.Render.Height != 64 {
		t.Fatalf("cleared Render = (%d, %d), want coded (64, 64)",
			hdr.Render.Width, hdr.Render.Height)
	}
	if err := e.SetRenderSize(-1, 480); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetRenderSize(-1, 480) err = %v, want govpx.ErrInvalidConfig", err)
	}
	if err := e.SetRenderSize(640, 0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("SetRenderSize(640, 0) err = %v, want govpx.ErrInvalidConfig", err)
	}
}

func encodeVP9HeaderForRenderSizeTest(t *testing.T, e *govpx.VP9Encoder) vp9dec.UncompressedHeader {
	t.Helper()
	dst := make([]byte, 65536)
	n, err := e.EncodeInto(vp9test.NewYCbCr(64, 64, 128, 128, 128), dst)
	if err != nil {
		t.Fatalf("EncodeInto: %v", err)
	}
	if n <= 0 {
		t.Fatalf("EncodeInto wrote %d bytes", n)
	}
	hdr, _ := vp9test.ParseHeader(t, dst[:n])
	return hdr
}
