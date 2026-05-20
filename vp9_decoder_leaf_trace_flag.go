//go:build !govpx_oracle_trace

package govpx

import (
	"github.com/thesyncim/govpx/internal/vp9/common"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

const vp9DecodedLeafTraceBuild = false

type vp9DecodedLeafTrace struct{}

type vp9DecodedLeafTraceState struct{}

func (d *VP9Decoder) disableVP9DecodedLeafTrace() {}

func (d *VP9Decoder) resetVP9DecodedLeafTrace() {}

func (d *VP9Decoder) vp9DecodedLeafTraceActive() bool { return false }

func (d *VP9Decoder) emitVP9DecodedLeafTrace(vp9DecodedLeafTrace) {}

func vp9DecodedLeafTraceForMI(*vp9dec.UncompressedHeader, int, int, *vp9dec.NeighborMi) vp9DecodedLeafTrace {
	return vp9DecodedLeafTrace{}
}

func vp9DecodedLeafTraceSetUVMode(*vp9DecodedLeafTrace, common.PredictionMode) {}

func vp9DecodedLeafTraceAddCoeffSummary(*vp9DecodedLeafTrace, int, int, []int16) {}

func vp9DecodedLeafTraceSetSkip(*vp9DecodedLeafTrace, uint8) {}
