package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
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
			opts := VP9EncoderOptions{
				Width:        64,
				Height:       64,
				FPS:          30,
				RenderWidth:  tc.w,
				RenderHeight: tc.h,
			}
			if _, err := NewVP9Encoder(opts); !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("RenderSize=(%d, %d) err = %v, want ErrInvalidConfig",
					tc.w, tc.h, err)
			}
		})
	}
}

func TestVP9EncoderRenderSizeAppliesToHeader(t *testing.T) {
	const codedW, codedH = 64, 64
	const renderW, renderH = 320, 240
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:        codedW,
		Height:       codedH,
		FPS:          30,
		RenderWidth:  renderW,
		RenderHeight: renderH,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
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
	e, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  codedW,
		Height: codedH,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
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

func TestVP9EncoderSetRenderSizeUpdatesOption(t *testing.T) {
	e, err := NewVP9Encoder(VP9EncoderOptions{Width: 64, Height: 64, FPS: 30})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	if err := e.SetRenderSize(640, 480); err != nil {
		t.Fatalf("SetRenderSize(640, 480): %v", err)
	}
	if e.opts.RenderWidth != 640 || e.opts.RenderHeight != 480 {
		t.Fatalf("opts.Render = (%d, %d), want (640, 480)",
			e.opts.RenderWidth, e.opts.RenderHeight)
	}
	if err := e.SetRenderSize(0, 0); err != nil {
		t.Fatalf("SetRenderSize(0, 0): %v", err)
	}
	if e.opts.RenderWidth != 0 || e.opts.RenderHeight != 0 {
		t.Fatalf("opts.Render after clear = (%d, %d), want (0, 0)",
			e.opts.RenderWidth, e.opts.RenderHeight)
	}
	if err := e.SetRenderSize(-1, 480); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRenderSize(-1, 480) err = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetRenderSize(640, 0); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetRenderSize(640, 0) err = %v, want ErrInvalidConfig", err)
	}
}
