//go:build govpx_oracle_trace

package govpx

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestVP8ChangeConfigTailParity pins task #209's diagnostic finding:
// the cpi-LEVEL state at the end of libvpx's vp8_change_config tail is
// IDENTICAL between the "with noise:0" and "without noise:0" runs of the
// 640x360 serial noise0_inter_diverge production runtime-control fuzz seed.
// Therefore the residual 240-byte gap on frame 1 is NOT caused by any
// cpi-level field divergence; it lives in some OTHER side-effect mutated
// by the extra `vp8_change_config` calls libvpx triggers on the noise:0
// control entry.
//
// What this test verifies:
//
//  1. The libvpx-side oracle binary emits a {"type":"change_config_tail",
//     ...} JSON line at the end of every vp8_change_config call when
//     GOVPX_ORACLE_TRACE_OUT is set. (Confirms the diagnostic hook is wired
//     in via internal/coracle/build_vpxenc_oracle.sh's anchor patch on
//     vp8/encoder/onyx_if.c around the closing brace of vp8_change_config.)
//
//  2. The WITH-noise run produces strictly more change_config_tail rows
//     than the WITHOUT-noise run (the noise:0 control triggers
//     update_extracfg -> vp8_change_config, which itself trips
//     pick_quickcompress_mode's Mode-flip -> vp8_change_config -> a second
//     call inside the same frame boundary). This is the structural
//     evidence that the gap is invocation-count-driven, not value-driven.
//
//  3. At the LAST change_config_tail row of each run (the cpi state right
//     before frame 1's encode begins), every "static" cpi field that
//     libvpx mutates inside vp8_change_config matches between WITH and
//     WITHOUT noise. The only legitimate diffs are natural state
//     progression: frame_index (we encode one extra config event in
//     WITH), buffer_level / bits_off_target (post-keyframe drain),
//     framerate / output_framerate (timestamp-driven drift from 30.0 to
//     30.00003 after frame 0), and the xd_* segmentation flags
//     (re-armed by setup_features which the WITHOUT case has not
//     observed at the dump point, but which cyclic_background_refresh
//     re-arms on the next frame regardless).
//
// The actual gap therefore lives in some state mutation that is NOT
// captured by this dump - likely in per-MB picker state (RD threshold
// multipliers, error_bins, mbs_tested_so_far, mode_test_hit_counts) that
// vp8_set_speed_features reseeds when Speed is reset by the extra
// vp8_change_config calls. That follow-up audit needs a per-MB tracer
// (which #206 is building); task #209's contribution is the
// change_config_tail diagnostic infrastructure that lets future audits
// produce the kind of field-level diff this test materializes.
//
// libvpx source references (v1.16.0):
//   - vp8/encoder/onyx_if.c:1448-1753 vp8_change_config (the dump fires
//     at the closing brace).
//   - vp8/vp8_cx_iface.c:525-534 update_extracfg (calls vp8_change_config
//     after every VP8E_SET_*).
//   - vp8/vp8_cx_iface.c:837-849 pick_quickcompress_mode (calls
//     vp8_change_config when Mode flips, which is forced after every
//     update_extracfg-driven Mode reset to BESTQUALITY).
//
// govpx source references:
//   - vp8_encoder_config.go:873 applyVP8ChangeConfigRuntimeSideEffects
//   - internal/coracle/build_vpxenc_oracle.sh: oracle_trace.c TU
//   - ensure_change_config_tail_hook anchor patch.
func TestVP8ChangeConfigTailParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1")
	}
	driver := coracletest.VpxencFrameFlags(t)
	const (
		w          = 640
		h          = 360
		cpuUsed    = 0
		targetKbps = 300
	)
	opts := oracleRuntimeBaseFuzzOptions(w, h, targetKbps, cpuUsed)
	opts.Threads = 0
	sources := oracleRuntimeFuzzSources(w, h, 2, 0)

	parseChangeConfigTailRows := func(t *testing.T, path string) []map[string]any {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read trace %q: %v", path, err)
		}
		var rows []map[string]any
		for _, line := range strings.Split(string(data), "\n") {
			if !strings.Contains(line, `"type":"change_config_tail"`) {
				continue
			}
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("parse trace row %q: %v", line, err)
			}
			rows = append(rows, m)
		}
		return rows
	}

	withPath := t.TempDir() + "/libvpx_with_noise.jsonl"
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", withPath)
	encodeFramesWithFrameFlagsDriver(t, driver, "task209-with-noise",
		opts, targetKbps, sources, nil,
		[]string{"--control-script=" + strings.Join([]string{"-", "noise:0"}, ",")})
	withRows := parseChangeConfigTailRows(t, withPath)

	withoutPath := t.TempDir() + "/libvpx_without_noise.jsonl"
	t.Setenv("GOVPX_ORACLE_TRACE_OUT", withoutPath)
	encodeFramesWithFrameFlagsDriver(t, driver, "task209-without-noise",
		opts, targetKbps, sources, nil,
		[]string{"--control-script=" + strings.Join([]string{"-", "-"}, ",")})
	withoutRows := parseChangeConfigTailRows(t, withoutPath)

	if len(withRows) == 0 {
		t.Fatalf("with-noise trace produced no change_config_tail rows; the "+
			"build_vpxenc_oracle.sh task #209 patch may have regressed. "+
			"Path: %s", withPath)
	}
	if len(withoutRows) == 0 {
		t.Fatalf("without-noise trace produced no change_config_tail rows. Path: %s", withoutPath)
	}
	// (2) WITH produces strictly more rows than WITHOUT: noise:0 fires two
	// extra vp8_change_config calls (one from update_extracfg, one from
	// pick_quickcompress_mode's Mode-flip back to REALTIME). The actual
	// row counts can drift with libvpx config changes; we pin only the
	// relative ordering here, not absolute values.
	if len(withRows) <= len(withoutRows) {
		t.Fatalf("expected with-noise trace to have more change_config_tail "+
			"rows than without-noise (noise:0 triggers two extra "+
			"vp8_change_config calls). got with=%d without=%d",
			len(withRows), len(withoutRows))
	}

	// (3) The "static" cpi state matches between the LAST change_config_tail
	// row of each run. Fields known to drift naturally are exempt from this
	// invariant; they record state progression (frame index, buffer drain,
	// timestamp framerate update, segmentation flags being re-armed by
	// setup_features) and are not the residual divergence trigger.
	naturalDrift := map[string]bool{
		"frame_index":                    true,
		"buffer_level":                   true,
		"bits_off_target":                true,
		"framerate":                      true,
		"output_framerate":               true,
		"xd_segmentation_enabled":        true,
		"xd_update_mb_segmentation_map":  true,
		"xd_update_mb_segmentation_data": true,
	}
	withLast := withRows[len(withRows)-1]
	withoutLast := withoutRows[len(withoutRows)-1]
	var staticDiffs []string
	for k, wv := range withLast {
		if naturalDrift[k] {
			continue
		}
		ov, ok := withoutLast[k]
		if !ok {
			continue
		}
		if fmt.Sprint(wv) != fmt.Sprint(ov) {
			staticDiffs = append(staticDiffs,
				fmt.Sprintf("%s: with=%v without=%v", k, wv, ov))
		}
	}
	if len(staticDiffs) > 0 {
		t.Fatalf("task #209 invariant violated: vp8_change_config tail emits "+
			"a 'static' cpi field that diverges between with-noise:0 and "+
			"without-noise:0 runs at the last change_config invocation. "+
			"If this fires, the 240B gap CAN be explained by a cpi-level "+
			"field mismatch and the next audit step should port that field "+
			"from libvpx vp8/encoder/onyx_if.c verbatim. Divergent fields: %v",
			staticDiffs)
	}
}
