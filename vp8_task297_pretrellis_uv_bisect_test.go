//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
)

// TestVP8Task297PretrellisUVBisect runs the BestARNR / GoodARNR audit
// pair with the per-UV-block pre-trellis qcoeff oracle tracer enabled on
// both the govpx and libvpx sides (task #296 landed both halves of the
// trace), captures the per-block coeff / qcoeff / dqcoeff / eob /
// zbin_extra / zbin_oq snapshots, and bisects the first MB+block+scan-pos
// at which the two diverge on frame 1.
//
// Method:
//   - Build the trace on each cohort: read the libvpx trace from the
//     GOVPX_ORACLE_TRACE_OUT path the oracle wrote during the audit's
//     re-encode (env GOVPX_ORACLE_PRETRELLIS_UV=1 also set so the patched
//     oracle actually emits the rows).
//   - Capture the govpx trace by setting SetOracleTracePretrellisUVDump
//     on the encoder before EncodeInto, with the trace writer pointing at
//     an in-memory buffer.
//   - Group rows by (mb_row, mb_col, block) on frame 1, iterate in
//     raster (mb_row, mb_col) × block (16..23) × scan-pos (0..15) order,
//     emit the first divergent triple. The walk-order matches libvpx's
//     row-major MB iteration so the first diverging (mb_row, mb_col,
//     block, scan_pos) is the byte-exact root.
//   - Report which layer (coeff vs qcoeff) carries the divergence:
//   - if coeff differs but qcoeff would match given that coeff
//     (i.e. coeff[i] differs) -> fdct or pre-DCT residual divergence.
//   - if coeff matches but qcoeff differs -> quantize step divergence
//     (zbin / round / quant_shift / dequant or zbin_extra / zbin_oq).
//   - Dump up to 12 divergent rows alongside the per-block payload so
//     the bisect localization is preserved in the test log.
//
// The test is gated on GOVPX_WITH_ORACLE=1 just like the audit pair —
// running cheap unit tests by default. The audit pin lengths are NOT
// re-verified here; this probe ONLY surfaces the per-block trace.
func TestVP8Task297PretrellisUVBisect(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #297 pre-trellis UV bisect")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := findVpxencOracle(t)

	cohorts := []struct {
		name          string
		seedHash      string
		opts          EncoderOptions
		extraArgs     []string
		targetKbps    int
		dumpGovpxOut  string
		dumpLibvpxOut string
	}{
		{
			name:     "Best",
			seedHash: "19981bff",
			opts: EncoderOptions{
				Width:             1280,
				Height:            720,
				FPS:               30,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineBestQuality,
				CpuUsed:           0,
				Tuning:            TuneSSIM,
				ScreenContentMode: 1,
				TokenPartitions:   1,
				Threads:           4,
				ARNRMaxFrames:     1,
				ARNRStrength:      1,
				ARNRType:          2,
			},
			extraArgs: libvpxEndUsageArgs([]string{
				"--end-usage=vbr",
				"--screen-content-mode=1",
				"--token-parts=1",
				"--threads=4",
				"--tune=ssim",
				"--arnr-maxframes=1",
				"--arnr-strength=1",
				"--arnr-type=2",
			}),
			targetKbps:    700,
			dumpGovpxOut:  "/tmp/297-govpx-best.jsonl",
			dumpLibvpxOut: "/tmp/297-libvpx-best.jsonl",
		},
		{
			name:     "Good",
			seedHash: "788d442c",
			opts: EncoderOptions{
				Width:             1280,
				Height:            720,
				FPS:               30,
				RateControlMode:   RateControlVBR,
				TargetBitrateKbps: 700,
				MinQuantizer:      4,
				MaxQuantizer:      56,
				KeyFrameInterval:  999,
				Deadline:          DeadlineGoodQuality,
				CpuUsed:           0,
				Tuning:            TuneSSIM,
				ScreenContentMode: 1,
				TokenPartitions:   1,
				Threads:           4,
				ARNRMaxFrames:     1,
				ARNRStrength:      1,
				ARNRType:          2,
			},
			extraArgs: libvpxEndUsageArgs([]string{
				"--end-usage=vbr",
				"--screen-content-mode=1",
				"--token-parts=1",
				"--threads=4",
				"--tune=ssim",
				"--arnr-maxframes=1",
				"--arnr-strength=1",
				"--arnr-type=2",
			}),
			targetKbps:    700,
			dumpGovpxOut:  "/tmp/297-govpx-good.jsonl",
			dumpLibvpxOut: "/tmp/297-libvpx-good.jsonl",
		},
	}

	for _, c := range cohorts {
		c := c
		t.Run(c.name, func(t *testing.T) {
			runTask297PretrellisUVBisect(t, vpxencOracle, c.seedHash, c.opts, c.targetKbps, c.extraArgs, c.dumpGovpxOut, c.dumpLibvpxOut)
		})
	}
}

func runTask297PretrellisUVBisect(t *testing.T, vpxencOracle string, seedHash string, opts EncoderOptions, targetKbps int, extraArgs []string, govpxOutPath string, libvpxOutPath string) {
	t.Helper()

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	// govpx side: capture pre-trellis UV qcoeff trace
	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	enc.SetOracleTracePretrellisUVDump(true)
	packet := make([]byte, opts.Width*opts.Height*4+4096)
	for i, src := range sources {
		_, err := enc.EncodeInto(packet, src, uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("EncodeInto frame %d: %v", i, err)
		}
	}
	enc.Close()

	// libvpx side: re-encode via vpxenc-oracle with both env gates
	dir := t.TempDir()
	yuvPath := filepath.Join(dir, seedHash+".yuv")
	ivfPath := filepath.Join(dir, seedHash+".ivf")
	libvpxTracePath := filepath.Join(dir, seedHash+".jsonl")
	writeEncoderValidationI420(t, yuvPath, sources)

	deadlineArg := "--good"
	switch opts.Deadline {
	case DeadlineBestQuality:
		deadlineArg = "--best"
	case DeadlineRealtime:
		deadlineArg = "--rt"
	}
	autoAltRefArg := "--auto-alt-ref=0"
	if opts.AutoAltRef {
		autoAltRefArg = "--auto-alt-ref=1"
	}
	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--disable-warning-prompt",
		deadlineArg,
		"--cpu-used=" + strconv.Itoa(opts.CpuUsed),
		"--lag-in-frames=" + strconv.Itoa(opts.LookaheadFrames),
		autoAltRefArg,
		"--target-bitrate=" + strconv.Itoa(targetKbps),
		"--min-q=" + strconv.Itoa(opts.MinQuantizer),
		"--max-q=" + strconv.Itoa(opts.MaxQuantizer),
		"--i420",
		"--width=" + strconv.Itoa(opts.Width),
		"--height=" + strconv.Itoa(opts.Height),
		"--timebase=" + libvpxOracleTimebaseArg(opts),
		"--fps=" + libvpxOracleFPSArg(opts),
		"--limit=" + strconv.Itoa(len(sources)),
		"--output=" + ivfPath,
		"--kf-min-dist=999",
		"--kf-max-dist=999",
	}
	args = append(args, extraArgs...)
	args = append(args, yuvPath)
	cmd := exec.Command(vpxencOracle, args...)
	cmd.Env = append(os.Environ(),
		"GOVPX_ORACLE_TRACE_OUT="+libvpxTracePath,
		"GOVPX_ORACLE_PRETRELLIS_UV=1",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Logf("vpxenc-oracle args: %v", args)
		t.Logf("vpxenc-oracle output:\n%s", out)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}

	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

	if err := os.WriteFile(govpxOutPath, govpxTraceBuf.Bytes(), 0o644); err != nil {
		t.Logf("write govpx trace dump %s: %v", govpxOutPath, err)
	}
	if err := os.WriteFile(libvpxOutPath, libvpxTrace, 0o644); err != nil {
		t.Logf("write libvpx trace dump %s: %v", libvpxOutPath, err)
	}
	t.Logf("task297 seed=%s govpx_trace=%s libvpx_trace=%s",
		seedHash, govpxOutPath, libvpxOutPath)

	gRows := task297ParsePretrellisUVRows(govpxTraceBuf.Bytes(), 1)
	lRows := task297ParsePretrellisUVRows(libvpxTrace, 1)
	t.Logf("task297 seed=%s frame1 govpx_rows=%d libvpx_rows=%d",
		seedHash, len(gRows), len(lRows))

	// Group by (mb_row, mb_col, block).
	type rowKey struct {
		mbRow, mbCol, block int
	}
	gByKey := map[rowKey]task297PretrellisRow{}
	lByKey := map[rowKey]task297PretrellisRow{}
	keys := []rowKey{}
	for _, r := range gRows {
		k := rowKey{r.MBRow, r.MBCol, r.Block}
		gByKey[k] = r
		keys = append(keys, k)
	}
	for _, r := range lRows {
		k := rowKey{r.MBRow, r.MBCol, r.Block}
		if _, present := lByKey[k]; !present {
			lByKey[k] = r
		}
	}
	// keys may contain duplicates if both gRows had them; dedupe.
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].mbRow != keys[j].mbRow {
			return keys[i].mbRow < keys[j].mbRow
		}
		if keys[i].mbCol != keys[j].mbCol {
			return keys[i].mbCol < keys[j].mbCol
		}
		return keys[i].block < keys[j].block
	})
	uniq := keys[:0]
	var prev rowKey
	firstSeen := false
	for _, k := range keys {
		if !firstSeen || k != prev {
			uniq = append(uniq, k)
			prev = k
			firstSeen = true
		}
	}
	keys = uniq

	// Bisect: find first divergent (mbRow, mbCol, block, scan_pos)
	// scanning raster MB order and within each block scan_pos 0..15.
	type divergence struct {
		key      rowKey
		scanPos  int
		layer    string // "coeff" or "qcoeff" or "eob" or "zbin_extra" or "zbin_oq"
		govVal   int
		libVal   int
		govCoeff [16]int16
		libCoeff [16]int16
		govQ     [16]int16
		libQ     [16]int16
		govEob   int
		libEob   int
		govZE    int
		libZE    int
		govZOQ   int
		libZOQ   int
	}
	var divs []divergence

	for _, k := range keys {
		g, gok := gByKey[k]
		l, lok := lByKey[k]
		if !gok || !lok {
			continue
		}
		// Layer priority: coeff > qcoeff > dqcoeff > eob > zbin meta.
		for i := 0; i < 16; i++ {
			if g.Coeff[i] != l.Coeff[i] {
				divs = append(divs, divergence{
					key: k, scanPos: i, layer: "coeff",
					govVal: int(g.Coeff[i]), libVal: int(l.Coeff[i]),
					govCoeff: g.Coeff, libCoeff: l.Coeff,
					govQ: g.QCoeff, libQ: l.QCoeff,
					govEob: g.EOB, libEob: l.EOB,
					govZE: g.ZbinExtra, libZE: l.ZbinExtra,
					govZOQ: g.ZbinOQ, libZOQ: l.ZbinOQ,
				})
				goto nextKey
			}
		}
		for i := 0; i < 16; i++ {
			if g.QCoeff[i] != l.QCoeff[i] {
				divs = append(divs, divergence{
					key: k, scanPos: i, layer: "qcoeff",
					govVal: int(g.QCoeff[i]), libVal: int(l.QCoeff[i]),
					govCoeff: g.Coeff, libCoeff: l.Coeff,
					govQ: g.QCoeff, libQ: l.QCoeff,
					govEob: g.EOB, libEob: l.EOB,
					govZE: g.ZbinExtra, libZE: l.ZbinExtra,
					govZOQ: g.ZbinOQ, libZOQ: l.ZbinOQ,
				})
				goto nextKey
			}
		}
		if g.EOB != l.EOB {
			divs = append(divs, divergence{
				key: k, scanPos: -1, layer: "eob",
				govVal: g.EOB, libVal: l.EOB,
				govCoeff: g.Coeff, libCoeff: l.Coeff,
				govQ: g.QCoeff, libQ: l.QCoeff,
				govEob: g.EOB, libEob: l.EOB,
				govZE: g.ZbinExtra, libZE: l.ZbinExtra,
				govZOQ: g.ZbinOQ, libZOQ: l.ZbinOQ,
			})
		}
	nextKey:
	}

	if len(divs) == 0 {
		t.Logf("task297 seed=%s frame1: ZERO divergent pre-trellis UV rows across %d shared (mb_row,mb_col,block) triples — divergence is downstream of pre-trellis quantize (trellis or coding context)", seedHash, len(keys))
		return
	}

	t.Logf("task297 seed=%s frame1 total_divergent_blocks=%d (first 12 below)", seedHash, len(divs))
	limit := len(divs)
	if limit > 12 {
		limit = 12
	}
	for i := 0; i < limit; i++ {
		d := divs[i]
		t.Logf("  div[%02d] mb_row=%d mb_col=%d block=%d scan_pos=%d layer=%s govVal=%d libVal=%d eob(gov/lib)=%d/%d zbin_extra(gov/lib)=%d/%d zbin_oq(gov/lib)=%d/%d",
			i, d.key.mbRow, d.key.mbCol, d.key.block, d.scanPos, d.layer,
			d.govVal, d.libVal, d.govEob, d.libEob,
			d.govZE, d.libZE, d.govZOQ, d.libZOQ)
		t.Logf("    coeff gov=%v", coeffSliceFmt(d.govCoeff))
		t.Logf("    coeff lib=%v", coeffSliceFmt(d.libCoeff))
		t.Logf("    qcoeff gov=%v", coeffSliceFmt(d.govQ))
		t.Logf("    qcoeff lib=%v", coeffSliceFmt(d.libQ))
	}

	first := divs[0]
	t.Logf("task297 seed=%s FIRST_DIVERGENCE mb_row=%d mb_col=%d block=%d scan_pos=%d layer=%s gov=%d lib=%d",
		seedHash, first.key.mbRow, first.key.mbCol, first.key.block,
		first.scanPos, first.layer, first.govVal, first.libVal)
}

type task297PretrellisRow struct {
	FrameIndex uint64    `json:"frame_index"`
	MBRow      int       `json:"mb_row"`
	MBCol      int       `json:"mb_col"`
	Block      int       `json:"block"`
	EOB        int       `json:"eob"`
	Coeff      [16]int16 `json:"coeff"`
	QCoeff     [16]int16 `json:"qcoeff"`
	DQCoeff    [16]int16 `json:"dqcoeff"`
	ZbinExtra  int       `json:"zbin_extra"`
	ZbinOQ     int       `json:"zbin_oq"`
}

func task297ParsePretrellisUVRows(buf []byte, wantFrameIndex uint64) []task297PretrellisRow {
	out := []task297PretrellisRow{}
	for _, line := range bytes.Split(buf, []byte("\n")) {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		// Quick type filter.
		if !bytes.Contains(line, []byte(`"type":"pretrellis_uv_qcoeff"`)) {
			continue
		}
		var r struct {
			Type       string  `json:"type"`
			FrameIndex uint64  `json:"frame_index"`
			MBRow      int     `json:"mb_row"`
			MBCol      int     `json:"mb_col"`
			Block      int     `json:"block"`
			EOB        int     `json:"eob"`
			Coeff      []int16 `json:"coeff"`
			QCoeff     []int16 `json:"qcoeff"`
			DQCoeff    []int16 `json:"dqcoeff"`
			ZbinExtra  int     `json:"zbin_extra"`
			ZbinOQ     int     `json:"zbin_oq"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.FrameIndex != wantFrameIndex {
			continue
		}
		row := task297PretrellisRow{
			FrameIndex: r.FrameIndex,
			MBRow:      r.MBRow,
			MBCol:      r.MBCol,
			Block:      r.Block,
			EOB:        r.EOB,
			ZbinExtra:  r.ZbinExtra,
			ZbinOQ:     r.ZbinOQ,
		}
		copy(row.Coeff[:], r.Coeff)
		copy(row.QCoeff[:], r.QCoeff)
		copy(row.DQCoeff[:], r.DQCoeff)
		out = append(out, row)
	}
	return out
}

func coeffSliceFmt(c [16]int16) string {
	parts := make([]string, 16)
	for i, v := range c {
		parts[i] = strconv.Itoa(int(v))
	}
	return "[" + joinStr(parts, ",") + "]"
}

func joinStr(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	n := len(sep) * (len(parts) - 1)
	for _, s := range parts {
		n += len(s)
	}
	b := make([]byte, 0, n)
	b = append(b, parts[0]...)
	for _, s := range parts[1:] {
		b = append(b, sep...)
		b = append(b, s...)
	}
	return string(b)
}
