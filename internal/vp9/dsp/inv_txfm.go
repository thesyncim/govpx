package dsp

// Ported from libvpx v1.16.0:
//   - vpx_dsp/inv_txfm.{h,c} (idct4_c, vpx_idct4x4_16_add_c, vpx_idct4x4_1_add_c,
//     iadst4_c, vpx_iwht4x4_16_add_c, vpx_iwht4x4_1_add_c)
//   - vpx_dsp/txfm_common.h (cospi / sinpi constants, DCT_CONST_BITS,
//     UNIT_QUANT_SHIFT)

// DCT_CONST_BITS / UNIT_QUANT_SHIFT are the two scaling factors that
// govern intermediate magnitudes through all VP9 inverse transforms.
// Their values are baked into the wire format — changing them changes
// every reconstructed pixel.
const (
	dctConstBits    = 14
	unitQuantShift  = 2
	dctConstRounding = 1 << (dctConstBits - 1)
)

// VP9 cospi / sinpi constants from vpx_dsp/txfm_common.h. The cospi_K_64
// values are 16-bit fixed-point cos((K * pi) / 64) * 2^14 (rounded). They
// appear in every VP9 inverse-transform butterfly; their exact integer
// values are wire-stable so the porting must match upstream byte-for-byte.
const (
	cospi1_64  = 16364
	cospi2_64  = 16305
	cospi3_64  = 16207
	cospi4_64  = 16069
	cospi5_64  = 15893
	cospi6_64  = 15679
	cospi7_64  = 15426
	cospi8_64  = 15137
	cospi9_64  = 14811
	cospi10_64 = 14449
	cospi11_64 = 14053
	cospi12_64 = 13623
	cospi13_64 = 13160
	cospi14_64 = 12665
	cospi15_64 = 12140
	cospi16_64 = 11585
	cospi17_64 = 11003
	cospi18_64 = 10394
	cospi19_64 = 9760
	cospi20_64 = 9102
	cospi21_64 = 8423
	cospi22_64 = 7723
	cospi23_64 = 7005
	cospi24_64 = 6270
	cospi25_64 = 5520
	cospi26_64 = 4756
	cospi27_64 = 3981
	cospi28_64 = 3196
	cospi29_64 = 2404
	cospi30_64 = 1606
	cospi31_64 = 804

	sinpi1_9 = 5283
	sinpi2_9 = 9929
	sinpi3_9 = 13377
	sinpi4_9 = 15212
)

// dctConstRoundShift implements libvpx's dct_const_round_shift: round
// the multiplied coefficient back into the int16 range using a
// half-rounding right shift by DCT_CONST_BITS.
func dctConstRoundShift(input int64) int32 {
	return int32((input + dctConstRounding) >> dctConstBits)
}

// wrapLow mirrors libvpx's WRAPLOW: a no-op cast in the standard build,
// or a wrap-to-int16 when emulating hardware. We follow the default
// configuration so this is just a typed pass-through.
func wrapLow(x int64) int32 { return int32(x) }

// clipPixelAdd mirrors clip_pixel_add: add the residual to the existing
// 8-bit pixel and clamp to [0, 255].
func clipPixelAdd(dest uint8, trans int32) uint8 {
	v := int32(dest) + trans
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// roundPowerOfTwo mirrors ROUND_POWER_OF_TWO: signed right shift with
// half-rounding. Used to fold the 1-D transform's normalization factor
// (4 bits for 4x4) back into the pixel domain.
func roundPowerOfTwo(value int32, n uint) int32 {
	return (value + (1 << (n - 1))) >> n
}
