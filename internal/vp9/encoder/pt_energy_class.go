package encoder

// VP9 prev-token energy classifier. Ported verbatim from libvpx v1.16.0
// vp9/common/vp9_entropy.c:95 — the table
//
//	const uint8_t vp9_pt_energy_class[ENTROPY_TOKENS] = { 0, 1, 2, 3, 3, 4,
//	                                                      4, 5, 5, 5, 5, 5 };
//
// drives libvpx's `token_cache[rc] = vp9_pt_energy_class[token]` lookup
// inside cost_coeffs (vp9_rdopt.c:397, 429, 442). The classifier collapses
// the 12-entry ENTROPY_TOKENS alphabet into the 6-class neighbour-energy
// space the next coefficient's get_coef_context uses, so that pinning
// govpx's per-coefficient rate to libvpx requires this exact mapping —
// any divergence shifts the (band, ctx) probability slot picked for the
// next coefficient and cascades into a different cost_coeffs total.
//
// Layout: indices follow libvpx's #defines in vp9_entropy.h:27-38:
//
//	ZERO_TOKEN=0 → class 0    (no nearby energy)
//	ONE_TOKEN=1  → class 1
//	TWO_TOKEN=2  → class 2
//	THREE_TOKEN=3, FOUR_TOKEN=4 → class 3
//	CATEGORY1_TOKEN=5, CATEGORY2_TOKEN=6 → class 4
//	CATEGORY3..6, EOB_TOKEN → class 5    (high energy / done)
var PtEnergyClass = [EntropyTokens]uint8{
	0, 1, 2, 3, 3, 4,
	4, 5, 5, 5, 5, 5,
}
