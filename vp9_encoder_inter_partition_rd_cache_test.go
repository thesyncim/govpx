package govpx

import (
	"image"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

// withDeepVP9InterRDReplayDisabled flips the SEARCH->WRITE replay OFF for the
// duration of a test while the deep recursion stays active. With replay off the
// search still runs and commits its decisions into the caches, but the writer
// re-decides the partition (via its divergent shallow early-exits) and re-picks
// each leaf — the architecture the caches fix. A test can therefore prove the
// caches are load-bearing by showing the decoded tree diverges from the search's
// committed tree without them.
func withDeepVP9InterRDReplayDisabled(t *testing.T) {
	t.Helper()
	prev := vp9InterDeepRDReplayWrites
	vp9InterDeepRDReplayWrites = false
	t.Cleanup(func() { vp9InterDeepRDReplayWrites = prev })
}

// vp9DeepInterRDMismatch describes the first node at which the decoded bitstream
// diverged from the deep-RD search's committed SEARCH->WRITE caches.
type vp9DeepInterRDMismatch struct {
	miRow, miCol int
	bsize        common.BlockSize
	kind         string // "partition", "no-leaf-entry", "mode", "ref", "mv"
	got, want    string
}

// vp9DeepInterRDWalkResult is the outcome of walking the search's committed
// partition tree against the decoded grid.
type vp9DeepInterRDWalkResult struct {
	interLeaves int
	mismatch    *vp9DeepInterRDMismatch
}

// walkVP9DeepInterRDTree walks the partition tree the deep-RD search COMMITTED
// (read from e's partition-decision cache, the writer's source of truth under
// replay) starting at every 64x64 SB, and checks the DECODED grid against it at
// each node:
//
//   - the decoded cell's SbType must equal the search's committed leaf block
//     size (partition geometry round-trips); and
//   - at each leaf, the decoded mode / ref_frame(s) / MV must equal the leaf the
//     search committed to the per-leaf decision cache (mode round-trips).
//
// It returns the count of inter leaves checked and the first mismatch, if any.
// When the writer replays the caches the decoded tree IS the committed tree, so
// there is no mismatch. When replay is disabled the writer descends its own
// (divergent) shallow tree, so the committed-tree walk hits a node whose decoded
// SbType no longer matches — catching the bug.
func (e *VP9Encoder) walkVP9DeepInterRDTree(d *VP9Decoder, miRows, miCols int) vp9DeepInterRDWalkResult {
	var res vp9DeepInterRDWalkResult
	var walk func(miRow, miCol int, bsize common.BlockSize)
	walk = func(miRow, miCol int, bsize common.BlockSize) {
		if miRow >= miRows || miCol >= miCols || res.mismatch != nil {
			return
		}
		target, ok := e.lookupVP9InterPartitionRDDecision(miRow, miCol, bsize)
		if !ok {
			// Below 8x8 the writer never queries the partition picker, so the
			// search records no node there; treat the parent's subsize as the
			// leaf. For >= 8x8 a miss means the search committed nothing here.
			target = bsize
		}
		bsl := int(common.BWidthLog2Lookup[bsize])
		bs := (1 << uint(bsl)) / 4
		partition := common.PartitionLookup[bsl][target]
		if partition == common.PartitionSplit && bsize > common.Block8x8 {
			subsize := common.SubsizeLookup[common.PartitionSplit][bsize]
			walk(miRow, miCol, subsize)
			walk(miRow, miCol+bs, subsize)
			walk(miRow+bs, miCol, subsize)
			walk(miRow+bs, miCol+bs, subsize)
			return
		}
		// Leaf: the search committed `target` as this node's block size.
		dec := d.miGrid[miRow*miCols+miCol]
		if dec.SbType != target {
			res.mismatch = &vp9DeepInterRDMismatch{
				miRow: miRow, miCol: miCol, bsize: bsize, kind: "partition",
				got: blockSizeName(dec.SbType), want: blockSizeName(target),
			}
			return
		}
		cached, ok := e.lookupVP9LeafInterRDDecision(miRow, miCol, target)
		if !ok {
			res.mismatch = &vp9DeepInterRDMismatch{
				miRow: miRow, miCol: miCol, bsize: target, kind: "no-leaf-entry",
			}
			return
		}
		want := vp9InterModeDecisionMi(target, cached)
		switch {
		case dec.Mode != want.Mode:
			res.mismatch = &vp9DeepInterRDMismatch{miRow: miRow, miCol: miCol,
				bsize: target, kind: "mode", got: modeName(dec.Mode), want: modeName(want.Mode)}
		case dec.RefFrame != want.RefFrame:
			res.mismatch = &vp9DeepInterRDMismatch{miRow: miRow, miCol: miCol,
				bsize: target, kind: "ref", got: refName(dec.RefFrame), want: refName(want.RefFrame)}
		case dec.Mv != want.Mv:
			res.mismatch = &vp9DeepInterRDMismatch{miRow: miRow, miCol: miCol,
				bsize: target, kind: "mv", got: mvName(dec.Mv), want: mvName(want.Mv)}
		default:
			if want.RefFrame[0] > vp9dec.IntraFrame {
				res.interLeaves++
				assertVP9DeepInterRDMvSane(e, miRow, miCol, target, dec.Mv, &res)
			}
		}
	}
	for miRow := 0; miRow < miRows; miRow += int(common.MiBlockSize) {
		for miCol := 0; miCol < miCols; miCol += int(common.MiBlockSize) {
			walk(miRow, miCol, common.Block64x64)
		}
	}
	return res
}

// TestVP9InterPartitionRDCacheRoundTrips is the pin for the SEARCH->WRITE inter
// decision cache. With the deep depth-first RD recursion active
// (vp9InterUseDeepRDPartition), pickVP9InterPartitionRD commits each node's
// partition and each leaf's full picker decision into the caches, and the
// bitstream write descent REPLAYS them rather than re-deciding the partition or
// re-picking the leaf with a desynced context. On the planted-MV inter content
// from the Blocker-1 serialize test it asserts:
//
//  1. the inter frame decodes cleanly;
//  2. the decoded partition tree equals the SEARCH's committed tree (partition
//     geometry round-trips), and every leaf carries exactly the mode /
//     ref_frame(s) / MV the search committed (mode round-trips — the write pass
//     did NOT re-decide / re-pick to a different value); and
//  3. no garbage MVs (the re-pick bug produced wild MVs such as (-10, 280));
//     every emitted MV stays inside the planted-motion search envelope.
//
// The companion negative-control test disables the replay and asserts the
// committed-tree walk then diverges, proving this test catches the bug rather
// than passing vacuously.
func TestVP9InterPartitionRDCacheRoundTrips(t *testing.T) {
	withDeepVP9InterRDPartition(t)
	for _, tc := range vp9DeepInterRDCacheCases() {
		t.Run(tc.name, func(t *testing.T) {
			e, miRows, miCols, key, inter := encodeVP9DeepInterRDCase(t, tc)
			d := decodeVP9DeepInterRDFrames(t, key, inter)
			// The decoded partition tree must independently round-trip (decode +
			// re-derivation stays in-frame); this is the Blocker-1 invariant.
			assertVP9InterPartitionTreeRoundTrips(t, d, miRows, miCols)

			res := e.walkVP9DeepInterRDTree(d, miRows, miCols)
			if res.mismatch != nil {
				m := res.mismatch
				t.Fatalf("SEARCH->WRITE cache did not round-trip at (%d,%d,%s): %s got=%s want=%s "+
					"(write pass diverged from the search's committed decision)",
					m.miRow, m.miCol, blockSizeName(m.bsize), m.kind, m.got, m.want)
			}
			if res.interLeaves == 0 {
				t.Fatal("planted-MV inter content produced no inter leaves; " +
					"the cache round-trip would be vacuous")
			}
		})
	}
}

// TestVP9InterPartitionRDCacheReplayCatchesRepickBug is the negative control:
// with the deep recursion active but the SEARCH->WRITE replay DISABLED, the
// writer re-decides the partition with its shallow early-exits (and re-picks the
// leaves) instead of replaying the search's committed caches. The decoded tree
// must then diverge from the search's committed tree (or fail to decode) for at
// least one planted-MV case — demonstrating that the round-trip test above is
// sensitive to the bug and does not pass vacuously.
func TestVP9InterPartitionRDCacheReplayCatchesRepickBug(t *testing.T) {
	withDeepVP9InterRDPartition(t)
	withDeepVP9InterRDReplayDisabled(t)
	sawBreak := false
	for _, tc := range vp9DeepInterRDCacheCases() {
		broke, detail := vp9DeepInterRDCaseRoundTripBroken(t, tc)
		t.Logf("case %s: round-trip broken without replay = %v (%s)", tc.name, broke, detail)
		if broke {
			sawBreak = true
		}
	}
	if !sawBreak {
		t.Fatal("disabling the SEARCH->WRITE replay did not break any planted-MV " +
			"case: the cache round-trip test cannot be shown to catch the bug")
	}
}

// vp9DeepInterRDCaseRoundTripBroken encodes a case under the current replay
// setting and reports whether the decoded bitstream diverged from the search's
// committed SEARCH->WRITE caches: a decode failure, or the committed-tree walk
// finding a partition/mode/ref/MV mismatch. It never fails the test itself; it
// classifies the outcome for the negative control.
func vp9DeepInterRDCaseRoundTripBroken(t *testing.T, tc vp9DeepInterRDCacheCase) (bool, string) {
	t.Helper()
	e, miRows, miCols, key, inter := encodeVP9DeepInterRDCase(t, tc)

	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		// A serialization desync from re-deciding surfaces as a decode failure.
		return true, "decode failed: " + err.Error()
	}
	res := e.walkVP9DeepInterRDTree(d, miRows, miCols)
	if res.mismatch != nil {
		m := res.mismatch
		return true, m.kind + " mismatch at (" +
			itoa(m.miRow) + "," + itoa(m.miCol) + "," + blockSizeName(m.bsize) +
			") got=" + m.got + " want=" + m.want
	}
	return false, "decoded tree matched the search's committed tree"
}

type vp9DeepInterRDCacheCase struct {
	name          string
	width, height int
	inter         func(t *testing.T, ref Image) *image.YCbCr
}

func vp9DeepInterRDCacheCases() []vp9DeepInterRDCacheCase {
	return []vp9DeepInterRDCacheCase{
		{
			name: "quadrant-motion-64x64", width: 64, height: 64,
			inter: func(t *testing.T, ref Image) *image.YCbCr {
				return quadrantShiftedVP9ReferenceYCbCrForTest(ref,
					image.Point{X: 8}, image.Point{X: -8},
					image.Point{Y: 8}, image.Point{Y: -8})
			},
		},
		{
			name: "horizontal-mixed-64x64", width: 64, height: 64,
			inter: func(t *testing.T, ref Image) *image.YCbCr {
				return splitShiftedVP9ReferenceYCbCrForTest(ref, 8, -8)
			},
		},
		{
			name: "eighth-pel-128x64", width: 128, height: 64,
			inter: func(t *testing.T, ref Image) *image.YCbCr {
				return predictedVP9ReferenceYCbCrForTest(t, ref, vp9dec.MV{Col: 57})
			},
		},
	}
}

func encodeVP9DeepInterRDCase(t *testing.T, tc vp9DeepInterRDCacheCase) (
	*VP9Encoder, int, int, []byte, []byte,
) {
	t.Helper()
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width: tc.width, Height: tc.height, CpuUsed: -3,
	})
	keySrc := vp9test.NewMotionYCbCr(tc.width, tc.height)
	key, err := e.Encode(keySrc)
	if err != nil {
		t.Fatalf("Encode keyframe: %v", err)
	}
	interSrc := tc.inter(t, e.refFrames[0].img)
	inter, err := e.Encode(interSrc)
	if err != nil {
		t.Fatalf("Encode inter: %v", err)
	}
	miCols := (tc.width + 7) >> 3
	miRows := (tc.height + 7) >> 3
	return e, miRows, miCols, key, inter
}

func decodeVP9DeepInterRDFrames(t *testing.T, key, inter []byte) *VP9Decoder {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame !ok after keyframe")
	}
	if err := d.Decode(inter); err != nil {
		t.Fatalf("Decode inter (deep RD partition) failed: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame !ok after deep-RD inter frame")
	}
	return d
}

// assertVP9DeepInterRDMvSane bounds a committed inter MV inside the
// planted-motion search envelope. The planted content displaces by at most 8
// integer pixels (= 64 in 1/8-pel units; the eighth-pel case plants Col=57),
// and the full-pel motion search refines within a bounded window around the
// predictors. The re-pick bug produced MVs an order of magnitude larger (e.g.
// (-10, 280)); a generous |component| <= vp9DeepInterRDMvBound bound rejects
// that garbage while admitting every legitimate planted-motion result.
func assertVP9DeepInterRDMvSane(e *VP9Encoder, miRow, miCol int,
	bsize common.BlockSize, mv [2]vp9dec.MV, res *vp9DeepInterRDWalkResult,
) {
	const vp9DeepInterRDMvBound = 160 // 1/8-pel; 20 integer px, well above the 8px plant.
	for _, v := range mv {
		if v == vp9dec.InvalidMV {
			continue
		}
		if v.Row < -vp9DeepInterRDMvBound || v.Row > vp9DeepInterRDMvBound ||
			v.Col < -vp9DeepInterRDMvBound || v.Col > vp9DeepInterRDMvBound {
			res.mismatch = &vp9DeepInterRDMismatch{
				miRow: miRow, miCol: miCol, bsize: bsize, kind: "garbage-mv",
				got: mvName(mv), want: "within ±160",
			}
			return
		}
	}
}

func blockSizeName(b common.BlockSize) string { return "bs" + itoa(int(b)) }
func modeName(m common.PredictionMode) string { return "mode" + itoa(int(m)) }
func refName(r [2]int8) string                { return "ref" + itoa(int(r[0])) + "/" + itoa(int(r[1])) }
func mvName(mv [2]vp9dec.MV) string {
	return "(" + itoa(int(mv[0].Row)) + "," + itoa(int(mv[0].Col)) + ")"
}
