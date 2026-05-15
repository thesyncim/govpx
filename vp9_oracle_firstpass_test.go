//go:build govpx_oracle_trace

package govpx

import (
	"encoding/binary"
	"math"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/coracle"
)

type vp9OracleFirstPassStats struct {
	Frame            float64
	Weight           float64
	IntraError       float64
	CodedError       float64
	SRCodedError     float64
	FrameNoiseEnergy float64
	PcntInter        float64
	PcntMotion       float64
	PcntSecondRef    float64
	PcntNeutral      float64
	PcntIntraLow     float64
	PcntIntraHigh    float64
	IntraSkipPct     float64
	IntraSmoothPct   float64
	InactiveZoneRows float64
	InactiveZoneCols float64
	MVr              float64
	MVrAbs           float64
	MVc              float64
	MVcAbs           float64
	MVrv             float64
	MVcv             float64
	MVInOutCount     float64
	Duration         float64
	Count            float64
	NewMVCount       float64
	SpatialLayerID   int64
	IsTotal          bool
}

func TestVP9OracleFirstPassStatsSchemaAndTotals(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run VP9 first-pass stats oracle")
	}
	requireVP9VpxencOracle(t)

	const width, height, frames = 320, 180, 6
	var raw []byte
	for frame := range frames {
		raw = appendVP9YCbCrI420(raw,
			newVP9PanningYCbCrForRateTest(width, height, frame))
	}
	data, diag, err := coracle.VpxencVP9FirstPassStatsI420(raw, width, height,
		frames, "--target-bitrate=900")
	if err != nil {
		t.Fatalf("VpxencVP9FirstPassStatsI420 failed: %v\n%s", err, diag)
	}
	stats := parseVP9OracleFirstPassStats(t, data)
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
	var accumulated vp9OracleFirstPassStats
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
		accumulateVP9OracleFirstPassStats(&accumulated, row)
	}
	assertVP9FirstPassClose(t, "total frame", accumulated.Frame, total.Frame)
	assertVP9FirstPassClose(t, "total weight", accumulated.Weight, total.Weight)
	assertVP9FirstPassClose(t, "total intra_error", accumulated.IntraError,
		total.IntraError)
	assertVP9FirstPassClose(t, "total coded_error", accumulated.CodedError,
		total.CodedError)
	assertVP9FirstPassClose(t, "total sr_coded_error", accumulated.SRCodedError,
		total.SRCodedError)
	assertVP9FirstPassClose(t, "total count", accumulated.Count, total.Count)
	// VP9 zero_stats seeds the terminal total duration to 1 before folding
	// frame rows, unlike the other additive double fields.
	assertVP9FirstPassClose(t, "total duration", accumulated.Duration+1,
		total.Duration)
}

func parseVP9OracleFirstPassStats(t *testing.T, data []byte) []vp9OracleFirstPassStats {
	t.Helper()
	const fields = 27
	const packetSize = fields * 8
	if len(data) == 0 || len(data)%packetSize != 0 {
		t.Fatalf("VP9 first-pass stats size = %d, want non-zero multiple of %d",
			len(data), packetSize)
	}
	stats := make([]vp9OracleFirstPassStats, len(data)/packetSize)
	for i := range stats {
		offset := i * packetSize
		readFloat := func(field int) float64 {
			start := offset + field*8
			return math.Float64frombits(binary.LittleEndian.Uint64(
				data[start : start+8]))
		}
		readInt := func(field int) int64 {
			start := offset + field*8
			return int64(binary.LittleEndian.Uint64(data[start : start+8]))
		}
		stats[i] = vp9OracleFirstPassStats{
			Frame:            readFloat(0),
			Weight:           readFloat(1),
			IntraError:       readFloat(2),
			CodedError:       readFloat(3),
			SRCodedError:     readFloat(4),
			FrameNoiseEnergy: readFloat(5),
			PcntInter:        readFloat(6),
			PcntMotion:       readFloat(7),
			PcntSecondRef:    readFloat(8),
			PcntNeutral:      readFloat(9),
			PcntIntraLow:     readFloat(10),
			PcntIntraHigh:    readFloat(11),
			IntraSkipPct:     readFloat(12),
			IntraSmoothPct:   readFloat(13),
			InactiveZoneRows: readFloat(14),
			InactiveZoneCols: readFloat(15),
			MVr:              readFloat(16),
			MVrAbs:           readFloat(17),
			MVc:              readFloat(18),
			MVcAbs:           readFloat(19),
			MVrv:             readFloat(20),
			MVcv:             readFloat(21),
			MVInOutCount:     readFloat(22),
			Duration:         readFloat(23),
			Count:            readFloat(24),
			NewMVCount:       readFloat(25),
			SpatialLayerID:   readInt(26),
			IsTotal:          i == len(stats)-1,
		}
	}
	return stats
}

func accumulateVP9OracleFirstPassStats(dst *vp9OracleFirstPassStats, row vp9OracleFirstPassStats) {
	if dst == nil {
		return
	}
	dst.Frame += row.Frame
	dst.Weight += row.Weight
	dst.IntraError += row.IntraError
	dst.CodedError += row.CodedError
	dst.SRCodedError += row.SRCodedError
	dst.FrameNoiseEnergy += row.FrameNoiseEnergy
	dst.PcntInter += row.PcntInter
	dst.PcntMotion += row.PcntMotion
	dst.PcntSecondRef += row.PcntSecondRef
	dst.PcntNeutral += row.PcntNeutral
	dst.PcntIntraLow += row.PcntIntraLow
	dst.PcntIntraHigh += row.PcntIntraHigh
	dst.IntraSkipPct += row.IntraSkipPct
	dst.IntraSmoothPct += row.IntraSmoothPct
	dst.InactiveZoneRows += row.InactiveZoneRows
	dst.InactiveZoneCols += row.InactiveZoneCols
	dst.MVr += row.MVr
	dst.MVrAbs += row.MVrAbs
	dst.MVc += row.MVc
	dst.MVcAbs += row.MVcAbs
	dst.MVrv += row.MVrv
	dst.MVcv += row.MVcv
	dst.MVInOutCount += row.MVInOutCount
	dst.Duration += row.Duration
	dst.Count += row.Count
	dst.NewMVCount += row.NewMVCount
	dst.SpatialLayerID = row.SpatialLayerID
}

func assertVP9FirstPassClose(t *testing.T, field string, got, want float64) {
	t.Helper()
	const absTol = 1e-9
	if math.Abs(got-want) > absTol {
		t.Fatalf("%s = %.12f, want %.12f", field, got, want)
	}
}
