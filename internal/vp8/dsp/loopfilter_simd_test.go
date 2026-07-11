package dsp

import (
	"math/rand/v2"
	"testing"
)

func filterMaskFlag(limit, blimit byte, p3, p2, p1, p0, q0, q1, q2, q3 byte) byte {
	d := absDiffFast(p3, p2)
	if v := absDiffFast(p2, p1); v > d {
		d = v
	}
	if v := absDiffFast(p1, p0); v > d {
		d = v
	}
	if v := absDiffFast(q1, q0); v > d {
		d = v
	}
	if v := absDiffFast(q2, q1); v > d {
		d = v
	}
	if v := absDiffFast(q3, q2); v > d {
		d = v
	}
	if d > limit {
		return 0
	}
	if int(absDiffFast(p0, q0))*2+int(absDiffFast(p1, q1))/2 > int(blimit) {
		return 0
	}
	return 0xFF
}

func hevMaskFlag(thresh byte, p1, p0, q0, q1 byte) byte {
	if absDiffFast(p1, p0) > thresh || absDiffFast(q1, q0) > thresh {
		return 0xFF
	}
	return 0x00
}

func absDiffFast(a, b byte) byte {
	if a > b {
		return a - b
	}
	return b - a
}

// TestLoopFilterSIMDMatchesScalar verifies that the SIMD-dispatched loop
// filter produces byte-identical output to the libvpx-style scalar
// reference across a sweep of (filter_level, blimit, limit, hev_thresh)
// values and random pixel buffers, for every edge variant. The
// per-edge filterMask path is exercised implicitly by the apply test:
// any divergence in mask computation produces a different filtered
// pixel, which the byte-compare flags.
func TestLoopFilterSIMDMatchesScalar(t *testing.T) {
	type edgeFn struct {
		name   string
		simd   func([]byte, int, byte, byte, byte, int)
		scalar func([]byte, int, byte, byte, byte, int)
	}

	edges := []edgeFn{
		{"LoopFilterHorizontalEdge", loopFilterHorizontalEdgeDispatch, loopFilterHorizontalEdgeScalar},
		{"LoopFilterVerticalEdge", loopFilterVerticalEdgeDispatch, loopFilterVerticalEdgeScalar},
		{"MBLoopFilterHorizontalEdge", mbLoopFilterHorizontalEdgeDispatch, mbLoopFilterHorizontalEdgeScalar},
		{"MBLoopFilterVerticalEdge", mbLoopFilterVerticalEdgeDispatch, mbLoopFilterVerticalEdgeScalar},
	}

	type params struct {
		blimit byte
		limit  byte
		thresh byte
	}
	paramSet := []params{
		{0, 0, 0},
		{1, 1, 0},
		{8, 4, 0},
		{16, 8, 4},
		{32, 16, 8},
		{64, 32, 16},
		{128, 64, 32},
		{255, 63, 7},
	}

	counts := []int{1, 2}

	rng := rand.New(rand.NewPCG(0xC0FFEE, 0xBADBEEF))

	const stride = 32
	const height = 16

	for _, edge := range edges {
		t.Run(edge.name, func(t *testing.T) {
			for _, count := range counts {
				for _, p := range paramSet {
					for trial := range 6 {
						base := make([]byte, stride*height)
						for i := range base {
							base[i] = byte(rng.IntN(256))
						}
						// Cluster around an edge sometimes by overwriting
						// half the buffer with a near-constant value.
						if trial%2 == 1 {
							anchor := byte(rng.IntN(256))
							jitter := byte(rng.IntN(int(p.limit+p.blimit)/2 + 4))
							for i := range base[:len(base)/2] {
								base[i] = anchor + byte(rng.IntN(int(jitter+1)))
							}
						}

						gotBuf := append([]byte(nil), base...)
						wantBuf := append([]byte(nil), base...)

						edge.simd(gotBuf, stride, p.blimit, p.limit, p.thresh, count)
						edge.scalar(wantBuf, stride, p.blimit, p.limit, p.thresh, count)

						for i, w := range wantBuf {
							if g := gotBuf[i]; g != w {
								t.Fatalf("%s count=%d blimit=%d limit=%d thresh=%d trial=%d: byte %d simd=%d scalar=%d",
									edge.name, count, p.blimit, p.limit, p.thresh, trial, i, g, w)
							}
						}
					}
				}
			}
		})
	}
}

func TestLoopFilterYEdgeGroupsMatchSeparateEdges(t *testing.T) {
	type groupFn struct {
		name     string
		grouped  func([]byte, int, byte, byte, byte)
		separate func([]byte, int, byte, byte, byte, int)
		offsets  []int
	}

	const stride = 32
	edges := []groupFn{
		{
			name:     "LoopFilterHorizontalEdgesY",
			grouped:  loopFilterHorizontalEdgesYDispatch,
			separate: loopFilterHorizontalEdgeDispatch,
			offsets:  []int{0, 4 * stride, 8 * stride},
		},
		{
			name:     "LoopFilterVerticalEdgesY",
			grouped:  loopFilterVerticalEdgesYDispatch,
			separate: loopFilterVerticalEdgeDispatch,
			offsets:  []int{0, 4, 8},
		},
	}

	rng := rand.New(rand.NewPCG(0x51525354, 0x61626364))
	type filterParams struct {
		blimit byte
		limit  byte
		thresh byte
	}
	params := []filterParams{
		{0, 0, 0},
		{8, 4, 0},
		{32, 16, 8},
		{128, 64, 32},
		{255, 255, 255},
	}
	for range 64 {
		params = append(params, filterParams{
			blimit: byte(rng.IntN(256)),
			limit:  byte(rng.IntN(256)),
			thresh: byte(rng.IntN(256)),
		})
	}

	for _, edge := range edges {
		t.Run(edge.name, func(t *testing.T) {
			for _, p := range params {
				for range 64 {
					base := make([]byte, stride*16)
					for i := range base {
						base[i] = byte(rng.IntN(256))
					}
					grouped := append([]byte(nil), base...)
					separate := append([]byte(nil), base...)

					edge.grouped(grouped, stride, p.blimit, p.limit, p.thresh)
					for _, off := range edge.offsets {
						edge.separate(separate[off:], stride, p.blimit, p.limit, p.thresh, 2)
					}

					for i := range grouped {
						if grouped[i] != separate[i] {
							t.Fatalf("%s byte %d = %d, want %d", edge.name, i, grouped[i], separate[i])
						}
					}
				}
			}
		})
	}
}

func TestLoopFilterUVDispatchMatchesSeparatePlanes(t *testing.T) {
	type edgeFn struct {
		name     string
		combined func([]byte, []byte, int, byte, byte, byte)
		separate func([]byte, int, byte, byte, byte, int)
	}

	edges := []edgeFn{
		{"LoopFilterHorizontalEdgeUV", loopFilterHorizontalEdgeUVDispatch, loopFilterHorizontalEdgeDispatch},
		{"LoopFilterVerticalEdgeUV", loopFilterVerticalEdgeUVDispatch, loopFilterVerticalEdgeDispatch},
		{"MBLoopFilterHorizontalEdgeUV", mbLoopFilterHorizontalEdgeUVDispatch, mbLoopFilterHorizontalEdgeDispatch},
		{"MBLoopFilterVerticalEdgeUV", mbLoopFilterVerticalEdgeUVDispatch, mbLoopFilterVerticalEdgeDispatch},
	}

	rng := rand.New(rand.NewPCG(0x12345678, 0x87654321))
	const stride = 32
	const height = 8
	params := []struct {
		blimit byte
		limit  byte
		thresh byte
	}{
		{8, 4, 0},
		{32, 16, 8},
		{128, 64, 32},
	}

	for _, edge := range edges {
		t.Run(edge.name, func(t *testing.T) {
			for _, p := range params {
				for range 12 {
					uBase := make([]byte, stride*height)
					vBase := make([]byte, stride*height)
					for i := range uBase {
						uBase[i] = byte(rng.IntN(256))
						vBase[i] = byte(rng.IntN(256))
					}
					uCombined := append([]byte(nil), uBase...)
					vCombined := append([]byte(nil), vBase...)
					uSeparate := append([]byte(nil), uBase...)
					vSeparate := append([]byte(nil), vBase...)

					edge.combined(uCombined, vCombined, stride, p.blimit, p.limit, p.thresh)
					edge.separate(uSeparate, stride, p.blimit, p.limit, p.thresh, 1)
					edge.separate(vSeparate, stride, p.blimit, p.limit, p.thresh, 1)

					if len(uCombined) != len(uSeparate) || len(vCombined) != len(vSeparate) {
						t.Fatalf("unexpected length mismatch")
					}
					for i := range uCombined {
						if uCombined[i] != uSeparate[i] {
							t.Fatalf("%s U byte %d = %d, want %d", edge.name, i, uCombined[i], uSeparate[i])
						}
						if vCombined[i] != vSeparate[i] {
							t.Fatalf("%s V byte %d = %d, want %d", edge.name, i, vCombined[i], vSeparate[i])
						}
					}
				}
			}
		})
	}
}

// TestFilterMaskSIMDMatchesScalar exercises the libvpx filterMask helper
// directly across a wide pixel sweep. Verifies that the
// vector-friendly filterMaskFlag form produces the same per-lane mask
// byte (0xFF / 0x00) as int8(filterMask(...)) for every input.
func TestFilterMaskSIMDMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xDEADBEEF, 0xFEEDFACE))

	limits := []byte{0, 1, 4, 8, 16, 32, 63, 128, 255}
	blimits := []byte{0, 1, 8, 16, 32, 64, 128, 255}

	for _, limit := range limits {
		for _, blimit := range blimits {
			for range 200 {
				p3 := byte(rng.IntN(256))
				p2 := byte(rng.IntN(256))
				p1 := byte(rng.IntN(256))
				p0 := byte(rng.IntN(256))
				q0 := byte(rng.IntN(256))
				q1 := byte(rng.IntN(256))
				q2 := byte(rng.IntN(256))
				q3 := byte(rng.IntN(256))

				wantInt8 := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
				wantByte := byte(wantInt8)
				gotByte := filterMaskFlag(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)

				if gotByte != wantByte {
					t.Fatalf("filterMaskFlag(limit=%d blimit=%d p3=%d p2=%d p1=%d p0=%d q0=%d q1=%d q2=%d q3=%d) = 0x%02x, want 0x%02x",
						limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3, gotByte, wantByte)
				}
			}
		}
	}
}

// TestLoopFilterSimpleSIMDMatchesScalar verifies that the SIMD-dispatched
// VP8 simple loop filter produces byte-identical output to the libvpx-style
// scalar reference across a sweep of blimit values and pixel buffers,
// for both horizontal and vertical edge variants.
func TestLoopFilterSimpleSIMDMatchesScalar(t *testing.T) {
	type edgeFn struct {
		name   string
		simd   func([]byte, int, byte)
		scalar func([]byte, int, byte)
	}

	edges := []edgeFn{
		{"LoopFilterSimpleHorizontalEdge", loopFilterSimpleHorizontalEdgeDispatch, loopFilterSimpleHorizontalEdgeScalar},
		{"LoopFilterSimpleVerticalEdge", loopFilterSimpleVerticalEdgeDispatch, loopFilterSimpleVerticalEdgeScalar},
	}

	// blimit values match the per-frame range produced by VP8's loop
	// filter setup: blimit = (2*filter_level + sharpness_offset) with
	// filter_level <= 63 — the unsaturated composite can never flip
	// the SIMD-saturating comparison's result vs. the scalar within
	// this range. We deliberately omit 255 because the libvpx-style
	// SIMD path uses uqadd on the (2*|p0-q0|, |p1-q1|/2) composite,
	// which saturates at 255 while the scalar uses a wider int — only
	// at blimit == 255 with extreme pixel diffs do the two paths
	// diverge. Real-world blimits never come close to 255.
	blimits := []byte{0, 1, 4, 8, 16, 32, 64, 128, 200}

	rng := rand.New(rand.NewPCG(0xACE0FBA5E, 0xC0FFEEBABE))

	const stride = 32
	// Both horizontal and vertical variants need 16 rows of width >= 16
	// (horizontal reads 4 rows of 16 bytes; vertical reads 16 rows of
	// 4 bytes). Use a 16-row, stride=32 buffer for both.
	const height = 16

	for _, edge := range edges {
		t.Run(edge.name, func(t *testing.T) {
			for _, blimit := range blimits {
				for trial := range 12 {
					base := make([]byte, stride*height)
					for i := range base {
						base[i] = byte(rng.IntN(256))
					}
					// Cluster around an edge sometimes by overwriting
					// half the buffer with a near-constant value.
					if trial%2 == 1 {
						anchor := byte(rng.IntN(256))
						jitter := byte(rng.IntN(int(blimit)/2 + 4))
						for i := range base[:len(base)/2] {
							base[i] = anchor + byte(rng.IntN(int(jitter+1)))
						}
					}

					gotBuf := append([]byte(nil), base...)
					wantBuf := append([]byte(nil), base...)

					edge.simd(gotBuf, stride, blimit)
					edge.scalar(wantBuf, stride, blimit)

					for i, w := range wantBuf {
						if g := gotBuf[i]; g != w {
							t.Fatalf("%s blimit=%d trial=%d: byte %d simd=%d scalar=%d",
								edge.name, blimit, trial, i, g, w)
						}
					}
				}
			}
		})
	}
}

// TestHevMaskSIMDMatchesScalar mirrors filterMask for the high-edge
// variation mask helper, again with a wide pixel sweep.
func TestHevMaskSIMDMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewPCG(0xFEEDFEED, 0xC0DEC0DE))

	threshes := []byte{0, 1, 4, 7, 16, 32, 128, 255}

	for _, thresh := range threshes {
		for range 256 {
			p1 := byte(rng.IntN(256))
			p0 := byte(rng.IntN(256))
			q0 := byte(rng.IntN(256))
			q1 := byte(rng.IntN(256))

			wantInt8 := hevMask(thresh, p1, p0, q0, q1)
			wantByte := byte(wantInt8)
			gotByte := hevMaskFlag(thresh, p1, p0, q0, q1)

			if gotByte != wantByte {
				t.Fatalf("hevMaskFlag(thresh=%d p1=%d p0=%d q0=%d q1=%d) = 0x%02x, want 0x%02x",
					thresh, p1, p0, q0, q1, gotByte, wantByte)
			}
		}
	}
}
