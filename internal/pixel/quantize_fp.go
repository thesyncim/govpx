package pixel

// QuantizeFPBlock handles the DC/AC parameter split inside one block walk.
// classes is optional; a supplied window receives every raster token class.
func QuantizeFPBlock(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	if n <= 0 {
		return 0
	}
	_ = coeff[n-1]
	_ = iscan[n-1]
	_ = dqcoeff[n-1]
	if qcoeff != nil {
		_ = qcoeff[n-1]
	}
	if classes != nil {
		_ = classes[n-1]
	}
	if Enabled && r[0] >= 0 && r[1] >= 0 && q[0] >= 0 && q[1] >= 0 && d[0] > 0 && d[1] > 0 {
		return quantizeFPBlockSIMD(coeff, iscan, qcoeff, dqcoeff, n, r, q, d, classes)
	}
	return quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff, 0, n, r, q, d, classes)
}
func quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff []int16, start, n int, r, q, d [2]int16, classes []byte) (eob int) {
	for i := start; i < n; i++ {
		p := 1
		if i == 0 {
			p = 0
		}
		c := int(coeff[i])
		a := max(c, -c)
		v := 0
		if a+int(r[p]) >= int(d[p]) {
			v = min(max(a+int(r[p]), -32768), 32767) * int(q[p]) >> 16
			if c < 0 {
				v = -v
			}
		}
		if qcoeff != nil {
			qcoeff[i] = int16(v)
		}
		dqcoeff[i] = int16(v * int(d[p]))
		if v != 0 {
			eob = max(eob, int(iscan[i]))
		}
		if classes != nil {
			a = max(v, -v)
			class := byte(0)
			if a > 0 {
				class++
			}
			if a > 1 {
				class++
			}
			if a > 2 {
				class++
			}
			if a > 4 {
				class++
			}
			if a > 10 {
				class++
			}
			classes[i] = class
		}
	}
	return
}
