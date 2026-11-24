package decoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/common"
	"github.com/thesyncim/govpx/internal/vp8/dsp"
	"testing"
)

func frozenAxisParentApply(img *common.Image, row int, col int, yOff int, mode *MacroblockMode, frameType common.FrameType, level byte, lfi *common.LoopFilterInfo, topContext bool) {
	skipLF := loopFilterSkipsInnerEdges(mode)
	hev := lfi.HEVThresh[lfi.HEVThreshLUT[frameType][level]]
	mblim := lfi.MBLimit[level]
	blim := lfi.BLimit[level]
	lim := lfi.Limit[level]

	fusedV := macroInternalLoopFilter && col > 0 && !skipLF && pixel.LoopFilterMacroInternal16(img.Y[yOff-4:], 1, img.YStride, int(mblim), int(blim), int(lim), int(hev))
	if col > 0 && !fusedV {
		dsp.MBLoopFilterVerticalEdge(img.Y[yOff-4:], img.YStride, mblim, lim, hev, 2)
	}
	if !skipLF && !fusedV {
		dsp.LoopFilterVerticalEdgesY(img.Y[yOff:], img.YStride, blim, lim, hev)
	}

	fusedH := macroInternalLoopFilter && (row > 0 || topContext) && !skipLF && pixel.LoopFilterMacroInternal16(loopFilterYAt(img, yOff-4*img.YStride), img.YStride, 1, int(mblim), int(blim), int(lim), int(hev))
	if (row > 0 || topContext) && !fusedH {
		dsp.MBLoopFilterHorizontalEdge(loopFilterYAt(img, yOff-4*img.YStride), img.YStride, mblim, lim, hev, 2)
	}
	if !skipLF && !fusedH {
		dsp.LoopFilterHorizontalEdgesY(img.Y[yOff:], img.YStride, blim, lim, hev)
	}
}

func TestMacroAxisCallerFrozenState(t *testing.T) {
	for _, pitch := range []int{-1, 0, 1, 15, 16, 19, 20, 21, 32, 1312, int(^uint(0) >> 1)} {
		for _, size := range []int{0, 1, 319, 320, 639, 640, 1024, 41000} {
			for _, off := range []int{-1, 0, 4, 16, 128} {
				for _, top := range []bool{false, true} {
					for _, clip := range []bool{false, true} {
						original := make([]byte, size+32)
						for i := range original {
							original[i] = byte(112 + (i*7+i/17)%23)
						}
						got, want := bytes.Clone(original), bytes.Clone(original)
						var lfi common.LoopFilterInfo
						common.InitLoopFilterInfo(&lfi, 0)
						common.InitLoopFilterFrame(&lfi, 32, common.LoopFilterFrameConfig{})
						run := func(p []byte, old bool) (panicText string) {
							defer func() {
								if r := recover(); r != nil {
									panicText = fmt.Sprint(r)
								}
							}()
							y := p[16 : 16+size]
							if clip {
								y = y[:size:size]
							}
							img := common.Image{Y: y, YFull: p, YOrigin: 16, YStride: pitch}
							mode := MacroblockMode{}
							if old {
								frozenAxisParentApply(&img, 1, 1, off, &mode, common.InterFrame, 32, &lfi, top)
							} else {
								applyNormalLoopFilterPartialLumaMB(&img, 1, 1, off, &mode, common.InterFrame, 32, &lfi, top)
							}
							return
						}
						gp, wp := run(got, false), run(want, true)
						if gp != wp || !bytes.Equal(got, want) {
							t.Fatalf("pitch%d size%d off%d top%v clip%v panic got%q want%q", pitch, size, off, top, clip, gp, wp)
						}
					}
				}
			}
		}
	}
}
