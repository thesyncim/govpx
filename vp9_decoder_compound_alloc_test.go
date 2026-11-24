package govpx

import "testing"

func TestVP9DecoderCompoundGoldenAltrefNewMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
	inter := vp9CompoundInterGoldenAltrefNewMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound golden/altref newmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound golden/altref newmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound golden/altref newmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundSignBiasLayoutsSteadyStateAlloc(t *testing.T) {
	for _, tc := range []struct {
		name  string
		frame func(*testing.T) []byte
	}{
		{"fixed-golden", vp9CompoundFixedGoldenSignBiasNewMvFrameForTest},
		{"fixed-last", vp9CompoundFixedLastSignBiasNewMvFrameForTest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder: %v", err)
			}
			seedVP9CompoundTripleRefsForTest(t, d, 64, 64)
			inter := tc.frame(t)
			if err := d.Decode(inter); err != nil {
				t.Fatalf("warm Decode compound %s sign-bias err = %v, want nil",
					tc.name, err)
			}

			allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
				err = d.Decode(inter)
			})
			if err != nil {
				t.Fatalf("Decode compound %s sign-bias err = %v, want nil",
					tc.name, err)
			}
			if allocs != 0 {
				t.Fatalf("compound %s sign-bias steady state: got %v allocs/op, want 0",
					tc.name, allocs)
			}
		})
	}
}

func TestVP9DecoderScaledCompoundInterNearestMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)
	inter := vp9ScaledCompoundInterNearestMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter nearestmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter nearestmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter nearestmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderScaledCompoundInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 128, 128)
	inter := vp9ScaledCompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode scaled compound inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode scaled compound inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("scaled compound inter nearmv steady state: got %v allocs/op, want 0", allocs)
	}
}

func TestVP9DecoderCompoundInterNearMvSteadyStateAlloc(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	seedVP9CompoundMotionRefsForTest(t, d, 64, 64)
	inter := vp9CompoundInterNearMvFrameForTest(t)
	if err := d.Decode(inter); err != nil {
		t.Fatalf("warm Decode compound inter nearmv err = %v, want nil", err)
	}

	allocs := testing.AllocsPerRun(vp9SteadyStateAllocRuns, func() {
		err = d.Decode(inter)
	})
	if err != nil {
		t.Fatalf("Decode compound inter nearmv err = %v, want nil", err)
	}
	if allocs != 0 {
		t.Fatalf("compound inter nearmv steady state: got %v allocs/op, want 0", allocs)
	}
}
