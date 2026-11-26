package encoder

import (
	"math/bits"

	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Pure-Go fallback dispatcher for the batched fast-quantize entry
// point. Mirrors libvpx v1.16.0 vp8/encoder/vp8_quantize.c
// vp8_fast_quantize_b_c per block.

func fastQuantizeBlockBatchSIMD(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	// Shared-table SIMD amortizes setup for the production eight/16-block batches.
	// Single blocks retain the lower-overhead per-block path.
	if count >= 2 && pixel.QuantizeVP8Batch(coeff, &quant.Round, &quant.QuantFast, &quant.Dequant, qcoeff, dqcoeff, &tables.DefaultInvZigZag, eobs, count) {
		return
	}
	for i := range count {
		eobs[i] = uint8(fastQuantizeBlockSIMD((*[16]int16)(coeff[i*16:]), quant, (*[16]int16)(qcoeff[i*16:]), (*[16]int16)(dqcoeff[i*16:])))
	}
}

func fastQuantizeBlockBatchScalar(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	for i := range count {
		// Pointer-cast the per-block slices to 16-element array views so
		// the kernel reads coeff and writes qcoeff/dqcoeff in place,
		// skipping the staging arrays and the three copies per block.
		c := (*[16]int16)(coeff[i*16 : i*16+16])
		q := (*[16]int16)(qcoeff[i*16 : i*16+16])
		dq := (*[16]int16)(dqcoeff[i*16 : i*16+16])
		eobs[i] = uint8(fastQuantizeBlockScalar(c, quant, q, dq))
	}
}

func fastQuantizeBlockBatchOnly(coeff []int16, quant *BlockQuant, qcoeff []int16, eobs []uint8, count int) {
	if count >= 2 && pixel.QuantizeVP8BatchOnly(coeff, &quant.Round, &quant.QuantFast, qcoeff, &tables.DefaultInvZigZag, eobs, count) {
		return
	}
	if !pixel.Enabled {
		if isUniformACQuant(quant) {
			r0, q0 := int(quant.Round[0]), int(quant.QuantFast[0])
			r1, q1 := int(quant.Round[1]), int(quant.QuantFast[1])
			_ = eobs[count-1]
			_ = coeff[count*16-1]
			_ = qcoeff[count*16-1]
			if q1 > 0 && r1 >= 0 {
				lim1 := 65535/q1 - r1
				aliasForward := false
				if lim1 >= 0 && lim1 <= 32767 {
					for s := 1; s < 16; s++ {
						if &coeff[s] == &qcoeff[0] {
							aliasForward = true
							break
						}
					}
				}
				if !aliasForward && lim1 >= 0 && lim1 <= 32767 {
					off := int32(lim1)
					lim := uint64(uint32(2*off)) + 1
					for i := range count {
						base := i * 16
						c := *(*[16]int16)(coeff[base : base+16])
						qb := (*[16]int16)(qcoeff[base : base+16])
						*qb = [16]int16{}
						var nz uint16
						if c[0] != 0 {
							z := int(c[0])
							s := z >> intSignShift
							y := ((((z ^ s) - s) + r0) * q0) >> 16
							if y != 0 {
								qb[0] = int16((y ^ s) - s)
								nz = 1
							}
						}
						if ((uint64(uint32(int32(c[1])+off))-lim)&(uint64(uint32(int32(c[2])+off))-lim)&(uint64(uint32(int32(c[3])+off))-lim))>>63 == 0 {
							if z := int(c[1]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[1] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 1
							}
							if z := int(c[2]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[2] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 5
							}
							if z := int(c[3]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[3] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 6
							}
						}
						if ((uint64(uint32(int32(c[4])+off))-lim)&(uint64(uint32(int32(c[5])+off))-lim)&(uint64(uint32(int32(c[6])+off))-lim)&(uint64(uint32(int32(c[7])+off))-lim))>>63 == 0 {
							if z := int(c[4]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[4] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 2
							}
							if z := int(c[5]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[5] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 4
							}
							if z := int(c[6]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[6] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 7
							}
							if z := int(c[7]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[7] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 12
							}
						}
						if ((uint64(uint32(int32(c[8])+off))-lim)&(uint64(uint32(int32(c[9])+off))-lim)&(uint64(uint32(int32(c[10])+off))-lim)&(uint64(uint32(int32(c[11])+off))-lim))>>63 == 0 {
							if z := int(c[8]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[8] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 3
							}
							if z := int(c[9]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[9] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 8
							}
							if z := int(c[10]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[10] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 11
							}
							if z := int(c[11]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[11] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 13
							}
						}
						if ((uint64(uint32(int32(c[12])+off))-lim)&(uint64(uint32(int32(c[13])+off))-lim)&(uint64(uint32(int32(c[14])+off))-lim)&(uint64(uint32(int32(c[15])+off))-lim))>>63 == 0 {
							if z := int(c[12]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[12] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 9
							}
							if z := int(c[13]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[13] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 10
							}
							if z := int(c[14]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[14] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 14
							}
							if z := int(c[15]); uint(int32(z)+off) >= uint(lim) {
								s := z >> intSignShift
								y := ((((z ^ s) - s) + r1) * q1) >> 16
								qb[15] = int16((y ^ s) - s)
								nz |= uint16(uint(-y)>>63) << 15
							}
						}
						eobs[i] = uint8(bits.Len16(nz))
					}
					return
				}
			}
			for i := range count {
				base := i * 16
				cb := (*[16]int16)(coeff[base : base+16])
				qb := (*[16]int16)(qcoeff[base : base+16])
				// Unrolled in linear order with compile-time zigzag bit positions
				// and branchless nz accumulation.
				z := int(cb[0])
				s := z >> intSignShift
				y := ((((z ^ s) - s) + r0) * q0) >> 16
				qb[0] = int16((y ^ s) - s)
				nz := uint16(uint(-y)>>63) << 0

				z = int(cb[1])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[1] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 1

				z = int(cb[2])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[2] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 5

				z = int(cb[3])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[3] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 6

				z = int(cb[4])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[4] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 2

				z = int(cb[5])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[5] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 4

				z = int(cb[6])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[6] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 7

				z = int(cb[7])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[7] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 12

				z = int(cb[8])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[8] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 3

				z = int(cb[9])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[9] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 8

				z = int(cb[10])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[10] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 11

				z = int(cb[11])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[11] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 13

				z = int(cb[12])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[12] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 9

				z = int(cb[13])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[13] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 10

				z = int(cb[14])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[14] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 14

				z = int(cb[15])
				s = z >> intSignShift
				y = ((((z ^ s) - s) + r1) * q1) >> 16
				qb[15] = int16((y ^ s) - s)
				nz |= uint16(uint(-y)>>63) << 15

				eobs[i] = uint8(bits.Len16(nz))
			}
			return
		}
		for i := range count {
			eobs[i] = uint8(fastQuantizeBlockOnlyScalar((*[16]int16)(coeff[i*16:]), quant, (*[16]int16)(qcoeff[i*16:])))
		}
		return
	}
	var dq [16]int16
	for i := range count {
		eobs[i] = uint8(fastQuantizeBlockSIMD((*[16]int16)(coeff[i*16:]), quant, (*[16]int16)(qcoeff[i*16:]), &dq))
	}
}
