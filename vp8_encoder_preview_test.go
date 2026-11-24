package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP8EncoderPreviewFrameMirrorsDecodedOutput(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	if _, ok := e.PreviewFrame(); ok {
		t.Fatalf("PreviewFrame before encode ok = true, want false")
	}

	src := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(src, 70, 90, 170)
	dst := make([]byte, 4096)
	result, err := e.EncodeInto(dst, src, 0, 1, 0)
	if err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}

	preview, ok := e.PreviewFrame()
	if !ok {
		t.Fatalf("PreviewFrame after key frame ok = false, want true")
	}
	decoded := decodeVP8FacadeFrame(t, result.Data)
	assertVP8FacadeImagesEqual(t, "preview", decoded, preview)

	copied := newVP8FacadeImage(16, 16)
	ok, err = e.CopyPreviewFrame(&copied)
	if err != nil || !ok {
		t.Fatalf("CopyPreviewFrame = (%v, %v), want (true, nil)", ok, err)
	}
	assertVP8FacadeImagesEqual(t, "copied preview", preview, copied)
}

func TestVP8EncoderPreviewPostProcessControlValidation(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	if err := e.SetPreviewPostProcessConfig(govpx.PostProcessDeblock, 17, 0); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("bad deblock error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetPreviewPostProcess(govpx.PostProcessDeblock, 4); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("noise without AddNoise error = %v, want ErrInvalidConfig", err)
	}
	if err := e.SetPreviewPostProcessConfig(govpx.PostProcessDeblock|govpx.PostProcessDemacroblock, 4, 0); err != nil {
		t.Fatalf("SetPreviewPostProcessConfig returned error: %v", err)
	}

	src := newVP8FacadeImage(16, 16)
	fillVP8FacadeImage(src, 64, 90, 170)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, src, 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	first, ok := e.PreviewFrame()
	if !ok {
		t.Fatalf("postprocessed PreviewFrame ok = false, want true")
	}
	second, ok := e.PreviewFrame()
	if !ok {
		t.Fatalf("cached PreviewFrame ok = false, want true")
	}
	assertVP8FacadeImagesEqual(t, "cached preview", first, second)
}

func TestVP8EncoderPreviewSuppressedAfterAltRefRefresh(t *testing.T) {
	e := newVP8FacadeEncoder(t)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 0, 1, 0); err != nil {
		t.Fatalf("key EncodeInto returned error: %v", err)
	}
	if _, ok := e.PreviewFrame(); !ok {
		t.Fatalf("PreviewFrame after key frame ok = false, want true")
	}

	flags := govpx.EncodeInvisibleFrame |
		govpx.EncodeForceAltRefFrame |
		govpx.EncodeNoUpdateLast |
		govpx.EncodeNoUpdateGolden
	if _, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 1, 1, flags); err != nil {
		t.Fatalf("alt-ref EncodeInto returned error: %v", err)
	}
	if _, ok := e.PreviewFrame(); ok {
		t.Fatalf("PreviewFrame after alt-ref refresh ok = true, want false")
	}

	if _, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 2, 1, 0); err != nil {
		t.Fatalf("visible inter EncodeInto returned error: %v", err)
	}
	if _, ok := e.PreviewFrame(); !ok {
		t.Fatalf("PreviewFrame after visible inter ok = false, want true")
	}
}

func TestVP8EncoderPreviewClosedAndInvalidCopy(t *testing.T) {
	var nilEnc *govpx.VP8Encoder
	if err := nilEnc.SetPreviewPostProcess(govpx.PostProcessDeblock, 0); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("nil SetPreviewPostProcess error = %v, want ErrClosed", err)
	}
	if _, ok := nilEnc.PreviewFrame(); ok {
		t.Fatalf("nil PreviewFrame ok = true, want false")
	}
	if _, err := nilEnc.CopyPreviewFrame(nil); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("nil CopyPreviewFrame error = %v, want ErrClosed", err)
	}

	e := newVP8FacadeEncoder(t)
	dst := make([]byte, 4096)
	if _, err := e.EncodeInto(dst, newVP8FacadeImage(16, 16), 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto returned error: %v", err)
	}
	wrong := newVP8FacadeImage(32, 16)
	if _, err := e.CopyPreviewFrame(&wrong); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("wrong-size CopyPreviewFrame error = %v, want ErrInvalidConfig", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if err := e.SetPreviewPostProcess(govpx.PostProcessDeblock, 0); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed SetPreviewPostProcess error = %v, want ErrClosed", err)
	}
	if _, err := e.CopyPreviewFrame(&wrong); !errors.Is(err, govpx.ErrClosed) {
		t.Fatalf("closed CopyPreviewFrame error = %v, want ErrClosed", err)
	}
}
