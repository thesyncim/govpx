package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// vp9FullRDInterSearchPathStreamSHA256 pins the SHA-256 of the full-RD inter
// SearchPartition path (CpuUsed:-3) over a fixed key + 5 inter frame sequence
// that stresses every partition arm (HORZ/VERT/SPLIT/NONE). The original
// skeleton no-op hash intentionally moved after the candidate[2] / full-RD
// follow-up work and again after the keyframe RD partition search/replay port
// made the opening keyframe more libvpx-shaped; this anchor now guards the
// current production SearchPartition stream.
const vp9FullRDInterSearchPathStreamSHA256 = "01ce162b781c8ce11a9060ae0fc30eeeec41cc48cde10dcabc44859b12346a51"

// vp9EncodeFullRDInterSearchPathStream encodes the fixed SearchPartition
// regression sequence and returns the concatenated bitstream.
func vp9EncodeFullRDInterSearchPathStream(t *testing.T) []byte {
	t.Helper()
	const width, height = 64, 64
	// CpuUsed:-3 pins PartitionSearchType=SearchPartition, the full-RD inter
	// path that reaches rdPickVP9InterPartition. The default speed-8 path is
	// VAR_BASED and never enters the skeleton.
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height, CpuUsed: -3})

	var stream []byte
	keySrc := vp9test.NewMotionYCbCr(width, height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	stream = append(stream, key...)

	srcs := []*image.YCbCr{
		splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8),
		splitYShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, -8),
		quadrantShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img,
			image.Point{X: 8}, image.Point{X: -8},
			image.Point{Y: 8}, image.Point{Y: -8}),
		shiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 8, 0),
		splitShiftedVP9ReferenceYCbCrForTest(e.refFrames[0].img, 4, -4),
	}
	for i, s := range srcs {
		buf, err := e.Encode(s)
		if err != nil {
			t.Fatalf("Encode inter %d: %v", i, err)
		}
		stream = append(stream, buf...)
	}
	return stream
}

// TestVP9FullRDInterSearchPartitionStreamPinned asserts that the full-RD
// SearchPartition inter path stays stable across the planted partition stress
// sequence.
func TestVP9FullRDInterSearchPartitionStreamPinned(t *testing.T) {
	stream := vp9EncodeFullRDInterSearchPathStream(t)
	sum := sha256.Sum256(stream)
	got := hex.EncodeToString(sum[:])
	if got != vp9FullRDInterSearchPathStreamSHA256 {
		t.Fatalf("full-RD inter bitstream SHA-256 = %s (%d bytes), want %s\n"+
			"the SearchPartition full-RD inter stream changed",
			got, len(stream), vp9FullRDInterSearchPathStreamSHA256)
	}
}

// TestVP9FullRDInterPartitionRDNodeSentinel pins the per-node predMv reset to
// the libvpx INT16_MAX sentinel (vp9/encoder/vp9_encodeframe.c:4215-4218), the
// home of x->pred_mv[] the step-(b)/(c) thread will populate. Guards against a
// future edit silently swapping in the INT16_MIN intra sentinel.
func TestVP9FullRDInterPartitionRDNodeSentinel(t *testing.T) {
	node := newVP9InterPartitionRDNode(common.Block64x64)
	want := vp9dec.MV{Row: int16(0x7fff), Col: int16(0x7fff)}
	for ref, mv := range node.predMv {
		if mv != want {
			t.Fatalf("predMv[%d] = %+v, want INT16_MAX sentinel %+v", ref, mv, want)
		}
	}
	if node.partitioning != common.Block64x64 {
		t.Fatalf("node.partitioning = %d, want Block64x64", node.partitioning)
	}
	// store_pred_mv / load_pred_mv round-trip is inert plumbing in step (a):
	// loadPredMv must return exactly what storePredMv wrote.
	seeded := [vp9dec.MaxRefFrames]vp9dec.MV{
		{Row: 1, Col: 2}, {Row: 3, Col: 4}, {Row: 5, Col: 6}, {Row: 7, Col: 8},
	}
	node.storePredMv(seeded)
	if got := node.loadPredMv(); got != seeded {
		t.Fatalf("loadPredMv after storePredMv = %+v, want %+v", got, seeded)
	}
}
