package decoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

// buildHeader uses the bitstream writer just for its bit-packing
// utility — strictly speaking the uncompressed header is byte-aligned
// MSB-first rather than the boolean range coding the entropy writer
// uses. We use a small bit-pack helper instead.
type bitPacker struct {
	buf    []byte
	bitPos int
}

func (p *bitPacker) writeBit(bit uint32) {
	if p.bitPos>>3 >= len(p.buf) {
		p.buf = append(p.buf, 0)
	}
	if bit != 0 {
		p.buf[p.bitPos>>3] |= 1 << uint(7-(p.bitPos&7))
	}
	p.bitPos++
}

func (p *bitPacker) writeLiteral(v uint32, bits int) {
	for b := bits - 1; b >= 0; b-- {
		p.writeBit((v >> uint(b)) & 1)
	}
}

// TestReadProfileRoundTrip verifies that ReadProfile correctly decodes
// every legal profile (0..3) when written with the libvpx encoding.
func TestReadProfileRoundTrip(t *testing.T) {
	for p := uint32(0); p < 4; p++ {
		var pk bitPacker
		// libvpx's vp9_read_profile decoding:
		//   raw  = bit0 | (bit1 << 1)
		//   if raw > 2 (i.e. raw == 3): raw += bit2
		// So for profile = 3 we write bit0=1, bit1=1, bit2=0 to keep
		// raw at 3 (bit2=1 would push it to invalid profile 4).
		pk.writeBit(p & 1)
		pk.writeBit((p >> 1) & 1)
		if p > 2 {
			pk.writeBit(0)
		}
		// pad to byte boundary
		for pk.bitPos&7 != 0 {
			pk.writeBit(0)
		}

		var r BitReader
		r.Init(pk.buf)
		got := ReadProfile(&r)
		if uint32(got) != p {
			t.Errorf("profile %d: ReadProfile returned %d", p, got)
		}
	}
}

func TestReadFrameMarker(t *testing.T) {
	// Marker is 0b10 = 0x80 in MSB-first 2-bit literal.
	var r BitReader
	r.Init([]byte{0x80})
	if err := ReadFrameMarker(&r); err != nil {
		t.Fatalf("good marker: %v", err)
	}

	r.Init([]byte{0x00}) // 0b00 — bad marker
	if err := ReadFrameMarker(&r); err == nil {
		t.Fatal("bad marker: expected error, got nil")
	}
}

func TestReadSyncCode(t *testing.T) {
	var r BitReader
	r.Init([]byte{common.VP9SyncCode0, common.VP9SyncCode1, common.VP9SyncCode2})
	if !ReadSyncCode(&r) {
		t.Fatal("good sync code not accepted")
	}
	r.Init([]byte{0x00, 0x00, 0x00})
	if ReadSyncCode(&r) {
		t.Fatal("bad sync code accepted")
	}
}

func TestReadFrameSize(t *testing.T) {
	// Encode width=1280, height=720. Wire form is (w-1, h-1) as two
	// 16-bit big-endian literals.
	w, h := uint32(1280), uint32(720)
	var pk bitPacker
	pk.writeLiteral(w-1, 16)
	pk.writeLiteral(h-1, 16)

	var r BitReader
	r.Init(pk.buf)
	gw, gh := ReadFrameSize(&r)
	if gw != w || gh != h {
		t.Errorf("got (%d, %d), want (%d, %d)", gw, gh, w, h)
	}
}

func TestReadBitdepthColorspaceSamplingProfile0(t *testing.T) {
	// Profile 0 / BT.601 / studio range / default 4:2:0 sampling.
	var pk bitPacker
	pk.writeLiteral(uint32(common.CSBT601), 3) // color space
	pk.writeBit(uint32(common.CRStudioRange))  // color range bit
	// Profile 0 doesn't read subsampling bits.
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	got, err := ReadBitdepthColorspaceSampling(&r, common.Profile0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.BitDepth != Bits8 {
		t.Errorf("BitDepth = %d, want %d", got.BitDepth, Bits8)
	}
	if got.ColorSpace != common.CSBT601 {
		t.Errorf("ColorSpace = %d, want %d", got.ColorSpace, common.CSBT601)
	}
	if got.ColorRange != common.CRStudioRange {
		t.Errorf("ColorRange = %d, want %d", got.ColorRange, common.CRStudioRange)
	}
	if got.SubsamplingX != 1 || got.SubsamplingY != 1 {
		t.Errorf("Subsampling = (%d, %d), want (1, 1)", got.SubsamplingX, got.SubsamplingY)
	}
}

func TestReadBitdepthColorspaceSamplingProfile0SRGBRejected(t *testing.T) {
	// Profile 0 with SRGB is illegal (4:4:4 only allowed in profile 1/3).
	var pk bitPacker
	pk.writeLiteral(uint32(common.CSSRGB), 3)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}

	var r BitReader
	r.Init(pk.buf)
	if _, err := ReadBitdepthColorspaceSampling(&r, common.Profile0); err == nil {
		t.Fatal("expected error for SRGB + profile 0")
	}
}

// Unused — silences "bitstream imported but not used" if the package
// already pulls it in elsewhere.
var _ = bitstream.Reader{}
