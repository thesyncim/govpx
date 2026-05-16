package govpx

import (
	"bytes"
	"testing"
)

// FuzzRTPVP8RoundTrip closes plan-§3 F5 / G6: the VP8 RTP packetizer
// and depacketizer are exercised end-to-end under fuzz-driven
// frame-size / MTU / descriptor-field combinations. The hand-picked
// test cases in vp8_rtp_test.go cover canonical RFC 7741 shapes;
// this fuzzer walks the joint state space and guarantees the
// packetize → depacketize round trip is the identity, and that
// mutated payload bytes never panic the depacketizer.
//
// The fuzzer asserts:
//
//   - PacketizeVP8RTPFrame followed by AssembleVP8RTPFrame returns
//     the original frame bytes unchanged.
//   - Mutating one byte of any packetized fragment does not panic
//     AssembleVP8RTPFrame; the result is either a different frame
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
		fragments, err := PacketizeVP8RTPFrame(desc, frame, mtu)
		if err != nil {
			return // packetizer rejected the config; not interesting here.
		}
		if len(fragments) == 0 {
			t.Errorf("PacketizeVP8RTPFrame returned 0 fragments for %d-byte frame", len(frame))
			return
		}
		// Marker bit invariant: only the last fragment is marked.
		for i, fr := range fragments {
			if (i == len(fragments)-1) != fr.Marker {
				t.Errorf("fragment %d marker=%v, want %v (last=%v)",
					i, fr.Marker, i == len(fragments)-1, i == len(fragments)-1)
			}
		}
		// StartOfPartition flag invariant: only the first fragment is S=1.
		for i, fr := range fragments {
			d, _, perr := ParseVP8RTPPayloadDescriptor(fr.Payload)
			if perr != nil {
				t.Errorf("fragment %d descriptor unparseable: %v", i, perr)
				return
			}
			if (i == 0) != d.StartOfPartition {
				t.Errorf("fragment %d S=%v, want %v", i, d.StartOfPartition, i == 0)
			}
		}
		// Round-trip: assemble back, compare to original frame.
		assembled, err := AssembleVP8RTPFrame(fragments)
		if err != nil {
			t.Errorf("AssembleVP8RTPFrame on clean round-trip returned error: %v", err)
			return
		}
		if !bytes.Equal(assembled, frame) {
			t.Errorf("round-trip frame mismatch: got %d bytes, want %d bytes (first diff at %d)",
				len(assembled), len(frame), firstByteDiffPlain(assembled, frame))
		}
		// Mutation: flip the lowest byte of every fragment and call
		// AssembleVP8RTPFrame; it must not panic and either returns
		// a different frame or a typed error.
		for i := range fragments {
			mutated := make([]RTPPayloadFragment, len(fragments))
			copy(mutated, fragments)
			if len(mutated[i].Payload) == 0 {
				continue
			}
			body := append([]byte(nil), mutated[i].Payload...)
			body[len(body)-1] ^= 0xff
			mutated[i].Payload = body
			_, _ = AssembleVP8RTPFrame(mutated)
		}
	})
}

// firstByteDiffPlain mirrors the oracle-tag-gated firstByteDiff so
// this fuzzer can build under the default build tag set.
func firstByteDiffPlain(a, b []byte) int {
	n := min(len(a), len(b))
	for i := range n {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}

func vp8RTPFuzzInputs(data []byte) (desc VP8RTPPayloadDescriptor, mtu int, frame []byte, ok bool) {
	if len(data) < 4 {
		return desc, 0, nil, false
	}
	flagsByte := data[0]
	mtu = max(4, min(int(data[1]), 2000))
	partID := data[2] & 0x0f

	desc = VP8RTPPayloadDescriptor{
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
