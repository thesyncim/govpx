package bitstream

import (
	"errors"
	"testing"

	vpxerrors "github.com/thesyncim/govpx/internal/vpx/errors"
)

// FuzzParseSuperframe feeds arbitrary bytes to the VP9 superframe-index parser.
// The parser must classify any input as a valid superframe, a non-superframe,
// or ErrInvalidVP9Data, and it must never panic.
func FuzzParseSuperframe(f *testing.F) {
	for _, seed := range superframeFuzzSeeds() {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, packet []byte) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("ParseSuperframe panicked on %d-byte input: %v",
					len(packet), r)
			}
		}()
		sf, err := ParseSuperframe(packet)
		if err != nil {
			if !errors.Is(err, vpxerrors.ErrInvalidVP9Data) {
				t.Fatalf("ParseSuperframe err = %v, want ErrInvalidVP9Data", err)
			}
			return
		}
		if sf.Count < 0 || sf.Count > 8 {
			t.Fatalf("superframe count = %d, want [0, 8]", sf.Count)
		}
		total := 0
		for i := 0; i < sf.Count; i++ {
			if sf.Frames[i] == nil {
				t.Fatalf("frame %d slice is nil", i)
			}
			if len(sf.Frames[i]) == 0 {
				t.Fatalf("frame %d slice is empty", i)
			}
			total += len(sf.Frames[i])
		}
		if total > len(packet) {
			t.Fatalf("frames total %d exceeds packet %d", total, len(packet))
		}
	})
}

func superframeFuzzSeeds() [][]byte {
	return [][]byte{
		nil,
		{},
		{0},
		{0xc0},
		{0xc0, 0x00},
		{0xc0, 0x00, 0xc0},
		{0xc1, 0x01, 0x01, 0xc1},
		{0xc7, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xc7},
		{0xff},
		{0x01, 0x02, 0xc1, 0x01, 0x01, 0xc1},
	}
}
