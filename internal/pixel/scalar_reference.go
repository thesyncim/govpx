// Independent scalar pixel kernels, also used for CPUs without AVX2.
package pixel

func SADScalar(src []byte, ss int, ref []byte, rs, w, h int) (sum int) {
	for y := range h {
		for x := range w {
			d := int(src[y*ss+x]) - int(ref[y*rs+x])
			sum += max(d, -d)
		}
	}
	return
}
func StatsScalar(src []byte, ss int, ref []byte, rs, w, h int) (sum int, sse uint32) {
	for y := range h {
		for x := range w {
			d := int(src[y*ss+x]) - int(ref[y*rs+x])
			sum += d
			sse += uint32(d * d)
		}
	}
	return
}
func SubtractScalar(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) (nonzero bool) {
	for y := range h {
		for x := range w {
			d := int16(src[y*ss+x]) - int16(pred[y*ps+x])
			out[y*os+x] = d
			nonzero = nonzero || d != 0
		}
	}
	return
}
func FilterScalar(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) {
	for y := range h {
		for x := range w {
			v := 64
			for k, c := range taps {
				v += int(src[y*ss+x+k*step]) * int(c)
			}
			dst[y*ds+x] = byte(min(max(v>>7, 0), 255))
		}
	}
}
func AverageScalar(src []byte, ss int, dst []byte, ds, w, h int) {
	for y := range h {
		for x := range w {
			dst[y*ds+x] = byte((int(src[y*ss+x]) + int(dst[y*ds+x]) + 1) >> 1)
		}
	}
}
func ErrorScalar(a, b []int16, n int) (sum uint64) {
	for i := range n {
		d := int64(a[i]) - int64(b[i])
		sum += uint64(d * d)
	}
	return
}

func QuantizeScalar(coeff, iscan, qcoeff, dqcoeff []int16, n, zbin, round, quant, shift, dequant int, fp bool) (eob int) {
	for i := range n {
		c := int(coeff[i])
		a := max(c, -c)
		q := 0
		if (fp && a+round >= dequant) || (!fp && a >= zbin) {
			v := min(max(a+round, -32768), 32767)
			if fp {
				q = v * quant >> 16
			} else {
				q = (((v * quant >> 16) + v) * shift) >> 16
			}
			if c < 0 {
				q = -q
			}
		}
		if qcoeff != nil {
			qcoeff[i] = int16(q)
		}
		dqcoeff[i] = int16(q * dequant)
		if q != 0 {
			eob = max(eob, int(iscan[i]))
		}
	}
	return
}

func GatherScalar(src []byte, ss int, pred []byte, ps int, out []int16, w int) {
	for y := range w {
		for x := range w {
			off := ((y/4)*(w/4)+x/4)*16 + (y%4)*4 + x%4
			out[off] = int16(src[y*ss+x]) - int16(pred[y*ps+x])
		}
	}
}
func TemporalScalar(src []byte, ss int, pred []byte, ps, w, strength, weight int, acc, count []uint32) {
	r := 0
	if strength > 0 {
		r = 1 << (strength - 1)
	}
	for y := range w {
		for x := range w {
			d := int(src[y*ss+x]) - int(pred[y*ps+x])
			m := (16 - min((d*d*3+r)>>uint(strength), 16)) * weight
			i := y*w + x
			count[i] += uint32(m)
			acc[i] += uint32(m) * uint32(pred[y*ps+x])
		}
	}
}
func VP8QuantScalar(c, r, q, d, qout, dout []int16) {
	for i := range 16 {
		v := int(c[i])
		a := max(v, -v)
		t := (a + int(r[i])) * int(q[i]) >> 16
		if v < 0 {
			t = -t
		}
		qout[i] = int16(t)
		dout[i] = int16(t * int(d[i]))
	}
}
func AddConstantScalar(pred []byte, ps int, dst []byte, ds, w, h, delta int) {
	for y := range h {
		for x := range w {
			dst[y*ds+x] = byte(min(max(int(pred[y*ps+x])+delta, 0), 255))
		}
	}
}
func TMPredictScalar(dst []byte, ds int, above, left []byte, top byte, w int) {
	for y := range w {
		for x := range w {
			dst[y*ds+x] = byte(min(max(int(above[x])+int(left[y])-int(top), 0), 255))
		}
	}
}

func SubpelStatsScalar(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (sum int, sse uint32) {
	for y := range h {
		for x := range w {
			a := (int(src[y*ss+x])*int(fx[0]) + int(src[y*ss+x+1])*int(fx[1]) + 64) >> 7
			b := (int(src[(y+1)*ss+x])*int(fx[0]) + int(src[(y+1)*ss+x+1])*int(fx[1]) + 64) >> 7
			v := (a*int(fy[0]) + b*int(fy[1]) + 64) >> 7
			d := v - int(ref[y*rs+x])
			sum += d
			sse += uint32(d * d)
		}
	}
	return
}
func BilinearFirstScalar(src []byte, ss int, dst []uint16, w, h int, f [2]int16) {
	for y := range h {
		for x := range w {
			dst[y*w+x] = uint16((int(src[y*ss+x])*int(f[0]) + int(src[y*ss+x+1])*int(f[1]) + 64) >> 7)
		}
	}
}
func BilinearSecondScalar(src []uint16, dst []byte, w, h int, f [2]int16) {
	for y := range h {
		for x := range w {
			i := y*w + x
			dst[i] = byte((int(src[i])*int(f[0]) + int(src[i+w])*int(f[1]) + 64) >> 7)
		}
	}
}

func SumScalar(src []byte, ss, w, h int) (sum int) {
	for y := range h {
		for x := range w {
			sum += int(src[y*ss+x])
		}
	}
	return
}
func EnergyScalar(src []int16) (sum uint64) {
	for _, s := range src {
		v := int64(s)
		sum += uint64(v * v)
	}
	return
}
func AbsSumScalar(src []int16, n int) (sum int) {
	for _, s := range src[:n] {
		v := int(s)
		sum += max(v, -v)
	}
	return
}
func ProjectionRowScalar(src []byte, ss, h int, out []int16, shift uint64) {
	for x := range 16 {
		v := 0
		for y := range h {
			v += int(src[y*ss+x])
		}
		out[x] = int16(v >> shift)
	}
}
func VectorVarianceScalar(ref, src []int16, n, shift int) int {
	sum, sse := 0, 0
	for i := range n {
		d := int(ref[i]) - int(src[i])
		sum += d
		sse += d * d
	}
	return sse - (sum * sum >> uint(shift))
}
func DenoiseFirstScalar(mc []byte, ms int, avg []byte, as int, sig []byte, ss, w, inc, level int) int {
	var columns [16]int
	for y := range w {
		for x := range w {
			d := int(mc[y*ms+x]) - int(sig[y*ss+x])
			a := max(d, -d)
			adj := level + 3
			if a <= 15 {
				adj = level + 1
			}
			if a <= 7 {
				adj = level
			}
			if a <= 3+inc {
				adj = a
			}
			if d < 0 {
				adj = -adj
			}
			avg[y*as+x] = byte(min(max(int(sig[y*ss+x])+adj, 0), 255))
			columns[x] += adj
		}
	}
	sum := 0
	for _, v := range columns[:w] {
		if w == 16 {
			v = min(v, 127)
		}
		sum += v
	}
	return sum
}

func Intra4Scalar(dst []byte, ds int, b *[16]byte, indices *[4][16]byte) {
	for i := range 16 {
		sum := 2
		for k := range 4 {
			sum += int(b[indices[k][i]])
		}
		dst[(i/4)*ds+i%4] = byte(sum >> 2)
	}
}
func FillBlockScalar(dst []byte, ds, w, h int, v byte) {
	for y := range h {
		for x := range w {
			dst[y*ds+x] = v
		}
	}
}
func RepeatRowScalar(dst []byte, ds int, a []byte, w int) {
	for y := range w {
		copy(dst[y*ds:y*ds+w], a[:w])
	}
}
func HorizontalScalar(dst []byte, ds int, l []byte, w int) {
	for y := range w {
		for x := range w {
			dst[y*ds+x] = l[y]
		}
	}
}

func Mul16Scalar(a, b, out []int16, n int) {
	for i := range n {
		out[i] = a[i] * b[i]
	}
}

func SAD16Scalar(src []byte, ss int, ref []byte, rs, h int) int {
	return SADScalar(src, ss, ref, rs, 16, h)
}
func SAD16LimitScalar(src []byte, ss int, ref []byte, rs, h, limit int) (sum int) {
	for y := range h {
		sum += SADScalar(src[y*ss:], ss, ref[y*rs:], rs, 16, 1)
		if sum > limit {
			return
		}
	}
	return
}
func SAD16x4Scalar(src []byte, ss int, r0, r1, r2, r3 []byte, rs, h int, out *[4]uint32) {
	out[0] = uint32(SADScalar(src, ss, r0, rs, 16, h))
	out[1] = uint32(SADScalar(src, ss, r1, rs, 16, h))
	out[2] = uint32(SADScalar(src, ss, r2, rs, 16, h))
	out[3] = uint32(SADScalar(src, ss, r3, rs, 16, h))
}
