//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"bytes"
	"testing"
)

func TestMacroInternalAMDDisabled(t *testing.T) {
	old := Enabled
	Enabled = false
	defer func() { Enabled = old }()
	p := bytes.Repeat([]byte{123}, 320)
	before := bytes.Clone(p)
	for _, shape := range [][2]int{{16, 1}, {1, 20}} {
		if LoopFilterMacroInternal16(p, shape[0], shape[1], 64, 32, 9, 0) || !bytes.Equal(p, before) {
			t.Fatal("disabled backend accepted or wrote")
		}
	}
}

func TestMacroInternalAMDVerticalFlatSelected(t *testing.T) {
	if !Enabled {
		t.Skip("SIMD unavailable")
	}
	p := bytes.Repeat([]byte{123}, 320)
	before := bytes.Clone(p)
	if !LoopFilterMacroInternal16(p, 1, 20, 64, 32, 9, 0) || !bytes.Equal(p, before) {
		t.Fatal("vertical window declined or flat pixels changed")
	}
}
