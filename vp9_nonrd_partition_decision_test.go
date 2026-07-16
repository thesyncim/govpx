package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9NonrdPredBufferPoolOwnership(t *testing.T) {
	var pool [4]vp9NonrdPredBuffer
	for i := range 3 {
		pool[i].data = make([]byte, 64)
		pool[i].stride = 8
	}

	for want := range 3 {
		if got := vp9NonrdGetPredBuffer(&pool); got != want {
			t.Fatalf("get %d = %d, want %d", want, got, want)
		}
	}
	if got := vp9NonrdGetPredBuffer(&pool); got != -1 {
		t.Fatalf("exhausted get = %d, want -1", got)
	}
	vp9NonrdFreePredBuffer(&pool, 1)
	if got := vp9NonrdGetPredBuffer(&pool); got != 1 {
		t.Fatalf("reused get = %d, want 1", got)
	}
	vp9NonrdFreePredBuffer(&pool, 3)
	if pool[3].inUse {
		t.Fatal("destination buffer remained in use")
	}
}

func TestVP9CopyPredRectFromStridedBuffer(t *testing.T) {
	src := make([]byte, 7*2)
	copy(src[0:], []byte{11, 12, 13})
	copy(src[7:], []byte{21, 22, 23})
	dst := make([]byte, 8*4)
	for i := range dst {
		dst[i] = 0xa5
	}

	vp9CopyPredRectFromBuffer(dst, 8, 2, 1, 3, 2, src, 7)

	for y, want := range [][]byte{{11, 12, 13}, {21, 22, 23}} {
		for x, v := range want {
			if got := dst[(y+1)*8+x+2]; got != v {
				t.Fatalf("dst(%d,%d) = %d, want %d", x+2, y+1, got, v)
			}
		}
	}
	if dst[8+1] != 0xa5 || dst[8+5] != 0xa5 || dst[3*8+2] != 0xa5 {
		t.Fatal("copy modified bytes outside the destination rectangle")
	}
}

// TestVP9NonrdPickPartitionSplitSize confirms vp9MLSplitSize maps each
// ML-eligible parent bsize to its libvpx subsize_lookup
// (vp9/common/vp9_common_data.c subsize_lookup[PARTITION_SPLIT]).
func TestVP9NonrdPickPartitionSplitSize(t *testing.T) {
	cases := []struct {
		in   common.BlockSize
		want common.BlockSize
		ok   bool
	}{
		{common.Block64x64, common.Block32x32, true},
		{common.Block32x32, common.Block16x16, true},
		{common.Block16x16, common.Block8x8, true},
		{common.Block8x8, common.BlockInvalid, false},
	}
	for _, tc := range cases {
		got, ok := vp9MLSplitSize(tc.in)
		if ok != tc.ok {
			t.Errorf("vp9MLSplitSize(%v) ok = %v, want %v", tc.in, ok, tc.ok)
		}
		if got != tc.want {
			t.Errorf("vp9MLSplitSize(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestVP9NonrdMLPartitionScoreBudgetUsesStrictLibvpxGuard(t *testing.T) {
	noBudget := vp9NonrdMLPartitionScoreBudget{}
	if !vp9NonrdMLPartitionScoreUnderBudget(100, noBudget) {
		t.Fatal("disabled budget rejected score")
	}
	if remaining, ok := vp9NonrdMLPartitionBudgetRemaining(100, noBudget); !ok || remaining.enabled {
		t.Fatalf("disabled budget remaining = %+v ok=%v, want disabled ok", remaining, ok)
	}

	budget := vp9NonrdMLPartitionBudgetFromScore(100)
	if !vp9NonrdMLPartitionScoreUnderBudget(99, budget) {
		t.Fatal("score below budget rejected")
	}
	if vp9NonrdMLPartitionScoreUnderBudget(100, budget) {
		t.Fatal("score equal to budget accepted; libvpx split loop is strictly < best_rdc")
	}
	if vp9NonrdMLPartitionScoreUnderBudget(101, budget) {
		t.Fatal("score above budget accepted")
	}

	remaining, ok := vp9NonrdMLPartitionBudgetRemaining(40, budget)
	if !ok || !remaining.enabled || remaining.score != 60 {
		t.Fatalf("remaining after 40 = %+v ok=%v, want enabled score 60", remaining, ok)
	}
	for _, spent := range []uint64{100, 101} {
		if remaining, ok := vp9NonrdMLPartitionBudgetRemaining(spent, budget); ok || remaining.enabled {
			t.Fatalf("remaining after %d = %+v ok=%v, want exhausted", spent, remaining, ok)
		}
	}
}

func TestVP9NonrdMLPartitionSnapshotRestoresPredFilterState(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{Width: width, Height: height})
	vp9dec.SetupBlockPlanes(&e.planes, 1, 1)
	e.ensureVP9EncoderModeBuffers(8, 8)
	e.prepareVP9EncoderOutputFrame(width, height)

	inter := &vp9InterEncodeState{
		predInterpFilter: vp9dec.InterpEighttapSmooth,
		predFilterValid:  true,
	}
	snap, ok := e.saveVP9NonrdMLPartitionSnapshot(inter, 8, 8, 0, 0,
		common.Block64x64)
	if !ok {
		t.Fatal("saveVP9NonrdMLPartitionSnapshot failed")
	}
	inter.predInterpFilter = vp9dec.InterpEighttapSharp
	inter.predFilterValid = false

	e.restoreVP9NonrdMLPartitionSnapshot(inter, snap)
	e.releaseVP9NonrdMLPartitionSnapshot(snap)
	if inter.predInterpFilter != vp9dec.InterpEighttapSmooth || !inter.predFilterValid {
		t.Fatalf("pred filter = %d/%v, want EighttapSmooth/valid",
			inter.predInterpFilter, inter.predFilterValid)
	}
}

func TestVP9MLPickPartitionEntryUsesLastBufferWhenLastRefMasked(t *testing.T) {
	const width, height = 64, 64
	e, _ := NewVP9Encoder(VP9EncoderOptions{
		Width:   width,
		Height:  height,
		CpuUsed: 8,
	})
	e.sf.PartitionSearchType = MlBasedPartition

	ref := vp9test.NewMotionYCbCr(width, height)
	src := shiftedVP9ReferenceYCbCrForTest(vp9ImageFromYCbCrForTest(ref), 0, 0)
	e.refFrames[vp9LastRefSlot] = vp9ReferenceFrameFromYCbCr(ref)
	e.vp9ResetMLPartitionCache(8, 8)

	var dq vp9dec.DequantTables
	inter := &vp9InterEncodeState{
		img:        src,
		dq:         &dq,
		refMask:    1 << uint(vp9dec.GoldenFrame),
		baseQindex: e.vp9EncoderModeDecisionQIndex(),
	}
	ctx := e.vp9MLPickPartitionEntry(inter, 8, 8, 0, 0)
	if ctx == nil {
		t.Fatal("ML partition entry returned nil when LAST buffer exists but LAST coding ref is masked")
	}
	if !ctx.ready || !ctx.frameValid {
		t.Fatalf("ML partition ctx ready/frameValid = %v/%v, want true/true",
			ctx.ready, ctx.frameValid)
	}
	if ctx.sbMiRow != 0 || ctx.sbMiCol != 0 {
		t.Fatalf("ML partition ctx SB = (%d,%d), want (0,0)", ctx.sbMiRow, ctx.sbMiCol)
	}
}

func TestVP9ReferencePartitionPredPixelReadyDirectLeaves(t *testing.T) {
	e := &VP9Encoder{}
	e.sf.ReuseInterPredSby = 1
	e.sf.PartitionSearchType = ReferencePartition

	const miRows, miCols = 8, 8
	setGrid := func(bsize common.BlockSize, valid bool) {
		e.varPartFrameValid = valid
		e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
		e.varPartGrid[0].SbType = bsize
	}

	for _, bsize := range []common.BlockSize{
		common.Block64x64,
		common.Block64x32,
		common.Block32x64,
	} {
		setGrid(bsize, true)
		if !e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0, bsize, true) {
			t.Fatalf("ReferencePartition %v pred_pixel_ready = false, want true for direct nonrd_select_partition leaf", bsize)
		}
	}

	setGrid(common.Block32x32, true)
	if e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0, common.Block32x32, true) {
		t.Fatal("ReferencePartition Block32x32 pred_pixel_ready = true, want false because libvpx delegates it to nonrd_pick_partition")
	}

	setGrid(common.Block64x64, false)
	if e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0, common.Block64x64, true) {
		t.Fatal("ReferencePartition without a valid var-part frame marked pred_pixel_ready")
	}

	setGrid(common.Block32x32, true)
	if e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0, common.Block64x64, true) {
		t.Fatal("ReferencePartition mismatched var-part leaf marked pred_pixel_ready")
	}

	e.sf.ReuseInterPredSby = 0
	setGrid(common.Block64x64, true)
	if e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0, common.Block64x64, true) {
		t.Fatal("ReferencePartition with ReuseInterPredSby=0 marked pred_pixel_ready")
	}

	e.sf.ReuseInterPredSby = 1
	e.sf.PartitionSearchType = MlBasedPartition
	if e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0, common.Block64x64, true) {
		t.Fatal("ML partition without ML context marked pred_pixel_ready")
	}
}

// TestVP9VarBasedPartitionPredPixelReadyLeaves pins the VAR_BASED_PARTITION
// reuse gate to libvpx nonrd_use_partition: EVERY >=8x8 leaf pick of the
// partition walk gets ctx->pred_pixel_ready = 1
// (vp9_encodeframe.c:5019/5030/5040/5052/5063) — the seed comes from the
// walker, never from re-consulting the mi-grid stamp, so clipped frame-edge
// leaves (whose geometry the walker derives without a matching stamp) reuse
// too. Sub-8x8 wrapper picks and partition-search probes (commitLeaf=false)
// do not (vp9_pickmode.c:2776).
func TestVP9VarBasedPartitionPredPixelReadyLeaves(t *testing.T) {
	e := &VP9Encoder{}
	e.sf.ReuseInterPredSby = 1
	e.sf.PartitionSearchType = VarBasedPartition

	const miRows, miCols = 12, 8
	setGrid := func(miRow, miCol int, bsize common.BlockSize, valid bool) {
		e.varPartFrameValid = valid
		e.varPartGrid = make([]vp9dec.NeighborMi, miRows*miCols)
		e.varPartGrid[miRow*miCols+miCol].SbType = bsize
	}

	// nonrd_use_partition seeds pred_pixel_ready = 1 for every leaf size,
	// including 8x8 (PARTITION_NONE at bsize 8x8) and rect halves.
	for _, tc := range []struct {
		miRow, miCol int
		bsize        common.BlockSize
	}{
		{0, 0, common.Block64x64},
		{0, 0, common.Block32x32},
		{0, 0, common.Block16x16},
		{0, 0, common.Block8x8},
		{0, 0, common.Block32x16},
		{2, 0, common.Block32x16}, // PARTITION_HORZ second half
		{0, 0, common.Block16x32},
		{0, 2, common.Block16x32}, // PARTITION_VERT second half
	} {
		setGrid(tc.miRow, tc.miCol, tc.bsize, true)
		if !e.vp9NonrdReuseInterPredReady(nil, miRows, miCols,
			tc.miRow, tc.miCol, tc.bsize, true) {
			t.Fatalf("VarBasedPartition %v at (%d,%d) pred_pixel_ready = false, want true (nonrd_use_partition leaf)",
				tc.bsize, tc.miRow, tc.miCol)
		}
	}

	// Clipped frame-edge leaf: the walker commits a 32x16 strip at the
	// partial bottom SB row without a matching grid stamp (the stamp stays
	// at the zero-init BLOCK_4X4). libvpx still seeds pred_pixel_ready = 1
	// for it — the PARTITION_HORZ case reads the walker's clipped dispatch,
	// not the stamp.
	setGrid(0, 0, common.Block64x64, true) // stamp elsewhere; (8,0) unstamped
	if !e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 8, 0,
		common.Block32x16, true) {
		t.Fatal("VarBasedPartition clipped frame-edge leaf (unstamped cell) not marked pred_pixel_ready")
	}

	// Sub-8x8 wrapper picks and partition-search probes carry
	// commitLeaf=false (vp9_pick_inter_mode_sub8x8 forces
	// ctx->pred_pixel_ready = 0; probes never receive the walker seed).
	setGrid(0, 0, common.Block64x64, true)
	if e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0,
		common.Block64x64, false) {
		t.Fatal("VarBasedPartition non-commit pick marked pred_pixel_ready")
	}

	// Speed feature off.
	e.sf.ReuseInterPredSby = 0
	setGrid(0, 0, common.Block64x64, true)
	if e.vp9NonrdReuseInterPredReady(nil, miRows, miCols, 0, 0,
		common.Block64x64, true) {
		t.Fatal("VarBasedPartition with ReuseInterPredSby=0 marked pred_pixel_ready")
	}
}
