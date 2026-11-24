package dsp

// Hand-coded 4x4 intra predictors ported from libvpx v1.16.0
// vpx_dsp/intrapred.c. The 4x4 directional predictors and the he/ve
// helpers are explicit unrolled DST(x,y) writes in libvpx, not the
// parametric helpers used for 8/16/32 sizes (those are gated by
// intra_pred_no_4x4). Each function writes exactly 16 bytes into the
// caller-owned dst plane at the supplied stride.
//
// As with the larger sizes, `above` is passed with index 0 as the [-1]
// top-left corner so callers can supply a single contiguous buffer.

// VpxD207Predictor4x4 mirrors vpx_d207_predictor_4x4_c. Left-only.
func VpxD207Predictor4x4(dst []uint8, stride int, above, left []uint8) {
	_ = above
	I, J, K, L := int(left[0]), int(left[1]), int(left[2]), int(left[3])
	dst[0*stride+0] = avg2(I, J)
	dst[0*stride+2] = avg2(J, K)
	dst[0*stride+1] = avg3(I, J, K)
	dst[0*stride+3] = avg3(J, K, L)
	dst[1*stride+0] = avg2(J, K)
	dst[1*stride+1] = avg3(J, K, L)
	dst[1*stride+2] = avg2(K, L)
	dst[1*stride+3] = avg3(K, L, L)
	dst[2*stride+0] = avg2(K, L)
	dst[2*stride+1] = avg3(K, L, L)
	dst[2*stride+2] = uint8(L)
	dst[2*stride+3] = uint8(L)
	dst[3*stride+0] = uint8(L)
	dst[3*stride+1] = uint8(L)
	dst[3*stride+2] = uint8(L)
	dst[3*stride+3] = uint8(L)
}

// VpxD63Predictor4x4 mirrors vpx_d63_predictor_4x4_c. Above-only.
// Reads above[0..6].
func VpxD63Predictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	_ = left
	above := aboveFull[1:]
	A, B, C, D := int(above[0]), int(above[1]), int(above[2]), int(above[3])
	E, F, G := int(above[4]), int(above[5]), int(above[6])
	dst[0*stride+0] = avg2(A, B)
	dst[0*stride+1] = avg2(B, C)
	dst[2*stride+0] = avg2(B, C)
	dst[0*stride+2] = avg2(C, D)
	dst[2*stride+1] = avg2(C, D)
	dst[0*stride+3] = avg2(D, E)
	dst[2*stride+2] = avg2(D, E)
	dst[2*stride+3] = avg2(E, F)
	dst[1*stride+0] = avg3(A, B, C)
	dst[1*stride+1] = avg3(B, C, D)
	dst[3*stride+0] = avg3(B, C, D)
	dst[1*stride+2] = avg3(C, D, E)
	dst[3*stride+1] = avg3(C, D, E)
	dst[1*stride+3] = avg3(D, E, F)
	dst[3*stride+2] = avg3(D, E, F)
	dst[3*stride+3] = avg3(E, F, G)
}

// VpxD45Predictor4x4 mirrors vpx_d45_predictor_4x4_c. Above-only.
// Note: differs from VP8 — last cell is the above_right pixel, not an
// AVG3.
func VpxD45Predictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	_ = left
	above := aboveFull[1:]
	A, B, C, D := int(above[0]), int(above[1]), int(above[2]), int(above[3])
	E, F, G, H := int(above[4]), int(above[5]), int(above[6]), int(above[7])
	dst[0*stride+0] = avg3(A, B, C)
	dst[0*stride+1] = avg3(B, C, D)
	dst[1*stride+0] = avg3(B, C, D)
	dst[0*stride+2] = avg3(C, D, E)
	dst[1*stride+1] = avg3(C, D, E)
	dst[2*stride+0] = avg3(C, D, E)
	dst[0*stride+3] = avg3(D, E, F)
	dst[1*stride+2] = avg3(D, E, F)
	dst[2*stride+1] = avg3(D, E, F)
	dst[3*stride+0] = avg3(D, E, F)
	dst[1*stride+3] = avg3(E, F, G)
	dst[2*stride+2] = avg3(E, F, G)
	dst[3*stride+1] = avg3(E, F, G)
	dst[2*stride+3] = avg3(F, G, H)
	dst[3*stride+2] = avg3(F, G, H)
	dst[3*stride+3] = uint8(H) // differs from vp8 (which used avg3(G, H, H))
}

// VpxD117Predictor4x4 mirrors vpx_d117_predictor_4x4_c. Above + left,
// reads the corner byte.
func VpxD117Predictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	I, J, K := int(left[0]), int(left[1]), int(left[2])
	X := int(aboveFull[0])
	above := aboveFull[1:]
	A, B, C, D := int(above[0]), int(above[1]), int(above[2]), int(above[3])
	dst[0*stride+0] = avg2(X, A)
	dst[2*stride+1] = avg2(X, A)
	dst[0*stride+1] = avg2(A, B)
	dst[2*stride+2] = avg2(A, B)
	dst[0*stride+2] = avg2(B, C)
	dst[2*stride+3] = avg2(B, C)
	dst[0*stride+3] = avg2(C, D)
	dst[3*stride+0] = avg3(K, J, I)
	dst[2*stride+0] = avg3(J, I, X)
	dst[1*stride+0] = avg3(I, X, A)
	dst[3*stride+1] = avg3(I, X, A)
	dst[1*stride+1] = avg3(X, A, B)
	dst[3*stride+2] = avg3(X, A, B)
	dst[1*stride+2] = avg3(A, B, C)
	dst[3*stride+3] = avg3(A, B, C)
	dst[1*stride+3] = avg3(B, C, D)
}

// VpxD135Predictor4x4 mirrors vpx_d135_predictor_4x4_c. Above + left,
// reads the corner byte. libvpx's DST(x, y) macro is column-then-row;
// we map that to dst[y*stride + x].
func VpxD135Predictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	I, J, K, L := int(left[0]), int(left[1]), int(left[2]), int(left[3])
	X := int(aboveFull[0])
	above := aboveFull[1:]
	A, B, C, D := int(above[0]), int(above[1]), int(above[2]), int(above[3])
	// DST(0,3) = AVG3(J,K,L)
	dst[3*stride+0] = avg3(J, K, L)
	// DST(1,3) = DST(0,2) = AVG3(I,J,K)
	dst[3*stride+1] = avg3(I, J, K)
	dst[2*stride+0] = avg3(I, J, K)
	// DST(2,3) = DST(1,2) = DST(0,1) = AVG3(X,I,J)
	dst[3*stride+2] = avg3(X, I, J)
	dst[2*stride+1] = avg3(X, I, J)
	dst[1*stride+0] = avg3(X, I, J)
	// DST(3,3) = DST(2,2) = DST(1,1) = DST(0,0) = AVG3(A,X,I)
	dst[3*stride+3] = avg3(A, X, I)
	dst[2*stride+2] = avg3(A, X, I)
	dst[1*stride+1] = avg3(A, X, I)
	dst[0*stride+0] = avg3(A, X, I)
	// DST(3,2) = DST(2,1) = DST(1,0) = AVG3(B,A,X)
	dst[2*stride+3] = avg3(B, A, X)
	dst[1*stride+2] = avg3(B, A, X)
	dst[0*stride+1] = avg3(B, A, X)
	// DST(3,1) = DST(2,0) = AVG3(C,B,A)
	dst[1*stride+3] = avg3(C, B, A)
	dst[0*stride+2] = avg3(C, B, A)
	// DST(3,0) = AVG3(D,C,B)
	dst[0*stride+3] = avg3(D, C, B)
}

// VpxD153Predictor4x4 mirrors vpx_d153_predictor_4x4_c. Above + left,
// reads the corner byte. libvpx's DST(x, y) maps to dst[y*stride + x].
func VpxD153Predictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	I, J, K, L := int(left[0]), int(left[1]), int(left[2]), int(left[3])
	X := int(aboveFull[0])
	above := aboveFull[1:]
	A, B, C := int(above[0]), int(above[1]), int(above[2])
	// DST(0,0) = DST(2,1) = AVG2(I, X)
	dst[0*stride+0] = avg2(I, X)
	dst[1*stride+2] = avg2(I, X)
	// DST(0,1) = DST(2,2) = AVG2(J, I)
	dst[1*stride+0] = avg2(J, I)
	dst[2*stride+2] = avg2(J, I)
	// DST(0,2) = DST(2,3) = AVG2(K, J)
	dst[2*stride+0] = avg2(K, J)
	dst[3*stride+2] = avg2(K, J)
	// DST(0,3) = AVG2(L, K)
	dst[3*stride+0] = avg2(L, K)
	// DST(3,0) = AVG3(A, B, C)
	dst[0*stride+3] = avg3(A, B, C)
	// DST(2,0) = AVG3(X, A, B)
	dst[0*stride+2] = avg3(X, A, B)
	// DST(1,0) = DST(3,1) = AVG3(I, X, A)
	dst[0*stride+1] = avg3(I, X, A)
	dst[1*stride+3] = avg3(I, X, A)
	// DST(1,1) = DST(3,2) = AVG3(J, I, X)
	dst[1*stride+1] = avg3(J, I, X)
	dst[2*stride+3] = avg3(J, I, X)
	// DST(1,2) = DST(3,3) = AVG3(K, J, I)
	dst[2*stride+1] = avg3(K, J, I)
	dst[3*stride+3] = avg3(K, J, I)
	// DST(1,3) = AVG3(L, K, J)
	dst[3*stride+1] = avg3(L, K, J)
}

// VpxHePredictor4x4 mirrors vpx_he_predictor_4x4_c (horizontal-with-
// neighbour-averaging). Above + left.
func VpxHePredictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	H := int(aboveFull[0])
	I, J, K, L := int(left[0]), int(left[1]), int(left[2]), int(left[3])
	v := avg3(H, I, J)
	dst[0*stride+0] = v
	dst[0*stride+1] = v
	dst[0*stride+2] = v
	dst[0*stride+3] = v
	v = avg3(I, J, K)
	dst[1*stride+0] = v
	dst[1*stride+1] = v
	dst[1*stride+2] = v
	dst[1*stride+3] = v
	v = avg3(J, K, L)
	dst[2*stride+0] = v
	dst[2*stride+1] = v
	dst[2*stride+2] = v
	dst[2*stride+3] = v
	v = avg3(K, L, L)
	dst[3*stride+0] = v
	dst[3*stride+1] = v
	dst[3*stride+2] = v
	dst[3*stride+3] = v
}

// VpxVePredictor4x4 mirrors vpx_ve_predictor_4x4_c (vertical-with-
// neighbour-averaging). Above-only.
func VpxVePredictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	_ = left
	H := int(aboveFull[0])
	above := aboveFull[1:]
	I, J, K, L, M := int(above[0]), int(above[1]), int(above[2]), int(above[3]), int(above[4])
	dst[0*stride+0] = avg3(H, I, J)
	dst[0*stride+1] = avg3(I, J, K)
	dst[0*stride+2] = avg3(J, K, L)
	dst[0*stride+3] = avg3(K, L, M)
	copy(dst[1*stride:1*stride+4], dst[0*stride:0*stride+4])
	copy(dst[2*stride:2*stride+4], dst[0*stride:0*stride+4])
	copy(dst[3*stride:3*stride+4], dst[0*stride:0*stride+4])
}

// VpxD63ePredictor4x4 mirrors vpx_d63e_predictor_4x4_c. Above-only.
// Reads above[0..7].
func VpxD63ePredictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	_ = left
	above := aboveFull[1:]
	A, B, C, D := int(above[0]), int(above[1]), int(above[2]), int(above[3])
	E, F, G, H := int(above[4]), int(above[5]), int(above[6]), int(above[7])
	dst[0*stride+0] = avg2(A, B)
	dst[0*stride+1] = avg2(B, C)
	dst[2*stride+0] = avg2(B, C)
	dst[0*stride+2] = avg2(C, D)
	dst[2*stride+1] = avg2(C, D)
	dst[0*stride+3] = avg2(D, E)
	dst[2*stride+2] = avg2(D, E)
	dst[2*stride+3] = avg3(E, F, G) // differs from vpx_d63_predictor: AVG3 not AVG2
	dst[1*stride+0] = avg3(A, B, C)
	dst[1*stride+1] = avg3(B, C, D)
	dst[3*stride+0] = avg3(B, C, D)
	dst[1*stride+2] = avg3(C, D, E)
	dst[3*stride+1] = avg3(C, D, E)
	dst[1*stride+3] = avg3(D, E, F)
	dst[3*stride+2] = avg3(D, E, F)
	dst[3*stride+3] = avg3(F, G, H)
}

// VpxD45ePredictor4x4 mirrors vpx_d45e_predictor_4x4_c. Above-only.
// Last cell is AVG3(G, H, H) — differs from vpx_d45_predictor_4x4_c.
func VpxD45ePredictor4x4(dst []uint8, stride int, aboveFull, left []uint8) {
	_ = left
	above := aboveFull[1:]
	A, B, C, D := int(above[0]), int(above[1]), int(above[2]), int(above[3])
	E, F, G, H := int(above[4]), int(above[5]), int(above[6]), int(above[7])
	dst[0*stride+0] = avg3(A, B, C)
	dst[0*stride+1] = avg3(B, C, D)
	dst[1*stride+0] = avg3(B, C, D)
	dst[0*stride+2] = avg3(C, D, E)
	dst[1*stride+1] = avg3(C, D, E)
	dst[2*stride+0] = avg3(C, D, E)
	dst[0*stride+3] = avg3(D, E, F)
	dst[1*stride+2] = avg3(D, E, F)
	dst[2*stride+1] = avg3(D, E, F)
	dst[3*stride+0] = avg3(D, E, F)
	dst[1*stride+3] = avg3(E, F, G)
	dst[2*stride+2] = avg3(E, F, G)
	dst[3*stride+1] = avg3(E, F, G)
	dst[2*stride+3] = avg3(F, G, H)
	dst[3*stride+2] = avg3(F, G, H)
	dst[3*stride+3] = avg3(G, H, H)
}
