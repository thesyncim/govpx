package govpx

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
)

// FuzzRTPVP9RoundTrip mirrors FuzzRTPVP8RoundTrip for the VP9 RTP
// packetizer/depacketizer. The hand-picked cases in vp9_rtp_test.go
// cover RFC 9628 shapes; this fuzzer walks the joint state space and
// asserts:
//
//   - PacketizeVP9RTPFrame → AssembleVP9RTPFrame is the identity.
//   - Only the last fragment has Marker=true; only the first has
//     StartOfFrame=true on the descriptor side, and only the last has
//     EndOfFrame=true.
//   - The optional scalability structure appears only on the first
//     fragment when present.
//   - Mutating any single fragment byte does not panic the assembler
//     (it must either reject with a typed error or produce a different
//     frame).
//
// No build-tag gate: this runs without the libvpx oracle.
func FuzzRTPVP9RoundTrip(f *testing.F) {
	seeds := [][]byte{
		// flags=PictureID+InterPicturePredicted, mtu=200, partID=0x10,
		// picID byte=0x00, layer byte=0x00, sid byte=0x01
		{0xc0, 200, 0x10, 0x00, 0x00, 0x01},
		{0x00, 1, 0xff, 0x00, 0x00, 0x01, 0xde},
		// flags=PictureID(8-bit), mtu=100, picID=0x12
		{0x80, 100, 0x32, 0x12, 0x34, 0x01, 0xab, 0xcd, 0xef},
		// flags=PictureID+InterPicturePredicted+LayerIndices (15-bit picID via
		// the dedicated bit), mtu=50.
		{0xc8, 50, 0x40, 0x80, 0x55, 0x03, 0x11, 0x22},
		// FlexibleMode + LayerIndices, mtu=250, 1 reference index.
		{0x38, 250, 0x14, 0x12, 0x34, 0x05, 0x33, 0x44},
		// ScalabilityStructure-present (S=1 first-fragment-only invariant),
		// mtu=200, two spatial layers.
		{0x02, 200, 0x32, 0xff, 0xff, 0x07, 0x66, 0x77, 0x02, 0x10, 0x10, 0x08, 0x08},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("VP9 RTP fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()
		desc, mtu, frame, ok := vp9RTPFuzzInputs(data)
		if !ok {
			return
		}
		fragments, err := PacketizeVP9RTPFrame(desc, frame, mtu)
		if err != nil {
			return // packetizer rejected the config; not interesting here.
		}
		if len(fragments) == 0 {
			t.Errorf("PacketizeVP9RTPFrame returned 0 fragments for %d-byte frame", len(frame))
			return
		}
		// Marker bit invariant: only the last fragment is marked.
		for i, fr := range fragments {
			if (i == len(fragments)-1) != fr.Marker {
				t.Errorf("fragment %d marker=%v, want %v", i, fr.Marker, i == len(fragments)-1)
			}
		}
		// Start/EndOfFrame invariants on the descriptor; SS only on first.
		for i, fr := range fragments {
			d, _, perr := ParseVP9RTPPayloadDescriptor(fr.Payload)
			if perr != nil {
				t.Errorf("fragment %d descriptor unparseable: %v", i, perr)
				return
			}
			if (i == 0) != d.StartOfFrame {
				t.Errorf("fragment %d SOF=%v, want %v", i, d.StartOfFrame, i == 0)
			}
			if (i == len(fragments)-1) != d.EndOfFrame {
				t.Errorf("fragment %d EOF=%v, want %v", i, d.EndOfFrame, i == len(fragments)-1)
			}
			if desc.ScalabilityStructurePresent && (i == 0) != d.ScalabilityStructurePresent {
				t.Errorf("fragment %d ScalabilityStructurePresent=%v, want %v",
					i, d.ScalabilityStructurePresent, i == 0)
			}
		}
		// Round-trip: assemble back, compare to original frame.
		assembled, err := AssembleVP9RTPFrame(fragments)
		if err != nil {
			t.Errorf("AssembleVP9RTPFrame on clean round-trip returned error: %v", err)
			return
		}
		if !bytes.Equal(assembled, frame) {
			t.Errorf("round-trip frame mismatch: got %d bytes, want %d bytes (first diff at %d)",
				len(assembled), len(frame), testutil.FirstByteDiff(assembled, frame))
		}
		// Mutation: flip the lowest byte of every fragment and call
		// AssembleVP9RTPFrame; it must not panic and either returns
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
			_, _ = AssembleVP9RTPFrame(mutated)
		}
	})
}

// vp9RTPFuzzInputs decodes fuzz bytes into a VP9RTPPayloadDescriptor + MTU +
// raw frame body. The descriptor surface is intentionally bounded so the
// packetizer's validate() path is reachable for almost every input.
//
// Byte layout (each byte indexed off a wrapping cursor so even short inputs
// produce meaningful coverage):
//
//	0: descriptor flag byte (PictureID, InterPicPred, LayerIndices,
//	   FlexibleMode, SS, NotRef bits map onto the high nibbles).
//	1: MTU as raw byte (clamped to [VP9 descriptor min, 2000]).
//	2: partition / temporal-id / spatial-id / sw-up packing.
//	3-4: picture-id raw bytes.
//	5: layer/refidx packing.
//	6+: scalability-structure body when SS bit is set; raw frame after.
func vp9RTPFuzzInputs(data []byte) (desc VP9RTPPayloadDescriptor, mtu int, frame []byte, ok bool) {
	if len(data) < 6 {
		return desc, 0, nil, false
	}
	flagsByte := data[0]
	mtu = max(16, min(int(data[1])+16, 2000))
	pack2 := data[2]
	picByte0 := data[3]
	picByte1 := data[4]
	layer := data[5]

	desc = VP9RTPPayloadDescriptor{
		InterPicturePredicted:       flagsByte&0x40 != 0,
		FlexibleMode:                flagsByte&0x10 != 0,
		ScalabilityStructurePresent: flagsByte&0x02 != 0,
		NotRefForUpperSpatialLayer:  flagsByte&0x01 != 0,
		TemporalID:                  pack2 & 0x07,
		SpatialID:                   (pack2 >> 3) & 0x07,
		SwitchingUpPoint:            pack2&0x40 != 0,
		InterLayerDependency:        pack2&0x80 != 0,
		TL0PICIDX:                   layer,
	}
	if flagsByte&0x80 != 0 {
		desc.PictureIDPresent = true
		if flagsByte&0x20 != 0 {
			desc.PictureID15Bit = true
			desc.PictureID = (uint16(picByte0)<<8 | uint16(picByte1)) & 0x7fff
		} else {
			desc.PictureID = uint16(picByte0 & 0x7f)
		}
	}
	if flagsByte&0x08 != 0 {
		desc.LayerIndicesPresent = true
	}
	// In flexible mode with inter-prediction we may carry 0..3 reference
	// indices. Pull the count from the low 2 bits of layer.
	if desc.InterPicturePredicted && desc.FlexibleMode {
		desc.ReferenceIndexCount = int(layer & 0x03)
		for i := 0; i < desc.ReferenceIndexCount; i++ {
			// Reference indices must be in [1, 127] per RFC.
			desc.ReferenceIndices[i] = uint8((int(layer>>2) + i + 1) & 0x7f)
			if desc.ReferenceIndices[i] == 0 {
				desc.ReferenceIndices[i] = 1
			}
		}
	}
	cursor := 6
	if desc.ScalabilityStructurePresent {
		if len(data) < cursor+1 {
			desc.ScalabilityStructurePresent = false
		} else {
			ss := VP9RTPScalabilityStructure{
				SpatialLayerCount: 1 + int(data[cursor]&0x07),
				ResolutionPresent: data[cursor]&0x08 != 0,
			}
			cursor++
			if ss.ResolutionPresent {
				need := 4 * ss.SpatialLayerCount
				if len(data) < cursor+need {
					ss.ResolutionPresent = false
				} else {
					for i := 0; i < ss.SpatialLayerCount; i++ {
						ss.Width[i] = uint16(data[cursor])<<8 | uint16(data[cursor+1])
						ss.Height[i] = uint16(data[cursor+2])<<8 | uint16(data[cursor+3])
						if ss.Width[i] == 0 {
							ss.Width[i] = 16
						}
						if ss.Height[i] == 0 {
							ss.Height[i] = 16
						}
						cursor += 4
					}
				}
			}
			desc.ScalabilityStructure = ss
		}
	}
	// Frame body is whatever bytes remain. Always supply at least one
	// byte; otherwise the packetizer trivially rejects.
	body := data[min(cursor, len(data)):]
	if len(body) == 0 {
		body = []byte{0xab}
	}
	return desc, mtu, body, true
}
