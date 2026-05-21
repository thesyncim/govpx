package rtp

import (
	"bytes"
	"errors"
	"testing"

	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

func TestPayloadBodySize(t *testing.T) {
	got, err := PayloadBodySize(3, 10)
	if err != nil {
		t.Fatalf("PayloadBodySize: %v", err)
	}
	if got != 13 {
		t.Fatalf("PayloadBodySize = %d, want 13", got)
	}
	if _, err := PayloadBodySize(0, 10); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("zero descriptor error = %v, want ErrInvalidConfig", err)
	}
	if _, err := PayloadBodySize(3, 0); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("zero payload error = %v, want ErrInvalidConfig", err)
	}
}

type testDescriptor struct {
	data       []byte
	sizeErr    error
	marshalErr error
}

func (d testDescriptor) Size() (int, error) {
	if d.sizeErr != nil {
		return 0, d.sizeErr
	}
	return len(d.data), nil
}

func (d testDescriptor) MarshalInto(dst []byte) (int, error) {
	if d.marshalErr != nil {
		return 0, d.marshalErr
	}
	if len(dst) < len(d.data) {
		return len(d.data), vpxerrors.ErrBufferTooSmall
	}
	return copy(dst, d.data), nil
}

func TestPackPayload(t *testing.T) {
	desc := testDescriptor{data: []byte{0x80, 0x01}}
	got, err := PackPayload(desc, []byte{0xaa, 0xbb, 0xcc})
	if err != nil {
		t.Fatalf("PackPayload: %v", err)
	}
	if !bytes.Equal(got, []byte{0x80, 0x01, 0xaa, 0xbb, 0xcc}) {
		t.Fatalf("packed payload = %x", got)
	}

	size, err := PayloadSize(desc, []byte{0xaa})
	if err != nil {
		t.Fatalf("PayloadSize: %v", err)
	}
	if size != 3 {
		t.Fatalf("PayloadSize = %d, want 3", size)
	}

	if _, err := PayloadSize(desc, nil); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("zero payload error = %v, want ErrInvalidConfig", err)
	}
	if _, err := PayloadSize(testDescriptor{sizeErr: vpxerrors.ErrInvalidData}, []byte{1}); !errors.Is(err, vpxerrors.ErrInvalidData) {
		t.Fatalf("descriptor size error = %v, want ErrInvalidData", err)
	}
	if _, err := PackPayload(testDescriptor{data: []byte{1}, marshalErr: vpxerrors.ErrInvalidData}, []byte{2}); !errors.Is(err, vpxerrors.ErrInvalidData) {
		t.Fatalf("descriptor marshal error = %v, want ErrInvalidData", err)
	}
}

func TestPackPayloadIntoShortBuffer(t *testing.T) {
	desc := testDescriptor{data: []byte{0x80, 0x01}}
	need, err := PackPayloadInto(make([]byte, 2), desc, []byte{0xaa})
	if !errors.Is(err, vpxerrors.ErrBufferTooSmall) {
		t.Fatalf("short buffer error = %v, want ErrBufferTooSmall", err)
	}
	if need != 3 {
		t.Fatalf("short buffer need = %d, want 3", need)
	}
}

func TestVariableFramePacketizationSize(t *testing.T) {
	packets, total, err := VariableFramePacketizationSize(20, 8, 3, 14)
	if err != nil {
		t.Fatalf("VariableFramePacketizationSize: %v", err)
	}
	if packets != 3 || total != 34 {
		t.Fatalf("size = packets:%d total:%d, want 3/34", packets, total)
	}

	packets, total, err = VariableFramePacketizationSize(5, 8, 3, 14)
	if err != nil {
		t.Fatalf("VariableFramePacketizationSize single: %v", err)
	}
	if packets != 1 || total != 13 {
		t.Fatalf("single size = packets:%d total:%d, want 1/13", packets, total)
	}

	if _, _, err := VariableFramePacketizationSize(20, 8, 3, 8); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("small first mtu error = %v, want ErrInvalidConfig", err)
	}
	if _, _, err := VariableFramePacketizationSize(20, 2, 14, 14); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("small rest mtu error = %v, want ErrInvalidConfig", err)
	}
}

func TestPacketizeBufferAndChunkHelpers(t *testing.T) {
	if err := CheckPacketizeBuffers(make([]PayloadFragment, 2), make([]byte, 10), 2, 10); err != nil {
		t.Fatalf("CheckPacketizeBuffers: %v", err)
	}
	if err := CheckPacketizeBuffers(make([]PayloadFragment, 1), make([]byte, 10), 2, 10); !errors.Is(err, vpxerrors.ErrBufferTooSmall) {
		t.Fatalf("short packet buffer error = %v, want ErrBufferTooSmall", err)
	}
	if err := CheckPacketizeBuffers(make([]PayloadFragment, 2), make([]byte, 9), 2, 10); !errors.Is(err, vpxerrors.ErrBufferTooSmall) {
		t.Fatalf("short payload buffer error = %v, want ErrBufferTooSmall", err)
	}

	chunk, err := FramePayloadChunkSize(12, 5, 20)
	if err != nil {
		t.Fatalf("FramePayloadChunkSize: %v", err)
	}
	if chunk != 7 {
		t.Fatalf("chunk = %d, want 7", chunk)
	}
	chunk, err = FramePayloadChunkSize(12, 5, 3)
	if err != nil {
		t.Fatalf("FramePayloadChunkSize tail: %v", err)
	}
	if chunk != 3 {
		t.Fatalf("tail chunk = %d, want 3", chunk)
	}
	if _, err := FramePayloadChunkSize(5, 5, 3); !errors.Is(err, vpxerrors.ErrInvalidConfig) {
		t.Fatalf("small mtu chunk error = %v, want ErrInvalidConfig", err)
	}
}

func TestPacketizeFrameUsesDescriptorCallback(t *testing.T) {
	frame := []byte{1, 2, 3, 4, 5, 6, 7}
	descriptor := func(i, fragments int) (testDescriptor, int, error) {
		desc := testDescriptor{data: []byte{0x80 | byte(i), byte(fragments)}}
		return desc, len(desc.data), nil
	}
	payloads, err := PacketizeFrame(frame, 5, 3, 13, descriptor)
	if err != nil {
		t.Fatalf("PacketizeFrame: %v", err)
	}
	want := [][]byte{
		{0x80, 0x03, 1, 2, 3},
		{0x81, 0x03, 4, 5, 6},
		{0x82, 0x03, 7},
	}
	if len(payloads) != len(want) {
		t.Fatalf("payload count = %d, want %d", len(payloads), len(want))
	}
	for i := range want {
		if payloads[i].Marker != LastFragment(i, len(want)) {
			t.Fatalf("payload %d marker = %v", i, payloads[i].Marker)
		}
		if !bytes.Equal(payloads[i].Payload, want[i]) {
			t.Fatalf("payload %d = % x, want % x", i, payloads[i].Payload, want[i])
		}
	}
}

func TestMarkerMatchesFragmentIndex(t *testing.T) {
	payloads := []PayloadFragment{
		{Marker: false},
		{Marker: false},
		{Marker: true},
	}
	for i := range payloads {
		if !MarkerMatchesFragmentIndex(payloads, i) {
			t.Fatalf("marker %d rejected", i)
		}
	}
	payloads[0].Marker = true
	if MarkerMatchesFragmentIndex(payloads, 0) {
		t.Fatalf("early marker accepted")
	}
}

func TestAssembleFrameValidatesCodecDescriptors(t *testing.T) {
	payloads := []PayloadFragment{
		{Payload: []byte{0, 1, 2}, Marker: false},
		{Payload: []byte{1, 3}, Marker: true},
	}
	parse := func(payload []byte) (byte, []byte, error) {
		if len(payload) == 0 {
			return 0, nil, vpxerrors.ErrInvalidData
		}
		return payload[0], payload[1:], nil
	}
	validate := func(i, _ int, desc byte) error {
		if desc != byte(i) {
			return vpxerrors.ErrInvalidData
		}
		return nil
	}

	size, err := FrameAssemblySize(payloads, vpxerrors.ErrInvalidData, parse, validate)
	if err != nil {
		t.Fatalf("FrameAssemblySize: %v", err)
	}
	if size != 3 {
		t.Fatalf("assembly size = %d, want 3", size)
	}
	got, err := AssembleFrame(payloads, vpxerrors.ErrInvalidData, parse, validate)
	if err != nil {
		t.Fatalf("AssembleFrame: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("assembled = %v, want [1 2 3]", got)
	}

	payloads[1].Payload[0] = 2
	if _, err := FrameAssemblySize(payloads, vpxerrors.ErrInvalidData, parse, validate); !errors.Is(err, vpxerrors.ErrInvalidData) {
		t.Fatalf("descriptor validation error = %v, want ErrInvalidData", err)
	}
}

func TestAssemblePayloadFragments(t *testing.T) {
	payloads := []PayloadFragment{
		{Payload: []byte{0xaa, 1, 2}},
		{Payload: []byte{0xaa, 3}},
	}
	parse := func(payload []byte) ([]byte, error) {
		if len(payload) == 0 || payload[0] != 0xaa {
			return nil, vpxerrors.ErrInvalidData
		}
		return payload[1:], nil
	}
	got, err := AssemblePayloadFragments(payloads, 3, parse)
	if err != nil {
		t.Fatalf("AssemblePayloadFragments: %v", err)
	}
	if !bytes.Equal(got, []byte{1, 2, 3}) {
		t.Fatalf("assembled = %v, want [1 2 3]", got)
	}

	if _, err := AssemblePayloadFragmentsInto(make([]byte, 2), payloads, 3, parse); !errors.Is(err, vpxerrors.ErrBufferTooSmall) {
		t.Fatalf("short assemble error = %v, want ErrBufferTooSmall", err)
	}
	payloads[1].Payload[0] = 0xbb
	if _, err := AssemblePayloadFragments(payloads, 3, parse); !errors.Is(err, vpxerrors.ErrInvalidData) {
		t.Fatalf("parse error = %v, want ErrInvalidData", err)
	}
}
