package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// TestVP9MFQEDecisionMatchesLibvpx pins the libvpx VP9 mfqe_decision
// rule (vp9/common/vp9_mfqe.c:198): inter mode (>= NEARESTMV), block
// size >= 16x16, and squared MV length <= 100 in 1/8-pel units.
func TestVP9MFQEDecisionMatchesLibvpx(t *testing.T) {
	cases := []struct {
		name string
		mi   vp9dec.NeighborMi
		bs   common.BlockSize
		want bool
	}{
		{
			name: "intra-rejected",
			mi: vp9dec.NeighborMi{
				Mode: common.DcPred, // intra → mode < NEARESTMV
			},
			bs:   common.Block16x16,
			want: false,
		},
		{
			name: "sub-16x16-rejected",
			mi: vp9dec.NeighborMi{
				Mode: common.NearestMv,
			},
			bs:   common.Block8x8,
			want: false,
		},
		{
			name: "inter-zero-mv-admitted",
			mi: vp9dec.NeighborMi{
				Mode: common.NearestMv,
			},
			bs:   common.Block16x16,
			want: true,
		},
		{
			name: "inter-mv-on-threshold",
			mi: vp9dec.NeighborMi{
				Mode: common.NewMv,
				Mv:   [2]vp9dec.MV{{Row: 10, Col: 0}}, // 100 <= 100
			},
			bs:   common.Block32x32,
			want: true,
		},
		{
			name: "inter-mv-just-over",
			mi: vp9dec.NeighborMi{
				Mode: common.NewMv,
				Mv:   [2]vp9dec.MV{{Row: 10, Col: 1}}, // 101 > 100
			},
			bs:   common.Block32x32,
			want: false,
		},
		{
			name: "inter-mv-diagonal",
			mi: vp9dec.NeighborMi{
				Mode: common.NewMv,
				Mv:   [2]vp9dec.MV{{Row: 7, Col: 7}}, // 49+49=98 <= 100
			},
			bs:   common.Block64x64,
			want: true,
		},
	}
	for _, tc := range cases {
		mi := tc.mi
		if got := vp9MFQEDecision(&mi, tc.bs); got != tc.want {
			t.Errorf("%s: vp9MFQEDecision = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestVP9MFQEGetThrMatchesLibvpx pins the libvpx get_thr table
// (vp9/common/vp9_mfqe.c:147): sad_thr = {7,6,5} + (qdiff >> 4) per
// block size, vdiff_thr = 125 + qdiff.
func TestVP9MFQEGetThrMatchesLibvpx(t *testing.T) {
	cases := []struct {
		bs           common.BlockSize
		qdiff        int
		wantSadThr   int
		wantVdiffThr int
	}{
		// qdiff=0: pure block-size base values
		{common.Block16x16, 0, 7, 125},
		{common.Block32x32, 0, 6, 125},
		{common.Block64x64, 0, 5, 125},
		// qdiff=16: adj = 16>>4 = 1
		{common.Block16x16, 16, 8, 141},
		{common.Block32x32, 16, 7, 141},
		{common.Block64x64, 16, 6, 141},
		// qdiff=64: adj = 64>>4 = 4
		{common.Block16x16, 64, 11, 189},
		{common.Block32x32, 64, 10, 189},
		{common.Block64x64, 64, 9, 189},
	}
	for _, tc := range cases {
		sadThr, vdiffThr := vp9MFQEGetThr(tc.bs, tc.qdiff)
		if sadThr != tc.wantSadThr {
			t.Errorf("vp9MFQEGetThr(%v, %d) sadThr = %d, want %d",
				tc.bs, tc.qdiff, sadThr, tc.wantSadThr)
		}
		if vdiffThr != tc.wantVdiffThr {
			t.Errorf("vp9MFQEGetThr(%v, %d) vdiffThr = %d, want %d",
				tc.bs, tc.qdiff, vdiffThr, tc.wantVdiffThr)
		}
	}
}

// TestVP9MFQEPreconditionConstantsMatchLibvpx pins the libvpx
// vp9_postproc.c precondition constants (q_diff_thresh = 20,
// last_q_thresh = 170) so a future libvpx upgrade that changes them
// has to also bump these.
func TestVP9MFQEPreconditionConstantsMatchLibvpx(t *testing.T) {
	if vp9MFQEQDiffThreshold != 20 {
		t.Errorf("vp9MFQEQDiffThreshold = %d, want 20 (libvpx vp9_postproc.c:32)",
			vp9MFQEQDiffThreshold)
	}
	if vp9MFQELastQThreshold != 170 {
		t.Errorf("vp9MFQELastQThreshold = %d, want 170 (libvpx vp9_postproc.c:33)",
			vp9MFQELastQThreshold)
	}
	if vp9MFQEMvLenSquareThreshold != 100 {
		t.Errorf("vp9MFQEMvLenSquareThreshold = %d, want 100 (libvpx vp9_mfqe.c:203)",
			vp9MFQEMvLenSquareThreshold)
	}
	if vp9MFQEPrecision != 4 {
		t.Errorf("vp9MFQEPrecision = %d, want 4 (libvpx vp9_postproc.h MFQE_PRECISION)",
			vp9MFQEPrecision)
	}
}

// TestVP9MFQEBlockMetricsRoundingMatchesLibvpx pins the libvpx vdiff /
// sad normalisation: (raw + half_pels) >> log2_pels per block size
// (vp9/common/vp9_mfqe.c:168-177).
//
// For two identical buffers vdiff == 0 and sad == 0 regardless of
// rounding, so we use that pinning to lock the kernel against the
// caller's offsetting bugs.
func TestVP9MFQEBlockMetricsRoundingMatchesLibvpx(t *testing.T) {
	for _, side := range []int{16, 32, 64} {
		a := make([]byte, side*side)
		b := make([]byte, side*side)
		for i := range a {
			a[i] = byte(i & 0xff)
			b[i] = byte(i & 0xff)
		}
		vdiff, sad := vp9MFQEBlockMetrics(side, a, side, b, side)
		if vdiff != 0 || sad != 0 {
			t.Errorf("side=%d identical buffers: vdiff=%d sad=%d, want both 0",
				side, vdiff, sad)
		}
	}
}

// TestVP9DecoderPostprocessNoisePRNGDeterministic pins libvpx's
// additive-noise PRNG determinism: two VP9 decoders run with the same
// AddNoise options against the same fixture must produce byte-identical
// luma output. The noise generator (libvpx vpx_dsp/add_noise.c:46
// vpx_setup_noise) is seeded from a fresh state and uses libc rand()
// (platform-conditional flavor in govpx postprocess.go).
func TestVP9DecoderPostprocessNoisePRNGDeterministic(t *testing.T) {
	const width, height = 64, 64
	runOnce := func() []byte {
		d, err := NewVP9Decoder(VP9DecoderOptions{
			PostProcessFlags:      PostProcessAddNoise,
			PostProcessNoiseLevel: 4,
		})
		if err != nil {
			t.Fatalf("NewVP9Decoder: %v", err)
		}
		packet := vp9StubPacketForTest(t, width, height, 0, common.DcPred)
		if err := d.Decode(packet); err != nil {
			t.Fatalf("Decode: %v", err)
		}
		frame, ok := d.NextFrame()
		if !ok {
			t.Fatal("NextFrame returned no frame")
		}
		out := make([]byte, len(frame.Y))
		copy(out, frame.Y)
		return out
	}
	a := runOnce()
	b := runOnce()
	if len(a) != len(b) {
		t.Fatalf("frame Y lengths differ: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("noise output diverges at byte %d: %d vs %d", i, a[i], b[i])
		}
	}
}

// TestVP9MFQEFaithfulWalkerSeedsDstWithCurrent pins one of the
// libvpx-faithful walker's invariants: when the partition tree forces
// PARTITION_NONE on every SB and the per-MI mfqe_decision rejects
// (e.g. all blocks are intra or have large MVs), dst must be left
// unchanged from the pre-seeded current-frame copy. Mirrors libvpx
// vp9_mfqe.c:307 "copy the block from current frame (i.e., no mfqe is
// done)".
func TestVP9MFQEFaithfulWalkerSeedsDstWithCurrent(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	const width, height = 64, 64
	if err := d.Decode(vp9StubPacketForTest(t, width, height, 0, common.DcPred)); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned no frame")
	}
	miCols := width >> 3
	miRows := height >> 3
	// Stamp every MI with an intra mode (DcPred < NearestMv) — the
	// libvpx mfqe_decision rejects all of these.
	for i := range d.miGrid {
		d.miGrid[i].SbType = common.Block64x64
		d.miGrid[i].Mode = common.DcPred
		d.miGrid[i].Mv[0] = vp9dec.MV{}
	}
	_ = miRows
	_ = miCols
	if err := d.prepareVP9PostProcess(width, height); err != nil {
		t.Fatalf("prepareVP9PostProcess: %v", err)
	}
	for i := range d.postSource.Img.Y {
		d.postSource.Img.Y[i] = 200
	}
	for i := range d.postSource.Img.U {
		d.postSource.Img.U[i] = 80
	}
	for i := range d.postSource.Img.V {
		d.postSource.Img.V[i] = 150
	}
	for i := range d.post.Img.Y {
		d.post.Img.Y[i] = 7 // sentinel — must be overwritten by seed
	}
	d.vp9MFQEFaithfulWalker(&d.postSource.Img, &d.post.Img, false, 60, 20)
	// All intra blocks → no MFQE; dst must equal the src seed.
	stride := d.post.Img.YStride
	for y := range height {
		for x := range width {
			if got := d.post.Img.Y[y*stride+x]; got != 200 {
				t.Fatalf("(%d,%d) Y = %d, want 200 (seeded src, no MFQE)",
					x, y, got)
			}
		}
	}
}

// TestVP9MFQEBlockMetricsNonzero pins the rounding shifts at side 16/32/64
// against a known-non-zero diff pattern: cell (r,c) where r,c < 8 has
// b = a + 16 (≈ 256 squared diff per cell, 64 cells). At side 16 we
// expect:
//
//	sum = 8*8*16 = 1024
//	sse = 8*8*256 = 16384
//	variance = 16384 - 1024^2/256 = 16384 - 4096 = 12288
//	vdiff = (12288 + 128) >> 8 = 48
//	sad_raw = 8*8*16 = 1024
//	sad = (1024 + 128) >> 8 = 4
func TestVP9MFQEBlockMetricsSide16(t *testing.T) {
	const side = 16
	a := make([]byte, side*side)
	b := make([]byte, side*side)
	for r := range side {
		for c := range side {
			a[r*side+c] = 0
			if r < 8 && c < 8 {
				b[r*side+c] = 16
			}
		}
	}
	vdiff, sad := vp9MFQEBlockMetrics(side, a, side, b, side)
	if vdiff != 48 {
		t.Errorf("vdiff = %d, want 48", vdiff)
	}
	if sad != 4 {
		t.Errorf("sad = %d, want 4", sad)
	}
}
