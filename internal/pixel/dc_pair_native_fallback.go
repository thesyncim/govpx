package pixel

// Keep libvpx v1.16.0 idct_dequant_0_2x_neon's int32 product rounding:
// input+4 wraps in int32 before the arithmetic shift, then clips the delta.
func dcPairNativeDelta(input int32) int16 {
	v := int((input + 4) >> 3)
	if v > 255 {
		v = 255
	} else if v < -255 {
		v = -255
	}
	return int16(v)
}

// dcPairNativeScalar retains the disabled DSP's row prechecks and block order.
// It is distinct from the generic pixel scalar per-pixel bounds behavior.
func dcPairNativeScalar(c0, c1 int32, pred []byte, ps int, dst []byte, ds int) {
	d0, d1 := dcPairNativeDelta(c0), dcPairNativeDelta(c1)
	dcPairNativeScalarBlock(int(d0), pred, ps, dst, ds)
	dcPairNativeScalarBlock(int(d1), pred[4:], ps, dst[4:], ds)
}
func dcPairNativeScalarBlock(delta int, pred []byte, ps int, dst []byte, ds int) {
	for y := range 4 {
		d := dst[y*ds : y*ds+4 : y*ds+4]
		p := pred[y*ps : y*ps+4 : y*ps+4]
		d[0] = byte(min(max(delta+int(p[0]), 0), 255))
		d[1] = byte(min(max(delta+int(p[1]), 0), 255))
		d[2] = byte(min(max(delta+int(p[2]), 0), 255))
		d[3] = byte(min(max(delta+int(p[3]), 0), 255))
	}
}
