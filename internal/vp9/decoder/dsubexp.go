package decoder

import "github.com/thesyncim/govpx/internal/vp9/bitstream"

// Probability-update sub-exponential helpers from the VP9 compressed
// header. Ported from libvpx v1.16.0 vp9/decoder/vp9_dsubexp.c.
// vp9_diff_update_prob walks a probability slot in the frame context
// and conditionally updates it via a remap-table-driven sub-exp code.
//
// DiffUpdateProb is the boolean coder prob used to gate the update —
// MAX_PROB-3 (= 252) per vp9/common/vp9_entropy.h.
const DiffUpdateProb = 252

// invMapTable mirrors inv_map_table in vp9_dsubexp.c. The probability
// space is permuted so the encoder can spend fewer bits on commonly
// updated values.
var invMapTable = [255]uint8{
	7, 20, 33, 46, 59, 72, 85, 98, 111, 124, 137, 150, 163, 176, 189,
	202, 215, 228, 241, 254, 1, 2, 3, 4, 5, 6, 8, 9, 10, 11,
	12, 13, 14, 15, 16, 17, 18, 19, 21, 22, 23, 24, 25, 26, 27,
	28, 29, 30, 31, 32, 34, 35, 36, 37, 38, 39, 40, 41, 42, 43,
	44, 45, 47, 48, 49, 50, 51, 52, 53, 54, 55, 56, 57, 58, 60,
	61, 62, 63, 64, 65, 66, 67, 68, 69, 70, 71, 73, 74, 75, 76,
	77, 78, 79, 80, 81, 82, 83, 84, 86, 87, 88, 89, 90, 91, 92,
	93, 94, 95, 96, 97, 99, 100, 101, 102, 103, 104, 105, 106, 107, 108,
	109, 110, 112, 113, 114, 115, 116, 117, 118, 119, 120, 121, 122, 123, 125,
	126, 127, 128, 129, 130, 131, 132, 133, 134, 135, 136, 138, 139, 140, 141,
	142, 143, 144, 145, 146, 147, 148, 149, 151, 152, 153, 154, 155, 156, 157,
	158, 159, 160, 161, 162, 164, 165, 166, 167, 168, 169, 170, 171, 172, 173,
	174, 175, 177, 178, 179, 180, 181, 182, 183, 184, 185, 186, 187, 188, 190,
	191, 192, 193, 194, 195, 196, 197, 198, 199, 200, 201, 203, 204, 205, 206,
	207, 208, 209, 210, 211, 212, 213, 214, 216, 217, 218, 219, 220, 221, 222,
	223, 224, 225, 226, 227, 229, 230, 231, 232, 233, 234, 235, 236, 237, 238,
	239, 240, 242, 243, 244, 245, 246, 247, 248, 249, 250, 251, 252, 253, 253,
}

// invRecenterNonneg mirrors inv_recenter_nonneg.
func invRecenterNonneg(v, m int) int {
	if v > 2*m {
		return v
	}
	if v&1 != 0 {
		return m - ((v + 1) >> 1)
	}
	return m + (v >> 1)
}

// decodeUniform mirrors decode_uniform in libvpx — read a uniformly
// distributed value in [0, 191) via the boolean coder using 7 then
// possibly 8 bits.
func decodeUniform(r *bitstream.Reader) int {
	const l = 8
	const m = (1 << l) - 191
	v := int(r.ReadLiteral(l - 1))
	if v < m {
		return v
	}
	return (v << 1) - m + int(r.ReadBit())
}

// decodeTermSubexp mirrors decode_term_subexp — the three-category
// prefix code that selects the magnitude bucket and then a uniform
// tail when the value exceeds 64.
func decodeTermSubexp(r *bitstream.Reader) int {
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(4))
	}
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(4)) + 16
	}
	if r.ReadBit() == 0 {
		return int(r.ReadLiteral(5)) + 32
	}
	return decodeUniform(r) + 64
}

// invRemapProb mirrors inv_remap_prob: pass through the permutation
// table, then re-center around the current probability with a
// triangular split at MAX_PROB.
func invRemapProb(v int, m uint8) uint8 {
	v = int(invMapTable[v])
	mm := int(m) - 1
	if (mm << 1) <= 255 {
		return uint8(1 + invRecenterNonneg(v, mm))
	}
	return uint8(255 - invRecenterNonneg(v, 255-1-mm))
}

// VpxDiffUpdateProb mirrors vp9_diff_update_prob. The boolean coder
// decides whether to update; on update the sub-exp delta selects the
// new probability via the inverse map.
func VpxDiffUpdateProb(r *bitstream.Reader, p *uint8) {
	if r.Read(DiffUpdateProb) != 0 {
		delp := decodeTermSubexp(r)
		*p = invRemapProb(delp, *p)
	}
}
