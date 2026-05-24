// Package rtptest holds shared RTP test mechanics for codec RTP packages.
package rtptest

import (
	"bytes"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

type AssembleFunc func([]vpxrtp.PayloadFragment) ([]byte, error)

type FragmentCheck func(testing.TB, int, int, vpxrtp.PayloadFragment) bool

func MustPackPayload[D vpxrtp.PayloadDescriptor](t testing.TB, desc D, payload []byte) []byte {
	t.Helper()
	packet, err := vpxrtp.PackPayload(desc, payload)
	if err != nil {
		t.Fatalf("vpxrtp.PackPayload: %v", err)
	}
	return packet
}

func CheckPacketizedRoundTrip(t testing.TB, frame []byte, fragments []vpxrtp.PayloadFragment,
	assemble AssembleFunc, check FragmentCheck,
) {
	t.Helper()
	if len(fragments) == 0 {
		t.Errorf("PacketizeFrame returned 0 fragments for %d-byte frame", len(frame))
		return
	}
	for i, fragment := range fragments {
		wantMarker := i == len(fragments)-1
		if fragment.Marker != wantMarker {
			t.Errorf("fragment %d marker=%v, want %v", i, fragment.Marker, wantMarker)
		}
	}
	for i, fragment := range fragments {
		if !check(t, i, len(fragments), fragment) {
			return
		}
	}
	assembled, err := assemble(fragments)
	if err != nil {
		t.Errorf("AssembleFrame on clean round-trip returned error: %v", err)
		return
	}
	if !bytes.Equal(assembled, frame) {
		t.Errorf("round-trip frame mismatch: got %d bytes, want %d bytes (first diff at %d)",
			len(assembled), len(frame), testutil.FirstByteDiff(assembled, frame))
	}
	MutateEachFragment(fragments, assemble)
}

func MutateEachFragment(fragments []vpxrtp.PayloadFragment, assemble AssembleFunc) {
	for i := range fragments {
		mutated := make([]vpxrtp.PayloadFragment, len(fragments))
		copy(mutated, fragments)
		if len(mutated[i].Payload) == 0 {
			continue
		}
		body := append([]byte(nil), mutated[i].Payload...)
		body[len(body)-1] ^= 0xff
		mutated[i].Payload = body
		_, _ = assemble(mutated)
	}
}
