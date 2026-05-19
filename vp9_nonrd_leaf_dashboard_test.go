//go:build govpx_oracle_trace

package govpx

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestVP9DeferredSeedsLeafDecisionDashboard keeps the nonrd pickmode lane
// focused on actual block decisions. It compares govpx and libvpx packets
// after decoding them through govpx's VP9 parser, then logs per-leaf mode,
// MV, filter, tx-size, qcoeff-count, and token-count deltas for the first
// mismatching frame in each historical deferred or regression seed.
//
// The subtests deliberately split speed-8 nonrd RefControl seeds from
// RuntimeControls RD/keyframe seeds, closed speed-8 nonrd regression seeds,
// and speed-4 realtime seeds so improvements in one lane do not get mistaken
// for global closure.
func TestVP9DeferredSeedsLeafDecisionDashboard(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 leaf-decision dashboard")
	}
	requireVP9VpxencFrameFlagsOracle(t)

	t.Run("RefControlSpeed8NonRD", func(t *testing.T) {
		for idx, seed := range vp9RefControlsRegressionSeeds {
			tc := newVP9RefControlsFuzzCase(seed)
			sum := sha256.Sum256(seed)
			label := fmt.Sprintf("refctrl-#%d-%s", idx, hex.EncodeToString(sum[:4]))
			logVP9LeafDecisionCase(t, label, tc.opts, tc.sources, tc.flags, tc.extraArgs)
		}
	})

	t.Run("RuntimeControlsRDKeyframeCPU0Neg3", func(t *testing.T) {
		logVP9RuntimeLeafDecisionCases(t, func(cpu int8) bool {
			return cpu == 0 || cpu == -3
		})
	})

	t.Run("RuntimeControlsSpeed8NonRD", func(t *testing.T) {
		logVP9RuntimeLeafDecisionSeeds(t, vp9RuntimeControlsRegressionSeeds)
	})

	t.Run("RuntimeControlsSpeed4Realtime", func(t *testing.T) {
		logVP9RuntimeLeafDecisionCases(t, func(cpu int8) bool {
			return cpu == 4
		})
	})
}

func logVP9RuntimeLeafDecisionCases(t *testing.T, includeCPU func(int8) bool) {
	t.Helper()
	count := 0
	for idx, seed := range vp9RuntimeControlsSeedsDeferred {
		tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
		if !includeCPU(tc.opts.CpuUsed) {
			continue
		}
		count++
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("runtimectrl-#%d-%s-cpu%d", idx,
			hex.EncodeToString(sum[:4]), tc.opts.CpuUsed)
		logVP9LeafDecisionCase(t, label, tc.opts, tc.sources, tc.flags,
			tc.extraArgs)
	}
	if count == 0 {
		t.Log("no runtime-control seeds matched this dashboard lane")
	}
}

func logVP9RuntimeLeafDecisionSeeds(t *testing.T, seeds [][]byte) {
	t.Helper()
	for idx, seed := range seeds {
		tc := vp9OracleRuntimeFuzzCaseFromBytes(seed)
		sum := sha256.Sum256(seed)
		label := fmt.Sprintf("runtimectrl-regression-#%d-%s-cpu%d", idx,
			hex.EncodeToString(sum[:4]), tc.opts.CpuUsed)
		logVP9LeafDecisionCase(t, label, tc.opts, tc.sources, tc.flags,
			tc.extraArgs)
	}
}

func logVP9LeafDecisionCase(t *testing.T, label string, opts VP9EncoderOptions,
	sources []*image.YCbCr, flags []EncodeFlags, extraArgs []string,
) {
	t.Helper()
	got := encodeVP9FramesWithGovpx(t, opts, sources, flags)
	want := encodeVP9FramesWithLibvpxFrameFlagsOracle(t, sources, flags, extraArgs)
	seedDelta := seedSizeDelta(got, want)
	firstFrame := firstVP9MismatchingFrame(got, want)
	if firstFrame < 0 {
		t.Logf("%s PASS: frames=%d size_delta=%+d", label, len(got), seedDelta)
		return
	}
	if firstFrame >= len(got) || firstFrame >= len(want) {
		t.Logf("%s FAIL: frame_count_mismatch got=%d want=%d size_delta=%+d",
			label, len(got), len(want), seedDelta)
		return
	}

	gotTrace := decodeVP9LeafTracesForPackets(t, label+" govpx", got)
	wantTrace := decodeVP9LeafTracesForPackets(t, label+" libvpx", want)
	var gotRows, wantRows []vp9DecodedLeafTrace
	if firstFrame < len(gotTrace) {
		gotRows = gotTrace[firstFrame]
	}
	if firstFrame < len(wantTrace) {
		wantRows = wantTrace[firstFrame]
	}
	t.Logf("%s FAIL: first_mismatch_frame=%d got_len=%d want_len=%d first_byte_diff=%d size_delta=%+d leaf_delta=%s first_leaf_diff=%s",
		label, firstFrame, len(got[firstFrame]), len(want[firstFrame]),
		firstVP9PacketDiffForTest(got[firstFrame], want[firstFrame]),
		seedDelta, vp9LeafTraceAggregateDelta(gotRows, wantRows),
		firstVP9LeafTraceDiff(gotRows, wantRows))
}

func firstVP9MismatchingFrame(got, want [][]byte) int {
	n := min(len(got), len(want))
	for i := 0; i < n; i++ {
		g := sha256.Sum256(got[i])
		w := sha256.Sum256(want[i])
		if g != w {
			return i
		}
	}
	if len(got) != len(want) {
		return n
	}
	return -1
}

func decodeVP9LeafTracesForPackets(t *testing.T, label string,
	packets [][]byte,
) [][]vp9DecodedLeafTrace {
	t.Helper()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("%s NewVP9Decoder: %v", label, err)
	}
	d.enableVP9DecodedLeafTrace()
	defer func() { _ = d.Close() }()

	out := make([][]vp9DecodedLeafTrace, len(packets))
	for i, packet := range packets {
		d.resetVP9DecodedLeafTrace()
		if len(packet) == 0 {
			continue
		}
		if err := d.Decode(packet); err != nil {
			t.Fatalf("%s Decode frame %d: %v", label, i, err)
		}
		out[i] = d.vp9DecodedLeafTraceRows()
	}
	return out
}

type vp9LeafTraceKey struct {
	row int
	col int
}

func firstVP9LeafTraceDiff(got, want []vp9DecodedLeafTrace) string {
	gotByKey := vp9LeafTraceByKey(got)
	wantByKey := vp9LeafTraceByKey(want)
	keys := vp9SortedLeafTraceKeys(gotByKey, wantByKey)
	for _, key := range keys {
		g, gok := gotByKey[key]
		w, wok := wantByKey[key]
		switch {
		case !gok:
			return fmt.Sprintf("mi=(%d,%d) missing govpx leaf want=%s",
				key.row, key.col, vp9LeafTraceBrief(w))
		case !wok:
			return fmt.Sprintf("mi=(%d,%d) extra govpx leaf got=%s",
				key.row, key.col, vp9LeafTraceBrief(g))
		}
		fields := vp9LeafTraceFieldDiffs(g, w)
		if len(fields) != 0 {
			return fmt.Sprintf("mi=(%d,%d) %s got=%s want=%s", key.row, key.col,
				strings.Join(fields, " "), vp9LeafTraceBrief(g), vp9LeafTraceBrief(w))
		}
	}
	return "none"
}

func vp9LeafTraceAggregateDelta(got, want []vp9DecodedLeafTrace) string {
	gotByKey := vp9LeafTraceByKey(got)
	wantByKey := vp9LeafTraceByKey(want)
	keys := vp9SortedLeafTraceKeys(gotByKey, wantByKey)
	var missingGovpx, extraGovpx int
	var bsizeDiff, modeDiff, refDiff, mvDiff, filterDiff, txDiff, skipDiff int
	for _, key := range keys {
		g, gok := gotByKey[key]
		w, wok := wantByKey[key]
		if !gok {
			missingGovpx++
			continue
		}
		if !wok {
			extraGovpx++
			continue
		}
		if g.BSize != w.BSize {
			bsizeDiff++
		}
		if g.Mode != w.Mode {
			modeDiff++
		}
		if g.Ref0 != w.Ref0 || g.Ref1 != w.Ref1 {
			refDiff++
		}
		if g.Mv0Row != w.Mv0Row || g.Mv0Col != w.Mv0Col ||
			g.Mv1Row != w.Mv1Row || g.Mv1Col != w.Mv1Col {
			mvDiff++
		}
		if g.InterpFilter != w.InterpFilter {
			filterDiff++
		}
		if g.TxSize != w.TxSize {
			txDiff++
		}
		if g.Skip != w.Skip {
			skipDiff++
		}
	}
	gotTokens, gotEOB, gotQCoeff, gotQAbs := vp9LeafTraceTotals(got)
	wantTokens, wantEOB, wantQCoeff, wantQAbs := vp9LeafTraceTotals(want)
	return fmt.Sprintf("leaves=%d/%d missing_govpx=%d extra_govpx=%d bsize=%d mode=%d ref=%d mv=%d filter=%d tx=%d skip=%d token_delta=%+d eob_delta=%+d qcoeff_delta=%+d qabs_delta=%+d",
		len(got), len(want), missingGovpx, extraGovpx, bsizeDiff, modeDiff,
		refDiff, mvDiff, filterDiff, txDiff, skipDiff,
		gotTokens-wantTokens, gotEOB-wantEOB, gotQCoeff-wantQCoeff,
		gotQAbs-wantQAbs)
}

func vp9LeafTraceByKey(rows []vp9DecodedLeafTrace) map[vp9LeafTraceKey]vp9DecodedLeafTrace {
	out := make(map[vp9LeafTraceKey]vp9DecodedLeafTrace, len(rows))
	for _, row := range rows {
		out[vp9LeafTraceKey{row: row.MIRow, col: row.MICol}] = row
	}
	return out
}

func vp9SortedLeafTraceKeys(maps ...map[vp9LeafTraceKey]vp9DecodedLeafTrace) []vp9LeafTraceKey {
	seen := make(map[vp9LeafTraceKey]struct{})
	for _, rows := range maps {
		for key := range rows {
			seen[key] = struct{}{}
		}
	}
	keys := make([]vp9LeafTraceKey, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].row != keys[j].row {
			return keys[i].row < keys[j].row
		}
		return keys[i].col < keys[j].col
	})
	return keys
}

func vp9LeafTraceFieldDiffs(got, want vp9DecodedLeafTrace) []string {
	var fields []string
	if got.BSize != want.BSize {
		fields = append(fields, fmt.Sprintf("bsize:%d/%d", got.BSize, want.BSize))
	}
	if got.Mode != want.Mode {
		fields = append(fields, fmt.Sprintf("mode:%d/%d", got.Mode, want.Mode))
	}
	if got.UvMode != want.UvMode {
		fields = append(fields, fmt.Sprintf("uv:%d/%d", got.UvMode, want.UvMode))
	}
	if got.Ref0 != want.Ref0 || got.Ref1 != want.Ref1 {
		fields = append(fields, fmt.Sprintf("ref:(%d,%d)/(%d,%d)",
			got.Ref0, got.Ref1, want.Ref0, want.Ref1))
	}
	if got.Mv0Row != want.Mv0Row || got.Mv0Col != want.Mv0Col ||
		got.Mv1Row != want.Mv1Row || got.Mv1Col != want.Mv1Col {
		fields = append(fields, fmt.Sprintf("mv:(%d,%d)(%d,%d)/(%d,%d)(%d,%d)",
			got.Mv0Row, got.Mv0Col, got.Mv1Row, got.Mv1Col,
			want.Mv0Row, want.Mv0Col, want.Mv1Row, want.Mv1Col))
	}
	if got.InterpFilter != want.InterpFilter {
		fields = append(fields, fmt.Sprintf("filter:%d/%d", got.InterpFilter, want.InterpFilter))
	}
	if got.TxSize != want.TxSize {
		fields = append(fields, fmt.Sprintf("tx:%d/%d", got.TxSize, want.TxSize))
	}
	if got.Skip != want.Skip {
		fields = append(fields, fmt.Sprintf("skip:%d/%d", got.Skip, want.Skip))
	}
	if got.TokenCount != want.TokenCount {
		fields = append(fields, fmt.Sprintf("tokens:%d/%d", got.TokenCount, want.TokenCount))
	}
	if got.QCoeffNonZero != want.QCoeffNonZero {
		fields = append(fields, fmt.Sprintf("qcoeff:%d/%d", got.QCoeffNonZero, want.QCoeffNonZero))
	}
	if got.QCoeffAbsSum != want.QCoeffAbsSum {
		fields = append(fields, fmt.Sprintf("qabs:%d/%d", got.QCoeffAbsSum, want.QCoeffAbsSum))
	}
	return fields
}

func vp9LeafTraceTotals(rows []vp9DecodedLeafTrace) (tokens, eob, qcoeff, qabs int) {
	for _, row := range rows {
		tokens += row.TokenCount
		eob += row.EOBTotal
		qcoeff += row.QCoeffNonZero
		qabs += row.QCoeffAbsSum
	}
	return tokens, eob, qcoeff, qabs
}

func vp9LeafTraceBrief(row vp9DecodedLeafTrace) string {
	return fmt.Sprintf("bsize=%d mode=%d uv=%d ref=(%d,%d) mv=(%d,%d)(%d,%d) filter=%d tx=%d skip=%d txblocks=%d eob=%d tokens=%d qcoeff=%d qabs=%d",
		row.BSize, row.Mode, row.UvMode, row.Ref0, row.Ref1, row.Mv0Row, row.Mv0Col,
		row.Mv1Row, row.Mv1Col, row.InterpFilter, row.TxSize, row.Skip,
		row.TxBlockCount, row.EOBTotal, row.TokenCount, row.QCoeffNonZero,
		row.QCoeffAbsSum)
}
