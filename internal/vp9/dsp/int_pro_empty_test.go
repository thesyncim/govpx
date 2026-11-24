package dsp

import "testing"

func TestIntProColsEmptyRowsDoNotAccessBuffers(t *testing.T) {
	for _, rows := range []int{0, -1} {
		for _, offset := range []int{1, -1, int(^uint(0) >> 1)} {
			got := [2]int16{123, -456}
			IntProCols(got[:], nil, offset, int(^uint(0)>>1), 16, rows, 3)
			if got != [2]int16{123, -456} {
				t.Fatalf("rows%d offset%d changed output: %v", rows, offset, got)
			}
		}
	}
}
