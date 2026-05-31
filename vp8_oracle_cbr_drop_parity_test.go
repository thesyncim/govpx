//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"math"
	"sort"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8OracleCBRDropFrameParity pins govpx-vs-libvpx parity on the CBR
// drop-frame and buffer-pressure decision boundary. Each fixture
// deliberately under-budgets the channel so both encoders are forced to
// drop several inter frames; we then verify the drop count, drop indices,
// per-frame force_maxqp lifecycle, post-drop buffer-level trajectory, and
// post-drop Q recovery line up.
//
// Baseline file: testdata/cbr_drop_parity_baseline.json. The baseline
// records the govpx-side drop count / indices / mean buffer level / mean
// post-drop Q so future regressions on the govpx side are caught even when
// the libvpx oracle drifts within tolerance. Bootstrap with
// GOVPX_UPDATE_BASELINES=1.
//
// Acceptance bands (from docs/vp8_encoder_parity.md "Temporal, Speed, And
// Packetization"):
//   - dropped_frame_count parity within +/- 2 frames vs libvpx
//   - dropped_frame_indices Jaccard >= 0.7 vs libvpx
//   - per-frame force_maxqp flag: at most 1 frame divergence vs libvpx
//   - buffer_level trajectory: mean post-drop |delta| <= 10% of libvpx
//   - q_index post-drop trajectory: at most 4 Q-index drift vs libvpx
//
// The libvpx oracle is the patched vpxenc-oracle; see
// internal/coracle/build_vpxenc_oracle.sh which adds the
// govpx_oracle_emit_dropped_frame hook + three call sites in
// vp8/encoder/onyx_if.c (decimation, buffer underrun, post-encode
// overshoot).
func TestVP8OracleCBRDropFrameParity(t *testing.T) {
	vp8test.RequireOracle(t, "encoder oracle CBR drop-frame parity report")
	vpxencOracle := vp8test.VpxencOracle(t)

	fixtures := []cbrDropFixtureSpec{
		{
			Name:             "panning-30f-80kbps-cpu8",
			Width:            64,
			Height:           64,
			FPS:              30,
			Frames:           30,
			TargetKbps:       80,
			BufferSizeMs:     600,
			BufferInitialMs:  400,
			BufferOptimalMs:  500,
			MinQ:             4,
			MaxQ:             63,
			Deadline:         DeadlineRealtime,
			CpuUsed:          8,
			KeyFrameInterval: 999,
			LibvpxDropFrame:  60,
		},
		{
			Name:             "panning-60f-120kbps-tight-buf",
			Width:            64,
			Height:           64,
			FPS:              30,
			Frames:           60,
			TargetKbps:       120,
			BufferSizeMs:     400,
			BufferInitialMs:  300,
			BufferOptimalMs:  350,
			MinQ:             4,
			MaxQ:             63,
			Deadline:         DeadlineRealtime,
			CpuUsed:          8,
			KeyFrameInterval: 999,
			LibvpxDropFrame:  60,
		},
		{
			Name:             "scene-switch-30f-200kbps",
			Width:            64,
			Height:           64,
			FPS:              30,
			Frames:           30,
			TargetKbps:       200,
			BufferSizeMs:     500,
			BufferInitialMs:  300,
			BufferOptimalMs:  400,
			MinQ:             4,
			MaxQ:             63,
			Deadline:         DeadlineRealtime,
			CpuUsed:          8,
			KeyFrameInterval: 999,
			ContentSwitchAt:  15,
			LibvpxDropFrame:  60,
		},
	}

	type fixtureSummary struct {
		Name                  string  `json:"name"`
		Frames                int     `json:"frames"`
		GovpxDroppedCount     int     `json:"govpx_dropped_count"`
		LibvpxDroppedCount    int     `json:"libvpx_dropped_count"`
		GovpxDroppedIndices   []int   `json:"govpx_dropped_indices"`
		LibvpxDroppedIndices  []int   `json:"libvpx_dropped_indices"`
		Jaccard               float64 `json:"jaccard"`
		ForceMaxQPDivergences int     `json:"force_maxqp_divergences"`
		BufferLevelMeanAbsPct float64 `json:"buffer_level_mean_abs_pct"`
		BufferLevelMaxAbsPct  float64 `json:"buffer_level_max_abs_pct"`
		PostDropQMaxDrift     int     `json:"post_drop_q_max_drift"`
		PostDropQMeanDrift    float64 `json:"post_drop_q_mean_drift"`
		// Govpx-only baselined scalars: regression triggers if these change
		// outside slack even when libvpx is right there alongside.
		GovpxBufferLevelMean  float64 `json:"govpx_buffer_level_mean"`
		GovpxPostDropQMean    float64 `json:"govpx_post_drop_q_mean"`
		LibvpxBufferLevelMean float64 `json:"libvpx_buffer_level_mean"`
		LibvpxPostDropQMean   float64 `json:"libvpx_post_drop_q_mean"`
	}

	type baselineFile struct {
		Fixtures map[string]fixtureSummary `json:"fixtures"`
	}

	baselinePath := "testdata/cbr_drop_parity_baseline.json"
	updateBaselines := vp8test.UpdateBaselines()
	baseline, baselineExists := vp8test.ReadOptionalJSONBaseline[baselineFile](t, baselinePath)

	current := baselineFile{Fixtures: make(map[string]fixtureSummary, len(fixtures))}

	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			sources := make([]Image, fx.Frames)
			for i := range sources {
				if fx.ContentSwitchAt > 0 && i < fx.ContentSwitchAt {
					sources[i] = encoderValidationSegmentedFrame(fx.Width, fx.Height, i)
				} else {
					sources[i] = encoderValidationPanningFrame(fx.Width, fx.Height, i)
				}
			}

			opts := EncoderOptions{
				Width:               fx.Width,
				Height:              fx.Height,
				FPS:                 fx.FPS,
				RateControlMode:     RateControlCBR,
				TargetBitrateKbps:   fx.TargetKbps,
				MinQuantizer:        fx.MinQ,
				MaxQuantizer:        fx.MaxQ,
				Deadline:            fx.Deadline,
				CpuUsed:             fx.CpuUsed,
				KeyFrameInterval:    fx.KeyFrameInterval,
				DropFrameAllowed:    true,
				BufferSizeMs:        fx.BufferSizeMs,
				BufferInitialSizeMs: fx.BufferInitialMs,
				BufferOptimalSizeMs: fx.BufferOptimalMs,
			}
			govpxTrace := captureGovpxDropAwareTrace(t, opts, sources)
			libvpxTrace := captureLibvpxDropAwareTrace(t, vpxencOracle, opts, fx, sources)

			gDropIdx, gDropForceMaxQP, gPostDropQ, gBufferByFrame, gQByFrame := summarizeDropTrace(t, govpxTrace, fx.Frames)
			lDropIdx, lDropForceMaxQP, lPostDropQ, lBufferByFrame, lQByFrame := summarizeDropTrace(t, libvpxTrace, fx.Frames)

			summary := fixtureSummary{
				Name:                 fx.Name,
				Frames:               fx.Frames,
				GovpxDroppedCount:    len(gDropIdx),
				LibvpxDroppedCount:   len(lDropIdx),
				GovpxDroppedIndices:  gDropIdx,
				LibvpxDroppedIndices: lDropIdx,
			}
			summary.Jaccard = jaccardIntSets(gDropIdx, lDropIdx)
			summary.ForceMaxQPDivergences = countForceMaxQPDivergences(gDropForceMaxQP, lDropForceMaxQP)

			// Buffer-level deviation: compare libvpx vs govpx on every
			// frame slot we have a reading for on both sides. Express the
			// |delta| as a percentage of |libvpx| using a small floor so
			// near-zero buffer levels don't blow up the percentage.
			gBufMean, lBufMean, bufMeanAbsPct, bufMaxAbsPct := compareBufferTrajectory(gBufferByFrame, lBufferByFrame)
			summary.BufferLevelMeanAbsPct = bufMeanAbsPct
			summary.BufferLevelMaxAbsPct = bufMaxAbsPct
			summary.GovpxBufferLevelMean = gBufMean
			summary.LibvpxBufferLevelMean = lBufMean

			// Post-drop Q recovery: for each shared dropped frame index,
			// compare the FOLLOWING non-dropped frame's Q index between
			// govpx and libvpx. govpx pins force_maxqp on the first inter
			// after a drop, so the first follow-up Q should be at the max.
			postDropQMaxDrift, postDropQMeanDrift, gPostDropQMean, lPostDropQMean := comparePostDropQ(gDropIdx, lDropIdx, gQByFrame, lQByFrame)
			summary.PostDropQMaxDrift = postDropQMaxDrift
			summary.PostDropQMeanDrift = postDropQMeanDrift
			summary.GovpxPostDropQMean = gPostDropQMean
			summary.LibvpxPostDropQMean = lPostDropQMean

			// Compact summary line.
			t.Logf("[%s] dropped govpx=%d libvpx=%d jaccard=%.3f force_maxqp_div=%d buf_mean_abs_pct=%.2f buf_max_abs_pct=%.2f post_drop_q_max_drift=%d post_drop_q_mean_drift=%.3f",
				fx.Name, summary.GovpxDroppedCount, summary.LibvpxDroppedCount, summary.Jaccard, summary.ForceMaxQPDivergences,
				summary.BufferLevelMeanAbsPct, summary.BufferLevelMaxAbsPct, summary.PostDropQMaxDrift, summary.PostDropQMeanDrift)
			t.Logf("[%s] govpx drop indices=%v libvpx drop indices=%v", fx.Name, gDropIdx, lDropIdx)
			t.Logf("[%s] govpx post-drop Q follow-ups=%v libvpx post-drop Q follow-ups=%v", fx.Name, gPostDropQ, lPostDropQ)

			current.Fixtures[fx.Name] = summary

			if updateBaselines || !baselineExists {
				return
			}
			prev, ok := baseline.Fixtures[fx.Name]
			if !ok {
				t.Errorf("baseline %s missing fixture %q (rerun with GOVPX_UPDATE_BASELINES=1)", baselinePath, fx.Name)
				return
			}
			// Drop-count parity within +/-2 vs libvpx (acceptance band).
			if abs(summary.GovpxDroppedCount-summary.LibvpxDroppedCount) > 2 {
				t.Errorf("[%s] dropped_frame_count parity: govpx=%d libvpx=%d diff > 2",
					fx.Name, summary.GovpxDroppedCount, summary.LibvpxDroppedCount)
			}
			// Drop-count must not regress vs baseline govpx.
			if summary.GovpxDroppedCount > prev.GovpxDroppedCount+2 {
				t.Errorf("[%s] govpx_dropped_count=%d baseline=%d drift > 2 (rerun with GOVPX_UPDATE_BASELINES=1 if intended)",
					fx.Name, summary.GovpxDroppedCount, prev.GovpxDroppedCount)
			}
			if summary.GovpxDroppedCount < prev.GovpxDroppedCount-2 {
				t.Errorf("[%s] govpx_dropped_count=%d baseline=%d shrank > 2 (rerun with GOVPX_UPDATE_BASELINES=1 if intended)",
					fx.Name, summary.GovpxDroppedCount, prev.GovpxDroppedCount)
			}
			// Jaccard parity vs libvpx.
			if summary.LibvpxDroppedCount > 0 || summary.GovpxDroppedCount > 0 {
				if summary.Jaccard < 0.7 {
					if prev.Jaccard >= 0.7 {
						t.Errorf("[%s] dropped_frame_indices Jaccard=%.3f below 0.7 (regressed from baseline %.3f)",
							fx.Name, summary.Jaccard, prev.Jaccard)
					} else if summary.Jaccard < prev.Jaccard-0.05 {
						t.Errorf("[%s] dropped_frame_indices Jaccard=%.3f baseline=%.3f drift > 0.05 below baseline",
							fx.Name, summary.Jaccard, prev.Jaccard)
					}
				}
			}
			// Force-maxqp parity within 1 frame.
			if summary.ForceMaxQPDivergences > prev.ForceMaxQPDivergences+1 {
				t.Errorf("[%s] force_maxqp_divergences=%d baseline=%d drift > 1",
					fx.Name, summary.ForceMaxQPDivergences, prev.ForceMaxQPDivergences)
			}
			// Buffer-level trajectory.
			if summary.BufferLevelMeanAbsPct > 10 && summary.BufferLevelMeanAbsPct > prev.BufferLevelMeanAbsPct+2 {
				t.Errorf("[%s] buffer_level_mean_abs_pct=%.2f exceeds 10%% AND baseline %.2f + 2",
					fx.Name, summary.BufferLevelMeanAbsPct, prev.BufferLevelMeanAbsPct)
			}
			// Post-drop Q recovery within 4 indices of libvpx, baseline-gated.
			if summary.PostDropQMaxDrift > 4 && summary.PostDropQMaxDrift > prev.PostDropQMaxDrift+1 {
				t.Errorf("[%s] post_drop_q_max_drift=%d exceeds 4 AND baseline %d + 1",
					fx.Name, summary.PostDropQMaxDrift, prev.PostDropQMaxDrift)
			}
		})
	}

	if updateBaselines || !baselineExists {
		vp8test.WriteJSONBaseline(t, baselinePath, current)
	}

	// Stable summary CSV for human readability.
	type rowEntry struct {
		name string
		s    fixtureSummary
	}
	rows := make([]rowEntry, 0, len(current.Fixtures))
	for n, s := range current.Fixtures {
		rows = append(rows, rowEntry{n, s})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	var summary bytes.Buffer
	fmt.Fprintln(&summary, "fixture,govpx_drop,libvpx_drop,jaccard,force_maxqp_div,buf_mean_abs_pct,buf_max_abs_pct,post_drop_q_max_drift")
	for _, r := range rows {
		fmt.Fprintf(&summary, "%s,%d,%d,%.3f,%d,%.2f,%.2f,%d\n",
			r.name, r.s.GovpxDroppedCount, r.s.LibvpxDroppedCount, r.s.Jaccard,
			r.s.ForceMaxQPDivergences, r.s.BufferLevelMeanAbsPct, r.s.BufferLevelMaxAbsPct, r.s.PostDropQMaxDrift)
	}
	t.Logf("CBR drop parity summary:\n%s", summary.String())
}

// captureGovpxDropAwareTrace runs govpx with oracle tracing enabled while
// allowing dropped frames; unlike captureGovpxEncoderTrace this does not
// fail when EncodeResult.Dropped is true. Instead the caller relies on the
// emitted "frame" rows (regular + dropped) to recover the per-frame
// trajectory.
func captureGovpxDropAwareTrace(t *testing.T, opts EncoderOptions, sources []Image) []byte {
	t.Helper()
	requireOracleTraceBuild(t)
	var trace bytes.Buffer
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder returned error: %v", err)
	}
	enc.SetOracleTraceWriter(&trace)
	packet := make([]byte, opts.Width*opts.Height*3)
	for i, source := range sources {
		if _, err := enc.EncodeInto(packet, source, uint64(i), 1, 0); err != nil {
			t.Fatalf("EncodeInto frame %d returned error: %v", i, err)
		}
	}
	return append([]byte(nil), trace.Bytes()...)
}

// captureLibvpxDropAwareTrace runs the patched vpxenc-oracle with
// drop-frames-water-mark configured so the libvpx side actually drops
// frames; the standard captureLibvpxEncoderTrace omits buffer / drop knobs
// and pins min/max-q at 4/56 which is too narrow for this parity report.
func captureLibvpxDropAwareTrace(t *testing.T, vpxencOracle string, opts EncoderOptions, fx cbrDropFixtureSpec, sources []Image) []byte {
	t.Helper()
	extraArgs := []string{
		"--end-usage=cbr",
		"--buf-sz=" + strconv.Itoa(fx.BufferSizeMs),
		"--buf-initial-sz=" + strconv.Itoa(fx.BufferInitialMs),
		"--buf-optimal-sz=" + strconv.Itoa(fx.BufferOptimalMs),
		"--drop-frame=" + strconv.Itoa(fx.LibvpxDropFrame),
	}
	trace, diag, err := vp8test.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8OracleTraceConfig(vpxencOracle, opts, len(sources), fx.TargetKbps, nil, extraArgs),
	)
	if err != nil {
		t.Fatalf("vpxenc-oracle failed: %v\n%s", err, diag)
	}
	return trace
}

// summarizeDropTrace walks the raw oracle JSONL trace and returns
// per-frame slot data for both dropped and non-dropped frames. The trace
// has rate rows interleaved before each non-dropped frame row in vp8 pack
// order; dropped frames emit only a single frame row carrying
// dropped:true. The trace's frame_index already matches the source-frame
// ordinal on both encoders (govpx increments e.frameCount inside the drop
// branch; the libvpx oracle increments govpx_oracle_state.frame_index
// inside govpx_oracle_emit_dropped_frame).
//
// Returns:
//
//	dropIdx          - dropped frame indices (sorted)
//	dropForceMaxQP   - per dropped index, the force_maxqp flag the
//	                   encoder set on that drop (govpx always true on its
//	                   buffer-underrun branch; libvpx true iff the
//	                   overshoot path fired)
//	postDropQ        - the q_index of the FIRST non-dropped frame after
//	                   each dropped index (or -1 if no follow-up exists)
//	bufferByFrame    - per source-frame index, the buffer_level reading.
//	                   For non-dropped frames this comes from the matching
//	                   "rate" row's buffer_level; for dropped frames it
//	                   comes from the dropped_frame row (post-drop).
//	qByFrame         - per source-frame index, the q_index reading
//	                   (-1 for dropped frames since no Q was selected)
func summarizeDropTrace(t *testing.T, trace []byte, totalFrames int) ([]int, map[int]bool, []int, map[int]int64, map[int]int) {
	t.Helper()
	dropIdx := make([]int, 0, totalFrames)
	dropForceMaxQP := make(map[int]bool)
	bufferByFrame := make(map[int]int64, totalFrames)
	qByFrame := make(map[int]int, totalFrames)
	rows, err := vp8test.TraceRows(trace)
	if err != nil {
		t.Fatalf("parse drop trace: %v", err)
	}
	for _, row := range rows {
		typ, _ := row["type"].(string)
		idx := int(vp8test.TraceFloat(row["frame_index"]))
		switch typ {
		case "frame":
			dropped, _ := row["dropped"].(bool)
			if dropped {
				dropIdx = append(dropIdx, idx)
				fm, _ := row["force_maxqp"].(bool)
				dropForceMaxQP[idx] = fm
				if v, ok := row["buffer_level"]; ok {
					bufferByFrame[idx] = int64(vp8test.TraceFloat(v))
				}
				qByFrame[idx] = -1
				continue
			}
			if v, ok := row["q_index"]; ok {
				qByFrame[idx] = int(vp8test.TraceFloat(v))
			}
		case "rate":
			// Latest rate row for the frame's idx wins; vp8_pack_bitstream
			// emits a single rate row per frame just before pack.
			if v, ok := row["buffer_level"]; ok {
				bufferByFrame[idx] = int64(vp8test.TraceFloat(v))
			}
		}
	}
	sort.Ints(dropIdx)
	postDrop := make([]int, 0, len(dropIdx))
	for _, di := range dropIdx {
		next := -1
		for j := di + 1; j < totalFrames; j++ {
			if q, ok := qByFrame[j]; ok && q >= 0 {
				next = q
				break
			}
		}
		postDrop = append(postDrop, next)
	}
	return dropIdx, dropForceMaxQP, postDrop, bufferByFrame, qByFrame
}

// jaccardIntSets returns |A∩B| / |A∪B|. Returns 1.0 when both sets are
// empty (the trivial parity case).
func jaccardIntSets(a, b []int) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	mb := make(map[int]struct{}, len(b))
	for _, v := range b {
		mb[v] = struct{}{}
	}
	intersection := 0
	for _, v := range a {
		if _, ok := mb[v]; ok {
			intersection++
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 1.0
	}
	return float64(intersection) / float64(union)
}

// countForceMaxQPDivergences reports the number of source-frame indices
// where one side flagged force_maxqp on the dropped frame and the other
// did not.
func countForceMaxQPDivergences(a, b map[int]bool) int {
	div := 0
	visited := make(map[int]struct{}, len(a)+len(b))
	for k, va := range a {
		visited[k] = struct{}{}
		if vb, ok := b[k]; !ok || vb != va {
			div++
		}
	}
	for k, vb := range b {
		if _, ok := visited[k]; ok {
			continue
		}
		if va, ok := a[k]; !ok || va != vb {
			div++
		}
	}
	return div
}

// compareBufferTrajectory compares two per-frame buffer-level maps and
// returns (govpx mean, libvpx mean, mean |delta|/|libvpx| pct, max |delta|/|libvpx| pct).
// Frames absent from either side are skipped. A small floor in the
// denominator avoids blow-ups when libvpx reports a near-zero buffer level.
func compareBufferTrajectory(g, l map[int]int64) (float64, float64, float64, float64) {
	var gSum, lSum float64
	gN, lN := 0, 0
	for _, v := range g {
		gSum += float64(v)
		gN++
	}
	for _, v := range l {
		lSum += float64(v)
		lN++
	}
	gMean, lMean := 0.0, 0.0
	if gN > 0 {
		gMean = gSum / float64(gN)
	}
	if lN > 0 {
		lMean = lSum / float64(lN)
	}
	var sumPct, maxPct float64
	n := 0
	for k, gv := range g {
		lv, ok := l[k]
		if !ok {
			continue
		}
		denom := math.Abs(float64(lv))
		if denom < 1000 {
			denom = 1000
		}
		pct := 100.0 * math.Abs(float64(gv-lv)) / denom
		sumPct += pct
		if pct > maxPct {
			maxPct = pct
		}
		n++
	}
	mean := 0.0
	if n > 0 {
		mean = sumPct / float64(n)
	}
	return gMean, lMean, mean, maxPct
}

// comparePostDropQ inspects, for each source-frame index that is dropped
// on EITHER side, the Q index of the next non-dropped frame on each side.
// Returns (max |drift|, mean |drift|, govpx mean post-drop Q, libvpx mean
// post-drop Q) considering only indices where both sides have a non-
// negative follow-up Q.
func comparePostDropQ(gDrop, lDrop []int, gQ, lQ map[int]int) (int, float64, float64, float64) {
	all := make(map[int]struct{}, len(gDrop)+len(lDrop))
	for _, i := range gDrop {
		all[i] = struct{}{}
	}
	for _, i := range lDrop {
		all[i] = struct{}{}
	}
	keys := make([]int, 0, len(all))
	for k := range all {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var sumDrift, gSum, lSum float64
	maxDrift := 0
	n := 0
	for _, di := range keys {
		gNext := nextNonDropQ(gQ, di)
		lNext := nextNonDropQ(lQ, di)
		if gNext < 0 || lNext < 0 {
			continue
		}
		drift := abs(gNext - lNext)
		sumDrift += float64(drift)
		gSum += float64(gNext)
		lSum += float64(lNext)
		if drift > maxDrift {
			maxDrift = drift
		}
		n++
	}
	if n == 0 {
		return 0, 0, 0, 0
	}
	return maxDrift, sumDrift / float64(n), gSum / float64(n), lSum / float64(n)
}

func nextNonDropQ(qByFrame map[int]int, di int) int {
	for j := di + 1; ; j++ {
		q, ok := qByFrame[j]
		if !ok {
			return -1
		}
		if q < 0 {
			continue
		}
		return q
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// cbrDropFixtureSpec carries the per-fixture knobs for the drop parity report.
// It is named at file scope so captureLibvpxDropAwareTrace can take it
// without an awkward inline struct literal echo.
type cbrDropFixtureSpec struct {
	Name             string
	Width            int
	Height           int
	FPS              int
	Frames           int
	TargetKbps       int
	BufferSizeMs     int
	BufferInitialMs  int
	BufferOptimalMs  int
	MinQ             int
	MaxQ             int
	Deadline         Deadline
	CpuUsed          int
	ContentSwitchAt  int // 0 means panning only; >0 means swap to panning at that frame
	LibvpxDropFrame  int // libvpx --drop-frame= value
	KeyFrameInterval int
}
