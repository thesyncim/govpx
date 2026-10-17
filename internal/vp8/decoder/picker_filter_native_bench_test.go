package decoder

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
)

var pickerFilterNativeSink byte

// The reference keeps the four original public edge operations separate.
// Pixel-level tests independently compare the fused arithmetic with scalar
// libvpx equations; this fixture also checks the complete public caller output.
func pickerFilterSeparateEdges(img *common.Image, off int, lfi *common.LoopFilterInfo) {
	const level = 32
	hev := lfi.HEVThresh[lfi.HEVThreshLUT[common.InterFrame][level]]
	mb, bl, lim := lfi.MBLimit[level], lfi.BLimit[level], lfi.Limit[level]
	dsp.MBLoopFilterVerticalEdge(img.Y[off-4:], img.YStride, mb, lim, hev, 2)
	dsp.LoopFilterVerticalEdgesY(img.Y[off:], img.YStride, bl, lim, hev)
	dsp.MBLoopFilterHorizontalEdge(img.Y[off-4*img.YStride:], img.YStride, mb, lim, hev, 2)
	dsp.LoopFilterHorizontalEdgesY(img.Y[off:], img.YStride, bl, lim, hev)
}

func BenchmarkVP8PickerFilterNative(b *testing.B) {
	for _, pitch := range []int{32, 1312} {
		for _, pattern := range []string{"flat", "ramp", "smooth", "mixed", "noop"} {
			b.Run(fmt.Sprintf("pitch%d/%s", pitch, pattern), func(b *testing.B) {
				original := make([]byte, 31*pitch+32)
				for y := range 32 {
					for x := range 32 {
						v := byte(112 + (x*3+y*5)%17)
						if pattern == "ramp" {
							v = byte(32 + x + y)
						}
						if pattern == "flat" {
							v = 128
						}
						if pattern == "noop" || (pattern == "mixed" && x%7 == 0) {
							v = byte((y*32+x)*37 + 11)
						}
						original[y*pitch+x] = v
					}
				}
				got, want := bytes.Clone(original), bytes.Clone(original)
				img := common.Image{Y: got, YStride: pitch, CodedWidth: 32, CodedHeight: 32}
				ref := common.Image{Y: want, YStride: pitch, CodedWidth: 32, CodedHeight: 32}
				var lfi common.LoopFilterInfo
				common.InitLoopFilterInfo(&lfi, 0)
				common.InitLoopFilterFrame(&lfi, 32, common.LoopFilterFrameConfig{})
				mode := MacroblockMode{}
				off := 16*pitch + 16
				pickerFilterSeparateEdges(&ref, off, &lfi)
				b.ReportAllocs()
				for b.Loop() {
					// Restore exactly the union of the horizontal/vertical read windows.
					for y := 12; y < 32; y++ {
						copy(got[y*pitch+12:y*pitch+32], original[y*pitch+12:y*pitch+32])
					}
					applyNormalLoopFilterPartialLumaMB(&img, 1, 1, off, &mode, common.InterFrame, 32, &lfi, false)
				}
				if !bytes.Equal(got, want) {
					b.Fatal("picker filter output differs from separate edges")
				}
				pickerFilterNativeSink = got[off]
			})
		}
	}
}
