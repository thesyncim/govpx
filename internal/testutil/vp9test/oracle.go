//go:build govpx_oracle_trace

package vp9test

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

func VpxencPackets(t testing.TB, sources []*image.YCbCr, extraArgs ...string) [][]byte {
	t.Helper()
	width, height := requireSameSizeSources(t, "VP9 vpxenc source", sources)
	var raw []byte
	for _, src := range sources {
		raw = AppendI420(raw, src)
	}
	ivf, diag, err := coracle.VpxencVP9EncodeI420(raw, width, height,
		len(sources), extraArgs...)
	if err != nil {
		t.Fatalf("vpxenc-vp9 encode failed: %v\n%s", err, diag)
	}
	return RequireIVFPackets(t, ivf, len(sources))
}

func requireSameSizeSources(t testing.TB, label string, sources []*image.YCbCr) (width, height int) {
	t.Helper()
	if len(sources) == 0 {
		t.Fatalf("empty %s", label)
	}
	width = sources[0].Rect.Dx()
	height = sources[0].Rect.Dy()
	for i, src := range sources {
		if src.Rect.Dx() != width || src.Rect.Dy() != height {
			t.Fatalf("%s %d dimension mismatch: got %dx%d want %dx%d",
				label, i, src.Rect.Dx(), src.Rect.Dy(), width, height)
		}
	}
	return width, height
}
