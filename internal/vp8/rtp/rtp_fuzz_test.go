package rtp

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/rtptest"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

// FuzzRTPVP8RoundTrip exercises the VP8 RTP packetizer and depacketizer
// end-to-end under fuzz-driven frame-size, MTU, and descriptor-field
// combinations. The hand-picked tests cover canonical RFC 7741 shapes;
// this fuzzer walks the joint state space and checks that the packetize
// and depacketize round trip is the identity, and that mutated payload
// bytes never panic the depacketizer.
//
// The fuzzer asserts:
//
//   - PacketizeFrame followed by AssembleFrame returns
//     the original frame bytes unchanged.
//   - Mutating one byte of any packetized fragment does not panic
//     AssembleFrame; the result is either a different frame
//     or a typed error.
//   - The first fragment's StartOfPartition flag (parsed from the
//     descriptor) is always true, and only the last fragment has
//     the marker bit set.
func FuzzRTPVP8RoundTrip(f *testing.F) {
	seeds := [][]byte{
		{0x00, 200, 0x10, 0x00, 0x00, 0x01},                   // tiny frame, mtu=16
		{0x00, 1, 0xff, 0x00, 0x00, 0x01, 0xde},               // single byte
		{0x80, 100, 0x32, 0x12, 0x34, 0x01, 0xab, 0xcd, 0xef}, // 7-bit picID
		{0xc0, 50, 0x40, 0x80, 0x55, 0x03, 0x11, 0x22},        // 15-bit picID
		{0xe0, 250, 0x14, 0x12, 0x34, 0x05, 0x33, 0x44},       // TL0PICIDX present
		{0xf0, 200, 0x32, 0xff, 0xff, 0x07, 0x66, 0x77},       // all fields, mtu=200
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("RTP fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()
		desc, mtu, frame, ok := vp8RTPFuzzInputs(data)
		if !ok {
			return
		}
		fragments, err := PacketizeFrame(desc, frame, mtu)
		if err != nil {
			return // packetizer rejected the config; not interesting here.
		}
		rtptest.CheckPacketizedRoundTrip(t, frame, fragments, AssembleFrame,
			func(t testing.TB, i, total int, fr vpxrtp.PayloadFragment) bool {
				d, _, perr := ParsePayloadDescriptor(fr.Payload)
				if perr != nil {
					t.Errorf("fragment %d descriptor unparseable: %v", i, perr)
					return false
				}
				if (i == 0) != d.StartOfPartition {
					t.Errorf("fragment %d S=%v, want %v", i, d.StartOfPartition, i == 0)
				}
				return true
			})
	})
}

func vp8RTPFuzzInputs(data []byte) (desc PayloadDescriptor, mtu int, frame []byte, ok bool) {
	if len(data) < 4 {
		return desc, 0, nil, false
	}
	flagsByte := data[0]
	mtu = max(4, min(int(data[1]), 2000))
	partID := data[2] & 0x0f

	desc = PayloadDescriptor{
		NonReferenceFrame: flagsByte&0x40 != 0,
		StartOfPartition:  true,
		PartitionID:       partID,
	}
	if flagsByte&0x80 != 0 {
		desc.PictureIDPresent = true
		desc.PictureID = uint16(data[3])
		if flagsByte&0x40 != 0 {
			desc.PictureID15Bit = true
			desc.PictureID = (uint16(data[3])<<8 | uint16(data[3])) & 0x7fff
		}
	}
	if flagsByte&0x20 != 0 && len(data) >= 5 {
		desc.TL0PICIDXPresent = true
		desc.TL0PICIDX = data[4]
	}
	if flagsByte&0x10 != 0 && len(data) >= 6 {
		desc.TemporalIDPresent = true
		desc.TemporalID = data[5] & 0x03
		desc.LayerSync = data[5]&0x04 != 0
	}
	if flagsByte&0x08 != 0 && len(data) >= 7 {
		desc.KeyIndexPresent = true
		desc.KeyIndex = data[6] & 0x1f
	}
	// Frame body is whatever bytes remain. Always supply at least
	// one byte; otherwise the packetizer trivially rejects.
	body := data[min(7, len(data)):]
	if len(body) == 0 {
		body = []byte{0xab}
	}
	return desc, mtu, body, true
}
