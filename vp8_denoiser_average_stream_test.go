package govpx

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"testing"
)

func TestDenoiserAverageStream(t *testing.T) {
	for _, sz := range [][2]int{{64, 48}, {65, 49}} {
		for _, threads := range []int{1, 2} {
			t.Run(fmt.Sprintf("%dx%d/threads%d", sz[0], sz[1], threads), func(t *testing.T) {

				opts := newSizedTestEncoder(t, sz[0], sz[1]).opts
				// Packet pins require a fixed speed: small adaptive frames use
				// real wall-clock feedback and can change picker dispatch.
				opts.CpuUsed = -8
				opts.NoiseSensitivity = 4
				opts.Threads = threads
				e, err := NewVP8Encoder(opts)
				if err != nil {
					t.Fatal(err)
				}
				dst := make([]byte, sz[0]*sz[1]*6+4096)
				hash := sha256.New()
				var n [8]byte
				for frame := range 8 {
					img := testImage(sz[0], sz[1])
					for y := range sz[1] {
						for x := range sz[0] {
							v := byte(95 + (x+3*y+frame%3)%5)
							if frame > 3 {
								v = byte((x*7 + y*11 + frame*13) ^ (x*y + frame*19))
							}
							img.Y[y*img.YStride+x] = v
						}
					}
					for i := range img.U {
						img.U[i] = byte(90 + i%3 + frame%2)
						img.V[i] = byte(170 + i%3 + frame%2)
					}
					result, err := e.EncodeInto(dst, img, uint64(frame), 1, 0)
					if err != nil {
						t.Fatal(err)
					}
					if e.libvpxCPUUsed() != 8 {
						t.Fatalf("fixed speed changed to %d", e.libvpxCPUUsed())
					}
					binary.LittleEndian.PutUint64(n[:], uint64(len(result.Data)))
					hash.Write(n[:])
					hash.Write(result.Data)
					fmt.Fprintf(hash, "%t/%t/%t|", result.KeyFrame, result.Dropped, result.Droppable)
				}
				got := fmt.Sprintf("%x", hash.Sum(nil))
				want := map[string]string{"64x48/threads1": "5bbc766d749d29e12dc496520ccdcb10b7b30737e45b2f03de136adc98e2c5b8", "64x48/threads2": "5d596825da65e6350753cd65fb4df7ebc6683075da3a62fca842dda89c9fe260", "65x49/threads1": "f5b4ca0def5a7e0ded48a0164a95f58bcb12546eca1d1dda3153011438a04f15", "65x49/threads2": "5d4f2efc5d3f3da952a986e4c250957374f9376ebaa5545ac3be3e5c8d905b17"}
				if got != want[t.Name()[len("TestDenoiserAverageStream/"):]] {
					t.Fatalf("noise stream hash %s", got)
				}
				t.Logf("denoiser_stream_sha256=%s", got)
			})
		}
	}
}
