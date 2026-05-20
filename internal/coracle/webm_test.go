package coracle

import (
	"bytes"
	"testing"
)

func TestExtractVP9WebMPackets(t *testing.T) {
	packet := []byte{0xaa, 0xbb, 0xcc}
	webm := webmElementBytes([]byte{0x18, 0x53, 0x80, 0x67},
		webmElementBytes([]byte{0x16, 0x54, 0xae, 0x6b},
			webmElementBytes([]byte{0xae},
				webmElementBytes([]byte{0xd7}, []byte{0x01}),
				webmElementBytes([]byte{0x83}, []byte{0x01}),
				webmElementBytes([]byte{0x86}, []byte("V_VP9")),
			),
		),
		webmElementBytes([]byte{0x1f, 0x43, 0xb6, 0x75},
			webmElementBytes([]byte{0xa3}, append([]byte{0x81, 0x00, 0x00, 0x00}, packet...)),
		),
	)

	packets, err := ExtractVP9WebMPackets(webm)
	if err != nil {
		t.Fatalf("ExtractVP9WebMPackets returned error: %v", err)
	}
	if len(packets) != 1 || !bytes.Equal(packets[0], packet) {
		t.Fatalf("packets = %x, want [%x]", packets, packet)
	}
}

func TestExtractVP9WebMPacketsRequiresVP9Track(t *testing.T) {
	webm := webmElementBytes([]byte{0x18, 0x53, 0x80, 0x67},
		webmElementBytes([]byte{0x16, 0x54, 0xae, 0x6b},
			webmElementBytes([]byte{0xae},
				webmElementBytes([]byte{0xd7}, []byte{0x01}),
				webmElementBytes([]byte{0x83}, []byte{0x01}),
				webmElementBytes([]byte{0x86}, []byte("V_VP8")),
			),
		),
	)

	if _, err := ExtractVP9WebMPackets(webm); err == nil {
		t.Fatalf("ExtractVP9WebMPackets accepted WebM without a VP9 video track")
	}
}

func webmElementBytes(id []byte, chunks ...[]byte) []byte {
	size := 0
	for _, chunk := range chunks {
		size += len(chunk)
	}
	if size > 126 {
		panic("test WebM element too large for one-byte size")
	}
	out := make([]byte, 0, len(id)+1+size)
	out = append(out, id...)
	out = append(out, byte(0x80|size))
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}
