package rtptest

import (
	"bytes"
	"testing"

	vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"
)

type testDescriptor byte

func (d testDescriptor) Size() (int, error) { return 1, nil }

func (d testDescriptor) MarshalInto(dst []byte) (int, error) {
	dst[0] = byte(d)
	return 1, nil
}

func TestMustPackPayload(t *testing.T) {
	got := MustPackPayload(t, testDescriptor(0xa5), []byte{0x01, 0x02})
	if want := []byte{0xa5, 0x01, 0x02}; !bytes.Equal(got, want) {
		t.Fatalf("packet = % x, want % x", got, want)
	}
}

func TestCheckPacketizedRoundTrip(t *testing.T) {
	fragments := []vpxrtp.PayloadFragment{
		{Payload: []byte{0xa0, 1, 2}},
		{Payload: []byte{0xa1, 3}, Marker: true},
	}
	checks := 0
	CheckPacketizedRoundTrip(t, []byte{1, 2, 3}, fragments, stripDescriptor,
		func(t testing.TB, i, total int, fragment vpxrtp.PayloadFragment) bool {
			t.Helper()
			checks++
			if total != len(fragments) {
				t.Fatalf("total = %d, want %d", total, len(fragments))
			}
			if len(fragment.Payload) == 0 {
				t.Fatalf("fragment %d payload empty", i)
			}
			return true
		})
	if checks != len(fragments) {
		t.Fatalf("checks = %d, want %d", checks, len(fragments))
	}
}

func stripDescriptor(fragments []vpxrtp.PayloadFragment) ([]byte, error) {
	var out []byte
	for _, fragment := range fragments {
		out = append(out, fragment.Payload[1:]...)
	}
	return out, nil
}
