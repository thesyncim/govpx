package govpx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestDiagPass2AllocTrace is a diagnostic helper that runs libvpx pass-1 on a
// fixture, then walks govpx's twoPassState forward emitting per-frame
// allocator state (kfGroupBitsRemaining, gfGroupBits, kfGroupErrorLeft,
// gfGroupErrorLeft, modErr, errorLeft, bitsLeft, target). Compares to libvpx
// rate trace. Run with:
//
//	GOVPX_WITH_ORACLE=1 GOVPX_DIAG_PASS2=1 go test -run TestDiagPass2Alloc \
//	  -v -count=1 -timeout 600s
func TestDiagPass2AllocTrace(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" || os.Getenv("GOVPX_DIAG_PASS2") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 and GOVPX_DIAG_PASS2=1 to run pass-2 diagnostic trace")
	}
	vpxenc := findVpxenc(t)

	// park-joy-90p fixture.
	path := filepath.Join("internal", "coracle", "build", "test-data", "encoder", "park_joy_90p_8_420.y4m")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("park-joy-90p missing at %s", path)
	}
	clip, ok := readExternalEncoderClip(t, path, 12)
	if !ok {
		t.Skip("park-joy-90p clip not loadable")
	}

	const targetKbps = 350
	opts := EncoderOptions{
		Width:             clip.width,
		Height:            clip.height,
		FPS:               clip.fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      56,
		KeyFrameInterval:  60,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           0,
	}

	dir := t.TempDir()
	yuvPath := filepath.Join(dir, "park-joy.yuv")
	fpfPath := filepath.Join(dir, "park-joy.fpf")
	ivf1Path := filepath.Join(dir, "park-joy-pass1.ivf")
	writeEncoderValidationI420(t, yuvPath, clip.frames)

	args := []string{
		"--codec=vp8",
		"--ivf",
		"--quiet",
		"--good",
		"--cpu-used=0",
		"--passes=2",
		"--pass=1",
		"--fpf=" + fpfPath,
		"--end-usage=vbr",
		"--target-bitrate=" + fmt.Sprint(targetKbps),
		"--min-q=4",
		"--max-q=56",
		"--kf-min-dist=60",
		"--kf-max-dist=60",
		"--i420",
		"--width=" + fmt.Sprint(opts.Width),
		"--height=" + fmt.Sprint(opts.Height),
		"--timebase=1/" + fmt.Sprint(opts.FPS),
		"--fps=" + fmt.Sprint(opts.FPS) + "/1",
		"--limit=" + fmt.Sprint(len(clip.frames)),
		"--output=" + ivf1Path,
		yuvPath,
	}
	cmd := exec.Command(vpxenc, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("vpxenc pass 1: %v\n%s", err, out)
	}

	fpfData, err := os.ReadFile(fpfPath)
	if err != nil {
		t.Fatalf("ReadFile %s: %v", fpfPath, err)
	}
	stats := parseLibvpxFirstPassStats(t, fpfData)

	// Walk govpx's twoPassState forward, emitting per-frame state.
	// The per-frame budget is target_bitrate * 1000 / fps.
	bitsPerFrame := int(int64(targetKbps) * 1000 / int64(opts.FPS))
	var st twoPassState
	st.configure(stats, bitsPerFrame, 0, 0, 0)

	t.Logf("len(stats)=%d (excl total), bitsPerFrame=%d, totalBits=%d, errorLeft=%.4f, totalStats.SSIMWeightedPredErr=%.4f totalStats.Count=%.0f",
		len(st.stats), bitsPerFrame, st.bitsLeft, st.errorLeft, st.totalStats.SSIMWeightedPredErr, st.totalStats.Count)
	st.configureFrameDims(opts.Width, opts.Height)
	t.Logf("frame dims: %dx%d, gfIntraErrMin=%.1f, kfIntraErrMin=%.1f, lastInterQ=%d",
		st.frameWidth, st.frameHeight, st.gfIntraErrMin, st.kfIntraErrMinForFrame(), st.lastInterQ)
	gfuBoost := computeGFUBoost(st.stats, 0, len(st.stats), true, st.gfIntraErrMin)
	kfBoost, decay := computeKFBoost(st.stats, 0, len(st.stats), st.kfIntraErrMinForFrame())
	t.Logf("computed gfu_boost=%d, kf_boost(raw)=%d, decay=%.4f", gfuBoost, kfBoost, decay)
	for i := 1; i <= len(st.stats); i++ {
		idx := i
		if idx >= len(st.stats) {
			break
		}
		f := st.stats[idx]
		t.Logf("  stat[%d] intra=%.1f coded=%.1f pcntInter=%.4f pcntMotion=%.4f mvIO=%.4f mvrAbs=%.2f mvcAbs=%.2f", idx, f.IntraError, f.CodedError, f.PcntInter, f.PcntMotion, f.MVInOutCount, f.MVrAbs, f.MVcAbs)
	}

	for i := uint64(0); i < uint64(len(st.stats)); i++ {
		modErr := st.modifiedError(st.stats[i])
		bitsLeftIn := st.bitsLeft
		errorLeftIn := st.errorLeft
		kfGB := st.kfGroupBitsRemaining
		kfGE := st.kfGroupErrorLeft
		gfGB := st.gfGroupBits
		gfGE := st.gfGroupErrorLeft
		ftk := st.framesToKeyRemaining
		ftgf := st.framesTillGFUpdate
		fsg := st.framesSinceGolden

		isKF := i == 0 // first frame is KF
		target := st.frameTargetBits(i, isKF, bitsPerFrame)

		t.Logf("frame %02d kf=%v target=%d modErr=%.2f bitsLeft=%d errorLeft=%.2f kfGB(in)=%d kfGE(in)=%.2f kfGB(out)=%d kfGE(out)=%.2f gfGB(in)=%d gfGE(in)=%.2f gfGB(out)=%d gfGE(out)=%.2f ftk=%d ftgf=%d fsg=%d altExtra=%d gfRefresh=%d",
			i, isKF, target, modErr, bitsLeftIn, errorLeftIn,
			kfGB, kfGE, st.kfGroupBitsRemaining, st.kfGroupErrorLeft,
			gfGB, gfGE, st.gfGroupBits, st.gfGroupErrorLeft,
			ftk, ftgf, fsg, st.altExtraBits, st.gfRefreshTarget)

		// Simulate finishFrame at "actual==target" for tracing only.
		st.finishFrame(target)
	}
}
