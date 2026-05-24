package govpx_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
)

func TestVP8RTPFacadePayloadRoundTrip(t *testing.T) {
	desc := govpx.VP8RTPPayloadDescriptor{StartOfPartition: true}
	payload := []byte{0x9d, 0x01, 0x2a}
	packet, err := govpx.PackVP8RTPPayload(desc, payload)
	if err != nil {
		t.Fatalf("PackVP8RTPPayload: %v", err)
	}
	got, rest, err := govpx.ParseVP8RTPPayloadDescriptor(packet)
	if err != nil {
		t.Fatalf("ParseVP8RTPPayloadDescriptor: %v", err)
	}
	if got != desc {
		t.Fatalf("descriptor = %+v, want %+v", got, desc)
	}
	if !bytes.Equal(rest, payload) {
		t.Fatalf("payload = % x, want % x", rest, payload)
	}
	if _, err := govpx.VP8RTPPayloadSize(desc, nil); !errors.Is(err, govpx.ErrInvalidConfig) {
		t.Fatalf("empty payload size error = %v, want ErrInvalidConfig", err)
	}
}
