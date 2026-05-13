package govpx

import "testing"

// TestCodecStringMapping confirms each Codec value renders to the
// matching libvpx short tag. The values are part of the public
// surface — they're used in log lines and error messages — so any
// rename here ripples to test corpora and external tooling.
func TestCodecStringMapping(t *testing.T) {
	cases := []struct {
		codec Codec
		want  string
	}{
		{CodecVP8, "vp8"},
		{CodecVP9, "vp9"},
		{Codec(99), "unknown"},
	}
	for _, tc := range cases {
		if got := tc.codec.String(); got != tc.want {
			t.Errorf("Codec(%d).String() = %q, want %q", tc.codec, got, tc.want)
		}
	}
}

// TestCodecVP9DistinctFromVP8 confirms VP9 didn't collide with VP8's
// reserved value. A future port that flipped these enum values would
// silently mis-dispatch every existing call site.
func TestCodecVP9DistinctFromVP8(t *testing.T) {
	if CodecVP8 == CodecVP9 {
		t.Fatal("CodecVP8 and CodecVP9 share the same value")
	}
}
