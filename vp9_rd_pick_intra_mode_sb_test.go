package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/encoder"
)

// TestVP9RDPickIntraModeSbComposeBothSkip mirrors libvpx
// vp9/encoder/vp9_rdopt.c:3258-3261: when y_skip && uv_skip the per-plane
// token rates are stripped, the skip-flag bit at vp9_cost_bit(skip_prob, 1)
// is added, and dist = dist_y + dist_uv.
func TestVP9RDPickIntraModeSbComposeBothSkip(t *testing.T) {
	const skipProb uint8 = 96
	in := vp9RDPickIntraModeSbInputs{
		rateY:       1234,
		rateYToken:  900,
		distY:       4096,
		ySkip:       true,
		rateUV:      567,
		rateUVToken: 400,
		distUV:      2048,
		uvSkip:      true,
		skipProb:    skipProb,
		rdmult:      48,
		rddiv:       encoder.RDDivBits,
	}
	got := vp9RDPickIntraModeSbCompose(in)

	// libvpx 3259-3260: rate = rate_y + rate_uv - rate_y_tokenonly -
	// rate_uv_tokenonly + vp9_cost_bit(skip_prob, 1).
	wantRate := in.rateY + in.rateUV - in.rateYToken - in.rateUVToken +
		encoder.VP9CostBit(skipProb, 1)
	// libvpx 3261: dist = dist_y + dist_uv.
	wantDist := in.distY + in.distUV
	wantRDCost := encoder.RDCost(in.rdmult, in.rddiv, wantRate, wantDist)

	if got.Rate != wantRate {
		t.Errorf("rate = %d, want %d", got.Rate, wantRate)
	}
	if got.Dist != wantDist {
		t.Errorf("dist = %d, want %d", got.Dist, wantDist)
	}
	if got.RDCost != wantRDCost {
		t.Errorf("rdcost = %d, want %d", got.RDCost, wantRDCost)
	}
}

// TestVP9RDPickIntraModeSbComposeNoSkip mirrors libvpx
// vp9/encoder/vp9_rdopt.c:3263-3265: when either y_skip or uv_skip is false
// the per-plane token rates remain, the skip-flag bit at
// vp9_cost_bit(skip_prob, 0) is added, and dist = dist_y + dist_uv.
func TestVP9RDPickIntraModeSbComposeNoSkip(t *testing.T) {
	const skipProb uint8 = 16
	cases := []struct {
		name          string
		ySkip, uvSkip bool
	}{
		{"y_only", true, false},
		{"uv_only", false, true},
		{"neither", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := vp9RDPickIntraModeSbInputs{
				rateY:       2000,
				rateYToken:  1500,
				distY:       8192,
				ySkip:       tc.ySkip,
				rateUV:      800,
				rateUVToken: 600,
				distUV:      4096,
				uvSkip:      tc.uvSkip,
				skipProb:    skipProb,
				rdmult:      72,
				rddiv:       encoder.RDDivBits,
			}
			got := vp9RDPickIntraModeSbCompose(in)

			// libvpx 3263-3264: rate = rate_y + rate_uv +
			// vp9_cost_bit(skip_prob, 0).
			wantRate := in.rateY + in.rateUV + encoder.VP9CostBit(skipProb, 0)
			wantDist := in.distY + in.distUV
			wantRDCost := encoder.RDCost(in.rdmult, in.rddiv, wantRate, wantDist)

			if got.Rate != wantRate {
				t.Errorf("rate = %d, want %d", got.Rate, wantRate)
			}
			if got.Dist != wantDist {
				t.Errorf("dist = %d, want %d", got.Dist, wantDist)
			}
			if got.RDCost != wantRDCost {
				t.Errorf("rdcost = %d, want %d", got.RDCost, wantRDCost)
			}
		})
	}
}

// TestVP9RDPickIntraModeSbComposeSkipBitDelta sanity-checks the libvpx
// invariant that the two branches differ by exactly
// (rate_y_token + rate_uv_token) plus the difference between the two
// skip-bit cost evaluations.  This is a property of the composition
// derived directly from vp9_rdopt.c:3258-3265 — useful as a regression
// guard against future refactors of the helper.
func TestVP9RDPickIntraModeSbComposeSkipBitDelta(t *testing.T) {
	const skipProb uint8 = 200
	base := vp9RDPickIntraModeSbInputs{
		rateY:       3000,
		rateYToken:  2200,
		distY:       16384,
		rateUV:      1100,
		rateUVToken: 900,
		distUV:      8192,
		skipProb:    skipProb,
		rdmult:      128,
		rddiv:       encoder.RDDivBits,
	}

	base.ySkip, base.uvSkip = true, true
	bothSkip := vp9RDPickIntraModeSbCompose(base)

	base.ySkip, base.uvSkip = false, false
	noSkip := vp9RDPickIntraModeSbCompose(base)

	// Rates differ by (-rate_y_token - rate_uv_token) + cost(skip,1) -
	// cost(skip,0).
	wantDelta := -base.rateYToken - base.rateUVToken +
		encoder.VP9CostBit(skipProb, 1) - encoder.VP9CostBit(skipProb, 0)
	gotDelta := bothSkip.Rate - noSkip.Rate
	if gotDelta != wantDelta {
		t.Errorf("rate delta = %d, want %d", gotDelta, wantDelta)
	}
	if bothSkip.Dist != noSkip.Dist {
		t.Errorf("dist must be path-independent: bothSkip=%d, noSkip=%d",
			bothSkip.Dist, noSkip.Dist)
	}
}
