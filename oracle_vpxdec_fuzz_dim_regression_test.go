//go:build govpx_oracle_trace

package govpx

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

// TestFuzzDecoderAgainstLibvpx_DimMismatchSeed exercises the task #308
// fix end-to-end: synthesize the f4f81f7d2e022caf-family mutation (IVF
// header height flipped to 0x1e1c=7708 while the VP8 key-frame body is
// untouched) and run both decoders through the fuzz harness's
// best-effort wrappers. Before #308 this disagreed with libvpx_frames=0
// (slicer wanted 10.4 MB/frame) and govpx_frames=2. After #308 both
// sides report 2 frames and byte-identical I420.
func TestFuzzDecoderAgainstLibvpx_DimMismatchSeed(t *testing.T) {
	if os.Getenv("GOVPX_WITH_ORACLE") != "1" {
		t.Skip("set GOVPX_WITH_ORACLE=1 to run decoder-vs-libvpx dim-mismatch regression")
	}
	vpxdec := findVpxdecForFuzz(t)
	smoke, err := hex.DecodeString(testutil.LibvpxEncodedSmokeIVFHex)
	if err != nil {
		t.Fatalf("decode smoke IVF: %v", err)
	}
	mutated := append([]byte(nil), smoke...)
	binary.LittleEndian.PutUint16(mutated[14:16], 7708)

	govpxFrames, govpxErr := decodeIVFGovpxBestEffort(mutated)
	libvpxFrames, libvpxErr := decodeIVFLibvpxBestEffort(t, vpxdec, mutated)

	if (len(govpxFrames) > 0) != (len(libvpxFrames) > 0) {
		t.Fatalf("acceptance disagreement: govpx_frames=%d libvpx_frames=%d govpx_err=%v libvpx_err=%v",
			len(govpxFrames), len(libvpxFrames), govpxErr, libvpxErr)
	}
	if len(govpxFrames) == 0 || len(libvpxFrames) == 0 {
		t.Fatalf("expected both decoders to produce frames, got govpx=%d libvpx=%d",
			len(govpxFrames), len(libvpxFrames))
	}
	if len(govpxFrames) != len(libvpxFrames) {
		t.Fatalf("frame count: govpx=%d libvpx=%d", len(govpxFrames), len(libvpxFrames))
	}
	for i := range govpxFrames {
		if len(govpxFrames[i]) != len(libvpxFrames[i]) {
			t.Errorf("frame %d size: govpx=%d libvpx=%d", i, len(govpxFrames[i]), len(libvpxFrames[i]))
		}
	}
}
