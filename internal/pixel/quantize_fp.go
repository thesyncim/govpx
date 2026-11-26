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
	if start >= n {
		return 0
	}
	coeff = coeff[:n]
	iscan = iscan[:n]
	dqcoeff = dqcoeff[:n]
	hasQ := qcoeff != nil
	if hasQ {
		qcoeff = qcoeff[:n]
	}
	hasC := classes != nil
	if hasC {
		classes = classes[:n]
	}
	i := start
	if i == 0 {
		r0, q0, d0 := int(r[0]), int(q[0]), int(d[0])
		c := int(coeff[0])
		a := max(c, -c)
		v := 0
		if a+r0 >= d0 {
			v = min(max(a+r0, -32768), 32767) * q0 >> 16
			if c < 0 {
				v = -v
			}
		}
		if hasQ {
			qcoeff[0] = int16(v)
		}
		dqcoeff[0] = int16(v * d0)
		if v != 0 {
			eob = max(eob, int(iscan[0]))
		}
		if hasC {
			classes[0] = tokenClass(v)
		}
		i = 1
	}
	r1, q1, d1 := int(r[1]), int(q[1]), int(d[1])
	thresh := d1 - r1
	if !hasC {
		if hasQ {
			if thresh > 1 && thresh <= 32768 {
				clear(qcoeff[i:n])
				clear(dqcoeff[i:n])
				off := int32(thresh - 1)
				lim := uint64(uint32(2*off)) + 1
				for ; i+3 < n; i += 4 {
					c0, c1, c2, c3 := int32(coeff[i]), int32(coeff[i+1]), int32(coeff[i+2]), int32(coeff[i+3])
					if ((uint64(uint32(c0+off))-lim)&(uint64(uint32(c1+off))-lim)&(uint64(uint32(c2+off))-lim)&(uint64(uint32(c3+off))-lim))>>63 != 0 {
						continue
					}
					for k := range 4 {
						idx := i + k
						c := int(coeff[idx])
						if uint(int32(c)+off) >= uint(lim) {
							a := max(c, -c)
							v := min(a+r1, 32767) * q1 >> 16
							if c < 0 {
								v = -v
							}
							qcoeff[idx] = int16(v)
							dqcoeff[idx] = int16(v * d1)
							if v != 0 {
								eob = max(eob, int(iscan[idx]))
							}
						}
					}
				}
				for ; i < n; i++ {
					c := int(coeff[i])
					if uint(int32(c)+off) >= uint(lim) {
						a := max(c, -c)
						v := min(a+r1, 32767) * q1 >> 16
						if c < 0 {
							v = -v
						}
						qcoeff[i] = int16(v)
						dqcoeff[i] = int16(v * d1)
						if v != 0 {
							eob = max(eob, int(iscan[i]))
						}
					}
				}
				return
			}
			for ; i < n; i++ {
				c := int(coeff[i])
				a := max(c, -c)
				if a >= thresh {
					v := min(max(a+r1, -32768), 32767) * q1 >> 16
					if c < 0 {
						v = -v
					}
					qcoeff[i] = int16(v)
					dqcoeff[i] = int16(v * d1)
					if v != 0 {
						eob = max(eob, int(iscan[i]))
					}
				} else {
					qcoeff[i] = 0
					dqcoeff[i] = 0
				}
			}
		} else {
			for ; i < n; i++ {
				c := int(coeff[i])
				a := max(c, -c)
				if a >= thresh {
					v := min(max(a+r1, -32768), 32767) * q1 >> 16
					if c < 0 {
						v = -v
					}
					dqcoeff[i] = int16(v * d1)
					if v != 0 {
						eob = max(eob, int(iscan[i]))
					}
				} else {
					dqcoeff[i] = 0
				}
			}
		}
		return
	}
	if hasQ && thresh > 1 && thresh <= 32768 {
		clear(qcoeff[i:n])
		clear(dqcoeff[i:n])
		clear(classes[i:n])
		off := int32(thresh - 1)
		lim := uint64(uint32(2*off)) + 1
		for ; i+3 < n; i += 4 {
			c0, c1, c2, c3 := int32(coeff[i]), int32(coeff[i+1]), int32(coeff[i+2]), int32(coeff[i+3])
			if ((uint64(uint32(c0+off))-lim)&(uint64(uint32(c1+off))-lim)&(uint64(uint32(c2+off))-lim)&(uint64(uint32(c3+off))-lim))>>63 != 0 {
				continue
			}
			for k := range 4 {
				idx := i + k
				c := int(coeff[idx])
				if uint(int32(c)+off) >= uint(lim) {
					a := max(c, -c)
					v := min(a+r1, 32767) * q1 >> 16
					if c < 0 {
						v = -v
					}
					qcoeff[idx] = int16(v)
					dqcoeff[idx] = int16(v * d1)
					if v != 0 {
						eob = max(eob, int(iscan[idx]))
						classes[idx] = tokenClass(v)
					}
				}
			}
		}
		for ; i < n; i++ {
			c := int(coeff[i])
			if uint(int32(c)+off) >= uint(lim) {
				a := max(c, -c)
				v := min(a+r1, 32767) * q1 >> 16
				if c < 0 {
					v = -v
				}
				qcoeff[i] = int16(v)
				dqcoeff[i] = int16(v * d1)
				if v != 0 {
					eob = max(eob, int(iscan[i]))
					classes[i] = tokenClass(v)
				}
			}
		}
		return
	}
	for ; i < n; i++ {
		c := int(coeff[i])
		a := max(c, -c)
		if a >= thresh {
			v := min(max(a+r1, -32768), 32767) * q1 >> 16
			if c < 0 {
				v = -v
			}
			if hasQ {
				qcoeff[i] = int16(v)
			}
			dqcoeff[i] = int16(v * d1)
			if v != 0 {
				eob = max(eob, int(iscan[i]))
			}
			classes[i] = tokenClass(v)
		} else {
			if hasQ {
				qcoeff[i] = 0
			}
			dqcoeff[i] = 0
			classes[i] = 0
		}
	}
	return
}

func tokenClass(v int) byte {
	a := max(v, -v)
	if a == 0 {
		return 0
	}
	if a == 1 {
		return 1
	}
	if a == 2 {
		return 2
	}
	if a <= 4 {
		return 3
	}
	if a <= 10 {
		return 4
	}
	return 5
}

// QuantizeVP8Block fuses quantization, dequantization and EOB over one block.
// round and quant retain their full signed scalar domain; the unsigned SIMD
// multiply handles nonnegative tables, including coefficient -32768 exactly.
// Quantized/dequantized output blocks must not overlap each other or tables.
func QuantizeVP8Block(coeff, round, quant, dequant, qcoeff, dqcoeff, iscan *[16]int16) int {
	if Enabled {
		if eob, ok := quantizeVP8SIMD(coeff, round, quant, dequant, qcoeff, dqcoeff, iscan); ok {
			return eob
		}
	}
	VP8QuantScalar(coeff[:], round[:], quant[:], dequant[:], qcoeff[:], dqcoeff[:])
	eob := 0
	for i, v := range qcoeff {
		if v != 0 {
			eob = max(eob, int(iscan[i]))
		}
	}
	return eob
}
