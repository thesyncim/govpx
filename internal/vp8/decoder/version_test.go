package decoder

import "testing"

func TestInterPredictionConfigForVersion(t *testing.T) {
	cases := []struct {
		version int
		want    InterPredictionConfig
		noLF    bool
		lfType  LoopFilterType
		ok      bool
	}{
		// LoopFilterHeaderForVersion must pass the header through unchanged
		// (task #295): libvpx's vp8_setup_version sets filter_type as a default,
		// but the bitstream filter_type read in decodeframe.c always overwrites
		// it, so the bitstream value is the effective filter_type.
		{version: 0, lfType: NormalLoopFilter, ok: true},
		{version: 1, want: InterPredictionConfig{UseBilinear: true}, lfType: NormalLoopFilter, ok: true},
		{version: 2, want: InterPredictionConfig{UseBilinear: true}, noLF: true, lfType: NormalLoopFilter, ok: true},
		{version: 3, want: InterPredictionConfig{UseBilinear: true, FullPixel: true}, noLF: true, lfType: NormalLoopFilter, ok: true},
		{version: 4, lfType: NormalLoopFilter, ok: true},
		{version: 5, lfType: NormalLoopFilter, ok: true},
		{version: 6, lfType: NormalLoopFilter, ok: true},
		{version: 7, lfType: NormalLoopFilter, ok: true},
		{version: 8},
	}

	for _, tc := range cases {
		if got := InterPredictionConfigForVersion(tc.version); got != tc.want {
			t.Fatalf("version %d config = %+v, want %+v", tc.version, got, tc.want)
		}
		if got := VersionSkipsLoopFilter(tc.version); got != tc.noLF {
			t.Fatalf("version %d no-lf = %v, want %v", tc.version, got, tc.noLF)
		}
		header := LoopFilterHeaderForVersion(tc.version, LoopFilterHeader{Type: NormalLoopFilter})
		if header.Type != tc.lfType {
			t.Fatalf("version %d loop filter type = %d, want %d", tc.version, header.Type, tc.lfType)
		}
		if got := IsSupportedVersion(tc.version); got != tc.ok {
			t.Fatalf("version %d supported = %v, want %v", tc.version, got, tc.ok)
		}
	}
}
