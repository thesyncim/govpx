//go:build govpx_oracle_trace && diag

package govpx

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestDiagBb41d74Frame4 dumps per-MB traces from both govpx and the libvpx
// oracle for the 0bb41d74 seed at frame 4.
func TestDiagBb41d74Frame4(t *testing.T) {
	if os.Getenv("GOVPX_DIAG") != "1" {
		t.Skip("set GOVPX_DIAG=1")
	}
	driver := findVpxencFrameFlagsOracle(t)
	tc := oracleRuntimeControlFuzzCaseFromBytes([]byte("020bA0C)a"))
	t.Logf("decoded fuzz case: name=%s script=%v", tc.name, tc.script)
	t.Logf("opts=%+v", tc.opts)

	// Run libvpx side with oracle trace enabled.
	libvpxTracePath := t.TempDir() + "/libvpx.jsonl"
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", libvpxTracePath)
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "diag-libvpx",
		tc.opts, tc.targetKbps, tc.sources, tc.flags,
		append(append([]string(nil), tc.extraArgs...), "--control-script="+strings.Join(tc.script, ",")))
	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

	// Run govpx side, capture trace.
	var govpxTrace bytes.Buffer
	enc, err := NewVP8Encoder(tc.opts)
	if err != nil {
		t.Fatalf("NewVP8Encoder: %v", err)
	}
	defer enc.Close()
	enc.SetOracleTraceWriter(&govpxTrace)
	buf := make([]byte, tc.opts.Width*tc.opts.Height*4+4096)
	var govpxFrames [][]byte
	for i, src := range tc.sources {
		if fn := tc.apply[i]; fn != nil {
			fn(t, enc)
		}
		t.Logf("frame %d pre-encode: rcf=%.6f gcf=%.6f kcf=%.6f cur_q=%d last_inter_q=%d ni_frames=%d ni_av_qi=%d avg_q=%d deadline=%d cpu=%d activeWorstQChanged=%v",
			i,
			enc.rc.rateCorrectionFactor,
			enc.rc.goldenCorrectionFactor,
			enc.rc.keyFrameCorrectionFactor,
			enc.rc.currentQuantizer,
			enc.rc.lastInterQuantizer,
			enc.rc.normalInterFrames,
			enc.rc.normalInterAvgQuantizer,
			enc.rc.avgFrameQuantizer,
			enc.opts.Deadline,
			enc.opts.CpuUsed,
			enc.rc.activeWorstQChanged,
		)
		t.Logf("frame %d pre-encode rdThreshMult: %v", i, enc.interRDThreshMult)
		t.Logf("frame %d pre-encode buffer_level=%d kf_overspend=%d gf_overspend=%d", i, enc.rc.bufferLevelBits, enc.rc.kfOverspendBits, enc.rc.gfOverspendBits)
		t.Logf("frame %d pre-encode framesTillGFUpdateDue=%d framesSinceGolden=%d baselineGFInterval=%d", i, enc.rc.framesTillGFUpdateDue, enc.rc.framesSinceGolden, enc.rc.currentGFInterval)
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

	for i := 0; i < len(libvpxFrames) && i < len(govpxFrames); i++ {
		match := bytes.Equal(libvpxFrames[i], govpxFrames[i])
		t.Logf("frame %d: govpx=%d libvpx=%d match=%v", i, len(govpxFrames[i]), len(libvpxFrames[i]), match)
	}

	// Dump traces for the user to inspect
	os.WriteFile("/tmp/diag_libvpx.jsonl", libvpxTrace, 0644)
	os.WriteFile("/tmp/diag_govpx.jsonl", govpxTrace.Bytes(), 0644)
	t.Logf("traces written to /tmp/diag_libvpx.jsonl and /tmp/diag_govpx.jsonl (libvpx=%d bytes, govpx=%d bytes)",
		len(libvpxTrace), govpxTrace.Len())
	_ = fmt.Sprintf
}
