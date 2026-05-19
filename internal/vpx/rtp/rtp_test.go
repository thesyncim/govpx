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
