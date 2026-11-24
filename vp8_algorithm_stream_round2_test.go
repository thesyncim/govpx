package govpx

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
)

// TestVP8Algorithm2Stream pins packets and frame flags to the immutable b57 baseline.
func TestVP8Algorithm2Stream(t *testing.T) {
	for _, sz := range [][2]int{{48, 48}, {65, 49}, {160, 90}} {
		for _, tune := range []Tuning{TunePSNR, TuneSSIM} {
			t.Run(fmt.Sprintf("%dx%d/tune%d", sz[0], sz[1], tune), func(t *testing.T) {
				e := newSizedTestEncoder(t, sz[0], sz[1])
				e.opts.Tuning = tune
				dst := make([]byte, sz[0]*sz[1]*6+4096)
				h := sha256.New()
				var length [8]byte
				for frame := 0; frame < 16; frame++ {
					img := testImage(sz[0], sz[1])
					for y := 0; y < sz[1]; y++ {
						for x := 0; x < sz[0]; x++ {
							v := byte((x*7 + y*11 + frame*13) ^ (x*y + frame*19))
							if frame < 4 {
								v = byte(96 + frame)
							}
							img.Y[y*img.YStride+x] = v
						}
					}
					for i := range img.U {
						img.U[i] = byte(i*13 + frame*7)
						img.V[i] = byte(i*29 + frame*11)
					}
					r, err := e.EncodeInto(dst, img, uint64(frame), 1, 0)
					if err != nil {
						t.Fatal(err)
					}
					binary.LittleEndian.PutUint64(length[:], uint64(len(r.Data)))
					h.Write(length[:])
					h.Write(r.Data)
					fmt.Fprintf(h, "%t,%t,%t,%d|", r.KeyFrame, r.Dropped, r.Droppable, r.LookaheadDepth)
				}
				got := fmt.Sprintf("%x", h.Sum(nil))
				expected := map[string]string{"48x48/tune0": "94d2d44afce27325c5e0e6298f53d87e65e826e9846d6ac609d274b114f9e162", "48x48/tune1": "94d2d44afce27325c5e0e6298f53d87e65e826e9846d6ac609d274b114f9e162", "65x49/tune0": "021db19fc47e375025f91a7767d68fbf1b5d06e08d4edc23c2b8803400fc42b0", "65x49/tune1": "a7e6780411fc2c1eb332cf2e4749f6e5213860fe84ffec30676ea3c2126cda02", "160x90/tune0": "205c81a59a3d27c6d50b0f7704366cd19a26a206ca0ce058551e4c35807cb8df", "160x90/tune1": "7768d90618c27fe28acd32327861d9f5980f28b9ea118f970b9713ae4177c32b"}
				if got != expected[t.Name()[len("TestVP8Algorithm2Stream/"):]] {
					t.Fatalf("stream hash %s", got)
				}
				t.Logf("vp8_stream_sha256=%s", got)
			})
		}
	}
}
