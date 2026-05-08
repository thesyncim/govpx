package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

// TestR9_7_CoefProbsAdlerSpeed8 dumps coef_probs_adler per frame for govpx vs
// libvpx at realtime cpu_used=8 to localize coef-prob update divergences.
func TestR9_7_CoefProbsAdlerSpeed8(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run R9-7 coef-probs trace")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 16
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "r9-7-coef-probs", opts, targetKbps, sources, []string{"--end-usage=cbr"})

	govpxFrames := r97ExtractFrameRows(t, govpxTrace)
	libvpxFrames := r97ExtractFrameRows(t, libvpxTrace)
	max := len(govpxFrames)
	if len(libvpxFrames) < max {
		max = len(libvpxFrames)
	}
	fmt.Println("idx | coef_g       | coef_l       | match | y_match | uv_match | piG | piL | sizeG | sizeL")
	mismatches := 0
	for i := 0; i < max; i++ {
		g := govpxFrames[i]
		l := libvpxFrames[i]
		gC := uint32Field(g["coef_probs_adler"])
		lC := uint32Field(l["coef_probs_adler"])
		match := gC == lC
		if !match {
			mismatches++
		}
		yMatch := uint32Field(g["y_adler32"]) == uint32Field(l["y_adler32"])
		uvMatch := uint32Field(g["u_adler32"]) == uint32Field(l["u_adler32"]) &&
			uint32Field(g["v_adler32"]) == uint32Field(l["v_adler32"])
		fmt.Printf("%3d | %12d | %12d | %5v | %7v | %8v | %3v | %3v | %5v | %5v\n",
			i, gC, lC, match, yMatch, uvMatch, g["prob_intra_coded"], l["prob_intra_coded"],
			g["size_bytes"], l["size_bytes"])
	}
	if max < len(govpxFrames) || max < len(libvpxFrames) {
		fmt.Printf("trace lengths: govpx=%d, libvpx=%d\n", len(govpxFrames), len(libvpxFrames))
	}
	if mismatches != 0 {
		t.Logf("%d/%d frames have divergent coef_probs_adler", mismatches, max)
	}
	// also dump raw JSON of first divergent govpx + libvpx rows
	for i := 0; i < max; i++ {
		gC := uint32Field(govpxFrames[i]["coef_probs_adler"])
		lC := uint32Field(libvpxFrames[i]["coef_probs_adler"])
		if gC != lC {
			gjson, _ := json.MarshalIndent(govpxFrames[i], "", "  ")
			ljson, _ := json.MarshalIndent(libvpxFrames[i], "", "  ")
			fmt.Printf("=== first divergent frame %d ===\n--- govpx ---\n%s\n--- libvpx ---\n%s\n", i, gjson, ljson)
			break
		}
	}
}

// TestR9_7_CoefProbsAdlerGoodCpu5 runs good-quality cpu_used=5 (max for good)
// to test high-Speed coef-probs without realtime auto-select timer noise.
func TestR9_7_CoefProbsAdlerGoodCpu5(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run R9-7 coef-probs trace")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 16
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "r9-7-coef-probs-good5", opts, targetKbps, sources, []string{"--end-usage=vbr"})

	govpxFrames := r97ExtractFrameRows(t, govpxTrace)
	libvpxFrames := r97ExtractFrameRows(t, libvpxTrace)
	max := len(govpxFrames)
	if len(libvpxFrames) < max {
		max = len(libvpxFrames)
	}
	mismatches := 0
	for i := 0; i < max; i++ {
		gC := uint32Field(govpxFrames[i]["coef_probs_adler"])
		lC := uint32Field(libvpxFrames[i]["coef_probs_adler"])
		if gC != lC {
			mismatches++
		}
	}
	fmt.Printf("[good cpu5 (high Speed without auto-select)] %d/%d frames have divergent coef_probs_adler\n", mismatches, max)
	if mismatches > 0 {
		// Show first divergence row.
		for i := 0; i < max; i++ {
			gC := uint32Field(govpxFrames[i]["coef_probs_adler"])
			lC := uint32Field(libvpxFrames[i]["coef_probs_adler"])
			if gC != lC {
				fmt.Printf("first divergent frame %d: coef govpx=%d libvpx=%d, y_match=%v\n",
					i, gC, lC,
					uint32Field(govpxFrames[i]["y_adler32"]) == uint32Field(libvpxFrames[i]["y_adler32"]))
				break
			}
		}
	}
}

// TestR9_7_CoefProbsLongCpu5 runs many frames to confirm coef_probs only ever
// diverge AFTER y_adler diverges (i.e., not due to coef-prob update logic).
func TestR9_7_CoefProbsLongCpu5(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run R9-7 coef-probs trace")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 32
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "r9-7-coef-probs-long", opts, targetKbps, sources, []string{"--end-usage=vbr"})

	govpxFrames := r97ExtractFrameRows(t, govpxTrace)
	libvpxFrames := r97ExtractFrameRows(t, libvpxTrace)
	max := len(govpxFrames)
	if len(libvpxFrames) < max {
		max = len(libvpxFrames)
	}
	firstCoefDiverge := -1
	firstYDiverge := -1
	for i := 0; i < max; i++ {
		gC := uint32Field(govpxFrames[i]["coef_probs_adler"])
		lC := uint32Field(libvpxFrames[i]["coef_probs_adler"])
		gY := uint32Field(govpxFrames[i]["y_adler32"])
		lY := uint32Field(libvpxFrames[i]["y_adler32"])
		if firstCoefDiverge < 0 && gC != lC {
			firstCoefDiverge = i
		}
		if firstYDiverge < 0 && gY != lY {
			firstYDiverge = i
		}
	}
	fmt.Printf("[long good cpu5 %d frames] firstYDiverge=%d firstCoefDiverge=%d\n", frames, firstYDiverge, firstCoefDiverge)
	if firstCoefDiverge >= 0 && firstYDiverge >= 0 && firstCoefDiverge < firstYDiverge {
		t.Errorf("coef_probs_adler diverged at frame %d BEFORE y_adler32 diverged at frame %d - this is a coef-prob update logic bug", firstCoefDiverge, firstYDiverge)
	}
}

// TestR9_7_DumpKeyFrameCoefProbsNoER dumps govpx's keyframe coef_probs in
// the regular (non-error-resilient) path for control.
func TestR9_7_DumpKeyFrameCoefProbsNoER(t *testing.T) {
	if os.Getenv("GOVPX_R9_7_DUMP") != "1" {
		t.Skip("set GOVPX_R9_7_DUMP=1 to dump keyframe coef_probs")
	}
	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
	}
	src := encoderValidationPanningFrame(width, height, 0)
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatal(err)
	}
	pkt := make([]byte, opts.Width*opts.Height*3)
	if _, err := enc.EncodeInto(pkt, src, 0, 1, 0); err != nil {
		t.Fatal(err)
	}
	for block := 0; block < 4; block++ {
		for band := 0; band < 8; band++ {
			for ctx := 0; ctx < 3; ctx++ {
				for node := 0; node < 11; node++ {
					p := enc.coefProbs[block][band][ctx][node]
					fmt.Printf("%d %d %d %d %d\n", block, band, ctx, node, p)
				}
			}
		}
	}
}

// TestR9_7_DumpKeyFrameCoefProbsERGovpx dumps govpx's keyframe coef_probs in
// error-resilient mode for direct comparison with the libvpx oracle.
func TestR9_7_DumpKeyFrameCoefProbsERGovpx(t *testing.T) {
	if os.Getenv("GOVPX_R9_7_DUMP") != "1" {
		t.Skip("set GOVPX_R9_7_DUMP=1 to dump keyframe coef_probs")
	}
	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
		ErrorResilient:    true,
	}
	src := encoderValidationPanningFrame(width, height, 0)
	enc, err := NewVP8Encoder(opts)
	if err != nil {
		t.Fatal(err)
	}
	pkt := make([]byte, opts.Width*opts.Height*3)
	if _, err := enc.EncodeInto(pkt, src, 0, 1, 0); err != nil {
		t.Fatal(err)
	}
	var nonZero int
	fmt.Println("# block band ctx node prob")
	for block := 0; block < 4; block++ {
		for band := 0; band < 8; band++ {
			for ctx := 0; ctx < 3; ctx++ {
				for node := 0; node < 11; node++ {
					p := enc.coefProbs[block][band][ctx][node]
					if p != 0 {
						nonZero++
					}
					fmt.Printf("%d %d %d %d %d\n", block, band, ctx, node, p)
				}
			}
		}
	}
	fmt.Printf("# nonzero=%d total=%d\n", nonZero, 4*8*3*11)
}

// TestR9_7_CoefProbsAdlerErrorResilient runs cpu_used=8 with error_resilient
// to exercise the independent_coef_context_savings / VPX_ERROR_RESILIENT
// branch in vp8_update_coef_probs.
func TestR9_7_CoefProbsAdlerErrorResilient(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run R9-7 coef-probs trace")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 12
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           5,
		KeyFrameInterval:  999,
		ErrorResilient:    true,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "r9-7-coef-probs-er", opts, targetKbps, sources, []string{"--end-usage=cbr", "--error-resilient=1"})

	govpxFrames := r97ExtractFrameRows(t, govpxTrace)
	libvpxFrames := r97ExtractFrameRows(t, libvpxTrace)
	max := len(govpxFrames)
	if len(libvpxFrames) < max {
		max = len(libvpxFrames)
	}
	firstCoefDiverge := -1
	firstYDiverge := -1
	for i := 0; i < max; i++ {
		gC := uint32Field(govpxFrames[i]["coef_probs_adler"])
		lC := uint32Field(libvpxFrames[i]["coef_probs_adler"])
		gY := uint32Field(govpxFrames[i]["y_adler32"])
		lY := uint32Field(libvpxFrames[i]["y_adler32"])
		if firstCoefDiverge < 0 && gC != lC {
			firstCoefDiverge = i
		}
		if firstYDiverge < 0 && gY != lY {
			firstYDiverge = i
		}
	}
	fmt.Printf("[error_resilient cpu5 %d frames] firstYDiverge=%d firstCoefDiverge=%d\n", frames, firstYDiverge, firstCoefDiverge)
	for i := 0; i < max; i++ {
		g := govpxFrames[i]
		l := libvpxFrames[i]
		gC := uint32Field(g["coef_probs_adler"])
		lC := uint32Field(l["coef_probs_adler"])
		gYM := uint32Field(g["ymode_probs_adler"])
		lYM := uint32Field(l["ymode_probs_adler"])
		gUV := uint32Field(g["uv_mode_probs_adler"])
		lUV := uint32Field(l["uv_mode_probs_adler"])
		gMV := uint32Field(g["mv_probs_adler"])
		lMV := uint32Field(l["mv_probs_adler"])
		fmt.Printf("  frame %2d coef_g=%d coef_l=%d sizeG=%v sizeL=%v (match=%v ymode=%v uv=%v mv=%v y=%v refresh=%v)\n",
			i, gC, lC, g["size_bytes"], l["size_bytes"], gC == lC, gYM == lYM, gUV == lUV, gMV == lMV,
			uint32Field(g["y_adler32"]) == uint32Field(l["y_adler32"]),
			g["refresh_entropy_probs"])
	}
}

// TestR9_7_CoefProbsAdlerSpeed3 runs at good-quality cpu_used=3 for control.
func TestR9_7_CoefProbsAdlerSpeed3(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run R9-7 coef-probs trace")
	}
	vpxencOracle := findVpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 700
		frames     = 8
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "r9-7-coef-probs-cpu3", opts, targetKbps, sources, []string{"--end-usage=vbr"})

	govpxFrames := r97ExtractFrameRows(t, govpxTrace)
	libvpxFrames := r97ExtractFrameRows(t, libvpxTrace)
	max := len(govpxFrames)
	if len(libvpxFrames) < max {
		max = len(libvpxFrames)
	}
	mismatches := 0
	for i := 0; i < max; i++ {
		g := govpxFrames[i]
		l := libvpxFrames[i]
		gC := uint32Field(g["coef_probs_adler"])
		lC := uint32Field(l["coef_probs_adler"])
		if gC != lC {
			mismatches++
		}
	}
	fmt.Printf("[cpu3 control] %d/%d frames have divergent coef_probs_adler\n", mismatches, max)
}

func r97ExtractFrameRows(t *testing.T, trace []byte) []map[string]any {
	t.Helper()
	var rows []map[string]any
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 1<<20), 1<<25)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			t.Fatalf("trace row not valid JSON: %v: %s", err, scan.Bytes())
		}
		if typ, _ := row["type"].(string); typ == "frame" {
			rows = append(rows, row)
		}
	}
	if err := scan.Err(); err != nil {
		t.Fatalf("scan trace: %v", err)
	}
	return rows
}

func uint32Field(v any) uint32 {
	switch x := v.(type) {
	case float64:
		return uint32(x)
	case int:
		return uint32(x)
	case int64:
		return uint32(x)
	case json.Number:
		i, _ := x.Int64()
		return uint32(i)
	default:
		return 0
	}
}
