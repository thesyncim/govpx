package tables

// Ported static motion-vector probability tables from libvpx v1.16.0
// vp8/common/entropymv.c and constants from vp8/common/entropymv.h.

const (
	MVPCount = 19
)

var MVUpdateProbs = [2][MVPCount]uint8{
	{
		237,
		246,
		253, 253, 254, 254, 254, 254, 254,
		254, 254, 254, 254, 254, 250, 250, 252, 254, 254,
	},
	{
		231,
		243,
		245, 253, 254, 254, 254, 254, 254,
		254, 254, 254, 254, 254, 251, 251, 254, 254, 254,
	},
}

var DefaultMVContext = [2][MVPCount]uint8{
	{
		162,
		128,
		225, 146, 172, 147, 214, 39, 156,
		128, 129, 132, 75, 145, 178, 206, 239, 254, 254,
	},
	{
		164,
		128,
		204, 170, 119, 235, 140, 230, 228,
		128, 130, 130, 74, 148, 180, 203, 236, 254, 254,
	},
}
