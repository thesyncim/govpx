//go:build govpx_oracle_trace

package govpx

import (
	"os"
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8enc "github.com/thesyncim/govpx/internal/vp8/encoder"
)

// TestVP8NewMVPickerResidualQuantization inspects the BestQuality ARNR cohort
// at frame 1 MB(0,0) for the NEWMV MV=(8,16) candidate. It verifies the source,
// predictor, residual, FDCT, and zbin state that feed picker-side Y
// quantization, then logs the per-block EOB shape used by the RD estimate.
func TestVP8NewMVPickerResidualQuantization(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run the NEWMV picker residual quantization check")
	}

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
		Threads:           4,
		ARNRMaxFrames:     1,
		ARNRStrength:      1,
		ARNRType:          2,
	}

	sources := make([]Image, 2)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(opts.Width, opts.Height, i)
	}

	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	buf := make([]byte, opts.Width*opts.Height*4+4096)

	// Encode frame 0 (KF) so e.lastRef is populated with the post-LF
	// reconstruction. Same as the BestARNR pin's frame 0.
	if _, err := enc.EncodeInto(buf, sources[0], 0, 1, 0); err != nil {
		t.Fatalf("EncodeInto frame 0: %v", err)
	}

	// MB(0,0). MV=(8, 16) in 1/8-pel units → integer-pel
	// (mv_row&7|mv_col&7 == 0): predictor = lastRef[mb_y + mv_row>>3,
	// mb_x + mv_col>>3] window of 16x16. mb_y=mb_x=0, mv_row>>3 = 1,
	// mv_col>>3 = 2.
	mbRow := 0
	mbCol := 0
	mvRow := int16(8)
	mvCol := int16(16)

	lastY := enc.lastRef.Img.Y
	lastStride := enc.lastRef.Img.YStride
	predBaseY := mbRow*16 + int(mvRow>>3) // 1
	predBaseX := mbCol*16 + int(mvCol>>3) // 2

	// Dump the predictor 16x16 (integer-pel copy).
	var predictor [16 * 16]byte
	for y := range 16 {
		copy(predictor[y*16:y*16+16], lastY[(predBaseY+y)*lastStride+predBaseX:(predBaseY+y)*lastStride+predBaseX+16])
	}

	// Dump source 16x16 at MB(0,0).
	srcImage := sources[1]
	srcStride := srcImage.YStride
	var src16 [16 * 16]byte
	for y := range 16 {
		copy(src16[y*16:y*16+16], srcImage.Y[y*srcStride:y*srcStride+16])
	}

	// Compute residual = src - pred.
	var residual [16 * 16]int16
	residualNonZero := 0
	for y := range 16 {
		for x := range 16 {
			r := int16(src16[y*16+x]) - int16(predictor[y*16+x])
			residual[y*16+x] = r
			if r != 0 {
				residualNonZero++
			}
		}
	}

	// Gather into 16 4x4 blocks in scan order matching
	// GatherMacroblockYResiduals4x4's layout.
	var yResiduals [16 * 16]int16
	for block := range 16 {
		blockRow := (block >> 2) * 4
		blockCol := (block & 3) * 4
		for r := range 4 {
			for c := range 4 {
				yResiduals[block*16+r*4+c] = residual[(blockRow+r)*16+blockCol+c]
			}
		}
	}

	// FDCT.
	var yDcts [16 * 16]int16
	vp8enc.ForwardDCT4x4Batch(yResiduals[:], yDcts[:], 16)

	// Look up quantizer for segment 0 at the actual frame 1 picker Q.
	// After encoding frame 0 (KF), e.rc.currentQuantizer holds the KF Q
	// (83); for frame 1 the rate trace shows Q=106. Use 106 to match the
	// actual picker run for this fixture.
	const frame1QIndex = 106
	qIndex := frame1QIndex
	zbinOverQuant := enc.rc.currentZbinOverQuant
	zbinModeBoost := 4 // MV_ZBIN_BOOST for NEWMV
	// Activity adjustment at MB(0,0) feeds the picker zbin threshold.
	actZbinAdj := 0
	if enc.activityMapValid {
		if adj, ok := enc.tunedZbinAdjustment(mbRow, mbCol); ok {
			actZbinAdj = adj
		}
	}

	var quants [vp8common.MaxMBSegments]vp8enc.MacroblockQuant
	quantDeltas := libvpxFrameQuantDeltas(qIndex, opts.ScreenContentMode)
	if err := vp8enc.InitSegmentMacroblockQuants(qIndex, quantDeltas, vp8enc.SegmentationConfig{}, &quants); err != nil {
		t.Fatalf("InitSegmentMacroblockQuants: %v", err)
	}
	quant := &quants[0]

	// Quantize block 0 only (for logging).
	var qcoeff0 [16]int16
	var dqcoeff0 [16]int16
	yDcts0 := (*[16]int16)(yDcts[0:16])
	yDcts0Saved := *yDcts0
	// Mirror buildPredictedMacroblockCoefficientsWork's pre-quant step:
	// for non-4x4 luma blocks, dct[0] is zeroed before quant; the DC is
	// folded into the Y2 second-order block via y2Input[block] = dct[0].
	// We DON'T zero dct[0] here yet — we want to log the raw FDCT output
	// including DC.
	eob0 := quantizeBlockWithZbinAndActivity(yDcts0, &quant.Y1, zbinOverQuant, zbinModeBoost, actZbinAdj, &qcoeff0, &dqcoeff0)

	// Also quantize block 0 with dct[0]=0 (the actual picker path for
	// non-4x4 luma) to see if AC alone is enough to produce non-zero
	// qcoeff.
	yDcts0NoDC := yDcts0Saved
	yDcts0NoDC[0] = 0
	var qcoeff0NoDC [16]int16
	var dqcoeff0NoDC [16]int16
	eob0NoDC := quantizeBlockWithZbinAndActivity(&yDcts0NoDC, &quant.Y1, zbinOverQuant, zbinModeBoost, actZbinAdj, &qcoeff0NoDC, &dqcoeff0NoDC)

	// Count total non-zero qcoeff across all 16 Y blocks (DC zeroed).
	var totalNonZero int
	var blockEOBSum int
	var perBlockEOB [16]int
	for block := range 16 {
		dctBlock := (*[16]int16)(yDcts[block*16 : block*16+16])
		saved := *dctBlock
		// Mirror non-4x4 luma: zero DC.
		(*dctBlock)[0] = 0
		var q [16]int16
		var dq [16]int16
		eob := quantizeBlockWithZbinAndActivity(dctBlock, &quant.Y1, zbinOverQuant, zbinModeBoost, actZbinAdj, &q, &dq)
		perBlockEOB[block] = eob
		blockEOBSum += eob
		for i := range 16 {
			if q[i] != 0 {
				totalNonZero++
			}
		}
		// Restore so we don't pollute later inspection.
		*dctBlock = saved
	}

	t.Logf("=== NEWMV picker Y residual quantization ===")
	t.Logf("opts: 1280x720 BestARNR cohort, frame 1 MB(0,0) NEWMV MV=(%d,%d) ref=LAST_FRAME", mvRow, mvCol)
	t.Logf("phase: (mv_row|mv_col)&7 = %d → integer-pel (vp8_copy_mem16x16 path)", (int(mvRow)|int(mvCol))&7)
	t.Logf("predictor base in lastRef.Y: row=%d col=%d stride=%d border=%d", predBaseY, predBaseX, lastStride, enc.lastRef.Img.YBorder)
	t.Logf("qIndex=%d zbinOverQuant=%d zbinModeBoost=%d actZbinAdj=%d", qIndex, zbinOverQuant, zbinModeBoost, actZbinAdj)
	t.Logf("activityMapValid=%v", enc.activityMapValid)
	t.Logf("residual nonzero count = %d / 256", residualNonZero)
	t.Logf("predictor row 0 = % d", predictor[:16])
	t.Logf("source    row 0 = % d", src16[:16])
	t.Logf("residual  row 0 = % d", residual[:16])
	t.Logf("predictor row 1 = % d", predictor[16:32])
	t.Logf("source    row 1 = % d", src16[16:32])
	t.Logf("residual  row 1 = % d", residual[16:32])
	t.Logf("predictor row 8 = % d", predictor[8*16:9*16])
	t.Logf("source    row 8 = % d", src16[8*16:9*16])
	t.Logf("residual  row 8 = % d", residual[8*16:9*16])
	t.Logf("FDCT block 0    = % d", yDcts0Saved)
	t.Logf("FDCT block 0 (no DC) = % d", yDcts0NoDC)
	t.Logf("qcoeff block 0 (raw, DC kept) eob=%d = % d", eob0, qcoeff0)
	t.Logf("qcoeff block 0 (DC zeroed)    eob=%d = % d", eob0NoDC, qcoeff0NoDC)
	t.Logf("total non-zero qcoeff (after DC-zero) across 16 Y blocks = %d", totalNonZero)
	t.Logf("EOB per Y block (DC-zeroed) = %v (sum=%d)", perBlockEOB, blockEOBSum)

	// Dump per-block FDCT outputs and max abs AC coeff so we can see which
	// (if any) block has |coeff| > zbin threshold.
	for block := range 16 {
		dct := yDcts[block*16 : block*16+16]
		maxAbs := 0
		maxIdx := -1
		// Skip DC.
		for i := 1; i < 16; i++ {
			a := int(dct[i])
			if a < 0 {
				a = -a
			}
			if a > maxAbs {
				maxAbs = a
				maxIdx = i
			}
		}
		t.Logf("block %d FDCT = % d (max |AC|=%d at scan rc=%d)", block, dct, maxAbs, maxIdx)
	}

	// Also compute zbin threshold for AC slot 1 at q=106.
	// quant.Y1.Zbin[1], ZbinBoost[0], Dequant[1].
	t.Logf("quant.Y1.Zbin[0..3] = %v", quant.Y1.Zbin[:4])
	t.Logf("quant.Y1.ZbinBoost[0..3] = %v", quant.Y1.ZbinBoost[:4])
	t.Logf("quant.Y1.Dequant[0..3] = %v", quant.Y1.Dequant[:4])
	t.Logf("quant.Y1.Round[0..3] = %v", quant.Y1.Round[:4])
	zbinExtra := (int(quant.Y1.Dequant[1]) * (zbinOverQuant + zbinModeBoost + actZbinAdj)) >> 7
	t.Logf("zbin_extra = (%d * (%d + %d + %d)) >> 7 = %d", quant.Y1.Dequant[1], zbinOverQuant, zbinModeBoost, actZbinAdj, zbinExtra)
	t.Logf("zbin_threshold (pos=1, rc=1, zeroRun=0) = Zbin[1]+ZbinBoost[0]+zbin_extra = %d + %d + %d = %d", quant.Y1.Zbin[1], quant.Y1.ZbinBoost[0], zbinExtra, int(quant.Y1.Zbin[1])+int(quant.Y1.ZbinBoost[0])+zbinExtra)

	// Compute SSE = sum of squared residuals.
	sse := 0
	predSum := 0
	srcSum := 0
	for i := range 256 {
		r := int(residual[i])
		sse += r * r
		predSum += int(predictor[i])
		srcSum += int(src16[i])
	}
	mean := (srcSum - predSum) * (srcSum - predSum) / 256
	variance := sse - mean
	threshold := (int(quant.Y1.Dequant[1]) * int(quant.Y1.Dequant[1])) >> 4
	t.Logf("Y SSE = %d  variance = %d  predSum=%d srcSum=%d", sse, variance, predSum, srcSum)
	t.Logf("encode_breakout threshold = (dequant[1]^2)>>4 = (%d^2)>>4 = %d", quant.Y1.Dequant[1], threshold)
	q2dc := int(quant.Y2.Dequant[0])
	q2dcSquared := q2dc * q2dc >> 4
	t.Logf("Y2 dequant[0] = %d → q2dc^2>>4 = %d", q2dc, q2dcSquared)
	t.Logf("sse(=%d) - variance(=%d) = %d  q2dc^2>>4 = %d  threshold(=%d)", sse, variance, sse-variance, q2dcSquared, threshold)
	t.Logf("breakout test: sse(=%d) < threshold(=%d)? %v", sse, threshold, sse < threshold)
	if sse < threshold {
		t.Logf("breakout condition 1: sse(=%d)-var(=%d)=%d < q2dc^2>>4(=%d)? %v", sse, variance, sse-variance, q2dcSquared, sse-variance < q2dcSquared)
		t.Logf("breakout condition 2: sse/2(=%d) > var(=%d) AND sse-var(=%d) < 64? %v",
			sse/2, variance, sse-variance, sse/2 > variance && sse-variance < 64)
	}
}
