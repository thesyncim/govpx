//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8ChromaOptimizeBlockParity compares govpx and libvpx post-trellis
// chroma block traces for the 1280x720 BestQuality ARNR fixture and reports
// the first frame-1 coefficient divergence in raster order.
func TestVP8ChromaOptimizeBlockParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run chroma optimize_b parity")
	}
	requireOracleTraceBuild(t)
	vpxencOracle := vp8test.VpxencOracle(t)

	cohorts := []struct {
		name       string
		seedHash   string
		opts       EncoderOptions
		extraArgs  []string
		targetKbps int
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
			targetKbps: 700,
		},
	}

	for _, c := range cohorts {
		c := c
		t.Run(c.name, func(t *testing.T) {
			runVP8ChromaOptimizeBlockParity(t, vpxencOracle, c.seedHash, c.opts, c.targetKbps, c.extraArgs)
		})
	}
}

func runVP8ChromaOptimizeBlockParity(t *testing.T, vpxencOracle string, seedHash string, opts EncoderOptions, targetKbps int, extraArgs []string) {
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

	libvpxTrace, diag, err := coracle.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8OracleTraceConfig(
			vpxencOracle,
			opts,
			len(sources),
			targetKbps,
			[]string{"GOVPX_ORACLE_CHROMA_OPTIMIZE_B=1"},
			extraArgs,
		),
	)
	if err != nil {
		t.Logf("vpxenc-oracle output:\n%s", diag)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}

	t.Logf("chroma_optimize_b seed=%s govpx_trace_bytes=%d libvpx_trace_bytes=%d",
		seedHash, govpxTraceBuf.Len(), len(libvpxTrace))

	gRows := parseChromaOptimizeBRows(govpxTraceBuf.Bytes(), 1)
	lRows := parseChromaOptimizeBRows(libvpxTrace, 1)
	t.Logf("chroma_optimize_b seed=%s frame1 govpx_rows=%d libvpx_rows=%d",
		seedHash, len(gRows), len(lRows))

	type rowKey struct {
		mbRow, mbCol, block int
	}
	gByKey := map[rowKey]chromaOptimizeBTraceRow{}
	lByKey := map[rowKey]chromaOptimizeBTraceRow{}
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
		t.Logf("chroma_optimize_b seed=%s frame1: ZERO divergent chroma-optimize_b rows across %d shared (mb_row,mb_col,block) triples; post-trellis chroma qcoeff is now byte-identical (no chroma trellis divergence remains)", seedHash, len(keys))
		return
	}

	t.Logf("chroma_optimize_b seed=%s frame1 total_divergent_blocks=%d dc_keep_gov=%d dc_keep_lib=%d ac_keep_gov=%d ac_keep_lib=%d (first 12 below)",
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
	t.Logf("chroma_optimize_b seed=%s FIRST_DIVERGENCE mb_row=%d mb_col=%d block=%d scan_pos=%d layer=%s gov=%d lib=%d",
		seedHash, first.key.mbRow, first.key.mbCol, first.key.block,
		first.scanPos, first.layer, first.govVal, first.libVal)
}

type chromaOptimizeBTraceRow struct {
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

func parseChromaOptimizeBRows(buf []byte, wantFrameIndex uint64) []chromaOptimizeBTraceRow {
	out := []chromaOptimizeBTraceRow{}
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
		row := chromaOptimizeBTraceRow{
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
