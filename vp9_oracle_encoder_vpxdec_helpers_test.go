//go:build govpx_oracle_trace

package govpx

import (
	"bytes"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func assertVP9EncoderVpxdecI420Match(t *testing.T, width, height int, packets ...[]byte) {
	t.Helper()
	ivf := vp9IVFForTest(width, height, packets...)
	want := vp9test.VpxdecI420(t, ivf)
	got := vp9DecodeVisibleI420ForTest(t, packets...)
	if !bytes.Equal(got, want) {
		t.Fatalf("I420 mismatch for encoder stream\nlibvpx=%s\ngovpx=%s",
			vp9test.MD5Hex(want),
			vp9test.MD5Hex(got))
	}
}
