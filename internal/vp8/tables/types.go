package tables

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
