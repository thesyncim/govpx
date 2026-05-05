package encoder_test

import (
	"errors"
	"testing"

	libgopx "github.com/thesyncim/libgopx"
	vp8enc "github.com/thesyncim/libgopx/internal/vp8/encoder"
)

func TestBuildNeutralPredictorKeyFrameCoefficientsDecodes(t *testing.T) {
	src := solidSourceImage(16, 16, 220, 90, 170)
	modes := make([]vp8enc.KeyFrameMacroblockMode, 1)
	coeffs := make([]vp8enc.MacroblockCoefficients, 1)

	if err := vp8enc.BuildNeutralPredictorKeyFrameCoefficients(src, 20, modes, coeffs); err != nil {
		t.Fatalf("BuildNeutralPredictorKeyFrameCoefficients returned error: %v", err)
	}
	if coeffs[0].QCoeff[24] == ([16]int16{}) {
		t.Fatalf("Y2 coefficients are zero, want luma residual")
	}

	packet := make([]byte, 4096)
	above := make([]vp8enc.TokenContextPlanes, 1)
	n, err := vp8enc.WriteCoefficientKeyFrame(packet, 16, 16, vp8enc.KeyFrameStateConfig{BaseQIndex: 20}, modes, coeffs, above)
	if err != nil {
		t.Fatalf("WriteCoefficientKeyFrame returned error: %v", err)
	}

	d, err := libgopx.NewVP8Decoder(libgopx.DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP8Decoder returned error: %v", err)
	}
	if err := d.Decode(packet[:n]); err != nil {
		t.Fatalf("Decode returned error: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatalf("NextFrame returned no frame")
	}
	if frame.Y[0] <= 128 || frame.U[0] >= 128 || frame.V[0] <= 128 {
		t.Fatalf("decoded samples = %d/%d/%d, want source-directed residuals", frame.Y[0], frame.U[0], frame.V[0])
	}
}

func TestBuildNeutralPredictorKeyFrameCoefficientsClearsNeutralBlocks(t *testing.T) {
	src := solidSourceImage(16, 16, 128, 128, 128)
	modes := make([]vp8enc.KeyFrameMacroblockMode, 1)
	coeffs := make([]vp8enc.MacroblockCoefficients, 1)
	coeffs[0].QCoeff[0][0] = 99
	coeffs[0].QCoeff[24][0] = 99

	if err := vp8enc.BuildNeutralPredictorKeyFrameCoefficients(src, 20, modes, coeffs); err != nil {
		t.Fatalf("BuildNeutralPredictorKeyFrameCoefficients returned error: %v", err)
	}
	if coeffs[0] != (vp8enc.MacroblockCoefficients{}) {
		t.Fatalf("coeffs = %+v, want zero for neutral source", coeffs[0])
	}
}

func TestBuildNeutralPredictorKeyFrameCoefficientsRejectsInvalidInput(t *testing.T) {
	src := solidSourceImage(16, 16, 128, 128, 128)
	modes := make([]vp8enc.KeyFrameMacroblockMode, 1)
	coeffs := make([]vp8enc.MacroblockCoefficients, 1)

	if err := vp8enc.BuildNeutralPredictorKeyFrameCoefficients(src, -1, modes, coeffs); !errors.Is(err, vp8enc.ErrInvalidPacketConfig) {
		t.Fatalf("bad quantizer error = %v, want ErrInvalidPacketConfig", err)
	}
	if err := vp8enc.BuildNeutralPredictorKeyFrameCoefficients(src, 20, nil, coeffs); !errors.Is(err, vp8enc.ErrModeBufferTooSmall) {
		t.Fatalf("short modes error = %v, want ErrModeBufferTooSmall", err)
	}
	src.Y = src.Y[:4]
	if err := vp8enc.BuildNeutralPredictorKeyFrameCoefficients(src, 20, modes, coeffs); !errors.Is(err, vp8enc.ErrInvalidPacketConfig) {
		t.Fatalf("short source error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestBuildNeutralPredictorKeyFrameCoefficientsAllocatesZero(t *testing.T) {
	src := solidSourceImage(16, 16, 200, 110, 150)
	modes := make([]vp8enc.KeyFrameMacroblockMode, 1)
	coeffs := make([]vp8enc.MacroblockCoefficients, 1)

	allocs := testing.AllocsPerRun(1000, func() {
		_ = vp8enc.BuildNeutralPredictorKeyFrameCoefficients(src, 20, modes, coeffs)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkBuildNeutralPredictorKeyFrameCoefficients(b *testing.B) {
	src := solidSourceImage(64, 64, 200, 110, 150)
	rows := (src.Height + 15) >> 4
	cols := (src.Width + 15) >> 4
	modes := make([]vp8enc.KeyFrameMacroblockMode, rows*cols)
	coeffs := make([]vp8enc.MacroblockCoefficients, rows*cols)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = vp8enc.BuildNeutralPredictorKeyFrameCoefficients(src, 20, modes, coeffs)
	}
}

func solidSourceImage(width int, height int, y byte, u byte, v byte) vp8enc.SourceImage {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	src := vp8enc.SourceImage{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
	for i := range src.Y {
		src.Y[i] = y
	}
	for i := range src.U {
		src.U[i] = u
	}
	for i := range src.V {
		src.V[i] = v
	}
	return src
}
