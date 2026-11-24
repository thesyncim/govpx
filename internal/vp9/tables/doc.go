// Package tables holds VP9 static constants ported byte-for-byte from
// libvpx: default coef / mode / MV probabilities, scan / inverse-scan
// orders for every transform size and type, quant lookup tables, partition
// context tables, neighbour offset tables, intra-prediction direction
// constants, and the various pred-context arrays used by the boolean
// coder's CDF lookups.
//
// Tables are generated and/or hand-ported from the pinned libvpx v1.16.0
// source and verified by oracle tests that read libvpx's compiled
// constants and compare byte-by-byte.
//
// Upstream:
//
//	libvpx v1.16.0 vp9/common/{vp9_entropy,vp9_entropymode,vp9_entropymv,
//	vp9_scan,vp9_pred_common,vp9_quant_common,vp9_common_data}.{c,h}
package tables
