//go:build goexperiment.simd && amd64 && !purego

package dsp

import (
	"testing"

	"github.com/thesyncim/govpx/internal/pixel"
)

func TestDCPairInPlaceDisabledState(t *testing.T) {
	previous := pixel.Enabled
	pixel.Enabled = false
	defer func() { pixel.Enabled = previous }()
	dcPairInPlaceRunStates(t)
}
