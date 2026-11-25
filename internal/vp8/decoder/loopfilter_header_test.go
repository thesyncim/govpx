package decoder

import (
	"bytes"
	"fmt"
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
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

func TestParseLoopFilterHeaderCarriesPreviousDeltasWithoutUpdate(t *testing.T) {
	previous := LoopFilterHeader{
		RefDeltas:  [4]int8{1, -2, 3, -4},
		ModeDeltas: [4]int8{5, -6, 7, -8},
	}
	payload := encodeLoopFilterHeaderNoDeltaUpdate(11, 2, true)
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	header := parseLoopFilterHeaderWithPrevious(&br, previous)

	if header.Type != NormalLoopFilter || header.Level != 11 || header.SharpnessLevel != 2 {
		t.Fatalf("loop header basics = %+v, want normal level 11 sharpness 2", header)
	}
	if !header.DeltaEnabled || header.DeltaUpdate {
		t.Fatalf("delta flags = enabled:%t update:%t, want enabled without update", header.DeltaEnabled, header.DeltaUpdate)
	}
	if header.RefDeltas != previous.RefDeltas || header.ModeDeltas != previous.ModeDeltas {
		t.Fatalf("deltas = %v/%v, want previous %v/%v", header.RefDeltas, header.ModeDeltas, previous.RefDeltas, previous.ModeDeltas)
	}
}

func TestParseLoopFilterHeaderAppliesPartialDeltaUpdates(t *testing.T) {
	previous := LoopFilterHeader{
		RefDeltas:  [4]int8{1, -2, 3, -4},
		ModeDeltas: [4]int8{5, -6, 7, -8},
	}
	payload := encodeLoopFilterHeaderPartialDeltaUpdate()
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	header := parseLoopFilterHeaderWithPrevious(&br, previous)

	if !header.DeltaEnabled || !header.DeltaUpdate {
		t.Fatalf("delta flags = enabled:%t update:%t, want enabled update", header.DeltaEnabled, header.DeltaUpdate)
	}
	if header.RefDeltas != ([4]int8{-5, -2, 3, -4}) {
		t.Fatalf("ref deltas = %v, want partial update", header.RefDeltas)
	}
	if header.ModeDeltas != ([4]int8{5, 7, 7, -8}) {
		t.Fatalf("mode deltas = %v, want partial update", header.ModeDeltas)
	}
}

func TestParseLoopFilterHeaderAllocatesZero(t *testing.T) {
	payload := encodeLoopFilterHeaderPartialDeltaUpdate()
	previous := LoopFilterHeader{RefDeltas: [4]int8{1, -2, 3, -4}, ModeDeltas: [4]int8{5, -6, 7, -8}}

	allocs := testing.AllocsPerRun(1000, func() {
		var br boolcoder.Decoder
		_ = br.Init(payload)
		_ = parseLoopFilterHeaderWithPrevious(&br, previous)
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func encodeLoopFilterHeaderNoDeltaUpdate(level uint8, sharpness uint8, enabled bool) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeLiteral(uint32(level), 6)
	w.writeLiteral(uint32(sharpness), 3)
	if enabled {
		w.writeBool(1, 128)
		w.writeBool(0, 128)
	} else {
		w.writeBool(0, 128)
	}
	return w.finish()
}

func encodeLoopFilterHeaderPartialDeltaUpdate() []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(1, 128)
	w.writeLiteral(20, 6)
	w.writeLiteral(3, 3)
	w.writeBool(1, 128)
	w.writeBool(1, 128)

	for i := range 4 {
		if i == 0 {
			w.writeBool(1, 128)
			w.writeLiteral(5, 6)
			w.writeBool(1, 128)
		} else {
			w.writeBool(0, 128)
		}
	}
	for i := range 4 {
		if i == 1 {
			w.writeBool(1, 128)
			w.writeLiteral(7, 6)
			w.writeBool(0, 128)
		} else {
			w.writeBool(0, 128)
		}
	}
	return w.finish()
}
