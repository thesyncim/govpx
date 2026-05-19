package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

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
