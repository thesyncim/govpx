//go:build govpx_oracle_trace && diag

package govpx

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDiagAebef841GoldenPlane is task #249's GOLDEN/LAST/ALTREF parity
// probe for the aebef841 fuzz seed. Originally written to investigate
// whether a 1-pixel GOLDEN-frame reconstruction drift at frame 1 caused
// the (now-closed via task #237) SPLITMV-GOLDEN frame 2 MB(2,1)
// per-Y-block divergence. The angle was falsified: GOLDEN/LAST/ALTREF
// Y/U/V plane adler32 is byte-identical at every frame entry, frames
// 1..8. The real fix landed in task #237 (rd_check_segment stale-eobs
// side-effect port at vp8/encoder/rdopt.c:1180).
//
// Probe approach: inject a `copyref:golden+copyref:last+copyref:altref`
// token bundle into the libvpx control-script at every post-keyframe
// frame (executes before that frame's encode). govpx mirrors by calling
// e.CopyReferenceFrame(...) at the same point in the in-process encode
// loop. Adler32 mismatch on any plane at any frame entry implicates the
// corresponding reference-frame reconstruction path (loop filter,
// token-recon, or intra-pred at the donating frame's commit).
//
// Post-#237 state: NO mismatch at any probed frame. Retained as a
// regression probe for future SPLITMV / reference-handling work — any
// reintroduced drift in encoder_reconstruct.go,
// encoder_reference_buffers.go, internal/vp8/common/loopfilter*, or
// the post-frame YV12 border-extend path will surface here as a
// non-zero "REF MISMATCH" log line and a DIAG249_RESULT mismatch=true
// summary the parent agent can grep.
//
// Run with:
//
//	GOVPX_VPXENC_FRAMEFLAGS_ORACLE=$(pwd)/internal/coracle/build/vpxenc-frameflags-oracle \
//	GOVPX_DIAG=1 \
//	go test -tags 'govpx_oracle_trace diag' \
//	  -run TestDiagAebef841GoldenPlane -v -count=1
func TestDiagAebef841GoldenPlane(t *testing.T) {
	if os.Getenv("GOVPX_DIAG") != "1" {
		t.Skip("set GOVPX_DIAG=1")
	}
	driver := findVpxencFrameFlagsOracle(t)
	tc := oracleRuntimeControlFuzzCaseFromBytes([]byte("020b00)a07"))
	t.Logf("decoded fuzz case: name=%s script=%v", tc.name, tc.script)

	// Append copyref:golden to every frame >=2 so we can compare the
	// GOLDEN plane state at the entry of every post-keyframe frame.
	probeFrames := []int{}
	patchedScript := append([]string(nil), tc.script...)
	for i := range patchedScript {
		if i == 0 {
			continue
		}
		probeFrames = append(probeFrames, i)
		token := "copyref:golden+copyref:last+copyref:altref"
		if patchedScript[i] == "-" || patchedScript[i] == "" {
			patchedScript[i] = token
		} else {
			patchedScript[i] = patchedScript[i] + "+" + token
		}
	}

	// libvpx side: run vpxenc-frameflags-oracle with --copy-ref-log.
	dir := t.TempDir()
	libvpxLog := filepath.Join(dir, "libvpx-copyref.log")
	extraArgs := append([]string(nil), tc.extraArgs...)
	extraArgs = append(extraArgs,
		"--copy-ref-log="+libvpxLog,
		"--control-script="+strings.Join(patchedScript, ","))
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "diag-249-libvpx",
		tc.opts, tc.targetKbps, tc.sources, tc.flags, extraArgs)

	libvpxChecksums := readCopyReferenceChecksumLog(t, libvpxLog)
	libvpxByFrame := map[string]copyReferenceChecksum{}
	for _, c := range libvpxChecksums {
		libvpxByFrame[fmt.Sprintf("%d/%s", c.Frame, c.Ref)] = c
	}

	// govpx side: in-process encode with CopyReferenceFrame at each probe.
	govpxByFrame := map[string]copyReferenceChecksum{}
	var govpxFrames [][]byte
	{
		enc, err := NewVP8Encoder(tc.opts)
		if err != nil {
			t.Fatalf("NewVP8Encoder: %v", err)
		}
		defer enc.Close()
		buf := make([]byte, tc.opts.Width*tc.opts.Height*4+4096)
		for i, src := range tc.sources {
			// Apply pre-encode hooks (matches what apply does).
			if fn := tc.apply[i]; fn != nil {
				fn(t, enc)
			}
			// Probe GOLDEN/LAST/ALTREF at frame entry (mirrors C oracle
			// applying copyref BEFORE frame encode).
			if probeFrameContains(probeFrames, i) {
				for _, ref := range []ReferenceFrame{ReferenceLast, ReferenceGolden, ReferenceAltRef} {
					var dst Image = freshGoldenProbeImage(tc.opts.Width, tc.opts.Height)
					if err := enc.CopyReferenceFrame(ref, &dst); err != nil {
						t.Fatalf("CopyReferenceFrame(%v) frame %d: %v", ref, i, err)
					}
					name := refProbeName(ref)
					cs := copyReferenceImageChecksum(i, name, dst)
					govpxByFrame[fmt.Sprintf("%d/%s", i, name)] = cs
				}
			}
			var f EncodeFlags
			if i < len(tc.flags) {
				f = tc.flags[i]
			}
			result, err := enc.EncodeInto(buf, src, uint64(i), 1, f)
			if err != nil {
				t.Fatalf("EncodeInto frame %d: %v", i, err)
			}
			if !result.Dropped {
				govpxFrames = append(govpxFrames, append([]byte(nil), result.Data...))
			}
		}
	}

	// Compare per-frame, per-ref.
	t.Logf("frames: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	for i := 0; i < len(libvpxFrames) && i < len(govpxFrames); i++ {
		eq := bytes.Equal(libvpxFrames[i], govpxFrames[i])
		t.Logf("BYTES frame=%d govpx=%d libvpx=%d match=%v", i, len(govpxFrames[i]), len(libvpxFrames[i]), eq)
	}
	mismatchFound := false
	firstMismatchFrame := -1
	firstMismatchRef := ""
	for _, frame := range probeFrames {
		for _, ref := range []string{"last", "golden", "altref"} {
			key := fmt.Sprintf("%d/%s", frame, ref)
			g, gok := govpxByFrame[key]
			l, lok := libvpxByFrame[key]
			if !gok || !lok {
				t.Logf("REF frame=%d ref=%s govpxLogged=%v libvpxLogged=%v", frame, ref, gok, lok)
				continue
			}
			yEq := g.YAdler32 == l.YAdler32
			uEq := g.UAdler32 == l.UAdler32
			vEq := g.VAdler32 == l.VAdler32
			match := yEq && uEq && vEq
			t.Logf("REF frame=%d ref=%-6s match=%-5v y[g=%08x l=%08x]%s u[g=%08x l=%08x]%s v[g=%08x l=%08x]%s",
				frame, ref, match, g.YAdler32, l.YAdler32, mismatchTag(yEq), g.UAdler32, l.UAdler32, mismatchTag(uEq), g.VAdler32, l.VAdler32, mismatchTag(vEq))
			if !match && !mismatchFound {
				mismatchFound = true
				firstMismatchFrame = frame
				firstMismatchRef = ref
			}
		}
	}
	if mismatchFound {
		t.Logf("FIRST REF MISMATCH frame=%d ref=%s — GOLDEN/LAST/ALTREF reference content diverges BEFORE the picker runs",
			firstMismatchFrame, firstMismatchRef)
	} else {
		t.Logf("NO REF MISMATCH at any probed frame — reference plane content is byte-identical between govpx and libvpx; SPLITMV-GOLDEN per-Y-block divergence is NOT due to reference image content. Pivot to label-RD picker (encoder_inter_split.go labelRD.rateDistortion).")
	}
	// Spot-check first GOLDEN ref mismatch (if any) by dumping the 16x16
	// Y window around MB(2,1) (rows 16..31, cols 32..47).
	if mismatchFound && firstMismatchRef == "golden" {
		t.Logf("GOLDEN PLANE MISMATCH at frame %d — manual byte dump not yet wired; rebuild oracle with --golden-y-dump to capture raw bytes",
			firstMismatchFrame)
	}
	// Emit a one-line summary the parent agent can grep.
	t.Logf("DIAG249_RESULT mismatch=%v firstFrame=%d firstRef=%s", mismatchFound, firstMismatchFrame, firstMismatchRef)
}

func probeFrameContains(frames []int, target int) bool {
	for _, f := range frames {
		if f == target {
			return true
		}
	}
	return false
}

func refProbeName(ref ReferenceFrame) string {
	switch ref {
	case ReferenceLast:
		return "last"
	case ReferenceGolden:
		return "golden"
	case ReferenceAltRef:
		return "altref"
	default:
		return "unknown"
	}
}

func mismatchTag(eq bool) string {
	if eq {
		return ""
	}
	return "*"
}

func freshGoldenProbeImage(width, height int) Image {
	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	return Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, uvWidth*uvHeight),
		V:       make([]byte, uvWidth*uvHeight),
		YStride: width,
		UStride: uvWidth,
		VStride: uvWidth,
	}
}
