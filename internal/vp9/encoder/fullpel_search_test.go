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
