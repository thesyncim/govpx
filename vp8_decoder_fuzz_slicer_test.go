//go:build govpx_oracle_trace

package govpx

import (
	"encoding/binary"
	"encoding/hex"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

// TestVP8PerFrameI420SizesIgnoreIVFHeaderDims verifies that per-frame I420
// sizes used by the libvpx oracle slicer are derived from the VP8 key-frame
// header, not from the attacker-controlled IVF header. Mutating IVF header
// bytes 14-15 to claim height=7708 must not change the expected per-frame
// size that the oracle slicer computes.
func TestVP8PerFrameI420SizesIgnoreIVFHeaderDims(t *testing.T) {
	smoke, err := hex.DecodeString(testutil.LibvpxEncodedSmokeIVFHex)
	if err != nil {
		t.Fatalf("decode smoke IVF: %v", err)
	}
	baseline := vp8PerFrameI420Sizes(smoke)
	if len(baseline) != 2 {
		t.Fatalf("smoke baseline expected 2 frames, got %d (sizes=%v)", len(baseline), baseline)
	}
	want := i420FrameSize(32, 32) // VP8 KF reports 32x32
	for i, sz := range baseline {
		if sz != want {
			t.Errorf("baseline frame %d size = %d, want %d", i, sz, want)
		}
	}

	// Mutate IVF header height bytes 14-15 to 7708 (0x1e1c), matching
	// the f4f81f7d2e022caf seed family. The VP8 KF body is untouched.
	mutated := append([]byte(nil), smoke...)
	binary.LittleEndian.PutUint16(mutated[14:16], 7708)
	mutatedSizes := vp8PerFrameI420Sizes(mutated)
	if len(mutatedSizes) != len(baseline) {
		t.Fatalf("mutated frame count = %d, want %d", len(mutatedSizes), len(baseline))
	}
	for i, sz := range mutatedSizes {
		if sz != want {
			t.Errorf("mutated frame %d size = %d, want %d (IVF-header height mutation must not affect slicer size)",
				i, sz, want)
		}
	}
}

// TestSliceRawByPerFrameSizesStopsAtRawEnd verifies that the slicer
// emits exactly as many frames as fit in the raw output, even when the
// per-frame size vector claims more frames than vpxdec wrote. This
// covers mid-stream rejection (libvpx errored after frame N but the IVF
// claimed N+M frames).
func TestSliceRawByPerFrameSizesStopsAtRawEnd(t *testing.T) {
	per := i420FrameSize(32, 30)
	if per <= 0 {
		t.Fatalf("i420FrameSize(32,30) = %d, want > 0", per)
	}
	raw := make([]byte, per*2) // vpxdec wrote 2 frames
	for i := range raw {
		raw[i] = byte(i & 0xff)
	}
	sizes := []int{per, per, per, per} // IVF claimed 4 frames
	frames := sliceRawByPerFrameSizes(raw, sizes)
	if len(frames) != 2 {
		t.Fatalf("got %d frames, want 2 (raw covers only 2 frames)", len(frames))
	}
	for i := 0; i < 2; i++ {
		if len(frames[i]) != per {
			t.Errorf("frame %d len = %d, want %d", i, len(frames[i]), per)
		}
	}
}

// TestVP8PerFrameI420SizesStopsAtBadFrameHeader checks that an
// unparseable VP8 frame body truncates the expected-size vector: libvpx
// won't emit raw output past a rejected frame, and our slicer must
// mirror that.
func TestVP8PerFrameI420SizesStopsAtBadFrameHeader(t *testing.T) {
	smoke, err := hex.DecodeString(testutil.LibvpxEncodedSmokeIVFHex)
	if err != nil {
		t.Fatalf("decode smoke IVF: %v", err)
	}
	mutated := append([]byte(nil), smoke...)
	// Smash the first VP8 byte (offset 32 + 12 = 44): force the
	// 3-byte start code mismatch on the key frame. ParseFrameHeader
	// will report ErrInvalidFrameHeader.
	mutated[44+3] = 0x00 // overwrite start code byte 0 (was 0x9d)
	sizes := vp8PerFrameI420Sizes(mutated)
	if len(sizes) != 0 {
		t.Fatalf("expected zero expected-frame sizes after KF rejection, got %v", sizes)
	}
}
