package govpx_test

import (
	"errors"
	"testing"

	"github.com/thesyncim/govpx"
)

// FuzzVP8DecoderErrorConcealment builds a tiny VP8 stream from the fuzz []byte
// (key + a handful of inter frames), then corrupts a random byte range
// (offset + length drawn from the fuzz bytes) of one of the inter packets and
// decodes the result with ErrorConcealment=true.
//
// Assertions:
//   - decoder must not panic;
//   - decoder either returns nil (concealment kicked in, frame is corrupted
//     but the API contract is preserved) or one of the documented sentinels
//     (ErrInvalidData / ErrInvalidConfig / ErrFrameRejected);
//   - any frame the decoder emits via NextFrame must report dimensions within
//     the originally-encoded bounds (i.e. concealment never silently
//     reallocates to a different resolution).
//
// The existing TestDecodeErrorConcealment* tests pin specific corruption
// scenarios; this fuzz harness expands the coverage to arbitrary offsets and
// lengths drawn from the fuzz corpus.
func FuzzVP8DecoderErrorConcealment(f *testing.F) {
	seeds := [][]byte{
		nil,
		{},
		{0x00},
		// 1 inter frame, corrupt 1 byte at offset 0.
		{0x01, 0x00, 0x00, 0x01, 0x80, 0x80, 0x80, 0x80},
		// 2 inter frames, corrupt 4 bytes at offset 3 of the second inter.
		{0x02, 0x01, 0x03, 0x04, 0x10, 0x20, 0x30, 0x40, 0x55, 0xaa},
		// Saturated corruption (length > frame; harness clamps).
		{0x01, 0x00, 0x05, 0xff, 0x42, 0x87, 0xab, 0xc0, 0x33, 0x66},
		// Mixed noise seed used by FuzzVP8DecoderDecode — re-used here as a
		// good starter for varied luma input.
		{0x03, 0x02, 0x01, 0x02, 0x10, 0x42, 0x87, 0xab, 0xc0, 0x55, 0xaa, 0xff, 0x00, 0x33, 0x66, 0x99},
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("EC fuzz panicked on %d-byte input: %v", len(data), r)
			}
		}()

		const width, height = 32, 32

		// Encode key + interFrames inter frames.
		interFrames := 1
		if len(data) >= 1 {
			interFrames = 1 + int(data[0])%3
		}
		packets := decoderECFuzzBuildStream(t, width, height, interFrames, data)
		if len(packets) < 2 {
			return // need at least key + 1 inter for EC to be meaningful.
		}

		// Pick which inter packet to corrupt, and the offset + length.
		corruptIndex := 1
		if len(data) >= 2 {
			corruptIndex = 1 + int(data[1])%(len(packets)-1)
		}
		offset := 0
		if len(data) >= 3 {
			offset = int(data[2])
		}
		length := 1
		if len(data) >= 4 {
			length = int(data[3])
		}
		corrupted := decoderECFuzzCorrupt(packets[corruptIndex], offset, length, data)
		packets[corruptIndex] = corrupted

		// Decode with EC enabled. Sentinel-only errors are acceptable.
		d, err := govpx.NewVP8Decoder(govpx.DecoderOptions{
			ErrorConcealment: true,
			MaxWidth:         width,
			MaxHeight:        height,
		})
		if err != nil {
			t.Fatalf("NewVP8Decoder(EC): %v", err)
		}
		defer func() { _ = d.Close() }()

		for i, p := range packets {
			err := d.Decode(p)
			if err != nil {
				if !decoderECFuzzAcceptableError(err) {
					t.Fatalf("packet %d Decode returned non-sentinel error: %v", i, err)
				}
				// On sentinel error we stop the loop — the decoder may be in a
				// non-recoverable state. The contract is only that no panic
				// occurred and the error is a documented one.
				return
			}
			img, ok := d.NextFrame()
			if !ok {
				continue
			}
			if img.Width <= 0 || img.Width > width || img.Height <= 0 || img.Height > height {
				t.Fatalf("packet %d emitted frame with dimensions %dx%d outside [1,%dx%d]",
					i, img.Width, img.Height, width, height)
			}
			// Concealment must never lie about plane lengths; the visible
			// region must fit in the emitted Y / U / V slices.
			if len(img.Y) < img.Width*img.Height {
				t.Fatalf("packet %d emitted Y plane too short: len=%d want>=%d",
					i, len(img.Y), img.Width*img.Height)
			}
			uvSize := ((img.Width + 1) >> 1) * ((img.Height + 1) >> 1)
			if len(img.U) < uvSize || len(img.V) < uvSize {
				t.Fatalf("packet %d emitted chroma planes too short: U=%d V=%d want>=%d",
					i, len(img.U), len(img.V), uvSize)
			}
		}
	})
}

// decoderECFuzzBuildStream encodes 1 key + interFrames inter packets from the
// fuzz bytes. Returns the packets in order. The encoder may drop some inter
// frames (rate control); we tolerate fewer than asked-for packets.
func decoderECFuzzBuildStream(t *testing.T, width, height, interFrames int, data []byte) [][]byte {
	t.Helper()
	e, err := govpx.NewVP8Encoder(govpx.EncoderOptions{
		Width:               width,
		Height:              height,
		FPS:                 30,
		RateControlMode:     govpx.RateControlCBR,
		TargetBitrateKbps:   500,
		MinQuantizer:        4,
		MaxQuantizer:        56,
		Deadline:            govpx.DeadlineRealtime,
		CpuUsed:             8,
		KeyFrameInterval:    120,
		BufferSizeMs:        600,
		BufferInitialSizeMs: 400,
		BufferOptimalSizeMs: 500,
	})
	if err != nil {
		return nil
	}
	defer e.Close()

	buf := make([]byte, width*height*4+1024)
	out := make([][]byte, 0, interFrames+1)

	keySrc := vp8FuzzYUVNoiseImage(width, height, data)
	keyResult, err := e.EncodeInto(buf, keySrc, 0, 1, govpx.EncodeForceKeyFrame)
	if err != nil || keyResult.Dropped || len(keyResult.Data) == 0 {
		return nil
	}
	out = append(out, append([]byte(nil), keyResult.Data...))

	for i := range interFrames {
		seed := data
		if len(data) > 4+i {
			seed = data[4+i:]
		}
		src := vp8FuzzYUVNoiseImage(width, height, seed)
		result, err := e.EncodeInto(buf, src, uint64(i+1), 1, 0)
		if err != nil || result.Dropped || len(result.Data) == 0 {
			continue
		}
		out = append(out, append([]byte(nil), result.Data...))
	}
	return out
}

// decoderECFuzzCorrupt overwrites packet[offset:offset+length] with fuzz-derived
// bytes. Out-of-range offset/length values are clamped so the call never
// panics. Returns a fresh slice (the original packet is not mutated).
func decoderECFuzzCorrupt(packet []byte, offset, length int, data []byte) []byte {
	if len(packet) == 0 {
		return packet
	}
	if offset < 0 {
		offset = 0
	}
	if length < 0 {
		length = 0
	}
	offset %= len(packet)
	if length > len(packet)-offset {
		length = len(packet) - offset
	}
	out := append([]byte(nil), packet...)
	for i := 0; i < length; i++ {
		var b byte
		if len(data) > 0 {
			b = data[(offset+i)%len(data)]
		}
		out[offset+i] ^= b ^ 0xff
	}
	return out
}

// decoderECFuzzAcceptableError reports whether err is one of the documented
// sentinels the decoder may return on uncorrectable corruption. The
// concealment path is expected to swallow most inter-frame corruption and
// return nil; only "the decoder genuinely cannot continue" cases produce a
// sentinel.
func decoderECFuzzAcceptableError(err error) bool {
	return errors.Is(err, govpx.ErrInvalidData) ||
		errors.Is(err, govpx.ErrInvalidConfig) ||
		errors.Is(err, govpx.ErrFrameRejected)
}
