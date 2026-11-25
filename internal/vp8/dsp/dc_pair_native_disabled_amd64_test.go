//go:build goexperiment.simd && amd64 && !purego

package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"testing"
)

func TestDCPairInPlaceDisabledState(t *testing.T) {
	previous := pixel.Enabled
	pixel.Enabled = false
	defer func() { pixel.Enabled = previous }()
	dcPairInPlaceRunStates(t)
}

func TestDCPairNativeDisabledState(t *testing.T) {
	saved := pixel.Enabled
	pixel.Enabled = false
	defer func() { pixel.Enabled = saved }()
	dcPairNativeRunStates(t)
}
