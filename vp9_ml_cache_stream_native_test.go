package govpx_test

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"github.com/thesyncim/govpx"
	"testing"
)

func TestMLCacheStreamNative(t *testing.T) {
	for _, size := range [][2]int{{64, 64}, {320, 180}, {352, 288}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			opts := vp9SyntheticRampOptions(size[0], size[1], 500)
			opts.CpuUsed = 8
			enc, err := govpx.NewVP9Encoder(opts)
			if err != nil {
				t.Fatal(err)
			}
			dst := make([]byte, size[0]*size[1]*6)
			h := sha256.New()
			var length [8]byte
			for frame := range 24 {
				img := makeSyntheticRampFrame(size[0], size[1], frame)
				if frame < 8 {
					for i := range img.Y {
						img.Y[i] = byte(96 + frame)
					}
				}
				r, err := enc.EncodeIntoWithResult(img, dst)
				if err != nil {
					t.Fatal(err)
				}
				binary.LittleEndian.PutUint64(length[:], uint64(len(r.Data)))
				h.Write(length[:])
				h.Write(r.Data)
			}
			t.Logf("stream_sha256=%x", h.Sum(nil))
		})
	}
}
