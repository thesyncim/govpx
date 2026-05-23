//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8PretrellisUVParity compares govpx and libvpx pre-trellis UV block
// traces for the 1280x720 ARNR fixtures and reports the first frame-1
// coefficient divergence in raster order.
func TestVP8PretrellisUVParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run pre-trellis UV parity")
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
			targetKbps: 700,
		},
	}

	for _, c := range cohorts {
		c := c
		t.Run(c.name, func(t *testing.T) {
			runVP8PretrellisUVParity(t, vpxencOracle, c.seedHash, c.opts, c.targetKbps, c.extraArgs)
		})
	}
}

func runVP8PretrellisUVParity(t *testing.T, vpxencOracle string, seedHash string, opts EncoderOptions, targetKbps int, extraArgs []string) {
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

	libvpxTrace, diag, err := vp8test.VpxencVP8OracleTraceI420(
		encoderValidationI420Bytes(t, sources),
		vp8OracleTraceConfig(
			vpxencOracle,
			opts,
			len(sources),
			targetKbps,
			[]string{"GOVPX_ORACLE_PRETRELLIS_UV=1"},
			extraArgs,
		),
	)
	if err != nil {
		t.Logf("vpxenc-oracle output:\n%s", diag)
		t.Skipf("vpxenc-oracle failed: %v", err)
	}

	t.Logf("pretrellis_uv seed=%s govpx_trace_bytes=%d libvpx_trace_bytes=%d",
		seedHash, govpxTraceBuf.Len(), len(libvpxTrace))

	gRows := parsePretrellisUVRows(govpxTraceBuf.Bytes(), 1)
	lRows := parsePretrellisUVRows(libvpxTrace, 1)
	t.Logf("pretrellis_uv seed=%s frame1 govpx_rows=%d libvpx_rows=%d",
		seedHash, len(gRows), len(lRows))

	// Group by (mb_row, mb_col, block).
	type rowKey struct {
		mbRow, mbCol, block int
	}
	gByKey := map[rowKey]pretrellisUVTraceRow{}
	lByKey := map[rowKey]pretrellisUVTraceRow{}
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
		t.Logf("pretrellis_uv seed=%s frame1: ZERO divergent pre-trellis UV rows across %d shared (mb_row,mb_col,block) triples; divergence is downstream of pre-trellis quantize (trellis or coding context)", seedHash, len(keys))
		return
	}

	t.Logf("pretrellis_uv seed=%s frame1 total_divergent_blocks=%d (first 12 below)", seedHash, len(divs))
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
	t.Logf("pretrellis_uv seed=%s FIRST_DIVERGENCE mb_row=%d mb_col=%d block=%d scan_pos=%d layer=%s gov=%d lib=%d",
		seedHash, first.key.mbRow, first.key.mbCol, first.key.block,
		first.scanPos, first.layer, first.govVal, first.libVal)
}

type pretrellisUVTraceRow struct {
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

func parsePretrellisUVRows(buf []byte, wantFrameIndex uint64) []pretrellisUVTraceRow {
	out := []pretrellisUVTraceRow{}
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
		row := pretrellisUVTraceRow{
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
