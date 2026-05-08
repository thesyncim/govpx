package govpx

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"sort"
	"testing"
)

// TestDiagCBRDropQ pulls per-frame state from both encoders for the 30f
// 80kbps panning CBR drop fixture and prints a side-by-side Q / target /
// buffer_level table so we can localize the post_drop_q_max_drift residual.
//
// Gated on GOVPX_DIAG_CBR_DROP_Q=1; requires GOVPX_WITH_ORACLE=1.
func TestDiagCBRDropQ(t *testing.T) {
	if os.Getenv("GOVPX_DIAG_CBR_DROP_Q") != "1" {
		t.Skip("set GOVPX_DIAG_CBR_DROP_Q=1 to run CBR drop Q diagnostic")
	}
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle CBR diagnostic")
	}
	vpxencOracle := findVpxencOracle(t)

	fx := cbrDropFixtureSpec{
		Name:             "panning-30f-80kbps-cpu8",
		Width:            64,
		Height:           64,
		FPS:              30,
		Frames:           30,
		TargetKbps:       80,
		BufferSizeMs:     600,
		BufferInitialMs:  400,
		BufferOptimalMs:  500,
		MinQ:             4,
		MaxQ:             63,
		Deadline:         DeadlineRealtime,
		CpuUsed:          8,
		KeyFrameInterval: 999,
		LibvpxDropFrame:  60,
	}
	sources := make([]Image, fx.Frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(fx.Width, fx.Height, i)
	}
	opts := EncoderOptions{
		Width:               fx.Width,
		Height:              fx.Height,
		FPS:                 fx.FPS,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   fx.TargetKbps,
		MinQuantizer:        fx.MinQ,
		MaxQuantizer:        fx.MaxQ,
		Deadline:            fx.Deadline,
		CpuUsed:             fx.CpuUsed,
		KeyFrameInterval:    fx.KeyFrameInterval,
		DropFrameAllowed:    true,
		BufferSizeMs:        fx.BufferSizeMs,
		BufferInitialSizeMs: fx.BufferInitialMs,
		BufferOptimalSizeMs: fx.BufferOptimalMs,
	}
	gov := captureGovpxDropAwareTrace(t, opts, sources)
	lib := captureLibvpxDropAwareTrace(t, vpxencOracle, "diag-cbrdrop", opts, fx, sources)
	gRows := parseCBRDropDiagRows(t, gov)
	lRows := parseCBRDropDiagRows(t, lib)
	t.Logf("idx | govpx q  buf      tgt    proj kfo   gfo  drp | libvpx q  buf      tgt    proj kfo   gfo  drp |  d_q  d_buf  d_tgt  d_proj  d_kfo")
	all := mergeFrameKeys(gRows, lRows)
	for _, idx := range all {
		g := gRows[idx]
		l := lRows[idx]
		t.Logf("%3d | gov q=%2d buf=%6d tgt=%5d proj=%5d kfo=%5d gfo=%5d drp=%v | lib q=%2d buf=%6d tgt=%5d proj=%5d kfo=%5d gfo=%5d drp=%v | d_q=%+d d_buf=%+d d_tgt=%+d d_proj=%+d d_kfo=%+d",
			idx,
			g.Q, g.Buf, g.Tgt, g.Proj, g.KFOverspend, g.GFOverspend, g.Dropped,
			l.Q, l.Buf, l.Tgt, l.Proj, l.KFOverspend, l.GFOverspend, l.Dropped,
			g.Q-l.Q, g.Buf-l.Buf, g.Tgt-l.Tgt, g.Proj-l.Proj, g.KFOverspend-l.KFOverspend)
	}
}

type diagCBRRow struct {
	Q           int
	Buf         int64
	Tgt         int
	Proj        int
	KFOverspend int
	GFOverspend int
	Dropped     bool
}

func parseCBRDropDiagRows(t *testing.T, trace []byte) map[int]diagCBRRow {
	t.Helper()
	out := make(map[int]diagCBRRow)
	scan := bufio.NewScanner(bytes.NewReader(trace))
	scan.Buffer(make([]byte, 0, 1<<16), 1<<22)
	for scan.Scan() {
		var row map[string]any
		if err := json.Unmarshal(scan.Bytes(), &row); err != nil {
			continue
		}
		typ, _ := row["type"].(string)
		idx := int(traceFloat(row["frame_index"]))
		switch typ {
		case "frame":
			cur := out[idx]
			dropped, _ := row["dropped"].(bool)
			if dropped {
				cur.Dropped = true
				cur.Q = -1
				if v, ok := row["buffer_level"]; ok {
					cur.Buf = int64(traceFloat(v))
				}
			} else {
				if v, ok := row["q_index"]; ok {
					cur.Q = int(traceFloat(v))
				}
			}
			out[idx] = cur
		case "rate":
			cur := out[idx]
			if v, ok := row["q_index"]; ok {
				cur.Q = int(traceFloat(v))
			}
			if v, ok := row["buffer_level"]; ok {
				cur.Buf = int64(traceFloat(v))
			}
			if v, ok := row["this_frame_target"]; ok {
				cur.Tgt = int(traceFloat(v))
			}
			if v, ok := row["projected_frame_size"]; ok {
				cur.Proj = int(traceFloat(v))
			}
			if v, ok := row["kf_overspend_bits"]; ok {
				cur.KFOverspend = int(traceFloat(v))
			}
			if v, ok := row["gf_overspend_bits"]; ok {
				cur.GFOverspend = int(traceFloat(v))
			}
			out[idx] = cur
		}
	}
	return out
}

func mergeFrameKeys(a, b map[int]diagCBRRow) []int {
	seen := make(map[int]struct{}, len(a)+len(b))
	for k := range a {
		seen[k] = struct{}{}
	}
	for k := range b {
		seen[k] = struct{}{}
	}
	keys := make([]int, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	return keys
}
