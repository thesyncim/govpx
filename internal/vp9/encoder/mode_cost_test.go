package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestInterModeRateCostIncludesNewMVBits(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	zeroRate := InterModeRateCost(&fc, 0, common.ZeroMv,
		vp9dec.MV{}, vp9dec.MV{}, false)
	newRate := InterModeRateCost(&fc, 0, common.NewMv,
		vp9dec.MV{Col: 64}, vp9dec.MV{}, false)
	compoundNewRate := InterModeRateCostN(&fc, 0, common.NewMv,
		[2]vp9dec.MV{{Col: 64}, {Col: -64}}, [2]vp9dec.MV{}, 2, false)
	if newRate <= zeroRate {
		t.Fatalf("NEWMV rate = %d, want greater than ZEROMV rate %d",
			newRate, zeroRate)
	}
	if compoundNewRate <= newRate {
		t.Fatalf("compound NEWMV rate = %d, want greater than single NEWMV rate %d",
			compoundNewRate, newRate)
	}
}

func TestSingleRefModeRateCostIncludesIntraInterBit(t *testing.T) {
	var fc vp9dec.FrameContext
	vp9dec.ResetFrameContext(&fc)

	got := SingleRefModeRateCost(&fc, nil, nil, vp9dec.SingleReference,
		vp9dec.CompoundFrameRefs{}, vp9dec.LastFrame)
	want := IntraInterRateCost(&fc, nil, nil, 1) +
		SingleRefRateCost(&fc, nil, nil, vp9dec.LastFrame)
	if got != want {
		t.Fatalf("single-ref LAST rate = %d, want intra/inter + single-ref %d",
			got, want)
	}
}
