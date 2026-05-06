package tables

// Ported token metadata shapes from libvpx v1.16.0 vp8/common/entropy.h and
// vp8/common/treecoder.h.

type Token struct {
	Value int16
	Len   int8
}

type ExtraBits struct {
	Tree    []int16
	Prob    []uint8
	Len     int8
	BaseVal int16
}
