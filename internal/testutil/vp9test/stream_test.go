package vp9test

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

func TestNewSteppedSources(t *testing.T) {
	sources := NewSteppedSources(16, 16, 3)
	if len(sources) != 3 {
		t.Fatalf("sources = %d, want 3", len(sources))
	}
	for i, src := range sources {
		if got := src.Rect.Dx(); got != 16 {
			t.Fatalf("source %d width = %d, want 16", i, got)
		}
		if got := src.Rect.Dy(); got != 16 {
			t.Fatalf("source %d height = %d, want 16", i, got)
		}
		wantY := byte(96 + i*8)
		if got := src.Y[0]; got != wantY {
			t.Fatalf("source %d Y[0] = %d, want %d", i, got, wantY)
		}
	}
}

func TestFillPanningYCbCrMatchesConstructor(t *testing.T) {
	want := NewPanningYCbCr(16, 12, 5)
	got := NewYCbCr(16, 12, 0, 0, 0)
	FillPanningYCbCr(got, 5)
	if !bytes.Equal(got.Y, want.Y) ||
		!bytes.Equal(got.Cb, want.Cb) ||
		!bytes.Equal(got.Cr, want.Cr) {
		t.Fatal("FillPanningYCbCr output differs from NewPanningYCbCr")
	}
}

func TestNewBlockCheckerYCbCr(t *testing.T) {
	img := NewBlockCheckerYCbCr(64, 64, 0)
	tests := []struct {
		x, y int
		want byte
	}{
		{0, 0, 96},
		{32, 0, 160},
		{0, 32, 160},
		{32, 32, 96},
	}
	for _, tc := range tests {
		if got := img.Y[tc.y*img.YStride+tc.x]; got != tc.want {
			t.Fatalf("Y(%d,%d) = %d, want %d", tc.x, tc.y, got, tc.want)
		}
	}

	next := NewBlockCheckerYCbCr(64, 64, 1)
	if got := next.Y[0]; got != 160 {
		t.Fatalf("frame-shifted Y(0,0) = %d, want 160", got)
	}
}

func TestNewRuntimeResizeSources(t *testing.T) {
	sources := NewRuntimeResizeSources(16, 16, 32, 24, 2, 4)
	for i, src := range sources {
		wantW, wantH := 16, 16
		if i >= 2 {
			wantW, wantH = 32, 24
		}
		if got := src.Rect.Dx(); got != wantW {
			t.Fatalf("source %d width = %d, want %d", i, got, wantW)
		}
		if got := src.Rect.Dy(); got != wantH {
			t.Fatalf("source %d height = %d, want %d", i, got, wantH)
		}
	}
}

func TestCountByteParityMatches(t *testing.T) {
	got := [][]byte{[]byte("a"), []byte("b"), []byte("c")}
	want := [][]byte{[]byte("a"), []byte("x"), []byte("c")}
	matches, firstMismatch := CountByteParityMatches(got, want)
	if matches != 2 || firstMismatch != 1 {
		t.Fatalf("matches=%d firstMismatch=%d, want 2 and 1", matches, firstMismatch)
	}

	matches, firstMismatch = CountByteParityMatches(got, got)
	if matches != 3 || firstMismatch != -1 {
		t.Fatalf("all-match matches=%d firstMismatch=%d, want 3 and -1",
			matches, firstMismatch)
	}
}

func TestRequireIVFPackets(t *testing.T) {
	payloads := [][]byte{[]byte("first"), []byte("second")}
	ivf := testutil.BuildIVF(testutil.IVFHeader{
		FourCC:              testutil.IVFFourCCVP9,
		Width:               16,
		Height:              16,
		TimebaseDenominator: 30,
		TimebaseNumerator:   1,
	}, payloads)

	packets := RequireIVFPackets(t, ivf, len(payloads))
	if !bytes.Equal(packets[0], payloads[0]) || !bytes.Equal(packets[1], payloads[1]) {
		t.Fatalf("packets = %q, want %q", packets, payloads)
	}
}
