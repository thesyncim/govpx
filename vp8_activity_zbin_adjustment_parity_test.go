package govpx

import (
	"testing"
)

// TestVP8ActivityZbinAdjustmentIsDeterministic verifies that the
// activity-derived zbin adjustment is a stable per-macroblock value once the
// activity map is prepared. VP8's RD picker and accepted encode path both read
// this value for the same row and column; this test protects that shared
// assumption without replaying a full encode fixture.
func TestVP8ActivityZbinAdjustmentIsDeterministic(t *testing.T) {
	opts := EncoderOptions{
		Width:             64,
		Height:            64,
		FPS:               30,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: 200,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  999,
		Deadline:          DeadlineBestQuality,
		CpuUsed:           0,
		Tuning:            TuneSSIM,
	}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}

	// Force the activity map valid with a deterministic content. Set
	// per-MB activities so tunedZbinAdjustment returns non-zero deltas
	// (matching libvpx's adjust_act_zbin branch at encodeframe.c:1086-1090).
	const rows = 4
	const cols = 4
	enc.activityMap = make([]uint32, rows*cols)
	for r := range rows {
		for c := range cols {
			// Spread activities so adjust_act_zbin produces a range of
			// deltas; libvpx uses a=act+4*avg, b=4*act+avg, then act>avg
			// branch (positive delta) vs act<=avg branch (negative or 0).
			enc.activityMap[r*cols+c] = uint32(1000 + r*1000 + c*250)
		}
	}
	enc.activityAvg = 2500
	enc.activityMapValid = true

	// Multiple calls to tunedZbinAdjustment for the same (row, col)
	// must return the same value; this is the invariant the picker
	// and accepted-path both rely on.
	for r := range rows {
		for c := range cols {
			adj1, ok1 := enc.tunedZbinAdjustment(r, c)
			adj2, ok2 := enc.tunedZbinAdjustment(r, c)
			adj3, ok3 := enc.tunedZbinAdjustment(r, c)
			if ok1 != ok2 || ok2 != ok3 {
				t.Fatalf("tunedZbinAdjustment ok flag drift at (%d,%d): %v %v %v",
					r, c, ok1, ok2, ok3)
			}
			if adj1 != adj2 || adj2 != adj3 {
				t.Fatalf("tunedZbinAdjustment skew at (%d,%d): %d %d %d",
					r, c, adj1, adj2, adj3)
			}
		}
	}

	// Sanity: with a non-trivial activityMap, at least one MB has a
	// non-zero delta, so the test verifies a non-degenerate path.
	nonZero := 0
	for r := range rows {
		for c := range cols {
			adj, ok := enc.tunedZbinAdjustment(r, c)
			if ok && adj != 0 {
				nonZero++
			}
		}
	}
	if nonZero == 0 {
		t.Fatalf("expected at least one non-zero activity-tuned zbin "+
			"adjustment for rows=%d cols=%d", rows, cols)
	}
}
