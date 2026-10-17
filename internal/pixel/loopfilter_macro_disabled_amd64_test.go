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
	if LoopFilterMacroInternal16(p, 16, 1, 64, 32, 9, 0) || !bytes.Equal(p, before) {
		t.Fatal("disabled backend accepted or wrote")
	}
}

func TestMacroInternalAMDVerticalDeclines(t *testing.T) {
	p := bytes.Repeat([]byte{123}, 320)
	before := bytes.Clone(p)
	if LoopFilterMacroInternal16(p, 1, 20, 64, 32, 9, 0) || !bytes.Equal(p, before) {
		t.Fatal("vertical window accepted or changed")
	}
}
