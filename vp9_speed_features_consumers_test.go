package govpx

import (
	"testing"

	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestVP9InterReferenceFramesEnabledUseAltrefOnepass(t *testing.T) {
	e := &VP9Encoder{}
	got := e.vp9InterReferenceFramesEnabled()
	if len(got) != 3 || got[2] != vp9dec.AltrefFrame {
		t.Fatalf("default refs = %v, want LAST/GOLDEN/ALTREF", got)
	}

	e.sf.UseNonrdPickMode = 1
	e.sf.UseAltrefOnepass = 0
	got = e.vp9InterReferenceFramesEnabled()
	if len(got) != 2 ||
		got[0] != vp9dec.LastFrame ||
		got[1] != vp9dec.GoldenFrame {
		t.Fatalf("nonrd no-altref refs = %v, want LAST/GOLDEN", got)
	}

	e.sf.UseAltrefOnepass = 1
	got = e.vp9InterReferenceFramesEnabled()
	if len(got) != 3 || got[2] != vp9dec.AltrefFrame {
		t.Fatalf("onepass-altref refs = %v, want LAST/GOLDEN/ALTREF", got)
	}
}
