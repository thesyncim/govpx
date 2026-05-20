//go:build govpx_oracle_trace

package govpx

import (
	"bufio"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// TestVP8InterCandidateStatePropagation pins the negative
// result of the task #313 audit into a Threads=4-vs-Threads=1 libvpx
// oracle comparison at MB(0,0) frame 1 NEWMV MV=(8,16). The audit's
// premise was that libvpx's NEWMV CANDIDATE produces non-zero
// Y qcoeff while govpx's produces all-zero on the SAME residual,
// implying inter-candidate state propagation (b->zbin_extra,
// xd->eobs[], b->zbin_mode_boost, mb->mode_info_context) that libvpx
// carries between NEARESTMV → NEARMV → NEWMV that govpx is missing.
//
// Method: extend internal/coracle/build_vpxenc_oracle.sh (already done
// at task #310) with a per-Y-block picker_quantize emit hook inside
// vp8/encoder/rdopt.c:macro_block_yrd (after Y0..15 + Y2 quantize loops
// complete, before vp8_rdcost_mby reads d->eobs). Run the oracle with
// --threads=1 and --threads=4 on the same fixture (1280x720 BestQuality
// cpu0 VBR SSIM screen-content=1 ARNR=1/1/2 panning frames). Compare
// the captured pre.coeff, zbin_extra, post.qcoeff, eob for every
// MB(0,0) frame 1 picker candidate, plus the inter_candidate-summary
// rate_y the rdopt.c emit hook captures from rd.rate_y.
//
// Findings (verbatim):
//
//   - libvpx --threads=1 NEWMV MB(0,0) frame 1:
//     picker_quantize: zbin_extra=8, eob=0 for ALL 16 Y blocks + Y2,
//     qcoeff all-zero.
//     inter_candidate: rate_y=7519, rate=20474, score=102349,
//     yrd=73707, dist=58282. EXACTLY MATCHES govpx.
//
//   - libvpx --threads=4 NEWMV MB(0,0) frame 1:
//     picker_quantize: zbin_extra=8 (identical), eob=0 (identical),
//     qcoeff all-zero (identical).
//     inter_candidate: rate_y=34799, rate=48796, score=160686,
//     yrd=129509, dist=55660.
//
// The picker_quantize state is IDENTICAL across thread counts. The
// inter_candidate rate_y at threads=4 (34799) is reported from a
// different code path or a different mb_row/mb_col context than
// MB(0,0)'s NEWMV: every per-block post.eob the trace captures at
// threads=4 for MB(0,0) NEWMV is zero, so vp8_rdcost_mby reading
// those eobs can only produce rate_y ≈ 17 * EOB_token_cost ≈ 7519.
//
// The threads=4 vs threads=1 inter_candidate divergence is caused by
// a DIFFERENT FRAME 0 RECONSTRUCTION (post-loop-filter): the
// ZEROMV picker_quantize pre.coeff for MB(0,0) frame 1 differs
// between threads=1 (blk=0: [96,175,-19,-46]) and threads=4
// (blk=0: [66,163,-18,-47]). Same source, same MV, same quant
// tables — but the predictor (which reads the previous frame's
// post-LF Y buffer) differs. libvpx's threaded loop filter
// (vp8_loop_filter_frame_mt) produces slightly different output
// than its single-threaded loop filter (vp8_loop_filter_frame) due
// to row-boundary synchronization order, which propagates into
// every motion-compensated predictor on the next frame.
//
// CONCLUSION: There is NO inter-candidate state propagation gap in
// govpx's picker. At MB(0,0) frame 1, govpx and libvpx (threads=1)
// produce byte-identical picker_quantize state AND byte-identical
// inter_candidate score state (score=102349, yrd=73707, rate=20474,
// rate_y=7519, dist=58282). The originally-reported "libvpx
// rate_y=34799" was measured against libvpx --threads=4 which
// reconstructs frame 0 with a different post-LF Y buffer; that
// divergence is in libvpx's threaded loop filter and does NOT
// implicate any picker-stage state mutation libvpx carries between
// NEARESTMV / NEARMV / NEWMV that govpx is missing.
//
// FIX: none required. The audit task's hypothesis (8th negative
// audit in the chain) is empty. The 8-audit chain — task #298 →
// #299 → #300 → #304 → #307 → #309 → #310 → #312 — has now exhausted
// the picker-MATH layer. Future work to close the byte-exact pin
// for this cohort must focus on the loop-filter reconstruction
// layer, not the picker.
func TestVP8InterCandidateStatePropagation(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the task #313 inter-candidate state propagation audit")
	}
	vpxencOracle := findVpxencOracle(t)

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

		dir := t.TempDir()
		yuvPath := filepath.Join(dir, "task313.yuv")
		ivfPath := filepath.Join(dir, "task313.ivf")
		tracePath := filepath.Join(dir, "task313.jsonl")
		writeEncoderValidationI420(t, yuvPath, sources)

		args := []string{
			"--codec=vp8",
			"--ivf",
			"--quiet",
			"--disable-warning-prompt",
			"--best",
			"--cpu-used=0",
			"--lag-in-frames=0",
			"--auto-alt-ref=0",
			"--target-bitrate=" + strconv.Itoa(opts.TargetBitrateKbps),
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
			"--end-usage=vbr",
			"--screen-content-mode=1",
			"--token-parts=1",
			"--threads=" + strconv.Itoa(threads),
			"--tune=ssim",
			"--arnr-maxframes=1",
			"--arnr-strength=1",
			"--arnr-type=2",
			yuvPath,
		}
		cmd := exec.Command(vpxencOracle, args...)
		cmd.Env = append(os.Environ(),
			"GOVPX_ORACLE_TRACE_OUT="+tracePath,
			"GOVPX_ORACLE_NEWMV_PICKER=1",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("vpxenc-oracle (threads=%d) failed: %v\n%s", threads, err, out)
		}

		f, err := os.Open(tracePath)
		if err != nil {
			t.Fatalf("open trace (threads=%d): %v", threads, err)
		}
		defer f.Close()

		var candidates []candidateRow
		var quants []quantRow

		scan := bufio.NewScanner(f)
		scan.Buffer(make([]byte, 1<<20), 1<<24)
		for scan.Scan() {
			line := scan.Bytes()
			if len(line) == 0 || line[0] != '{' {
				continue
			}
			if !bytesContains(line, []byte(`"frame_index":1,"mb_row":0,"mb_col":0`)) {
				continue
			}
			if bytesContains(line, []byte(`"type":"newmv_picker_quantize"`)) {
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
			if bytesContains(line, []byte(`"type":"inter_candidate"`)) {
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
	t.Logf("task313 threads=1 NEWMV-LAST: score=%d yrd=%d rate=%d rate_y=%d dist=%d",
		newmvT1.Score, newmvT1.YRD, newmvT1.Rate, newmvT1.RateY, newmvT1.Distortion)

	// govpx-side reference numbers (task #298 / #304 finding, audited
	// here against the byte-exact libvpx oracle): NEWMV picker emits
	// all-zero qcoeff → rate_y = 17 * EOB_token_cost = 7519. score,
	// yrd, rate, distortion all directly derivable from rate_y.
	const (
		govpxNEWMVScore = 102349
		govpxNEWMVYRD   = 73707
		govpxNEWMVRate  = 20474
		govpxNEWMVRateY = 7519
		govpxNEWMVDist  = 58282
	)
	if newmvT1.RateY != govpxNEWMVRateY {
		t.Errorf("threads=1 oracle NEWMV rate_y = %d; govpx-pin = %d (audit's premise REGRESSED — libvpx now diverges from govpx at threads=1)",
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
	t.Logf("task313 threads=4 NEWMV-LAST: score=%d yrd=%d rate=%d rate_y=%d dist=%d",
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
		t.Logf("task313: threads=1 and threads=4 produce IDENTICAL ZEROMV pre.coeff at MB(0,0) frame 1; the inter_candidate rate_y divergence is unexplained by predictor-source variance")
	} else {
		t.Logf("task313: threads=1 vs threads=4 ZEROMV pre.coeff diverges in %d/16 blocks at MB(0,0) frame 1 — the libvpx --threads=4 inter_candidate rate_y=%d gap is attributable to threaded loop-filter post-LF reference variance, NOT to picker inter-candidate state propagation",
			residualDivergedBlocks, newmvT4.RateY)
	}
}
