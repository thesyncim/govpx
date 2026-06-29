package main

import (
	"testing"

	"github.com/pion/rtp"

	"github.com/thesyncim/govpx"
)

type captureRTPWriter struct {
	packets []rtp.Packet
}

func (w *captureRTPWriter) WriteRTP(packet *rtp.Packet) error {
	w.packets = append(w.packets, *packet)
	return nil
}

func TestWriteWebRTCRTPAccessUnitPreservesGovpxFragments(t *testing.T) {
	writer := &captureRTPWriter{}
	fragments := []govpx.RTPPayloadFragment{
		{Payload: []byte{0x90, 0x01}, Marker: false},
		{Payload: []byte{0x10, 0x02}, Marker: true},
	}
	sequence := uint16(0xfffe)
	const timestamp = uint32(0x12345678)

	written, err := writeWebRTCRTPAccessUnit(writer, fragments, timestamp, &sequence)
	if err != nil {
		t.Fatalf("writeWebRTCRTPAccessUnit returned error: %v", err)
	}
	if written != len(fragments) {
		t.Fatalf("written = %d, want %d", written, len(fragments))
	}
	if sequence != 0 {
		t.Fatalf("sequence after wrap = %d, want 0", sequence)
	}
	if len(writer.packets) != len(fragments) {
		t.Fatalf("captured packets = %d, want %d", len(writer.packets), len(fragments))
	}
	for i, packet := range writer.packets {
		if packet.Header.Version != 2 {
			t.Fatalf("packet %d RTP version = %d, want 2", i, packet.Header.Version)
		}
		if packet.Header.Timestamp != timestamp {
			t.Fatalf("packet %d timestamp = %#x, want %#x", i, packet.Header.Timestamp, timestamp)
		}
		wantSeq := uint16(0xfffe + i)
		if packet.Header.SequenceNumber != wantSeq {
			t.Fatalf("packet %d sequence = %#x, want %#x", i, packet.Header.SequenceNumber, wantSeq)
		}
		if packet.Header.Marker != fragments[i].Marker {
			t.Fatalf("packet %d marker = %t, want %t", i, packet.Header.Marker, fragments[i].Marker)
		}
		if string(packet.Payload) != string(fragments[i].Payload) {
			t.Fatalf("packet %d payload = % x, want % x", i, packet.Payload, fragments[i].Payload)
		}
	}
}
