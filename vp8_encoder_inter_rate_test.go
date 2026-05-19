package govpx

import "testing"

func TestGatherClampedLumaBlockEdges(t *testing.T) {
	img := testImage(4, 3)
	for row := range img.Height {
		for col := range img.Width {
			img.Y[row*img.YStride+col] = byte(row*10 + col)
		}
	}
	src := sourceImageFromPublic(img)

	tests := []struct {
		name  string
		baseY int
		baseX int
		want  []byte
	}{
		{
			name:  "bottom edge full x",
			baseY: 1,
			baseX: 0,
			want: []byte{
				10, 11, 12, 13,
				20, 21, 22, 23,
				20, 21, 22, 23,
				20, 21, 22, 23,
			},
		},
		{
			name:  "bottom right edge",
			baseY: 1,
			baseX: 2,
			want: []byte{
				12, 13, 13, 13,
				22, 23, 23, 23,
				22, 23, 23, 23,
				22, 23, 23, 23,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dst := make([]byte, 4*4)
			gatherClampedLumaBlock(src, tt.baseY, tt.baseX, 4, 4, dst, 4)
			for i, want := range tt.want {
				if dst[i] != want {
					t.Fatalf("dst[%d] = %d, want %d (dst=%v)", i, dst[i], want, dst)
				}
			}
		})
	}
}
