package gopvx

import (
	"errors"
	"testing"
)

func TestTemporalLayeringPatternsMatchLibvpxExample(t *testing.T) {
	f := temporalTestFlags
	tests := []struct {
		mode            TemporalLayeringMode
		layers          int
		periodicity     int
		flagPeriodicity int
		rateDecimator   []int
		layerID         []int
		flags           []EncodeFlags
	}{
		{
			mode:            TemporalLayeringOneLayer,
			layers:          1,
			periodicity:     1,
			flagPeriodicity: 1,
			rateDecimator:   []int{1},
			layerID:         []int{0},
			flags:           []EncodeFlags{0},
		},
		{
			mode:            TemporalLayeringTwoLayers,
			layers:          2,
			periodicity:     2,
			flagPeriodicity: 2,
			rateDecimator:   []int{2, 1},
			layerID:         []int{0, 1},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoReferenceGolden, EncodeNoReferenceAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateLast, EncodeNoReferenceAltRef),
			},
		},
		{
			mode:            TemporalLayeringTwoLayersThreeFrame,
			layers:          2,
			periodicity:     3,
			flagPeriodicity: 3,
			rateDecimator:   []int{3, 1},
			layerID:         []int{0, 1, 1},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
			},
		},
		{
			mode:            TemporalLayeringThreeLayersSixFrame,
			layers:          3,
			periodicity:     6,
			flagPeriodicity: 6,
			rateDecimator:   []int{6, 3, 1},
			layerID:         []int{0, 2, 2, 1, 2, 2},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateGolden, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateLast),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateLast),
			},
		},
		{
			mode:            TemporalLayeringThreeLayersNoInterLayerPrediction,
			layers:          3,
			periodicity:     4,
			flagPeriodicity: 4,
			rateDecimator:   []int{4, 2, 1},
			layerID:         []int{0, 2, 1, 2},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
			},
		},
		{
			mode:            TemporalLayeringThreeLayersLayerOnePrediction,
			layers:          3,
			periodicity:     4,
			flagPeriodicity: 4,
			rateDecimator:   []int{4, 2, 1},
			layerID:         []int{0, 2, 1, 2},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
			},
		},
		{
			mode:            TemporalLayeringThreeLayers,
			layers:          3,
			periodicity:     4,
			flagPeriodicity: 4,
			rateDecimator:   []int{4, 2, 1},
			layerID:         []int{0, 2, 1, 2},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden),
			},
		},
		{
			mode:            TemporalLayeringFiveLayers,
			layers:          5,
			periodicity:     16,
			flagPeriodicity: 16,
			rateDecimator:   []int{16, 8, 4, 2, 1},
			layerID:         []int{0, 4, 3, 4, 2, 4, 3, 4, 1, 4, 3, 4, 2, 4, 3, 4},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateGolden),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceLast, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateGolden),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceLast, EncodeNoReferenceGolden),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateGolden),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceLast, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateGolden),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
			},
		},
		{
			mode:            TemporalLayeringTwoLayersWithSync,
			layers:          2,
			periodicity:     2,
			flagPeriodicity: 8,
			rateDecimator:   []int{2, 1},
			layerID:         []int{0, 1},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoReferenceGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceGolden, EncodeNoUpdateLast, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceGolden, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoReferenceGolden, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoReferenceGolden, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateLast),
			},
		},
		{
			mode:            TemporalLayeringThreeLayersWithSync,
			layers:          3,
			periodicity:     4,
			flagPeriodicity: 8,
			rateDecimator:   []int{4, 2, 1},
			layerID:         []int{0, 2, 1, 2},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateGolden),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden),
				f(EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateAltRef),
				f(EncodeNoUpdateLast, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
			},
		},
		{
			mode:            TemporalLayeringThreeLayersAltRefWithSync,
			layers:          3,
			periodicity:     4,
			flagPeriodicity: 8,
			rateDecimator:   []int{4, 2, 1},
			layerID:         []int{0, 2, 1, 2},
			flags: []EncodeFlags{
				f(EncodeForceKeyFrame, EncodeNoUpdateAltRef, EncodeNoReferenceGolden),
				f(EncodeNoReferenceGolden, EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoReferenceGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoReferenceGolden),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
			},
		},
		{
			mode:            TemporalLayeringThreeLayersOneReference,
			layers:          3,
			periodicity:     4,
			flagPeriodicity: 4,
			rateDecimator:   []int{4, 2, 1},
			layerID:         []int{0, 2, 1, 2},
			flags: []EncodeFlags{
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateGolden, EncodeNoUpdateAltRef),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateGolden),
				f(EncodeNoReferenceGolden, EncodeNoReferenceAltRef, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoReferenceLast, EncodeNoReferenceAltRef, EncodeNoUpdateLast, EncodeNoUpdateGolden),
			},
		},
		{
			mode:            TemporalLayeringThreeLayersNoSync,
			layers:          3,
			periodicity:     4,
			flagPeriodicity: 8,
			rateDecimator:   []int{4, 2, 1},
			layerID:         []int{0, 2, 1, 2},
			flags: []EncodeFlags{
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoReferenceGolden),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoReferenceGolden),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateAltRef, EncodeNoUpdateLast),
				f(EncodeNoUpdateGolden, EncodeNoUpdateAltRef, EncodeNoUpdateLast),
			},
		},
	}

	for _, tc := range tests {
		pattern, ok := temporalLayeringPattern(tc.mode)
		if !ok {
			t.Fatalf("mode %d not available", tc.mode)
		}
		if pattern.Layers != tc.layers || pattern.Periodicity != tc.periodicity || pattern.FlagPeriodicity != tc.flagPeriodicity {
			t.Fatalf("mode %d shape = layers:%d periodicity:%d flag:%d, want %d/%d/%d", tc.mode, pattern.Layers, pattern.Periodicity, pattern.FlagPeriodicity, tc.layers, tc.periodicity, tc.flagPeriodicity)
		}
		assertIntPrefix(t, "rate decimator", pattern.RateDecimator[:], tc.rateDecimator)
		assertIntPrefix(t, "layer id", pattern.LayerID[:], tc.layerID)
		assertFlagPrefix(t, "flags", pattern.Flags[:], tc.flags)
	}
}

func TestTemporalScalabilityConfigValidation(t *testing.T) {
	_, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringMode(13)},
	})
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("invalid mode error = %v, want ErrInvalidConfig", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1200,
		TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringFiveLayers},
	})
	if !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("five-layer default bitrate error = %v, want ErrInvalidBitrate", err)
	}

	_, err = NewVP8Encoder(EncoderOptions{
		Width:             16,
		Height:            16,
		FPS:               30,
		RateControlMode:   RateControlCBR,
		TargetBitrateKbps: 1200,
		TemporalScalability: TemporalScalabilityConfig{
			Enabled:                true,
			Mode:                   TemporalLayeringTwoLayers,
			LayerTargetBitrateKbps: [MaxTemporalLayers]int{900, 800},
		},
	})
	if !errors.Is(err, ErrInvalidBitrate) {
		t.Fatalf("non-monotonic bitrate error = %v, want ErrInvalidBitrate", err)
	}
}

func TestTemporalScalabilityDerivesLibvpxVP8RTCBitrates(t *testing.T) {
	two, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1000,
		TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringTwoLayers},
	})
	if err != nil {
		t.Fatalf("two-layer NewVP8Encoder returned error: %v", err)
	}
	if got := two.opts.TemporalScalability.LayerTargetBitrateKbps; got[0] != 600 || got[1] != 1000 {
		t.Fatalf("two-layer bitrates = %v, want 600/1000", got)
	}

	three, err := NewVP8Encoder(EncoderOptions{
		Width:               16,
		Height:              16,
		FPS:                 30,
		RateControlMode:     RateControlCBR,
		TargetBitrateKbps:   1000,
		TemporalScalability: TemporalScalabilityConfig{Enabled: true, Mode: TemporalLayeringThreeLayers},
	})
	if err != nil {
		t.Fatalf("three-layer NewVP8Encoder returned error: %v", err)
	}
	if got := three.opts.TemporalScalability.LayerTargetBitrateKbps; got[0] != 400 || got[1] != 600 || got[2] != 1000 {
		t.Fatalf("three-layer bitrates = %v, want 400/600/1000", got)
	}
}

func temporalTestFlags(flags ...EncodeFlags) EncodeFlags {
	var out EncodeFlags
	for _, flag := range flags {
		out |= flag
	}
	return out
}

func assertIntPrefix(t *testing.T, name string, got []int, want []int) {
	t.Helper()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = %d, want %d", name, i, got[i], want[i])
		}
	}
}

func assertFlagPrefix(t *testing.T, name string, got []EncodeFlags, want []EncodeFlags) {
	t.Helper()
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d] = 0x%x, want 0x%x", name, i, uint32(got[i]), uint32(want[i]))
		}
	}
}
