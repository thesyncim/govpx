package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
)

func TestFinalizeVP9FirstPassStats(t *testing.T) {
	rows := []VP9FirstPassFrameStats{
		{
			Frame:            0,
			Weight:           1.5,
			IntraError:       10,
			CodedError:       8,
			SRCodedError:     7,
			FrameNoiseEnergy: 2,
			PcntInter:        0.25,
			PcntMotion:       0.125,
			PcntSecondRef:    0.0625,
			PcntNeutral:      0.5,
			PcntIntraLow:     0.25,
			PcntIntraHigh:    0.75,
			IntraSkipPct:     0.125,
			IntraSmoothPct:   0.375,
			InactiveZoneRows: 1,
			InactiveZoneCols: 2,
			MVr:              3,
			MVrAbs:           4,
			MVc:              -5,
			MVcAbs:           6,
			MVrv:             7,
			MVcv:             8,
			MVInOutCount:     0.25,
			Duration:         1,
			Count:            1,
			NewMVCount:       2,
			SpatialLayerID:   0,
		},
		{
			Frame:            1,
			Weight:           2.5,
			IntraError:       11,
			CodedError:       9,
			SRCodedError:     8,
			FrameNoiseEnergy: 3,
			PcntInter:        0.5,
			PcntMotion:       0.25,
			PcntSecondRef:    0.125,
			PcntNeutral:      0.25,
			PcntIntraLow:     0.5,
			PcntIntraHigh:    0.5,
			IntraSkipPct:     0.25,
			IntraSmoothPct:   0.125,
			InactiveZoneRows: 3,
			InactiveZoneCols: 4,
			MVr:              5,
			MVrAbs:           6,
			MVc:              -7,
			MVcAbs:           8,
			MVrv:             9,
			MVcv:             10,
			MVInOutCount:     -0.125,
			Duration:         2,
			Count:            1,
			NewMVCount:       3,
			SpatialLayerID:   2,
		},
	}
	finalized := FinalizeVP9FirstPassStats(rows)
	if got, want := len(finalized), 3; got != want {
		t.Fatalf("FinalizeVP9FirstPassStats len = %d, want %d", got, want)
	}
	total := finalized[2]
	if !total.IsTotal {
		t.Fatal("total row IsTotal=false, want true")
	}
	if total.Frame != 1 || total.Weight != 4 || total.IntraError != 21 ||
		total.CodedError != 17 || total.SRCodedError != 15 ||
		total.FrameNoiseEnergy != 5 || total.PcntInter != 0.75 ||
		total.PcntMotion != 0.375 || total.PcntSecondRef != 0.1875 ||
		total.PcntNeutral != 0.75 || total.PcntIntraLow != 0.75 ||
		total.PcntIntraHigh != 1.25 || total.IntraSkipPct != 0.375 ||
		total.IntraSmoothPct != 0.5 || total.InactiveZoneRows != 4 ||
		total.InactiveZoneCols != 6 || total.MVr != 8 ||
		total.MVrAbs != 10 || total.MVc != -12 || total.MVcAbs != 14 ||
		total.MVrv != 16 || total.MVcv != 18 ||
		total.MVInOutCount != 0.125 || total.Duration != 3 ||
		total.Count != 2 || total.NewMVCount != 5 ||
		total.SpatialLayerID != 2 {
		t.Fatalf("VP9 first-pass total = %+v", total)
	}
	if got := FinalizeVP9FirstPassStats(finalized); len(got) != len(finalized) {
		t.Fatalf("FinalizeVP9FirstPassStats idempotent len = %d, want %d",
			len(got), len(finalized))
	}
}

func TestVP9EncoderCollectFirstPassStatsPopulatesMotionFields(t *testing.T) {
	const width, height = 64, 64
	enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:  width,
		Height: height,
		FPS:    30,
	})
	if err != nil {
		t.Fatalf("NewVP9Encoder: %v", err)
	}
	first := vp9test.NewPanningYCbCr(width, height, 0)
	second := vp9test.NewPanningYCbCr(width, height, 1)

	stats0, err := enc.CollectFirstPassStats(first, 0, 1, 0)
	if err != nil {
		t.Fatalf("CollectFirstPassStats[0]: %v", err)
	}
	if stats0.Frame != 0 || stats0.Count != 1 || stats0.Duration != 1 {
		t.Fatalf("stats0 frame/count/duration = %.0f/%.0f/%.0f, want 0/1/1",
			float64(stats0.Frame), stats0.Count, stats0.Duration)
	}
	if stats0.IntraError <= 0 || stats0.CodedError <= 0 ||
		stats0.Weight <= 0 {
		t.Fatalf("stats0 = %+v, want positive errors and weight", stats0)
	}

	stats1, err := enc.CollectFirstPassStats(second, 1, 1, 0)
	if err != nil {
		t.Fatalf("CollectFirstPassStats[1]: %v", err)
	}
	if stats1.Frame != 1 || stats1.Count != 1 {
		t.Fatalf("stats1 frame/count = %.0f/%.0f, want 1/1",
			float64(stats1.Frame), stats1.Count)
	}
	if stats1.PcntInter <= 0 || stats1.PcntMotion <= 0 ||
		(stats1.MVrAbs == 0 && stats1.MVcAbs == 0) ||
		stats1.NewMVCount <= 0 {
		t.Fatalf("stats1 motion = inter %.3f motion %.3f mvr_abs %.3f mvc_abs %.3f new %.3f, want motion signal",
			stats1.PcntInter, stats1.PcntMotion, stats1.MVrAbs,
			stats1.MVcAbs, stats1.NewMVCount)
	}
	if stats1.CodedError > stats1.IntraError {
		t.Fatalf("stats1 coded error = %.2f, want <= intra %.2f",
			stats1.CodedError, stats1.IntraError)
	}
}
