package encoder

import (
	"testing"

	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
)

var lumaSSENativeSink int

func BenchmarkLoopFilterLumaSSENative(b *testing.B) {
	for _, tc := range []struct {
		name    string
		w, h    int
		partial bool
	}{
		{"128x128", 128, 128, false}, {"720p", 1280, 720, false}, {"720pPartial", 1280, 720, true}, {"1088p", 1920, 1088, false},
	} {
		b.Run(tc.name, func(b *testing.B) {
			ss, rs := tc.w+32, tc.w+64
			a, ref := make([]byte, (tc.h-1)*ss+tc.w), make([]byte, (tc.h-1)*rs+tc.w)
			for i := range a {
				a[i] = byte(i*37 + 11)
			}
			for i := range ref {
				ref[i] = byte(i*19 + 53)
			}
			src := SourceImage{Y: a, YStride: ss, Width: tc.w, Height: tc.h}
			img := vp8common.Image{Y: ref, YStride: rs, CodedWidth: tc.w, CodedHeight: tc.h}
			rows, cols := tc.h/16, tc.w/16
			start, count := 0, rows
			if tc.partial {
				start, count = LoopFilterPartialFrameWindow(rows)
			}
			want := 0
			for y := start * 16; y < (start+count)*16; y++ {
				for x := range tc.w {
					d := int(a[y*ss+x]) - int(ref[y*rs+x])
					want += d * d
				}
			}
			got := 0
			b.ReportAllocs()
			for b.Loop() {
				got = LoopFilterLumaSSE(src, &img, rows, cols, tc.partial)
			}
			if got != want {
				b.Fatalf("got%d want%d", got, want)
			}
			lumaSSENativeSink = got
		})
	}
}
