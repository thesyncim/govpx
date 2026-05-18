//go:build govpx_oracle_trace && diag

package govpx

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestDiagAebef841Frame6 dumps per-MB traces from both govpx and the libvpx
// oracle for the aebef841 seed at frame 6. Documentation-only repro hook for
// the task #226 audit (vp8_task226_aebef841_frame6_picker_audit_test.go).
func TestDiagAebef841Frame6(t *testing.T) {
	if os.Getenv("GOVPX_DIAG") != "1" {
		t.Skip("set GOVPX_DIAG=1")
	}
	driver := findVpxencFrameFlagsOracle(t)
	tc := oracleRuntimeControlFuzzCaseFromBytes([]byte("020b00)a07"))
	t.Logf("decoded fuzz case: name=%s script=%v", tc.name, tc.script)

	libvpxTracePath := t.TempDir() + "/libvpx.jsonl"
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", libvpxTracePath)
	libvpxFrames := encodeFramesWithFrameFlagsDriver(t, driver, "diag-libvpx",
		tc.opts, tc.targetKbps, tc.sources, tc.flags,
		append(append([]string(nil), tc.extraArgs...), "--control-script="+strings.Join(tc.script, ",")))
	libvpxTrace, err := os.ReadFile(libvpxTracePath)
	if err != nil {
		t.Fatalf("read libvpx trace: %v", err)
	}

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

	os.WriteFile("/tmp/diag_aebef841_libvpx.jsonl", libvpxTrace, 0644)
	os.WriteFile("/tmp/diag_aebef841_govpx.jsonl", govpxTrace.Bytes(), 0644)
	t.Logf("traces written (libvpx=%d bytes, govpx=%d bytes)",
		len(libvpxTrace), govpxTrace.Len())
	_ = fmt.Sprintf
}
