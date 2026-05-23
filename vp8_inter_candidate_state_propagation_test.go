//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp8test"
)

// TestVP8InterCandidateStatePropagation compares libvpx VP8 picker traces at
// threads=1 and threads=4 for the same panning fixture. The threads=1 trace
// pins the NEWMV candidate state that govpx is expected to match; the
// threads=4 trace documents that the remaining byte difference comes from
// libvpx's threaded loop-filter reconstruction feeding a different predictor,
// not from missing state propagation between NEARESTMV, NEARMV, and NEWMV.
func TestVP8InterCandidateStatePropagation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP8 inter-candidate state propagation parity")
	}
	vpxencOracle := vp8test.VpxencOracle(t)

	type candidateRow struct {
		Mode       string
		RefFrame   int
		MVRow      int
		MVCol      int
		Score      int
		YRD        int
		Rate       int
		RateY      int
		Distortion int
	}
	type quantRow struct {
		Mode      string
		Block     int
		ZbinExtra int
		EOB       int
		PreCoeff  []int
		QCoeff    []int
	}

	runOracle := func(threads int) ([]candidateRow, []quantRow) {
		opts := EncoderOptions{
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
			Threads:           threads,
			ARNRMaxFrames:     1,
			ARNRStrength:      1,
			ARNRType:          2,
		}
		sources := make([]Image, 2)
		for i := range sources {
			sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
		}

		cfg := vp8test.VpxencVP8Config{
			BinaryPath:           vpxencOracle,
			Width:                opts.Width,
			Height:               opts.Height,
			Frames:               len(sources),
			Deadline:             libvpxOracleDeadline(opts.Deadline),
			DisableWarningPrompt: true,
			CPUUsed:              opts.CpuUsed,
			LagInFrames:          0,
			AutoAltRef:           false,
			TargetBitrateKbps:    opts.TargetBitrateKbps,
			MinQ:                 opts.MinQuantizer,
			MaxQ:                 opts.MaxQuantizer,
			Timebase:             libvpxOracleTimebaseArg(opts),
			FPS:                  libvpxOracleFPSArg(opts),
			KeyFrameDistSet:      true,
			KeyFrameMinDist:      999,
			KeyFrameMaxDist:      999,
			ExtraEnv:             []string{"GOVPX_ORACLE_NEWMV_PICKER=1"},
			ExtraArgs: []string{
				"--end-usage=vbr",
				"--screen-content-mode=1",
				"--token-parts=1",
				"--threads=" + strconv.Itoa(threads),
				"--tune=ssim",
				"--arnr-maxframes=1",
				"--arnr-strength=1",
				"--arnr-type=2",
			},
		}
		trace, diag, err := vp8test.VpxencVP8OracleTraceI420(
			encoderValidationI420Bytes(t, sources), cfg)
		if err != nil {
			t.Fatalf("vpxenc-oracle (threads=%d) failed: %v\n%s", threads, err, diag)
		}

		var candidates []candidateRow
		var quants []quantRow

		scan := bufio.NewScanner(bytes.NewReader(trace))
		scan.Buffer(make([]byte, 1<<20), 1<<24)
		for scan.Scan() {
			line := scan.Bytes()
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			if !bytes.Contains(line, []byte(`"frame_index":1,"mb_row":0,"mb_col":0`)) {
				continue
			}
			if bytes.Contains(line, []byte(`"type":"newmv_picker_quantize"`)) {
				var r struct {
					Block     int    `json:"block"`
					Mode      string `json:"mode"`
					RefFrame  int    `json:"ref_frame"`
					MV        []int  `json:"mv"`
					ZbinExtra int    `json:"zbin_extra"`
					EOB       int    `json:"eob"`
					Pre       struct {
						Coeff []int `json:"coeff"`
					} `json:"pre"`
					Post struct {
						QCoeff []int `json:"qcoeff"`
					} `json:"post"`
				}
				if err := json.Unmarshal(line, &r); err == nil {
					quants = append(quants, quantRow{
						Mode:      r.Mode,
						Block:     r.Block,
						ZbinExtra: r.ZbinExtra,
						EOB:       r.EOB,
						PreCoeff:  r.Pre.Coeff,
						QCoeff:    r.Post.QCoeff,
					})
				}
				continue
			}
			if bytes.Contains(line, []byte(`"type":"inter_candidate"`)) {
				var r struct {
					Mode       string `json:"mode"`
					RefFrame   string `json:"ref_frame"`
					MVRow      int    `json:"mv_row"`
					MVCol      int    `json:"mv_col"`
					Score      int    `json:"score"`
					YRD        int    `json:"yrd"`
					Rate       int    `json:"rate"`
					RateY      int    `json:"rate_y"`
					Distortion int    `json:"distortion"`
				}
				if err := json.Unmarshal(line, &r); err == nil {
					candidates = append(candidates, candidateRow{
						Mode:       r.Mode,
						MVRow:      r.MVRow,
						MVCol:      r.MVCol,
						Score:      r.Score,
						YRD:        r.YRD,
						Rate:       r.Rate,
						RateY:      r.RateY,
						Distortion: r.Distortion,
					})
				}
				continue
			}
		}
		if err := scan.Err(); err != nil {
			t.Fatalf("scan trace (threads=%d): %v", threads, err)
		}
		return candidates, quants
	}

	// Capture threads=1 baseline. Locks the picker MATH/state at the
	// cohort's known-MATCHING configuration.
	candT1, quantT1 := runOracle(1)
	if len(candT1) == 0 || len(quantT1) == 0 {
		t.Fatalf("no MB(0,0) frame 1 oracle rows captured under threads=1; oracle hook may be missing")
	}

	// Locate NEWMV-LAST candidate at MB(0,0) frame 1.
	var newmvT1 candidateRow
	for _, c := range candT1 {
		if c.Mode == "NEWMV" && c.MVRow == 8 && c.MVCol == 16 {
			newmvT1 = c
			break
		}
	}
	if newmvT1.Mode == "" {
		t.Fatalf("threads=1 oracle did not test NEWMV-LAST at MB(0,0) frame 1")
	}
	t.Logf("threads=1 NEWMV-LAST: score=%d yrd=%d rate=%d rate_y=%d dist=%d",
		newmvT1.Score, newmvT1.YRD, newmvT1.Rate, newmvT1.RateY, newmvT1.Distortion)

	// govpx-side reference numbers for this fixture: NEWMV picker emits
	// all-zero qcoeff, so rate_y = 17 * EOB_token_cost = 7519. score,
	// yrd, rate, and distortion are directly derivable from rate_y.
	const (
		govpxNEWMVScore = 102349
		govpxNEWMVYRD   = 73707
		govpxNEWMVRate  = 20474
		govpxNEWMVRateY = 7519
		govpxNEWMVDist  = 58282
	)
	if newmvT1.RateY != govpxNEWMVRateY {
		t.Errorf("threads=1 oracle NEWMV rate_y = %d; govpx-pin = %d; libvpx now diverges from govpx at threads=1",
			newmvT1.RateY, govpxNEWMVRateY)
	}
	if newmvT1.Score != govpxNEWMVScore || newmvT1.YRD != govpxNEWMVYRD ||
		newmvT1.Rate != govpxNEWMVRate || newmvT1.Distortion != govpxNEWMVDist {
		t.Errorf("threads=1 oracle NEWMV mismatch vs govpx-pin: score=(%d vs %d) yrd=(%d vs %d) rate=(%d vs %d) dist=(%d vs %d)",
			newmvT1.Score, govpxNEWMVScore,
			newmvT1.YRD, govpxNEWMVYRD,
			newmvT1.Rate, govpxNEWMVRate,
			newmvT1.Distortion, govpxNEWMVDist)
	}

	// Picker quantize state: every NEWMV block must have eob=0.
	newmvQuantBlocks := 0
	for _, q := range quantT1 {
		if q.Mode != "NEWMV" {
			continue
		}
		newmvQuantBlocks++
		if q.EOB != 0 {
			t.Errorf("threads=1 NEWMV picker_quantize block=%d has eob=%d, want 0 (govpx all-zero-qcoeff pin)", q.Block, q.EOB)
		}
		nz := 0
		for _, v := range q.QCoeff {
			if v != 0 {
				nz++
			}
		}
		if nz != 0 {
			t.Errorf("threads=1 NEWMV picker_quantize block=%d has qcoeff_nonzero=%d, want 0 (govpx all-zero-qcoeff pin)", q.Block, nz)
		}
	}
	if newmvQuantBlocks != 17 {
		t.Errorf("threads=1 NEWMV picker_quantize emitted %d block rows, want 17 (Y0..15 + Y2)", newmvQuantBlocks)
	}

	// Capture threads=4 sentinel. Documents the libvpx threading
	// non-determinism in the loop filter.
	candT4, quantT4 := runOracle(4)
	if len(candT4) == 0 || len(quantT4) == 0 {
		t.Fatalf("no MB(0,0) frame 1 oracle rows captured under threads=4")
	}
	var newmvT4 candidateRow
	for _, c := range candT4 {
		if c.Mode == "NEWMV" && c.MVRow == 8 && c.MVCol == 16 {
			newmvT4 = c
			break
		}
	}
	if newmvT4.Mode == "" {
		t.Fatalf("threads=4 oracle did not test NEWMV-LAST at MB(0,0) frame 1")
	}
	t.Logf("threads=4 NEWMV-LAST: score=%d yrd=%d rate=%d rate_y=%d dist=%d",
		newmvT4.Score, newmvT4.YRD, newmvT4.Rate, newmvT4.RateY, newmvT4.Distortion)

	// Picker quantize state at threads=4 MUST also be all-zero (proving
	// the quantize MATH is identical; the divergence is upstream in
	// the residual, which depends on the predictor source bytes from
	// the previous frame's post-LF reconstruction).
	newmvQuantBlocksT4 := 0
	for _, q := range quantT4 {
		if q.Mode != "NEWMV" {
			continue
		}
		newmvQuantBlocksT4++
		if q.EOB != 0 {
			t.Errorf("threads=4 NEWMV picker_quantize block=%d has eob=%d, want 0 (quantize math invariant)", q.Block, q.EOB)
		}
	}
	if newmvQuantBlocksT4 != 17 {
		t.Errorf("threads=4 NEWMV picker_quantize emitted %d block rows, want 17", newmvQuantBlocksT4)
	}

	// Compare per-block pre.coeff between threads=1 and threads=4 for
	// ZEROMV-LAST (the first inter candidate, predicted from MV=(0,0)).
	// If the post-LF reference frame is byte-identical between thread
	// counts, the residual and FDCT pre.coeff must match too. If they
	// differ, the divergence is in libvpx's threaded loop filter, NOT
	// in picker state mutation.
	zeromvCoeffT1 := map[int][]int{}
	for _, q := range quantT1 {
		if q.Mode == "ZEROMV" && len(q.PreCoeff) > 0 {
			zeromvCoeffT1[q.Block] = q.PreCoeff
		}
	}
	zeromvCoeffT4 := map[int][]int{}
	for _, q := range quantT4 {
		if q.Mode == "ZEROMV" && len(q.PreCoeff) > 0 {
			zeromvCoeffT4[q.Block] = q.PreCoeff
		}
	}
	residualDivergedBlocks := 0
	for block := 0; block < 16; block++ {
		c1 := zeromvCoeffT1[block]
		c4 := zeromvCoeffT4[block]
		if len(c1) != 16 || len(c4) != 16 {
			continue
		}
		for i := 0; i < 16; i++ {
			if c1[i] != c4[i] {
				residualDivergedBlocks++
				break
			}
		}
	}
	if residualDivergedBlocks == 0 {
		t.Logf("threads=1 and threads=4 produce IDENTICAL ZEROMV pre.coeff at MB(0,0) frame 1; the inter_candidate rate_y divergence is unexplained by predictor-source variance")
	} else {
		t.Logf("threads=1 vs threads=4 ZEROMV pre.coeff diverges in %d/16 blocks at MB(0,0) frame 1; the libvpx --threads=4 inter_candidate rate_y=%d gap is attributable to threaded loop-filter post-LF reference variance, not picker inter-candidate state propagation",
			residualDivergedBlocks, newmvT4.RateY)
	}
}
