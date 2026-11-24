//go:build goexperiment.simd && arm64 && !purego

package encoder

// Token categories follow libvpx v1.16.0; record packing follows tokenpack.go.

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

const denseTokenAvailable = true

var denseK19 = archsimd.BroadcastUint16x8(19)
var denseK5 = archsimd.BroadcastUint16x8(5)
var denseK3 = archsimd.BroadcastUint16x8(3)
var denseK10 = archsimd.BroadcastUint16x8(10)
var denseK4 = archsimd.BroadcastUint16x8(4)
var denseSignShift = archsimd.BroadcastInt16x8(-15)
var denseMagnitudeShift = archsimd.BroadcastInt32x4(15)
var denseRecordSignShift = archsimd.BroadcastInt32x4(27)

func denseTokenPrepack(q *[16]int16, out *[16]uint32) bool {
	if !pixel.Enabled || q == nil || out == nil {
		return false
	}
	a, b := archsimd.LoadInt16x8(q[:8]), archsimd.LoadInt16x8(q[8:])
	ma, mb := a.Abs().ToBits(), b.Abs().ToBits()
	if ma.Max(mb).ReduceMax() > 2048 {
		return false
	}
	denseTokenPacket(a, ma, out[:8])
	denseTokenPacket(b, mb, out[8:])
	return true
}

func denseTokenPacket(v archsimd.Int16x8, m archsimd.Uint16x8, out []uint32) {
	large := denseK19.Sub(m.Max(denseK5).Sub(denseK3).LeadingZeros()).Min(denseK10)
	token := large.IfElse(m.Greater(denseK4), m)
	sign := v.ToBits().Shift(denseSignShift)
	lo := m.ExtendLo4ToUint32().Shift(denseMagnitudeShift).Or(sign.ExtendLo4ToUint32().Shift(denseRecordSignShift)).Or(token.ExtendLo4ToUint32())
	hi := m.HiToLo().ExtendLo4ToUint32().Shift(denseMagnitudeShift).Or(sign.HiToLo().ExtendLo4ToUint32().Shift(denseRecordSignShift)).Or(token.HiToLo().ExtendLo4ToUint32())
	lo.Store(out[:4])
	hi.Store(out[4:])
}
