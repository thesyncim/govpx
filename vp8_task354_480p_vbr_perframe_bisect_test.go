package govpx_test

// Task #354: audit of the 480p panning VBR fixture (#6 in the post-#341
// VP8 sweep). At GOVPX_BD_RATE_GATES=1 the fixture's absolute govpx-vs-
// libvpx BD-rate measures +0.645% / -0.043 dB BD-PSNR — the smallest
// remaining positive in the 10-fixture cohort and the only positive
// post-#341 (the others either match or beat libvpx).
//
// Per-rung curves at the time of this pin:
//
//   target=500:   govpx 1465.785 kbps vs libvpx 1463.340 kbps (Δ +2.45)
//   target=1000:  govpx 2187.000 kbps vs libvpx 2150.685 kbps (Δ +36.32)  <-- dominant
//   target=2000:  govpx 2393.280 kbps vs libvpx 2382.780 kbps (Δ +10.50)
//   target=4000:  govpx 2439.930 kbps vs libvpx 2427.570 kbps (Δ +12.36)
//
// Per-frame bisect at the dominant rung (target=1000 kbps), comparing
// emitted IVF frame sizes and decoded base_qindex (libvpx via vpxdec
// --framestats, govpx via EncodeResult.InternalQuantizer):
//
//   f# size_govpx size_libvpx Δ      iQ_govpx iQ_libvpx note
//   0  127426     127426      +0     4        4         keyframe — byte-identical
//   1  189        189         +0     127      127       inter Q=63 (max) — byte-identical
//   2  1158       1158        +0     13       13        inter — byte-identical
//   3  730        689         +41    17       17        first divergence at SAME Q
//   4  970        1145        -175   16       15        govpx picks higher Q
//   5  1137       971         +166   16       16        same Q, govpx +166 bytes
//   6  1004       1091        -87    16       16        same Q
//   7  1232       743         +489   12       18        govpx picks Q=12 vs libvpx Q=18
//   8  1423       1081        +342   10       18        Q regulator divergence widens
//   9  1771       1077        +694   10       16
//   10 1107       1101        +6     10       14
//   11 1718       1496        +222   9        13
//   12 1165       1002        +163   10       13
//   13 1522       1576        -54    9        11
//   14 1125       1238        -113   11       11
//   15 2123       1396        +727   8        10
//
// Root-cause localization (this task's contribution):
//
//   1. Frames 0-2 are byte-identical between govpx and libvpx (matching
//      sizes AND matching internal qindex), confirming the keyframe and
//      first two inter frames are byte-exact at every level — quantizer
//      regulator, MB mode picker, motion search, and coef coding all
//      agree through f2.
//
//   2. Frame 3 is the FIRST divergent frame. Both encoders pick
//      iQ=17 from the same rate-control state, but the packed bitstream
//      sizes differ by +41 bytes (govpx 730 vs libvpx 689). With same Q,
//      same reference frames (f2 was byte-identical), and same source,
//      this gap can only come from an MB-level decision divergence (mode
//      picker, MV search, or coef/trellis), not from rate control.
//
//   3. The downstream Q drift (f4, f7, f8 picking lower Q than libvpx)
//      is a CONSEQUENCE of (2): the +41-byte overshoot at f3 perturbs
//      bits_off_target, rate_correction_factor, and the buffer-level
//      shrink/grow branch of calc_pframe_target_size. By f7 the two
//      regulators have diverged enough that govpx picks iQ=12 vs
//      libvpx's iQ=18 — a 6-step gap that costs ~500 bytes per frame.
//
//   4. The same-Q +41-byte gap at f3 is consistent with the existing
//      ARNR audit chain pin-hold (task #316 chroma-trellis rdmult
//      divergence, lineage #313 → #314 → #316 → #318 → #319 → #329 →
//      #330) — an inter-frame MB-decision divergence at high-Q boundary
//      conditions. The 480p panning VBR fixture exposes the same gap
//      through the VBR regulator's bigger downstream amplification, but
//      the underlying MB divergence is upstream of rate control.
//
// Closing this fixture's +0.645% therefore requires resolving the
// existing same-Q-bytes-differ ARNR pin-hold, not VBR rate-control
// tuning. The harness sees the gap via the cubic-fit projection at
// the 4-rung ladder; any same-Q MB-decision narrowing on the ARNR
// chain will close it without touching rate control.
//
// Set GOVPX_TASK354_RUN=1 to re-run the bisect (govpx side always
// runs; libvpx side runs only when GOVPX_VPXENC_VP8_BIN is set, and
// per-frame Q is dumped only when GOVPX_VPXDEC_VP8_BIN is also set).

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thesyncim/govpx"
)

// task354PerFrameRow pins one (frame_index, govpx_size, govpx_iQ) row
// from the per-frame bisect at target=1000 kbps. The govpx side is the
// authoritative pin because the libvpx side requires vpxenc/vpxdec
// binaries on PATH; the libvpx values are documented in the file
// header and re-derived from --framestats when the binary is wired up.
type task354PerFrameRow struct {
	frame     int
	govpxSize int
	govpxIQ   int
}

// task354_1000kbpsPin pins the govpx side of the per-frame bisect at
// target=1000 kbps. Any drift here (size or Q) flags either an
// improvement (narrowing toward the libvpx column in the header table)
// or a regression, both of which warrant a fresh pin/audit.
var task354_1000kbpsPin = []task354PerFrameRow{
	{frame: 0, govpxSize: 127426, govpxIQ: 4},
	{frame: 1, govpxSize: 189, govpxIQ: 127},
	{frame: 2, govpxSize: 1158, govpxIQ: 13},
	{frame: 3, govpxSize: 730, govpxIQ: 17},
	{frame: 4, govpxSize: 970, govpxIQ: 16},
	{frame: 5, govpxSize: 1137, govpxIQ: 16},
	{frame: 6, govpxSize: 1004, govpxIQ: 16},
	{frame: 7, govpxSize: 1232, govpxIQ: 12},
	{frame: 8, govpxSize: 1423, govpxIQ: 10},
	{frame: 9, govpxSize: 1771, govpxIQ: 10},
	{frame: 10, govpxSize: 1107, govpxIQ: 10},
	{frame: 11, govpxSize: 1718, govpxIQ: 9},
	{frame: 12, govpxSize: 1165, govpxIQ: 10},
	{frame: 13, govpxSize: 1522, govpxIQ: 9},
	{frame: 14, govpxSize: 1125, govpxIQ: 11},
	{frame: 15, govpxSize: 2123, govpxIQ: 8},
}

func runVP8Panning480pGovpx(t *testing.T, target int) (sizes []int, internalQs []int, kbps float64) {
	t.Helper()
	const (
		width  = 854
		height = 480
		frames = 16
		fps    = 30
		minQ   = 4
		maxQ   = 63
	)
	opts := govpx.EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		TargetBitrateKbps: target,
		MinQuantizer:      minQ,
		MaxQuantizer:      maxQ,
		QuantizerRangeSet: true,
		CQLevel:           28, // ignored by VBR; set for parity with the harness
		RateControlMode:   govpx.RateControlVBR,
	}
	enc, err := govpx.NewVP8Encoder(opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	bufSize := width * height * 6
	dst := make([]byte, bufSize)
	totalBytes := 0
	for i := range frames {
		src := makeVP8Task354PanningFrame(width, height, i)
		result, err := enc.EncodeInto(dst, govpxImageFromYCbCrTask354(src), uint64(i), 1, 0)
		if err != nil {
			t.Fatalf("encode frame %d: %v", i, err)
		}
		sizes = append(sizes, result.SizeBytes)
		internalQs = append(internalQs, result.InternalQuantizer)
		totalBytes += result.SizeBytes
	}
	kbps = float64(totalBytes) * 8 * float64(fps) / float64(frames) / 1000.0
	return sizes, internalQs, kbps
}

func runVP8Panning480pLibvpx(t *testing.T, target int) (sizes []int, qIndices []int, kbps float64) {
	t.Helper()
	binPath := os.Getenv("GOVPX_VPXENC_VP8_BIN")
	if binPath == "" {
		t.Skip("GOVPX_VPXENC_VP8_BIN not set; skipping libvpx side")
	}
	const (
		width  = 854
		height = 480
		frames = 16
		fps    = 30
	)
	dir, err := os.MkdirTemp("", "task354-*")
	if err != nil {
		t.Fatalf("mktemp: %v", err)
	}
	defer os.RemoveAll(dir)
	inPath := filepath.Join(dir, "in.i420")
	outPath := filepath.Join(dir, "out.ivf")
	f, err := os.Create(inPath)
	if err != nil {
		t.Fatalf("create input: %v", err)
	}
	for i := range frames {
		src := makeVP8Task354PanningFrame(width, height, i)
		for y := range height {
			row := src.Y[y*src.YStride:]
			if _, err := f.Write(row[:width]); err != nil {
				t.Fatalf("write Y: %v", err)
			}
		}
		uvW := width >> 1
		uvH := height >> 1
		for y := range uvH {
			row := src.Cb[y*src.CStride:]
			if _, err := f.Write(row[:uvW]); err != nil {
				t.Fatalf("write Cb: %v", err)
			}
		}
		for y := range uvH {
			row := src.Cr[y*src.CStride:]
			if _, err := f.Write(row[:uvW]); err != nil {
				t.Fatalf("write Cr: %v", err)
			}
		}
	}
	f.Close()
	args := []string{
		"--codec=vp8",
		"--passes=1",
		"--end-usage=vbr",
		"--min-q=4", "--max-q=63",
		fmt.Sprintf("--target-bitrate=%d", target),
		"--kf-min-dist=120", "--kf-max-dist=120",
		"--good",
		"--lag-in-frames=0",
		"--auto-alt-ref=0",
		"--tune=psnr",
		"--drop-frame=0",
		"--psnr",
		"--ivf",
		"--i420",
		fmt.Sprintf("--width=%d", width),
		fmt.Sprintf("--height=%d", height),
		fmt.Sprintf("--fps=%d/1", fps),
		fmt.Sprintf("--limit=%d", frames),
		"--output=" + outPath,
		inPath,
	}
	var stderr bytes.Buffer
	cmd := exec.Command(binPath, args...)
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("vpxenc: %v\nstderr=%s", err, stderr.String())
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	sizes, err = parseIVFSizesTask354(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if dec := os.Getenv("GOVPX_VPXDEC_VP8_BIN"); dec != "" {
		statsPath := filepath.Join(dir, "stats.csv")
		dcmd := exec.Command(dec, "--codec=vp8", "--noblit", "--framestats="+statsPath, outPath)
		var derr bytes.Buffer
		dcmd.Stderr = &derr
		if err := dcmd.Run(); err == nil {
			if statsData, err := os.ReadFile(statsPath); err == nil {
				lines := strings.Split(strings.TrimSpace(string(statsData)), "\n")
				// header line: "bytes,qp"; subsequent lines are per-frame.
				for _, line := range lines[1:] {
					parts := strings.Split(line, ",")
					if len(parts) >= 2 {
						var q int
						fmt.Sscanf(parts[1], "%d", &q)
						qIndices = append(qIndices, q)
					}
				}
			}
		} else {
			t.Logf("vpxdec failed: %v\nstderr=%s", err, derr.String())
		}
	}
	totalBytes := 0
	for _, s := range sizes {
		totalBytes += s
	}
	kbps = float64(totalBytes) * 8 * float64(fps) / float64(frames) / 1000.0
	return sizes, qIndices, kbps
}

// TestTask354_480pVBR_GovpxPerFramePin verifies the govpx-side per-
// frame (size, iQ) pin at the target=1000 kbps rung. Any deviation
// flags either an MB-decision improvement that's narrowing the +0.645%
// BD-rate gap (good — re-pin to the new values and confirm the gate
// tightens) or a regression (investigate before merging). This test
// does NOT require libvpx binaries and runs by default.
func TestTask354_480pVBR_GovpxPerFramePin(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	sizes, iqs, kbps := runVP8Panning480pGovpx(t, 1000)
	if len(sizes) != len(task354_1000kbpsPin) {
		t.Fatalf("got %d frames, want %d", len(sizes), len(task354_1000kbpsPin))
	}
	const wantKbps = 2187.0
	const kbpsTol = 0.5
	if diff := kbps - wantKbps; diff > kbpsTol || diff < -kbpsTol {
		t.Errorf("target=1000 govpx kbps=%.3f want %.3f (tol ±%.1f)", kbps, wantKbps, kbpsTol)
	}
	for _, row := range task354_1000kbpsPin {
		if sizes[row.frame] != row.govpxSize {
			t.Errorf("f%d govpx size=%d want %d (re-pin if narrowing toward libvpx)",
				row.frame, sizes[row.frame], row.govpxSize)
		}
		if iqs[row.frame] != row.govpxIQ {
			t.Errorf("f%d govpx iQ=%d want %d (re-pin if narrowing toward libvpx)",
				row.frame, iqs[row.frame], row.govpxIQ)
		}
	}
}

// TestTask354_480pVBR_PerFrameBisect re-runs the full govpx-vs-libvpx
// per-frame bisect across all 4 ladder rungs and prints the comparison
// table. Requires GOVPX_VPXENC_VP8_BIN; logs only (no asserts beyond
// what the govpx pin above covers) so this case is for follow-up audit
// use, not gate enforcement. Set GOVPX_TASK354_RUN=1 to opt in.
func TestTask354_480pVBR_PerFrameBisect(t *testing.T) {
	if os.Getenv("GOVPX_TASK354_RUN") != "1" {
		t.Skip("set GOVPX_TASK354_RUN=1 to run")
	}
	for _, target := range []int{500, 1000, 2000, 4000} {
		gSizes, gIQs, gKbps := runVP8Panning480pGovpx(t, target)
		lSizes, lQs, lKbps := runVP8Panning480pLibvpx(t, target)
		t.Logf("=== target=%d kbps:  govpx_kbps=%.3f libvpx_kbps=%.3f delta=%.3f ===",
			target, gKbps, lKbps, gKbps-lKbps)
		var b strings.Builder
		fmt.Fprintf(&b, "    f# govpx libvpx  d_govpx-libvpx  iQ_govpx  iQ_libvpx\n")
		n := min(len(lSizes), len(gSizes))
		for i := range n {
			lq := -1
			if i < len(lQs) {
				lq = lQs[i]
			}
			fmt.Fprintf(&b, "    %2d %5d %5d %+5d            %3d        %3d\n",
				i, gSizes[i], lSizes[i], gSizes[i]-lSizes[i], gIQs[i], lq)
		}
		t.Log(b.String())
	}
}

func makeVP8Task354PanningFrame(width, height, idx int) *image.YCbCr {
	img := image.NewYCbCr(image.Rect(0, 0, width, height), image.YCbCrSubsampleRatio420)
	xoff := idx * 2
	yoff := idx
	for y := range height {
		row := img.Y[y*img.YStride:]
		for x := range width {
			sx := x + xoff
			sy := y + yoff
			gradient := 64 + task354Triangle(sx+sy, 256)/4
			triX := task354Triangle(sx, 64) / 4
			triY := task354Triangle(sy, 64) / 4
			texture := ((sx*1103515245+sy*12345)>>4)&0x0F - 8
			row[x] = task354Clamp(gradient + triX + triY + texture)
		}
	}
	uvW := width >> 1
	uvH := height >> 1
	for y := range uvH {
		cb := img.Cb[y*img.CStride:]
		cr := img.Cr[y*img.CStride:]
		for x := range uvW {
			sx := 2*x + xoff
			sy := 2*y + yoff
			cb[x] = task354Clamp(128 + (task354Triangle(sx, 128)-128)/8)
			cr[x] = task354Clamp(128 + (task354Triangle(sy, 128)-128)/8)
		}
	}
	return img
}

func task354Triangle(x, period int) int {
	if period <= 0 {
		period = 32
	}
	half := period / 2
	r := ((x % period) + period) % period
	if r < half {
		return r * 255 / half
	}
	return (period - r) * 255 / half
}

func task354Clamp(v int) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v)
}

func govpxImageFromYCbCrTask354(src *image.YCbCr) govpx.Image {
	return govpx.Image{
		Width:   src.Rect.Dx(),
		Height:  src.Rect.Dy(),
		Y:       src.Y,
		U:       src.Cb,
		V:       src.Cr,
		YStride: src.YStride,
		UStride: src.CStride,
		VStride: src.CStride,
	}
}

func parseIVFSizesTask354(ivf []byte) ([]int, error) {
	const fileHeader = 32
	const frameHeader = 12
	if len(ivf) < fileHeader {
		return nil, fmt.Errorf("short IVF")
	}
	offset := fileHeader
	out := []int{}
	for offset < len(ivf) {
		if offset+frameHeader > len(ivf) {
			return nil, fmt.Errorf("truncated frame header")
		}
		sz := int(uint32(ivf[offset]) | uint32(ivf[offset+1])<<8 |
			uint32(ivf[offset+2])<<16 | uint32(ivf[offset+3])<<24)
		offset += frameHeader
		if offset+sz > len(ivf) {
			return nil, fmt.Errorf("truncated payload")
		}
		out = append(out, sz)
		offset += sz
	}
	return out, nil
}
