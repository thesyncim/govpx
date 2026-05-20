//go:build govpx_oracle_trace

package govpx

import (
	"math"
	"os"
	"slices"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
	"github.com/thesyncim/govpx/internal/coracle/coracletest"
)

// TestOracleRecodeRowParity gates the libvpx VP8 recode loop semantics by
// driving a tight rate-control fixture that should force frame-size recodes
// on at least one side and comparing the emitted recode rows.
func TestOracleRecodeRowParity(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run encoder oracle recode comparison")
	}
	vpxencOracle := coracletest.VpxencOracle(t)

	const (
		width      = 64
		height     = 64
		fps        = 30
		targetKbps = 200
		frames     = 8
	)
	opts := EncoderOptions{
		Width:             width,
		Height:            height,
		FPS:               fps,
		RateControlMode:   RateControlVBR,
		TargetBitrateKbps: targetKbps,
		MinQuantizer:      4,
		MaxQuantizer:      8,
		Deadline:          DeadlineGoodQuality,
		CpuUsed:           3,
		KeyFrameInterval:  999,
	}
	sources := make([]Image, frames)
	for i := range sources {
		sources[i] = encoderValidationPanningFrame(width, height, i)
	}

	govpxTrace := captureGovpxEncoderTrace(t, opts, sources)
	libvpxTrace := captureLibvpxEncoderTrace(t, vpxencOracle, "recode-vbr-tight", opts, targetKbps, sources, []string{"--end-usage=vbr"})

	gRows, err := coracle.TraceRowsByFrame(govpxTrace, "recode")
	if err != nil {
		t.Fatalf("parse govpx recode rows: %v", err)
	}
	lRows, err := coracle.TraceRowsByFrame(libvpxTrace, "recode")
	if err != nil {
		t.Fatalf("parse libvpx recode rows: %v", err)
	}
	if len(gRows) == 0 && len(lRows) == 0 {
		t.Skipf("no recode rows on either side; fixture needs tightening")
	}

	asymmetric := 0
	matched := 0
	// Build the union of frame indices.
	seen := make(map[int64]struct{}, len(gRows)+len(lRows))
	for fi := range gRows {
		seen[fi] = struct{}{}
	}
	for fi := range lRows {
		seen[fi] = struct{}{}
	}
	frameIndices := make([]int64, 0, len(seen))
	for fi := range seen {
		frameIndices = append(frameIndices, fi)
	}
	// Sort for stable output.
	slices.Sort(frameIndices)

	for _, fi := range frameIndices {
		g, gOK := gRows[fi]
		l, lOK := lRows[fi]
		if gOK && !lOK {
			t.Logf("recode asymmetry at frame %d: govpx emitted but libvpx did not (govpx reason=%v final_q=%v loop_count=%v)", fi, g["reason"], g["final_q"], g["loop_count"])
			asymmetric++
			continue
		}
		if lOK && !gOK {
			t.Logf("recode asymmetry at frame %d: libvpx emitted but govpx did not (libvpx reason=%v final_q=%v loop_count=%v)", fi, l["reason"], l["final_q"], l["loop_count"])
			asymmetric++
			continue
		}
		// Both present.
		matched++
		gReason, _ := g["reason"].(string)
		lReason, _ := l["reason"].(string)
		if gReason != lReason {
			t.Errorf("frame %d recode reason govpx=%q libvpx=%q", fi, gReason, lReason)
		}
		gFinal := coracle.TraceFloat(g["final_q"])
		lFinal := coracle.TraceFloat(l["final_q"])
		if math.Abs(gFinal-lFinal) > 2 {
			t.Errorf("frame %d recode final_q govpx=%v libvpx=%v exceeds 2 qindex", fi, gFinal, lFinal)
		}
		gLoop := coracle.TraceFloat(g["loop_count"])
		lLoop := coracle.TraceFloat(l["loop_count"])
		if math.Abs(gLoop-lLoop) > 1 {
			t.Errorf("frame %d recode loop_count govpx=%v libvpx=%v exceeds 1", fi, gLoop, lLoop)
		}
	}
	t.Logf("recode rows matched=%d asymmetric=%d (govpx_total=%d libvpx_total=%d)", matched, asymmetric, len(gRows), len(lRows))
}
