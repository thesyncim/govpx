//go:build govpx_oracle_trace

package govpx

// oneAtIndex returns a length-total flag slice with index `at` set to
// f and every other entry zero. Useful for "single-frame-only"
// schedules that exercise the upd-mask transition on both edges.
func oneAtIndex(total, at int, f EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, total)
	if at >= 0 && at < total {
		out[at] = f
	}
	return out
}

// alternateFlags returns a length-total flag slice with index 0 set
// to 0 (initial keyframe) and the remaining slots alternating between
// a (odd indices) and b (even indices).
func alternateFlags(total int, a, b EncodeFlags) []EncodeFlags {
	out := make([]EncodeFlags, total)
	for i := 1; i < total; i++ {
		if i%2 == 1 {
			out[i] = a
		} else {
			out[i] = b
		}
	}
	return out
}
