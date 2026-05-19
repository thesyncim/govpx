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

// TestVP8Task316ChromaOptimizeBBisect runs the BestARNR audit with the
// per-UV-block POST-trellis qcoeff oracle tracer enabled on both sides
// (task #316 hook splices into vp8_encode_inter16x16 right after
// optimize_mb on libvpx; mirrored on the govpx side in
// reconstructMacroblockUVCoefficients after each
// quantizeEncodedBlockWithRDZbinAndActivity call), captures the per-block
// qcoeff / dqcoeff / dequant / coeff snapshots, and bisects the first
// post-trellis (mb_row, mb_col, block, scan_pos) at which the two diverge
// on frame 1.
//
// This is the chroma trellis counterpart to task #297's pre-trellis bisect.
// Per task #314 evidence the ARNR pin-hold residual is in the
// post-encode chroma trellis (2241/3600 MBs diverge on encode-side
// qcoeff for frame 1; 2115 chroma-only; 85% DC-only ±1 keep/drop
// splits). The pre-trellis snapshots from #297 already showed the
// pre-trellis (post-regular-quantize) qcoeff was identical across the
// 2115 chroma blocks for the seeds that matter; this bisect surfaces
// which scan_pos optimize_b flipped one way on libvpx and a different
// way on govpx — the actual Viterbi divergence position.
//
// Method:
//   - govpx side: SetOracleTraceChromaOptimizeBDump(true) + a buffer
//     writer; encode the BestARNR cohort frames.
//   - libvpx side: re-encode via vpxenc-oracle with
//     GOVPX_ORACLE_CHROMA_OPTIMIZE_B=1 + GOVPX_ORACLE_TRACE_OUT.
//   - Group rows by (mb_row, mb_col, block) on frame 1, iterate in
//     raster MB × block order × scan-pos, log the first divergent triple
//     and up to 12 sample rows for review.
//   - When pairing with task #297's pre-trellis rows, callers can
//     identify the exact ±1 DC drop direction (govpx-keep / libvpx-drop
//     or the reverse) per (mb_row, mb_col, block).
//
// Gated on GOVPX_WITH_ORACLE=1.
func TestVP8Task316ChromaOptimizeBBisect(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #316 chroma optimize_b bisect")
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
			dumpGovpxOut:  "/tmp/316-govpx-best.jsonl",
			dumpLibvpxOut: "/tmp/316-libvpx-best.jsonl",
		},
	}

	for _, c := range cohorts {
		c := c
		t.Run(c.name, func(t *testing.T) {
			runTask316ChromaOptimizeBBisect(t, vpxencOracle, c.seedHash, c.opts, c.targetKbps, c.extraArgs, c.dumpGovpxOut, c.dumpLibvpxOut)
		})
	}
}

func runTask316ChromaOptimizeBBisect(t *testing.T, vpxencOracle string, seedHash string, opts EncoderOptions, targetKbps int, extraArgs []string, govpxOutPath string, libvpxOutPath string) {
	t.Helper()

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	// govpx side: capture chroma-optimize_b post-trellis trace
	govpxTraceBuf := &bytes.Buffer{}
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	enc.SetOracleTraceWriter(govpxTraceBuf)
	enc.SetOracleTraceChromaOptimizeBDump(true)
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
		"GOVPX_ORACLE_CHROMA_OPTIMIZE_B=1",
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
	t.Logf("task316 seed=%s govpx_trace=%s libvpx_trace=%s",
		seedHash, govpxOutPath, libvpxOutPath)

	gRows := task316ParseChromaOptimizeBRows(govpxTraceBuf.Bytes(), 1)
	lRows := task316ParseChromaOptimizeBRows(libvpxTrace, 1)
	t.Logf("task316 seed=%s frame1 govpx_rows=%d libvpx_rows=%d",
		seedHash, len(gRows), len(lRows))

	type rowKey struct {
		mbRow, mbCol, block int
	}
	gByKey := map[rowKey]task316ChromaOptimizeBRow{}
	lByKey := map[rowKey]task316ChromaOptimizeBRow{}
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

	type divergence struct {
		key     rowKey
		scanPos int
		layer   string // "qcoeff" or "dqcoeff" or "eob" or "rdmult" or "rddiv" or "dequant" or "coeff" or "intra"
		govVal  int
		libVal  int
		govQ    [16]int16
		libQ    [16]int16
		govDQ   [16]int16
		libDQ   [16]int16
		govC    [16]int16
		libC    [16]int16
		govDeq  [16]int16
		libDeq  [16]int16
		govEob  int
		libEob  int
		govRDM  int
		libRDM  int
		govRDD  int
		libRDD  int
	}
	var divs []divergence

	// Bin counters for DC vs AC divergence direction.
	var dcKeepGov, dcKeepLib, acKeepGov, acKeepLib int

	for _, k := range keys {
		g, gok := gByKey[k]
		l, lok := lByKey[k]
		if !gok || !lok {
			continue
		}
		// Per-position qcoeff divergence with directional bin counters.
		hadDiv := false
		for i := 0; i < 16; i++ {
			if g.QCoeff[i] != l.QCoeff[i] {
				if !hadDiv {
					divs = append(divs, divergence{
						key: k, scanPos: i, layer: "qcoeff",
						govVal: int(g.QCoeff[i]), libVal: int(l.QCoeff[i]),
						govQ: g.QCoeff, libQ: l.QCoeff,
						govDQ: g.DQCoeff, libDQ: l.DQCoeff,
						govC: g.Coeff, libC: l.Coeff,
						govDeq: g.Dequant, libDeq: l.Dequant,
						govEob: g.EOB, libEob: l.EOB,
						govRDM: g.RDMult, libRDM: l.RDMult,
						govRDD: g.RDDiv, libRDD: l.RDDiv,
					})
					hadDiv = true
				}
				// DC = scan position 0 (libvpx uses default_zig_zag1d
				// which has rc[0]=0). The bisect bins on direction:
				// "gov keeps non-zero where lib drops to 0" vs reverse.
				gv := int(g.QCoeff[i])
				lv := int(l.QCoeff[i])
				if i == 0 {
					if gv != 0 && lv == 0 {
						dcKeepGov++
					} else if gv == 0 && lv != 0 {
						dcKeepLib++
					}
				} else {
					if gv != 0 && lv == 0 {
						acKeepGov++
					} else if gv == 0 && lv != 0 {
						acKeepLib++
					}
				}
			}
		}
		if hadDiv {
			continue
		}
		if g.EOB != l.EOB {
			divs = append(divs, divergence{
				key: k, scanPos: -1, layer: "eob",
				govVal: g.EOB, libVal: l.EOB,
				govQ: g.QCoeff, libQ: l.QCoeff,
				govDQ: g.DQCoeff, libDQ: l.DQCoeff,
				govC: g.Coeff, libC: l.Coeff,
				govDeq: g.Dequant, libDeq: l.Dequant,
				govEob: g.EOB, libEob: l.EOB,
				govRDM: g.RDMult, libRDM: l.RDMult,
				govRDD: g.RDDiv, libRDD: l.RDDiv,
			})
			continue
		}
		// rdmult / rddiv parity check (per-MB scalar).
		if g.RDMult != l.RDMult {
			divs = append(divs, divergence{
				key: k, scanPos: -1, layer: "rdmult",
				govVal: g.RDMult, libVal: l.RDMult,
				govQ: g.QCoeff, libQ: l.QCoeff,
				govDQ: g.DQCoeff, libDQ: l.DQCoeff,
				govC: g.Coeff, libC: l.Coeff,
				govDeq: g.Dequant, libDeq: l.Dequant,
				govEob: g.EOB, libEob: l.EOB,
				govRDM: g.RDMult, libRDM: l.RDMult,
				govRDD: g.RDDiv, libRDD: l.RDDiv,
			})
			continue
		}
		if g.RDDiv != l.RDDiv {
			divs = append(divs, divergence{
				key: k, scanPos: -1, layer: "rddiv",
				govVal: g.RDDiv, libVal: l.RDDiv,
				govQ: g.QCoeff, libQ: l.QCoeff,
				govDQ: g.DQCoeff, libDQ: l.DQCoeff,
				govC: g.Coeff, libC: l.Coeff,
				govDeq: g.Dequant, libDeq: l.Dequant,
				govEob: g.EOB, libEob: l.EOB,
				govRDM: g.RDMult, libRDM: l.RDMult,
				govRDD: g.RDDiv, libRDD: l.RDDiv,
			})
			continue
		}
	}

	if len(divs) == 0 {
		t.Logf("task316 seed=%s frame1: ZERO divergent chroma-optimize_b rows across %d shared (mb_row,mb_col,block) triples — post-trellis chroma qcoeff is now byte-identical (no chroma trellis divergence remains)", seedHash, len(keys))
		return
	}

	t.Logf("task316 seed=%s frame1 total_divergent_blocks=%d dc_keep_gov=%d dc_keep_lib=%d ac_keep_gov=%d ac_keep_lib=%d (first 12 below)",
		seedHash, len(divs), dcKeepGov, dcKeepLib, acKeepGov, acKeepLib)
	limit := len(divs)
	if limit > 12 {
		limit = 12
	}
	for i := 0; i < limit; i++ {
		d := divs[i]
		t.Logf("  div[%02d] mb_row=%d mb_col=%d block=%d scan_pos=%d layer=%s govVal=%d libVal=%d eob(gov/lib)=%d/%d rdmult(gov/lib)=%d/%d rddiv(gov/lib)=%d/%d",
			i, d.key.mbRow, d.key.mbCol, d.key.block, d.scanPos, d.layer,
			d.govVal, d.libVal, d.govEob, d.libEob,
			d.govRDM, d.libRDM, d.govRDD, d.libRDD)
		t.Logf("    coeff gov=%v", coeffSliceFmt(d.govC))
		t.Logf("    coeff lib=%v", coeffSliceFmt(d.libC))
		t.Logf("    qcoeff gov=%v", coeffSliceFmt(d.govQ))
		t.Logf("    qcoeff lib=%v", coeffSliceFmt(d.libQ))
		t.Logf("    dequant gov=%v", coeffSliceFmt(d.govDeq))
		t.Logf("    dequant lib=%v", coeffSliceFmt(d.libDeq))
	}

	first := divs[0]
	t.Logf("task316 seed=%s FIRST_DIVERGENCE mb_row=%d mb_col=%d block=%d scan_pos=%d layer=%s gov=%d lib=%d",
		seedHash, first.key.mbRow, first.key.mbCol, first.key.block,
		first.scanPos, first.layer, first.govVal, first.libVal)
}

type task316ChromaOptimizeBRow struct {
	FrameIndex uint64    `json:"frame_index"`
	MBRow      int       `json:"mb_row"`
	MBCol      int       `json:"mb_col"`
	Block      int       `json:"block"`
	EOB        int       `json:"eob"`
	RDMult     int       `json:"rdmult"`
	RDDiv      int       `json:"rddiv"`
	Intra      int       `json:"intra"`
	QCoeff     [16]int16 `json:"qcoeff"`
	DQCoeff    [16]int16 `json:"dqcoeff"`
	Dequant    [16]int16 `json:"dequant"`
	Coeff      [16]int16 `json:"coeff"`
}

func task316ParseChromaOptimizeBRows(buf []byte, wantFrameIndex uint64) []task316ChromaOptimizeBRow {
	out := []task316ChromaOptimizeBRow{}
	for _, line := range bytes.Split(buf, []byte("\n")) {
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		if !bytes.Contains(line, []byte(`"type":"chroma_optimize_b"`)) {
			continue
		}
		var r struct {
			Type       string  `json:"type"`
			FrameIndex uint64  `json:"frame_index"`
			MBRow      int     `json:"mb_row"`
			MBCol      int     `json:"mb_col"`
			Block      int     `json:"block"`
			EOB        int     `json:"eob"`
			RDMult     int     `json:"rdmult"`
			RDDiv      int     `json:"rddiv"`
			Intra      int     `json:"intra"`
			QCoeff     []int16 `json:"qcoeff"`
			DQCoeff    []int16 `json:"dqcoeff"`
			Dequant    []int16 `json:"dequant"`
			Coeff      []int16 `json:"coeff"`
		}
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		if r.FrameIndex != wantFrameIndex {
			continue
		}
		row := task316ChromaOptimizeBRow{
			FrameIndex: r.FrameIndex,
			MBRow:      r.MBRow,
			MBCol:      r.MBCol,
			Block:      r.Block,
			EOB:        r.EOB,
			RDMult:     r.RDMult,
			RDDiv:      r.RDDiv,
			Intra:      r.Intra,
		}
		copy(row.QCoeff[:], r.QCoeff)
		copy(row.DQCoeff[:], r.DQCoeff)
		copy(row.Dequant[:], r.Dequant)
		copy(row.Coeff[:], r.Coeff)
		out = append(out, row)
	}
	return out
}
