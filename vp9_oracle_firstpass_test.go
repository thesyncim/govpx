//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"image"
	"math"
	"os"
	"testing"
)

func TestVP9OracleFirstPassStatsSchemaAndTotals(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 first-pass stats oracle")
	vp9test.RequireVpxenc(t)

	const width, height, frames = 320, 180, 6
	sources := make([]*image.YCbCr, frames)
	for frame := range frames {
		sources[frame] = vp9test.NewPanningYCbCr(width, height, frame)
	}
	stats := vp9test.VpxencFirstPassStats(t, sources, "--target-bitrate=900")
	if got, want := len(stats), frames+1; got != want {
		t.Fatalf("VP9 first-pass stats len = %d, want %d", got, want)
	}
	total := stats[len(stats)-1]
	if !total.IsTotal {
		t.Fatal("last VP9 first-pass stats row is not marked total")
	}
	if total.Count != frames {
		t.Fatalf("VP9 first-pass total count = %.0f, want %d",
			total.Count, frames)
	}
	var accumulated vp9test.FirstPassStats
	for i := range frames {
		row := stats[i]
		if row.IsTotal {
			t.Fatalf("VP9 first-pass row %d unexpectedly marked total", i)
		}
		if row.Frame != float64(i) || row.Count != 1 {
			t.Fatalf("VP9 first-pass row %d frame/count = %.0f/%.0f, want %d/1",
				i, row.Frame, row.Count, i)
		}
		if row.IntraError <= 0 || row.CodedError <= 0 {
			t.Fatalf("VP9 first-pass row %d errors = intra %.2f coded %.2f, want positive",
				i, row.IntraError, row.CodedError)
		}
		vp9test.AccumulateFirstPassStats(&accumulated, row)
	}
	vp9test.AssertFirstPassClose(t, "total frame", accumulated.Frame, total.Frame)
	vp9test.AssertFirstPassClose(t, "total weight", accumulated.Weight, total.Weight)
	vp9test.AssertFirstPassClose(t, "total intra_error", accumulated.IntraError,
		total.IntraError)
	vp9test.AssertFirstPassClose(t, "total coded_error", accumulated.CodedError,
		total.CodedError)
	vp9test.AssertFirstPassClose(t, "total sr_coded_error", accumulated.SRCodedError,
		total.SRCodedError)
	vp9test.AssertFirstPassClose(t, "total count", accumulated.Count, total.Count)
	// VP9 zero_stats seeds the terminal total duration to 1 before folding
	// frame rows, unlike the other additive double fields.
	vp9test.AssertFirstPassClose(t, "total duration", accumulated.Duration+1,
		total.Duration)
}

func TestVP9OracleFirstPassStatsCompare(t *testing.T) {
	vp9test.RequireOracle(t, "VP9 first-pass stats oracle")
	vp9test.RequireVpxenc(t)

	cases := []struct {
		name       string
		width      int
		height     int
		frames     int
		targetKbps int
	}{
		{name: "panning-64x64", width: 64, height: 64, frames: 6, targetKbps: 300},
		{name: "panning-320x180", width: 320, height: 180, frames: 6, targetKbps: 900},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc, err := NewVP9Encoder(VP9EncoderOptions{
				Width:  tc.width,
				Height: tc.height,
				FPS:    30,
			})
			if err != nil {
				t.Fatalf("NewVP9Encoder: %v", err)
			}

			govpxRows := make([]VP9FirstPassFrameStats, tc.frames)
			sources := make([]*image.YCbCr, tc.frames)
			for frame := range tc.frames {
				src := vp9test.NewPanningYCbCr(tc.width, tc.height, frame)
				sources[frame] = src
				govpxRows[frame], err = enc.CollectFirstPassStats(src,
					uint64(frame), 1, 0)
				if err != nil {
					t.Fatalf("CollectFirstPassStats[%d]: %v", frame, err)
				}
			}
			govpxStats := FinalizeVP9FirstPassStats(govpxRows)
			libvpxStats := vp9test.VpxencFirstPassStats(t, sources,
				"--target-bitrate="+fmt.Sprint(tc.targetKbps))
			if len(govpxStats) != len(libvpxStats) {
				t.Fatalf("VP9 first-pass rows = %d, want %d",
					len(govpxStats), len(libvpxStats))
			}

			summary := summarizeVP9FirstPassComparison(govpxStats, libvpxStats)
			t.Logf("VP9 first-pass comparison %s: %s", tc.name, summary)
			t.Logf("VP9 first-pass rows %s:\n%s", tc.name,
				formatVP9FirstPassComparisonRows(govpxStats, libvpxStats))
			assertVP9FirstPassComparisonShape(t, govpxStats, libvpxStats)
			if os.Getenv("GOVPX_VP9_FIRSTPASS_STRICT") == "1" {
				assertVP9FirstPassComparisonStrict(t, summary)
			}
		})
	}
}

type vp9FirstPassComparisonSummary struct {
	MaxIntraRel     float64
	MaxCodedRel     float64
	MaxSRCodedRel   float64
	MaxPcntInter    float64
	MaxPcntMotion   float64
	MaxPcntSecond   float64
	MaxPcntNeutral  float64
	MaxNewMV        float64
	MaxMVAbs        float64
	MaxWeightRel    float64
	MaxNoiseEnergy  float64
	MaxInactiveZone float64
}

func (s vp9FirstPassComparisonSummary) String() string {
	return fmt.Sprintf("max_rel(intra=%.4f coded=%.4f sr=%.4f weight=%.4f) max_abs(inter=%.4f motion=%.4f second=%.4f neutral=%.4f newmv=%.4f mv=%.4f noise=%.4f inactive=%.4f)",
		s.MaxIntraRel, s.MaxCodedRel, s.MaxSRCodedRel, s.MaxWeightRel,
		s.MaxPcntInter, s.MaxPcntMotion, s.MaxPcntSecond, s.MaxPcntNeutral,
		s.MaxNewMV, s.MaxMVAbs, s.MaxNoiseEnergy, s.MaxInactiveZone)
}

func summarizeVP9FirstPassComparison(govpxStats []VP9FirstPassFrameStats, libvpxStats []vp9test.FirstPassStats) vp9FirstPassComparisonSummary {
	var s vp9FirstPassComparisonSummary
	n := min(len(govpxStats), len(libvpxStats))
	for i := range n {
		g := govpxStats[i]
		l := libvpxStats[i]
		s.MaxIntraRel = max(s.MaxIntraRel, vp9FirstPassRelDelta(g.IntraError,
			l.IntraError))
		s.MaxCodedRel = max(s.MaxCodedRel, vp9FirstPassRelDelta(g.CodedError,
			l.CodedError))
		s.MaxSRCodedRel = max(s.MaxSRCodedRel,
			vp9FirstPassRelDelta(g.SRCodedError, l.SRCodedError))
		s.MaxWeightRel = max(s.MaxWeightRel, vp9FirstPassRelDelta(g.Weight,
			l.Weight))
		s.MaxPcntInter = max(s.MaxPcntInter, math.Abs(g.PcntInter-l.PcntInter))
		s.MaxPcntMotion = max(s.MaxPcntMotion, math.Abs(g.PcntMotion-l.PcntMotion))
		s.MaxPcntSecond = max(s.MaxPcntSecond,
			math.Abs(g.PcntSecondRef-l.PcntSecondRef))
		s.MaxPcntNeutral = max(s.MaxPcntNeutral,
			math.Abs(g.PcntNeutral-l.PcntNeutral))
		s.MaxNewMV = max(s.MaxNewMV, math.Abs(g.NewMVCount-l.NewMVCount))
		s.MaxMVAbs = max(s.MaxMVAbs, math.Abs(g.MVr-l.MVr))
		s.MaxMVAbs = max(s.MaxMVAbs, math.Abs(g.MVrAbs-l.MVrAbs))
		s.MaxMVAbs = max(s.MaxMVAbs, math.Abs(g.MVc-l.MVc))
		s.MaxMVAbs = max(s.MaxMVAbs, math.Abs(g.MVcAbs-l.MVcAbs))
		s.MaxMVAbs = max(s.MaxMVAbs, math.Abs(g.MVrv-l.MVrv))
		s.MaxMVAbs = max(s.MaxMVAbs, math.Abs(g.MVcv-l.MVcv))
		s.MaxMVAbs = max(s.MaxMVAbs, math.Abs(g.MVInOutCount-l.MVInOutCount))
		s.MaxNoiseEnergy = max(s.MaxNoiseEnergy,
			math.Abs(g.FrameNoiseEnergy-l.FrameNoiseEnergy))
		s.MaxInactiveZone = max(s.MaxInactiveZone,
			math.Abs(g.InactiveZoneRows-l.InactiveZoneRows))
		s.MaxInactiveZone = max(s.MaxInactiveZone,
			math.Abs(g.InactiveZoneCols-l.InactiveZoneCols))
	}
	return s
}

func formatVP9FirstPassComparisonRows(govpxStats []VP9FirstPassFrameStats, libvpxStats []vp9test.FirstPassStats) string {
	var b bytes.Buffer
	fmt.Fprintln(&b, "row,total,govpx_frame,libvpx_frame,govpx_intra,libvpx_intra,intra_rel,govpx_coded,libvpx_coded,coded_rel,govpx_sr,libvpx_sr,sr_rel,govpx_inter,libvpx_inter,govpx_motion,libvpx_motion,govpx_second,libvpx_second,govpx_newmv,libvpx_newmv")
	n := min(len(govpxStats), len(libvpxStats))
	for i := range n {
		g := govpxStats[i]
		l := libvpxStats[i]
		fmt.Fprintf(&b, "%d,%t,%d,%.0f,%.0f,%.0f,%.4f,%.0f,%.0f,%.4f,%.0f,%.0f,%.4f,%.4f,%.4f,%.4f,%.4f,%.4f,%.4f,%.2f,%.2f\n",
			i, g.IsTotal || l.IsTotal, g.Frame, l.Frame,
			g.IntraError, l.IntraError,
			vp9FirstPassRelDelta(g.IntraError, l.IntraError),
			g.CodedError, l.CodedError,
			vp9FirstPassRelDelta(g.CodedError, l.CodedError),
			g.SRCodedError, l.SRCodedError,
			vp9FirstPassRelDelta(g.SRCodedError, l.SRCodedError),
			g.PcntInter, l.PcntInter, g.PcntMotion, l.PcntMotion,
			g.PcntSecondRef, l.PcntSecondRef, g.NewMVCount, l.NewMVCount)
	}
	return b.String()
}

func assertVP9FirstPassComparisonShape(t *testing.T, govpxStats []VP9FirstPassFrameStats, libvpxStats []vp9test.FirstPassStats) {
	t.Helper()
	for i := range govpxStats {
		g := govpxStats[i]
		l := libvpxStats[i]
		if g.IsTotal != l.IsTotal {
			t.Fatalf("row %d IsTotal = %v, want %v", i, g.IsTotal, l.IsTotal)
		}
		if g.IsTotal {
			if l.Count != float64(len(govpxStats)-1) {
				t.Fatalf("VP9 first-pass total count = %.0f, want %d",
					l.Count, len(govpxStats)-1)
			}
			continue
		}
		if g.Frame != uint64(i) || l.Frame != float64(i) ||
			g.Count != 1 || l.Count != 1 {
			t.Fatalf("row %d frame/count = %d/%.0f %.0f/%.0f",
				i, g.Frame, l.Frame, g.Count, l.Count)
		}
		if g.IntraError <= 0 || g.CodedError <= 0 ||
			l.IntraError <= 0 || l.CodedError <= 0 {
			t.Fatalf("row %d errors govpx %.2f/%.2f libvpx %.2f/%.2f, want positive",
				i, g.IntraError, g.CodedError, l.IntraError, l.CodedError)
		}
	}
}

func assertVP9FirstPassComparisonStrict(t *testing.T, s vp9FirstPassComparisonSummary) {
	t.Helper()
	if s.MaxIntraRel > 0.01 || s.MaxCodedRel > 0.01 ||
		s.MaxSRCodedRel > 0.01 || s.MaxWeightRel > 0.01 ||
		s.MaxPcntInter > 1e-12 || s.MaxPcntMotion > 1e-12 ||
		s.MaxPcntSecond > 1e-12 || s.MaxPcntNeutral > 1e-12 ||
		s.MaxNewMV > 1e-12 || s.MaxMVAbs > 1e-12 ||
		s.MaxNoiseEnergy > 1e-12 || s.MaxInactiveZone > 1e-12 {
		t.Fatalf("strict VP9 first-pass comparison drift: %s", s)
	}
}

func vp9FirstPassRelDelta(got, want float64) float64 {
	den := math.Abs(want)
	if den < 1 {
		den = 1
	}
	return math.Abs(got-want) / den
}
