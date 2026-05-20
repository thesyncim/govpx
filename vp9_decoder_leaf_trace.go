//go:build govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

type vp9DecodedLeafTrace struct {
	KeyFrame      bool
	IntraOnly     bool
	MIRow         int
	MICol         int
	BSize         int
	Mode          int
	UvMode        int
	Ref0          int
	Ref1          int
	Mv0Row        int
	Mv0Col        int
	Mv1Row        int
	Mv1Col        int
	InterpFilter  int
	TxSize        int
	Skip          int
	SegmentID     int
	TxBlockCount  int
	TokenCount    int
	EOBTotal      int
	QCoeffNonZero int
	QCoeffAbsSum  int
}

func vp9DecodedLeafTraceForMI(hdr *vp9dec.UncompressedHeader, miRow, miCol int,
	mi *vp9dec.NeighborMi,
) vp9DecodedLeafTrace {
	if hdr == nil || mi == nil {
		return vp9DecodedLeafTrace{}
	}
	return vp9DecodedLeafTrace{
		KeyFrame:     hdr.FrameType == common.KeyFrame,
		IntraOnly:    hdr.IntraOnly,
		MIRow:        miRow,
		MICol:        miCol,
		BSize:        int(mi.SbType),
		Mode:         int(mi.Mode),
		Ref0:         int(mi.RefFrame[0]),
		Ref1:         int(mi.RefFrame[1]),
		Mv0Row:       int(mi.Mv[0].Row),
		Mv0Col:       int(mi.Mv[0].Col),
		Mv1Row:       int(mi.Mv[1].Row),
		Mv1Col:       int(mi.Mv[1].Col),
		InterpFilter: int(mi.InterpFilter),
		TxSize:       int(mi.TxSize),
		Skip:         int(mi.Skip),
		SegmentID:    int(mi.SegmentID),
	}
}

func vp9DecodedLeafTraceSetUVMode(trace *vp9DecodedLeafTrace, uvMode common.PredictionMode) {
	trace.UvMode = int(uvMode)
}

func vp9DecodedLeafTraceAddCoeffSummary(trace *vp9DecodedLeafTrace, eob, maxEOB int, coeffs []int16) {
	trace.TxBlockCount++
	trace.EOBTotal += eob
	trace.TokenCount += eob
	if eob < maxEOB {
		trace.TokenCount++
	}
	nonZero, absSum := vp9DecodedCoeffSummary(coeffs)
	trace.QCoeffNonZero += nonZero
	trace.QCoeffAbsSum += absSum
}

func vp9DecodedLeafTraceSetSkip(trace *vp9DecodedLeafTrace, skip uint8) {
	trace.Skip = int(skip)
}

func vp9DecodedCoeffSummary(coeffs []int16) (nonZero, absSum int) {
	for _, coeff := range coeffs {
		if coeff == 0 {
			continue
		}
		nonZero++
		v := int(coeff)
		if v < 0 {
			v = -v
		}
		absSum += v
	}
	return nonZero, absSum
}
