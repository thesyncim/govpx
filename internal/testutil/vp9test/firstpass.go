package vp9test

import (
	"encoding/binary"
	"math"
	"testing"
)

type FirstPassStats struct {
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

func ParseFirstPassStats(t testing.TB, data []byte) []FirstPassStats {
	t.Helper()
	const fields = 27
	const packetSize = fields * 8
	if len(data) == 0 || len(data)%packetSize != 0 {
		t.Fatalf("VP9 first-pass stats size = %d, want non-zero multiple of %d",
			len(data), packetSize)
	}
	stats := make([]FirstPassStats, len(data)/packetSize)
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
		stats[i] = FirstPassStats{
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

func AccumulateFirstPassStats(dst *FirstPassStats, row FirstPassStats) {
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

func AssertFirstPassClose(t testing.TB, field string, got, want float64) {
	t.Helper()
	const absTol = 1e-9
	if math.Abs(got-want) > absTol {
		t.Fatalf("%s = %.12f, want %.12f", field, got, want)
	}
}
