package tables

// VP9 motion-vector reference mode-context lookup tables. Ported
// byte-for-byte from libvpx v1.16.0 vp9/common/vp9_mvref_common.h —
// motion_vector_context enum, mode_2_counter[14], counter_to_context[19].
//
// The neighbor walk in find_mv_refs_idx accumulates a context counter
// over the 8-neighbor scan (1 per NEWMV, 3 per ZEROMV, 9 per intra,
// 0 per NEAR/NEAREST). The resulting value (0..18) indexes
// CounterToContext to pick one of the seven motion_vector_context
// states the inter-mode tree consults.

// VP9 motion-vector context states, mirroring the
// motion_vector_context enum.
const (
	BothZero          = 0
	ZeroPlusPredicted = 1
	BothPredicted     = 2
	NewPlusNonIntra   = 3
	BothNew           = 4
	IntraPlusNonIntra = 5
	BothIntra         = 6
	InvalidCase       = 9
)

// Mode2Counter mirrors libvpx's mode_2_counter[MB_MODE_COUNT].
// 9 for any intra mode, 0 for NEAREST/NEAR, 3 for ZEROMV, 1 for
// NEWMV. The MB_MODE_COUNT layout follows PREDICTION_MODE: intra
// modes 0..9 then NEARESTMV(10), NEARMV(11), ZEROMV(12), NEWMV(13).
var Mode2Counter = [14]uint8{
	9, // DcPred
	9, // VPred
	9, // HPred
	9, // D45Pred
	9, // D135Pred
	9, // D117Pred
	9, // D153Pred
	9, // D207Pred
	9, // D63Pred
	9, // TmPred
	0, // NearestMv
	0, // NearMv
	3, // ZeroMv
	1, // NewMv
}

// CounterToContext mirrors libvpx's counter_to_context[19]. Index it
// with the accumulated neighbor-walk counter to pick the matching
// motion_vector_context state.
var CounterToContext = [19]uint8{
	BothPredicted,     // 0
	NewPlusNonIntra,   // 1
	BothNew,           // 2
	ZeroPlusPredicted, // 3
	NewPlusNonIntra,   // 4
	InvalidCase,       // 5
	BothZero,          // 6
	InvalidCase,       // 7
	InvalidCase,       // 8
	IntraPlusNonIntra, // 9
	IntraPlusNonIntra, // 10
	InvalidCase,       // 11
	IntraPlusNonIntra, // 12
	InvalidCase,       // 13
	InvalidCase,       // 14
	InvalidCase,       // 15
	InvalidCase,       // 16
	InvalidCase,       // 17
	BothIntra,         // 18
}
