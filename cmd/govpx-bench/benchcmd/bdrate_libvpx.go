package benchcmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	govpx "github.com/thesyncim/govpx"
	"github.com/thesyncim/govpx/internal/coracle"
)

// encodeBDLibvpxCurve drives the libvpx vpxenc-vp9-frameflags helper
// at every Q in qs with the on-feature flags pulled from opts.Test,
// mapped to libvpx CLI tokens by libvpxVP9FrameFlagsCLIArgs. The
// returned (kbps, PSNR) curve sits on the same Q-derived PSNR proxy
// as the govpx fallback path so BDRate over govpx vs libvpx is
// dominated by the rate gap and not by decoder-frontend artefacts.
//
// A missing helper binary returns ErrVpxencVP9FrameFlagsNotBuilt
// (wrapped) so callers can either run -build-libvpx or t.Skip.
func encodeBDLibvpxCurve(opts BDRateOptions, qs []int) ([]QualityPoint, error) {
	binPath, err := resolveLibvpxVP9FrameFlagsBinary(opts.BuildLibvpx)
	if err != nil {
		return nil, err
	}
	// Resolve the test-flag callback's effect on a fresh options struct
	// once; the resulting govpx VP9EncoderOptions drives the libvpx CLI
	// flag mapping. Width/Height/FPS/Lookahead come from opts so the
	// libvpx run sees the same source dimensions and lookahead window.
	testOpts := govpx.VP9EncoderOptions{
		Width:           opts.Width,
		Height:          opts.Height,
		FPS:             opts.FPS,
		LookaheadFrames: opts.Lookahead,
	}
	if opts.Test != nil {
		opts.Test(&testOpts)
	}
	// Materialise the source corpus once so each Q iteration encodes
	// the same bytes through libvpx (and so the proxy-PSNR fit isn't
	// shifted by source jitter).
	srcFrames := make([]*image.YCbCr, opts.Frames)
	for i := range srcFrames {
		srcFrames[i] = opts.Source(i)
		if srcFrames[i] == nil {
			return nil, fmt.Errorf("Source returned nil at %d", i)
		}
	}
	raw, err := writeI420ToBytes(srcFrames, opts.Width, opts.Height)
	if err != nil {
		return nil, fmt.Errorf("write libvpx I420 input: %w", err)
	}
	pts := make([]QualityPoint, 0, len(qs))
	for _, q := range qs {
		pt, err := encodeLibvpxBDOperatingPoint(binPath, raw, opts, testOpts, q)
		if err != nil {
			return nil, fmt.Errorf("libvpx Q=%d: %w", q, err)
		}
		pts = append(pts, pt)
	}
	return pts, nil
}

// encodeLibvpxBDOperatingPoint runs the vpxenc-vp9-frameflags helper
// once and returns one (kbps, PSNR-proxy) point. The CLI flag list is
// produced by libvpxVP9FrameFlagsCLIArgs so a single source-of-truth
// owns the govpx -> libvpx field mapping.
func encodeLibvpxBDOperatingPoint(binPath string, raw []byte, opts BDRateOptions, testOpts govpx.VP9EncoderOptions, q int) (QualityPoint, error) {
	dir, err := os.MkdirTemp("", "govpx-bdrate-libvpx-*")
	if err != nil {
		return QualityPoint{}, err
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "input.i420")
	outPath := filepath.Join(dir, "output.ivf")
	tracePath := filepath.Join(dir, "trace.jsonl")
	if err := os.WriteFile(inPath, raw, 0o600); err != nil {
		return QualityPoint{}, err
	}
	args := libvpxVP9FrameFlagsCLIArgs(opts, testOpts, q)
	args = append(args,
		"--infile="+inPath,
		"--outfile="+outPath,
		"--trace-out="+tracePath,
		"--width="+strconv.Itoa(opts.Width),
		"--height="+strconv.Itoa(opts.Height),
		"--fps-num="+strconv.Itoa(opts.FPS),
		"--fps-den=1",
		"--frames="+strconv.Itoa(opts.Frames),
	)
	cmd := exec.Command(binPath, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return QualityPoint{}, fmt.Errorf("libvpx run: %w\nargs=%v\nstderr:\n%s",
			err, args, stderr.Bytes())
	}
	trace, err := os.ReadFile(tracePath)
	if err != nil {
		return QualityPoint{}, fmt.Errorf("read trace: %w", err)
	}
	totalBytes, meanQ, visible, err := parseLibvpxBDTrace(trace)
	if err != nil {
		return QualityPoint{}, err
	}
	if visible == 0 {
		return QualityPoint{}, fmt.Errorf("no visible libvpx frames at Q=%d", q)
	}
	kbps := float64(totalBytes) * 8 * float64(opts.FPS) / float64(opts.Frames) / 1000
	if kbps <= 0 {
		return QualityPoint{}, fmt.Errorf("nonpositive libvpx kbps at Q=%d (bytes=%d frames=%d)",
			q, totalBytes, opts.Frames)
	}
	return QualityPoint{Rate: kbps, PSNR: bdRateQIndexPSNRProxy(meanQ)}, nil
}

// libvpxVP9FrameFlagsCLIArgs is the single source of truth that maps
// govpx VP9EncoderOptions feature flags to libvpx vpxenc-vp9-frameflags
// CLI tokens. The cited `// libvpx token: <flag>` comments anchor each
// govpx field to the libvpx CLI flag it drives.
//
// libvpx token reference (per
// internal/coracle/vpxenc_vp9_frameflags.c arg-parser):
//
//	govpx.VP9EncoderOptions.AQMode            -> --aq-mode=N
//	govpx.VP9EncoderOptions.AutoAltRef        -> --auto-alt-ref=N
//	govpx.VP9EncoderOptions.ARNRMaxFrames     -> --arnr-maxframes=N
//	govpx.VP9EncoderOptions.ARNRStrength      -> --arnr-strength=N
//	govpx.VP9EncoderOptions.ARNRType          -> --arnr-type=N
//	govpx.VP9EncoderOptions.LookaheadFrames   -> --lag-in-frames=N
//	govpx.VP9EncoderOptions.AltRefAQ          -> --alt-ref-aq=N
//	govpx.VP9EncoderOptions.FramePeriodicBoost-> --frame-boost=N
//	govpx.VP9EncoderOptions.MinQuantizer      -> --min-q=N
//	govpx.VP9EncoderOptions.MaxQuantizer      -> --max-q=N
//	govpx.VP9EncoderOptions.CQLevel           -> --cq-level=N
//	govpx.VP9EncoderOptions.NoiseSensitivity  -> --noise-sensitivity=N
//	govpx.VP9EncoderOptions.Sharpness         -> --sharpness=N
//	govpx.VP9EncoderOptions.StaticThreshold   -> --static-thresh=N
//	govpx.VP9EncoderOptions.GFCBRBoostPct     -> --gf-cbr-boost=N
//	govpx.VP9EncoderOptions.MaxIntraBitratePct-> --max-intra-rate=N
//	govpx.VP9EncoderOptions.MaxInterBitratePct-> (no libvpx CLI; encoded via --max-bitrate / not mapped here)
//	govpx.VP9EncoderOptions.MinGFInterval     -> --min-gf-interval=N
//	govpx.VP9EncoderOptions.MaxGFInterval     -> --max-gf-interval=N
//	govpx.VP9EncoderOptions.DisableLoopfilter -> --disable-loopfilter=N
//	govpx.RateControl{Q,VBR,CBR,CQ}           -> --end-usage={q,vbr,cbr,cq}
//	(BD-rate harness always pins end-usage=q + cq-level=Q so the libvpx
//	curve sits on the same constant-quality anchor as govpx.)
//
// Feature flags not exercised by the current per-feature BD-rate gates
// (segmentation, ROI map, temporal layers, render size, color tags,
// runtime drop schedule, etc.) are intentionally not mapped here; add
// new fields with a `// libvpx token:` citation when those gates land.
func libvpxVP9FrameFlagsCLIArgs(opts BDRateOptions, t govpx.VP9EncoderOptions, q int) []string {
	args := []string{
		// end-usage=q matches govpx's RateControlQ so libvpx's qindex
		// selection is constant-quality anchored at cq-level=Q.
		"--end-usage=q",
		// libvpx token: --cq-level
		"--cq-level=" + strconv.Itoa(q),
		// libvpx token: --min-q (4 is libvpx's good-quality floor)
		"--min-q=4",
		// libvpx token: --max-q
		"--max-q=63",
		// libvpx token: --tune
		"--tune=psnr",
		// Keep deadline at good-quality so feature toggles (AltRef,
		// ARNR, TPL, AQ) actually exercise the high-quality VP9 path
		// libvpx uses for its BD-rate measurements. The govpx harness
		// uses lookahead-aware encoding so the cpu-used floor here
		// matches govpx's BD-rate runs (which target a similar
		// quality/speed budget).
		"--deadline=good",
		"--cpu-used=2",
		// libvpx token: --kf-min-dist / --kf-max-dist (keyframe cadence
		// matches the govpx BD-rate run's open-GOP default).
		"--kf-min-dist=0",
		"--kf-max-dist=128",
		// libvpx token: --row-mt / --tile-columns / --tile-rows
		// (single-tile single-row encoder so bitstream is the
		// deterministic baseline, matching the govpx default).
		"--row-mt=0",
		"--tile-columns=0",
		"--tile-rows=0",
		// libvpx token: --frame-parallel (off so per-frame entropy
		// counts are applied — matches govpx BD-rate's default).
		"--frame-parallel=0",
	}
	// libvpx token: --target-bitrate (unused in end-usage=q, but
	// vpxenc-vp9-frameflags requires a positive default).
	target := t.TargetBitrateKbps
	if target <= 0 {
		target = 1000
	}
	args = append(args, "--target-bitrate="+strconv.Itoa(target))
	// libvpx token: --lag-in-frames
	if t.LookaheadFrames > 0 {
		args = append(args, "--lag-in-frames="+strconv.Itoa(t.LookaheadFrames))
	} else {
		args = append(args, "--lag-in-frames=0")
	}
	// libvpx token: --auto-alt-ref
	if t.AutoAltRef {
		args = append(args, "--auto-alt-ref=1")
	} else {
		args = append(args, "--auto-alt-ref=0")
	}
	// libvpx token: --arnr-maxframes / --arnr-strength / --arnr-type
	if t.ARNRMaxFrames > 0 {
		args = append(args, "--arnr-maxframes="+strconv.Itoa(t.ARNRMaxFrames))
	}
	if t.ARNRStrength > 0 {
		args = append(args, "--arnr-strength="+strconv.Itoa(t.ARNRStrength))
	}
	if t.ARNRType > 0 {
		args = append(args, "--arnr-type="+strconv.Itoa(t.ARNRType))
	}
	// libvpx token: --aq-mode (govpx VP9AQMode 0..5 maps 1:1 to libvpx
	// AQ_MODE enum: NO_AQ=0, VARIANCE_AQ=1, COMPLEXITY_AQ=2,
	// CYCLIC_REFRESH_AQ=3, EQUATOR360_AQ=4, PERCEPTUAL_AQ=5).
	args = append(args, "--aq-mode="+strconv.Itoa(int(t.AQMode)))
	// libvpx token: --alt-ref-aq
	if t.AltRefAQ {
		args = append(args, "--alt-ref-aq=1")
	} else {
		args = append(args, "--alt-ref-aq=0")
	}
	// libvpx token: --frame-boost (FramePeriodicBoost mirrors libvpx's
	// VP9E_SET_FRAME_PERIODIC_BOOST, exposed via --frame-boost).
	if t.FramePeriodicBoost {
		args = append(args, "--frame-boost=1")
	} else {
		args = append(args, "--frame-boost=0")
	}
	// libvpx token: --noise-sensitivity / --sharpness / --static-thresh
	if t.NoiseSensitivity > 0 {
		args = append(args, "--noise-sensitivity="+strconv.Itoa(int(t.NoiseSensitivity)))
	}
	if t.Sharpness > 0 {
		args = append(args, "--sharpness="+strconv.Itoa(int(t.Sharpness)))
	}
	if t.StaticThreshold > 0 {
		args = append(args, "--static-thresh="+strconv.Itoa(t.StaticThreshold))
	}
	// libvpx token: --max-intra-rate / --gf-cbr-boost
	if t.MaxIntraBitratePct > 0 {
		args = append(args, "--max-intra-rate="+strconv.Itoa(t.MaxIntraBitratePct))
	}
	if t.GFCBRBoostPct > 0 {
		args = append(args, "--gf-cbr-boost="+strconv.Itoa(t.GFCBRBoostPct))
	}
	// libvpx token: --min-gf-interval / --max-gf-interval
	if t.MinGFInterval > 0 {
		args = append(args, "--min-gf-interval="+strconv.Itoa(t.MinGFInterval))
	}
	if t.MaxGFInterval > 0 {
		args = append(args, "--max-gf-interval="+strconv.Itoa(t.MaxGFInterval))
	}
	// libvpx token: --disable-loopfilter
	if t.DisableLoopfilter > 0 {
		args = append(args, "--disable-loopfilter="+strconv.Itoa(int(t.DisableLoopfilter)))
	}
	return args
}

// parseLibvpxBDTrace walks the JSONL trace emitted by
// vpxenc-vp9-frameflags and totals the emitted bytes and visible-frame
// qindex. The helper emits one row per packet, plus drop rows for
// frames the rate controller suppressed; both are accounted for.
func parseLibvpxBDTrace(trace []byte) (totalBytes int, meanQ float64, visible int, err error) {
	dec := json.NewDecoder(bytes.NewReader(trace))
	qSum := 0
	for dec.More() {
		var row struct {
			SizeBytes  int  `json:"size_bytes"`
			ShowFrame  bool `json:"show_frame"`
			BaseQindex int  `json:"base_qindex"`
			Dropped    bool `json:"dropped"`
		}
		if err := dec.Decode(&row); err != nil {
			return 0, 0, 0, fmt.Errorf("decode trace row: %w", err)
		}
		totalBytes += row.SizeBytes
		if row.ShowFrame && !row.Dropped {
			qSum += row.BaseQindex
			visible++
		}
	}
	if visible == 0 {
		return totalBytes, 0, 0, nil
	}
	return totalBytes, float64(qSum) / float64(visible), visible, nil
}

// writeI420ToBytes serialises the source frames into one contiguous
// I420 buffer in the format the libvpx helper expects (Y, then U,
// then V; each plane is visible-width packed).
func writeI420ToBytes(frames []*image.YCbCr, width, height int) ([]byte, error) {
	if len(frames) == 0 {
		return nil, errors.New("no source frames")
	}
	uvW := (width + 1) >> 1
	uvH := (height + 1) >> 1
	frameSize := width*height + 2*uvW*uvH
	out := make([]byte, 0, frameSize*len(frames))
	for i, f := range frames {
		if f.Rect.Dx() != width || f.Rect.Dy() != height {
			return nil, fmt.Errorf("frame %d size %dx%d != harness size %dx%d",
				i, f.Rect.Dx(), f.Rect.Dy(), width, height)
		}
		for y := range height {
			out = append(out, f.Y[y*f.YStride:y*f.YStride+width]...)
		}
		for y := range uvH {
			out = append(out, f.Cb[y*f.CStride:y*f.CStride+uvW]...)
		}
		for y := range uvH {
			out = append(out, f.Cr[y*f.CStride:y*f.CStride+uvW]...)
		}
	}
	return out, nil
}

// resolveLibvpxVP9FrameFlagsBinary locates the vpxenc-vp9-frameflags
// helper. When buildIfMissing is true and the binary is absent, the
// resolver invokes the build script and re-resolves; a still-missing
// binary at that point is a hard failure (mandate from the BD-rate
// harness: build was requested, must succeed). When buildIfMissing is
// false, the missing-binary path is the skip signal.
//
// Because coracle.VpxencVP9FrameFlagsPath caches the first-seen result
// via sync.Once, this helper does not call through coracle when a
// build was requested; instead it asks the build script to print the
// resolved binary path and re-stats that location directly.
func resolveLibvpxVP9FrameFlagsBinary(buildIfMissing bool) (string, error) {
	if bin, err := coracle.VpxencVP9FrameFlagsPath(); err == nil {
		return bin, nil
	}
	if envBin := os.Getenv("GOVPX_VPXENC_VP9_FRAMEFLAGS_BIN"); envBin != "" {
		if st, err := os.Stat(envBin); err == nil && !st.IsDir() {
			return envBin, nil
		}
	}
	root, ok := findGovpxRoot()
	if !ok {
		return "", coracle.ErrVpxencVP9FrameFlagsNotBuilt
	}
	candidate := filepath.Join(root, "internal", "coracle", "build",
		"vpxenc-vp9-frameflags")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, nil
	}
	if !buildIfMissing {
		return "", coracle.ErrVpxencVP9FrameFlagsNotBuilt
	}
	script := filepath.Join(root, "internal", "coracle",
		"build_vpxenc_vp9_frameflags.sh")
	build := exec.Command("sh", script)
	build.Dir = root
	var bout, berr bytes.Buffer
	build.Stdout = &bout
	build.Stderr = &berr
	if rerr := build.Run(); rerr != nil {
		return "", fmt.Errorf("libvpx build failed: %w\nstdout:\n%s\nstderr:\n%s",
			rerr, bout.Bytes(), berr.Bytes())
	}
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("libvpx build script ran but %s still missing", candidate)
}
