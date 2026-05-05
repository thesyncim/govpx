package encoder

import "github.com/thesyncim/libgopx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vp8/encoder/mcomp.c and bitstream MV component
// packing in vp8/encoder/bitstream.c.

const (
	mvProbIsShort = 0
	mvProbSign    = 1
	mvProbShort   = 2
	mvNumShort    = 8
	mvProbBits    = mvProbShort + mvNumShort - 1
	mvLongWidth   = 10
)

type MotionVector struct {
	Row int16
	Col int16
}

var smallMVTokens = initSmallMVTokens()

func WriteMotionVector(w *BoolWriter, probs *[2][tables.MVPCount]uint8, mv MotionVector) error {
	if w == nil || probs == nil || mv.Row&1 != 0 || mv.Col&1 != 0 {
		return ErrInvalidPacketConfig
	}
	if !writeMVComponent(w, probs[0][:], int(mv.Row/2)) {
		return ErrInvalidPacketConfig
	}
	if !writeMVComponent(w, probs[1][:], int(mv.Col/2)) {
		return ErrInvalidPacketConfig
	}
	if w.Err() != nil {
		return w.Err()
	}
	return nil
}

func writeMVComponent(w *BoolWriter, probs []uint8, component int) bool {
	negative := component < 0
	if negative {
		component = -component
	}
	if component >= 8 {
		return writeLargeMVComponent(w, probs, component, negative)
	}
	if len(probs) < tables.MVPCount {
		return false
	}
	w.WriteBool(0, probs[mvProbIsShort])
	if !WriteTreeToken(w, tables.SmallMVTree[:], probs[mvProbShort:], smallMVTokens[component]) {
		return false
	}
	if component != 0 {
		writeBoolProb(w, negative, probs[mvProbSign])
	}
	return w.Err() == nil
}

func writeLargeMVComponent(w *BoolWriter, probs []uint8, component int, negative bool) bool {
	if len(probs) < tables.MVPCount || component < 8 || component > 0x7ff {
		return false
	}
	w.WriteBool(1, probs[mvProbIsShort])

	coded := component
	if component < 16 {
		coded = component - 8
	}
	for i := 0; i < 3; i++ {
		w.WriteBool(uint8((coded>>i)&1), probs[mvProbBits+i])
	}
	for i := mvLongWidth - 1; i > 3; i-- {
		w.WriteBool(uint8((coded>>i)&1), probs[mvProbBits+i])
	}
	if coded&0xfff0 != 0 {
		w.WriteBool(uint8((component>>3)&1), probs[mvProbBits+3])
	}
	if component != 0 {
		writeBoolProb(w, negative, probs[mvProbSign])
	}
	return w.Err() == nil
}

func writeBoolProb(w *BoolWriter, value bool, prob uint8) {
	if value {
		w.WriteBool(1, prob)
		return
	}
	w.WriteBool(0, prob)
}

func initSmallMVTokens() [8]TreeToken {
	var out [8]TreeToken
	for i := range out {
		BuildTreeToken(tables.SmallMVTree[:], i, &out[i])
	}
	return out
}
