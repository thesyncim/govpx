package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestFastDiamondPatternSearchSADFindsLocalMinimum(t *testing.T) {
	limits := MvLimits{RowMin: -16, RowMax: 16, ColMin: -16, ColMax: 16}
	sadAt, scoreMv := quadraticSearchSurface(5, -3)
	startSad, _ := sadAt(0, 0)

	dx, dy, sad, score := FastDiamondPatternSearchSAD(0, 0,
		startSad, startSad, 0, &limits, sadAt, scoreMv)
	if dx != 5 || dy != -3 || sad != 0 || score != 0 {
		t.Fatalf("fast diamond = dx=%d dy=%d sad=%d score=%d, want 5/-3/0/0",
			dx, dy, sad, score)
	}
}

func TestFastHexPatternSearchSADFindsLocalMinimum(t *testing.T) {
	limits := MvLimits{RowMin: -16, RowMax: 16, ColMin: -16, ColMax: 16}
	sadAt, scoreMv := quadraticSearchSurface(6, -3)
	startSad, _ := sadAt(0, 0)

	dx, dy, sad, score := FastHexPatternSearchSAD(0, 0,
		startSad, startSad, 0, &limits, sadAt, scoreMv)
	if dx != 6 || dy != -3 || sad != 0 || score != 0 {
		t.Fatalf("fast hex = dx=%d dy=%d sad=%d score=%d, want 6/-3/0/0",
			dx, dy, sad, score)
	}
}

func TestFastPatternSearchSADWithBatchMatchesScalar(t *testing.T) {
	limits := MvLimits{RowMin: -32, RowMax: 32, ColMin: -32, ColMax: 32}
	tests := []struct {
		name   string
		target [2]int
		scalar func(int, int, uint64, uint64, int, *MvLimits,
			func(int, int) (uint64, bool),
			func(int, int, uint64) uint64) (int, int, uint64, uint64)
		batch func(int, int, uint64, uint64, int, *MvLimits,
			func(int, int) (uint64, bool), PatternSAD4Func,
			func(int, int, uint64) uint64) (int, int, uint64, uint64)
	}{
		{
			name:   "fast diamond",
			target: [2]int{5, -3},
			scalar: FastDiamondPatternSearchSAD,
			batch:  FastDiamondPatternSearchSADWithBatch,
		},
		{
			name:   "fast hex",
			target: [2]int{6, -3},
			scalar: FastHexPatternSearchSAD,
			batch:  FastHexPatternSearchSADWithBatch,
		},
		{
			name:   "n-step",
			target: [2]int{-7, 6},
			scalar: NStepDiamondSearchSAD,
			batch:  NStepDiamondSearchSADWithBatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sadAt, scoreMv := quadraticSearchSurface(tc.target[0], tc.target[1])
			startSad, _ := sadAt(0, 0)
			wantDx, wantDy, wantSad, wantScore := tc.scalar(0, 0,
				startSad, startSad, 0, &limits, sadAt, scoreMv)

			batchCalls := 0
			sadAt4 := func(dx0, dy0, dx1, dy1, dx2, dy2, dx3, dy3 int) (
				uint64, uint64, uint64, uint64, bool,
			) {
				batchCalls++
				sad0, ok0 := sadAt(dx0, dy0)
				sad1, ok1 := sadAt(dx1, dy1)
				sad2, ok2 := sadAt(dx2, dy2)
				sad3, ok3 := sadAt(dx3, dy3)
				return sad0, sad1, sad2, sad3, ok0 && ok1 && ok2 && ok3
			}
			gotDx, gotDy, gotSad, gotScore := tc.batch(0, 0, startSad,
				startSad, 0, &limits, sadAt, sadAt4, scoreMv)

			if gotDx != wantDx || gotDy != wantDy || gotSad != wantSad ||
				gotScore != wantScore {
				t.Fatalf("batch result = dx=%d dy=%d sad=%d score=%d, want dx=%d dy=%d sad=%d score=%d",
					gotDx, gotDy, gotSad, gotScore, wantDx, wantDy, wantSad,
					wantScore)
			}
			if batchCalls == 0 {
				t.Fatal("batch search did not call sadAt4")
			}
		})
	}
}

func TestRegularPatternSearchSADFindsLocalMinimum(t *testing.T) {
	limits := MvLimits{RowMin: -64, RowMax: 64, ColMin: -64, ColMax: 64}
	tests := []struct {
		name     string
		targetDx int
		targetDy int
		fn       func(int, int, uint64, uint64, int, *MvLimits,
			func(int, int) (uint64, bool),
			func(int, int, uint64) uint64) (int, int, uint64, uint64)
	}{
		{name: "bigdia", targetDx: -11, targetDy: 9, fn: BigDiamondPatternSearchSAD},
		{name: "hex", targetDx: 13, targetDy: -7, fn: HexPatternSearchSAD},
		{name: "square", targetDx: -9, targetDy: -12, fn: SquarePatternSearchSAD},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sadAt, scoreMv := quadraticSearchSurface(tc.targetDx, tc.targetDy)
			startSad, _ := sadAt(0, 0)
			dx, dy, sad, score := tc.fn(0, 0, startSad, startSad, 0,
				&limits, sadAt, scoreMv)
			if dx != tc.targetDx || dy != tc.targetDy || sad != 0 || score != 0 {
				t.Fatalf("%s pattern = dx=%d dy=%d sad=%d score=%d, want %d/%d/0/0",
					tc.name, dx, dy, sad, score, tc.targetDx, tc.targetDy)
			}
		})
	}
}

func TestRegularPatternSearchSADWithBatchMatchesScalar(t *testing.T) {
	limits := MvLimits{RowMin: -64, RowMax: 64, ColMin: -64, ColMax: 64}
	tests := []struct {
		name   string
		target [2]int
		scalar func(int, int, uint64, uint64, int, *MvLimits,
			func(int, int) (uint64, bool),
			func(int, int, uint64) uint64) (int, int, uint64, uint64)
		batch func(int, int, uint64, uint64, int, *MvLimits,
			func(int, int) (uint64, bool), PatternSAD4Func,
			func(int, int, uint64) uint64) (int, int, uint64, uint64)
	}{
		{
			name:   "bigdia",
			target: [2]int{-11, 9},
			scalar: BigDiamondPatternSearchSAD,
			batch:  BigDiamondPatternSearchSADWithBatch,
		},
		{
			name:   "hex",
			target: [2]int{13, -7},
			scalar: HexPatternSearchSAD,
			batch:  HexPatternSearchSADWithBatch,
		},
		{
			name:   "square",
			target: [2]int{-9, -12},
			scalar: SquarePatternSearchSAD,
			batch:  SquarePatternSearchSADWithBatch,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sadAt, scoreMv := quadraticSearchSurface(tc.target[0], tc.target[1])
			startSad, _ := sadAt(0, 0)
			wantDx, wantDy, wantSad, wantScore := tc.scalar(0, 0,
				startSad, startSad, 0, &limits, sadAt, scoreMv)

			batchCalls := 0
			sadAt4 := func(dx0, dy0, dx1, dy1, dx2, dy2, dx3, dy3 int) (
				uint64, uint64, uint64, uint64, bool,
			) {
				batchCalls++
				sad0, ok0 := sadAt(dx0, dy0)
				sad1, ok1 := sadAt(dx1, dy1)
				sad2, ok2 := sadAt(dx2, dy2)
				sad3, ok3 := sadAt(dx3, dy3)
				return sad0, sad1, sad2, sad3, ok0 && ok1 && ok2 && ok3
			}
			gotDx, gotDy, gotSad, gotScore := tc.batch(0, 0, startSad,
				startSad, 0, &limits, sadAt, sadAt4, scoreMv)

			if gotDx != wantDx || gotDy != wantDy || gotSad != wantSad ||
				gotScore != wantScore {
				t.Fatalf("batch result = dx=%d dy=%d sad=%d score=%d, want dx=%d dy=%d sad=%d score=%d",
					gotDx, gotDy, gotSad, gotScore, wantDx, wantDy, wantSad,
					wantScore)
			}
			if batchCalls == 0 {
				t.Fatal("batch search did not call sadAt4")
			}
		})
	}
}

func TestBigDiamondPatternSearchSADScale9Candidate(t *testing.T) {
	limits := MvLimits{RowMin: -1024, RowMax: 1024, ColMin: -1024, ColMax: 1024}
	sadAt, scoreMv := quadraticSearchSurface(-256, 256)
	startSad, _ := sadAt(0, 0)

	dx, dy, sad, score := BigDiamondPatternSearchSAD(0, 0,
		startSad, startSad, 1, &limits, sadAt, scoreMv)
	if dx != -256 || dy != 256 || sad != 0 || score != 0 {
		t.Fatalf("bigdia scale9 = dx=%d dy=%d sad=%d score=%d, want -256/256/0/0",
			dx, dy, sad, score)
	}
}

func TestNStepDiamondSearchSADFindsLocalMinimum(t *testing.T) {
	limits := MvLimits{RowMin: -32, RowMax: 32, ColMin: -32, ColMax: 32}
	sadAt, scoreMv := quadraticSearchSurface(-7, 6)
	startSad, _ := sadAt(0, 0)

	dx, dy, sad, score := NStepDiamondSearchSAD(0, 0,
		startSad, startSad, 0, &limits, sadAt, scoreMv)
	if dx != -7 || dy != 6 || sad != 0 || score != 0 {
		t.Fatalf("n-step diamond = dx=%d dy=%d sad=%d score=%d, want -7/6/0/0",
			dx, dy, sad, score)
	}
}

func TestEncoderMvLimitsMatchesFrameGeometry(t *testing.T) {
	got := EncoderMvLimits(45, 80, 8, 16, common.Block16x16)
	want := MvLimits{
		RowMin: -((8 + 2) * common.MiSize) - common.VP9InterpExtend,
		ColMin: -((16 + 2) * common.MiSize) - common.VP9InterpExtend,
		RowMax: (45-8)*common.MiSize + common.VP9InterpExtend,
		ColMax: (80-16)*common.MiSize + common.VP9InterpExtend,
	}
	if got != want {
		t.Fatalf("EncoderMvLimits = %+v, want %+v", got, want)
	}
}

func TestSADPerBit16ClampsQIndex(t *testing.T) {
	if got, want := SADPerBit16(-1), SADPerBit16(0); got != want {
		t.Fatalf("SADPerBit16(-1) = %d, want %d", got, want)
	}
	if got, want := SADPerBit16(vp9dec.MaxQ+1), SADPerBit16(vp9dec.MaxQ); got != want {
		t.Fatalf("SADPerBit16(max+1) = %d, want %d", got, want)
	}
	if low, high := SADPerBit16(16), SADPerBit16(192); high <= low {
		t.Fatalf("SADPerBit16 not increasing: q16=%d q192=%d", low, high)
	}
}

func TestFullPelMVSADCostUsesJointAndComponentCosts(t *testing.T) {
	const sadPerBit = 16
	if got, want := FullPelMVSADCost(0, 0, 0, 0, sadPerBit),
		(600*sadPerBit+256)>>9; got != want {
		t.Fatalf("zero MV cost = %d, want %d", got, want)
	}
	if got, want := FullPelMVSADCost(0, 1, 0, 0, sadPerBit),
		((300+MVSADComponentCost(1))*sadPerBit+256)>>9; got != want {
		t.Fatalf("one-col MV cost = %d, want %d", got, want)
	}
}

func quadraticSearchSurface(targetDx, targetDy int) (
	func(dx, dy int) (uint64, bool),
	func(dx, dy int, sad uint64) uint64,
) {
	sadAt := func(dx, dy int) (uint64, bool) {
		x := dx - targetDx
		y := dy - targetDy
		return uint64(x*x + y*y), true
	}
	scoreMv := func(_, _ int, sad uint64) uint64 {
		return sad
	}
	return sadAt, scoreMv
}

var fullpelPatternBenchmarkSink uint64

func BenchmarkRegularPatternSearchSAD(b *testing.B) {
	type scalarPatternSearch func(int, int, uint64, uint64, int, *MvLimits,
		func(int, int) (uint64, bool),
		func(int, int, uint64) uint64) (int, int, uint64, uint64)
	type batchPatternSearch func(int, int, uint64, uint64, int, *MvLimits,
		func(int, int) (uint64, bool), PatternSAD4Func,
		func(int, int, uint64) uint64) (int, int, uint64, uint64)

	limits := MvLimits{RowMin: -128, RowMax: 128, ColMin: -128, ColMax: 128}
	sadAt, sadAt4 := patternSADBenchmarkSurface(&limits)
	scoreMv := func(_, _ int, sad uint64) uint64 { return sad }
	startSad, _ := sadAt(0, 0)
	const stepParam = 5
	tests := []struct {
		name   string
		scalar scalarPatternSearch
		batch  batchPatternSearch
	}{
		{name: "bigdia", scalar: BigDiamondPatternSearchSAD, batch: BigDiamondPatternSearchSADWithBatch},
		{name: "hex", scalar: HexPatternSearchSAD, batch: HexPatternSearchSADWithBatch},
		{name: "square", scalar: SquarePatternSearchSAD, batch: SquarePatternSearchSADWithBatch},
	}

	for _, tc := range tests {
		b.Run(tc.name+"/scalar", func(b *testing.B) {
			b.ReportAllocs()
			var sum uint64
			for b.Loop() {
				dx, dy, sad, score := tc.scalar(0, 0, startSad, startSad,
					stepParam, &limits, sadAt, scoreMv)
				sum += sad + score + uint64(dx&0xff) + uint64(dy&0xff)
			}
			fullpelPatternBenchmarkSink = sum
		})
		b.Run(tc.name+"/batch", func(b *testing.B) {
			b.ReportAllocs()
			var sum uint64
			for b.Loop() {
				dx, dy, sad, score := tc.batch(0, 0, startSad, startSad,
					stepParam, &limits, sadAt, sadAt4, scoreMv)
				sum += sad + score + uint64(dx&0xff) + uint64(dy&0xff)
			}
			fullpelPatternBenchmarkSink = sum
		})
	}
}

func patternSADBenchmarkSurface(limits *MvLimits) (
	func(dx, dy int) (uint64, bool),
	PatternSAD4Func,
) {
	const (
		w          = 32
		h          = 32
		stride     = 384
		height     = 384
		srcX       = 19
		srcY       = 23
		refCenterX = 176
		refCenterY = 171
	)
	src := make([]byte, stride*height)
	ref := make([]byte, stride*height)
	for i := range src {
		src[i] = byte((i*31 + i/7 + 29) & 0xff)
		ref[i] = byte((i*17 + i/11 + 103) & 0xff)
	}
	srcOff := srcY*stride + srcX
	refOffset := func(dx, dy int) (int, bool) {
		if !limits.InFullpelRange(dy, dx) {
			return 0, false
		}
		refX := refCenterX + dx
		refY := refCenterY + dy
		if refX < 0 || refY < 0 || refX+w > stride || refY+h > height {
			return 0, false
		}
		return refY*stride + refX, true
	}
	sadAt := func(dx, dy int) (uint64, bool) {
		refOff, ok := refOffset(dx, dy)
		if !ok {
			return 0, false
		}
		sad, ok := BlockSADNoLimitOffsets(src, srcOff, stride, ref, refOff,
			stride, w, h)
		return uint64(sad), ok
	}
	sadAt4 := func(dx0, dy0, dx1, dy1, dx2, dy2, dx3, dy3 int) (
		uint64, uint64, uint64, uint64, bool,
	) {
		refOff0, ok0 := refOffset(dx0, dy0)
		refOff1, ok1 := refOffset(dx1, dy1)
		refOff2, ok2 := refOffset(dx2, dy2)
		refOff3, ok3 := refOffset(dx3, dy3)
		if !(ok0 && ok1 && ok2 && ok3) {
			return 0, 0, 0, 0, false
		}
		var out [4]uint32
		if !BlockSAD4NoLimitOffsets(src, srcOff, stride, ref,
			[4]int{refOff0, refOff1, refOff2, refOff3}, stride, w, h, &out) {
			return 0, 0, 0, 0, false
		}
		return uint64(out[0]), uint64(out[1]), uint64(out[2]), uint64(out[3]),
			true
	}
	return sadAt, sadAt4
}
