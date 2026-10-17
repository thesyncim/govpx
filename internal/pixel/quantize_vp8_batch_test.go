package pixel

import (
	"reflect"
	"testing"
)

func TestVP8QuantBatchDeclinesBeforeWriting(t *testing.T) {
	type blockTables struct{ round, quant, dequant, scan [16]int16 }
	var tables blockTables
	for i := range 16 {
		tables.round[i] = 3
		tables.quant[i] = 6553
		tables.dequant[i] = 10
		tables.scan[i] = int16(i + 1)
	}
	const count = 8
	for _, sizes := range [][4]int{{127, 128, 128, 8}, {128, 127, 128, 8}, {128, 128, 127, 8}, {128, 128, 128, 7}} {
		c, q, dq, e := make([]int16, sizes[0]), make([]int16, sizes[1]), make([]int16, sizes[2]), make([]uint8, sizes[3])
		for i := range q {
			q[i] = 123
		}
		for i := range dq {
			dq[i] = 99
		}
		for i := range e {
			e[i] = 77
		}
		beforeQ, beforeD, beforeE := append([]int16(nil), q...), append([]int16(nil), dq...), append([]uint8(nil), e...)
		if QuantizeVP8Batch(c, &tables.round, &tables.quant, &tables.dequant, q, dq, &tables.scan, e, count) {
			t.Fatal("short batch accepted")
		}
		if !reflect.DeepEqual(q, beforeQ) || !reflect.DeepEqual(dq, beforeD) || !reflect.DeepEqual(e, beforeE) {
			t.Fatal("declined batch modified output")
		}
	}
	if !Enabled {
		return
	}
	for table := range 4 {
		// Each table is backed by a complete output allocation. The alias guard
		// must decline before caching or overwriting any of its original values.
		storage := make([]int16, 128)
		for i := range storage {
			storage[i] = 123
		}
		q, dq, c := make([]int16, 128), make([]int16, 128), make([]int16, 128)
		e := make([]uint8, 8)
		r, qt, d, scan := &tables.round, &tables.quant, &tables.dequant, &tables.scan
		switch table {
		case 0:
			r = (*[16]int16)(storage)
		case 1:
			qt = (*[16]int16)(storage)
		case 2:
			d = (*[16]int16)(storage)
		case 3:
			scan = (*[16]int16)(storage)
		}
		q = storage
		before := append([]int16(nil), storage...)
		if QuantizeVP8Batch(c, r, qt, d, q, dq, scan, e, 8) {
			t.Fatal("table alias accepted")
		}
		if !reflect.DeepEqual(storage, before) {
			t.Fatal("declined alias changed table")
		}
	}
}

func TestVP8QuantBatchPreservesOverlappingInputOrder(t *testing.T) {
	if !Enabled {
		t.Skip("native SIMD unavailable")
	}
	type blockTables struct{ round, quant, dequant, scan [16]int16 }
	var tables blockTables
	for i := range 16 {
		tables.round[i] = 3
		tables.quant[i] = 6553
		tables.dequant[i] = 10
		tables.scan[i] = int16(i + 1)
	}
	const count = 8
	for _, offsets := range [][2]int{{0, 512}, {1, 512}, {15, 512}, {16, 512}, {256, 512}, {512, 0}, {512, 1}, {512, 15}, {512, 16}} {
		original := make([]int16, 512+count*16+13)
		for i := range original {
			original[i] = int16(i * 1193)
		}
		got, want := append([]int16(nil), original...), append([]int16(nil), original...)
		ge, we := make([]uint8, count+16), make([]uint8, count+16)
		for i := range ge {
			ge[i] = 77
			we[i] = 77
		}
		qo, dqo := offsets[0], offsets[1]
		if !QuantizeVP8Batch(got[:count*16], &tables.round, &tables.quant, &tables.dequant, got[qo:qo+count*16], got[dqo:dqo+count*16], &tables.scan, ge[8:8+count], count) {
			t.Fatal("eligible overlap declined")
		}
		// Existing per-block native calls define the block read/store sequence.
		// Full signed numeric parity is checked separately against the scalar port.
		for i := range count {
			off := i * 16
			we[8+i] = uint8(QuantizeVP8Block((*[16]int16)(want[off:]), &tables.round, &tables.quant, &tables.dequant, (*[16]int16)(want[qo+off:]), (*[16]int16)(want[dqo+off:]), &tables.scan))
		}
		if !reflect.DeepEqual(got, want) || !reflect.DeepEqual(ge, we) {
			t.Fatalf("input/output overlap %v changed block ordering or sentinels", offsets)
		}
	}
}
