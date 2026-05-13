package tables

// VP9 motion-vector default probabilities and the canonical MV token
// trees that drive read_mv / read_mv_component / read_mv_class /
// read_mv_class0_fp via the boolean range coder. Ported byte-for-byte
// from libvpx v1.16.0 vp9/common/vp9_entropymv.c.

// MV joint type values mirroring vp9_entropymv.h's MV_JOINT_*.
const (
	MvJointZero   = 0
	MvJointHnzVz  = 1
	MvJointHzVnz  = 2
	MvJointHnzVnz = 3
)

// MvJointTree mirrors libvpx's vp9_mv_joint_tree — a 4-leaf binary
// tree over the four MV_JOINT codes.
var MvJointTree = [6]int8{
	-MvJointZero, 2,
	-MvJointHnzVz, 4,
	-MvJointHzVnz, -MvJointHnzVnz,
}

// MV class enum values mirror MV_CLASS_0..MV_CLASS_10.
const (
	MvClass0  = 0
	MvClass1  = 1
	MvClass2  = 2
	MvClass3  = 3
	MvClass4  = 4
	MvClass5  = 5
	MvClass6  = 6
	MvClass7  = 7
	MvClass8  = 8
	MvClass9  = 9
	MvClass10 = 10
)

// MvClassTree mirrors libvpx's vp9_mv_class_tree — 11 classes.
var MvClassTree = [20]int8{
	-MvClass0, 2, -MvClass1, 4, 6,
	8, -MvClass2, -MvClass3, 10, 12,
	-MvClass4, -MvClass5, -MvClass6, 14, 16,
	18, -MvClass7, -MvClass8, -MvClass9, -MvClass10,
}

// MvClass0Tree mirrors libvpx's vp9_mv_class0_tree — a 2-leaf binary
// tree (single bit).
var MvClass0Tree = [2]int8{-0, -1}

// MvFpTree mirrors libvpx's vp9_mv_fp_tree — fractional-pel position
// over 4 leaves.
var MvFpTree = [6]int8{
	-0, 2,
	-1, 4,
	-2, -3,
}

// DefaultNmvComponent mirrors one of the two nmv_component blocks
// inside libvpx's default_nmv_context. The component struct layout
// matches the libvpx struct field order so the matching Go type in
// internal/vp9/decoder/compressed_mv.go can be seeded directly.
type DefaultNmvComponent struct {
	Sign     uint8
	Classes  [10]uint8
	Class0   [1]uint8
	Bits     [10]uint8
	Class0Fp [2][3]uint8
	Fp       [3]uint8
	Class0Hp uint8
	Hp       uint8
}

// DefaultNmvJoints mirrors libvpx's default_nmv_context.joints.
var DefaultNmvJoints = [3]uint8{32, 64, 96}

// DefaultNmvComps mirrors libvpx's default_nmv_context.comps —
// (vertical, horizontal) seed probabilities for the motion-vector
// boolean coder.
var DefaultNmvComps = [2]DefaultNmvComponent{
	{
		// Vertical component
		Sign:     128,
		Classes:  [10]uint8{224, 144, 192, 168, 192, 176, 192, 198, 198, 245},
		Class0:   [1]uint8{216},
		Bits:     [10]uint8{136, 140, 148, 160, 176, 192, 224, 234, 234, 240},
		Class0Fp: [2][3]uint8{{128, 128, 64}, {96, 112, 64}},
		Fp:       [3]uint8{64, 96, 64},
		Class0Hp: 160,
		Hp:       128,
	},
	{
		// Horizontal component
		Sign:     128,
		Classes:  [10]uint8{216, 128, 176, 160, 176, 176, 192, 198, 198, 208},
		Class0:   [1]uint8{208},
		Bits:     [10]uint8{136, 140, 148, 160, 176, 192, 224, 234, 234, 240},
		Class0Fp: [2][3]uint8{{128, 128, 64}, {96, 112, 64}},
		Fp:       [3]uint8{64, 96, 64},
		Class0Hp: 160,
		Hp:       128,
	},
}
