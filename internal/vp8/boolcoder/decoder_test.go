package boolcoder

import (
	"errors"
	"testing"
)

func TestReadLiteralDeterministic(t *testing.T) {
	var d Decoder
	if err := d.Init([]byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	got := []uint32{
		d.ReadLiteral(1),
		d.ReadLiteral(3),
		d.ReadLiteral(8),
		d.ReadLiteral(5),
		d.ReadLiteral(12),
	}
	want := []uint32{0, 6, 172, 6, 3635}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("literal[%d] = %d, want %d; got sequence %v", i, got[i], want[i], got)
		}
	}
	if err := d.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
}

func TestReadBoolDeterministic(t *testing.T) {
	var d Decoder
	if err := d.Init([]byte{0x9d, 0x01, 0x2a, 0xff, 0x00, 0x80, 0x42, 0x24}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	probs := []uint8{128, 1, 255, 17, 64, 192, 99, 128, 128, 220, 7, 33}
	got := make([]uint8, len(probs))
	for i, p := range probs {
		got[i] = d.ReadBool(p)
	}
	want := []uint8{1, 1, 0, 1, 0, 0, 1, 1, 1, 0, 1, 1}
	for i, p := range probs {
		if got[i] != want[i] {
			t.Fatalf("bit[%d] with prob %d = %d, want %d; got sequence %v", i, p, got[i], want[i], got)
		}
	}
	if err := d.Err(); err != nil {
		t.Fatalf("Err = %v, want nil", err)
	}
}

func TestTruncatedInputReportsStableError(t *testing.T) {
	var d Decoder
	if err := d.Init(nil); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	_ = d.ReadBit()
	if !errors.Is(d.Err(), ErrTruncated) {
		t.Fatalf("Err = %v, want ErrTruncated", d.Err())
	}
	if !d.Corrupted() {
		t.Fatalf("Corrupted = false, want true")
	}
}

func TestDecoderReuse(t *testing.T) {
	var d Decoder
	if err := d.Init([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	first := d.ReadLiteral(4)
	if err := d.Init([]byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}); err != nil {
		t.Fatalf("second Init returned error: %v", err)
	}
	second := d.ReadLiteral(4)
	if first == second {
		t.Fatalf("reuse did not reset state: first=%d second=%d", first, second)
	}
}

func TestDecoderAllocatesZeroAfterInit(t *testing.T) {
	var d Decoder
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	if err := d.Init(src); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	allocs := testing.AllocsPerRun(1000, func() {
		_ = d.ReadBool(128)
		_ = d.ReadLiteral(3)
		_ = d.Err()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func TestReadCoefUpdateProbsIntoMatchesScalarLoop(t *testing.T) {
	// Build a synthetic update header: every-other entry triggers an update.
	const (
		blocks = 4
		bands  = 8
		ctxs   = 3
		nodes  = 11
	)
	total := blocks * bands * ctxs * nodes

	updateProbs := make([]uint8, total)
	for i := range updateProbs {
		// Use a mix of low and high probabilities so the bool stream exercises
		// both renormalisation paths.
		updateProbs[i] = uint8(((i * 37) + 17) & 0xff)
		if updateProbs[i] == 0 {
			updateProbs[i] = 1
		}
	}

	updateBits := make([]uint8, total)
	literals := make([]uint8, total)
	for i := range updateBits {
		updateBits[i] = uint8(i % 5)
		if updateBits[i] > 1 {
			updateBits[i] = 0
		}
		literals[i] = uint8((i * 13) & 0xff)
	}

	var w testWriter
	w.init()
	wantUpdates := 0
	wantNonDefault := false
	for i := 0; i < total; i++ {
		w.writeBool(updateBits[i], updateProbs[i])
		if updateBits[i] != 0 {
			for k := 7; k >= 0; k-- {
				w.writeBool(uint8((literals[i]>>uint(k))&1), 128)
			}
			wantUpdates++
			if (i/nodes)%ctxs != 0 {
				wantNonDefault = true
			}
		}
	}
	payload := w.finish()

	// Compute the want probs by running the scalar ReadBool/ReadLiteral path.
	scalarProbs := make([]uint8, total)
	for i := range scalarProbs {
		scalarProbs[i] = 200 // sentinel
	}
	wantProbs := append([]uint8(nil), scalarProbs...)
	{
		var d Decoder
		_ = d.Init(payload)
		for i := 0; i < total; i++ {
			if d.ReadBool(updateProbs[i]) != 0 {
				wantProbs[i] = uint8(d.ReadLiteral(8))
			}
		}
		if err := d.Err(); err != nil {
			t.Fatalf("scalar Err = %v", err)
		}
	}

	// Now run the batched path.
	gotProbs := append([]uint8(nil), scalarProbs...)
	var d Decoder
	_ = d.Init(payload)
	gotUpdates, gotNonDefault := d.ReadCoefUpdateProbsInto(updateProbs, gotProbs, nodes, ctxs)
	if err := d.Err(); err != nil {
		t.Fatalf("batched Err = %v", err)
	}
	if gotUpdates != wantUpdates {
		t.Fatalf("updateCount = %d, want %d", gotUpdates, wantUpdates)
	}
	if gotNonDefault != wantNonDefault {
		t.Fatalf("nonDefault = %v, want %v", gotNonDefault, wantNonDefault)
	}
	for i := range wantProbs {
		if gotProbs[i] != wantProbs[i] {
			t.Fatalf("probs[%d] = %d, want %d", i, gotProbs[i], wantProbs[i])
		}
	}
}

func TestReadCoefUpdateProbsIntoNilProbs(t *testing.T) {
	updateProbs := make([]uint8, 32)
	for i := range updateProbs {
		updateProbs[i] = 200
	}
	var w testWriter
	w.init()
	want := 0
	for i := range updateProbs {
		bit := uint8(i & 1)
		w.writeBool(bit, updateProbs[i])
		if bit != 0 {
			for k := 7; k >= 0; k-- {
				w.writeBool(uint8((123>>uint(k))&1), 128)
			}
			want++
		}
	}
	payload := w.finish()

	var d Decoder
	_ = d.Init(payload)
	got, _ := d.ReadCoefUpdateProbsInto(updateProbs, nil, 1, 1)
	if got != want {
		t.Fatalf("updateCount = %d, want %d", got, want)
	}
}

type testWriter struct {
	low   uint32
	rng   uint32
	count int
	buf   []byte
}

func (w *testWriter) init() {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.buf = w.buf[:0]
}

func (w *testWriter) finish() []byte {
	for range 32 {
		w.writeBool(0, 128)
	}
	return w.buf
}

func (w *testWriter) writeBool(bit uint8, prob uint8) {
	split := uint32(1 + (((w.rng - 1) * uint32(prob)) >> 8))
	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}
	var norm = [256]uint8{
		0, 7, 6, 6, 5, 5, 5, 5, 4, 4, 4, 4, 4, 4, 4, 4,
		3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3, 3,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	}
	shift := int(norm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		offset := shift - count
		if ((low << uint(offset-1)) & 0x80000000) != 0 {
			for i := len(w.buf) - 1; i >= 0; i-- {
				if w.buf[i] != 0xff {
					w.buf[i]++
					break
				}
				w.buf[i] = 0
			}
		}
		w.buf = append(w.buf, byte((low>>uint(24-offset))&0xff))
		shift = count
		low = uint32((uint64(low) << uint(offset)) & 0xffffff)
		count -= 8
	}
	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}

func BenchmarkReadBool(b *testing.B) {
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	var d Decoder
	_ = d.Init(src)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if d.Err() != nil {
			_ = d.Init(src)
		}
		_ = d.ReadBool(128)
	}
}

func BenchmarkReadLiteral8(b *testing.B) {
	src := []byte{0x6a, 0xc3, 0x71, 0x9d, 0x55, 0x00, 0xff, 0x13, 0x88}
	var d Decoder
	_ = d.Init(src)

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if d.Err() != nil {
			_ = d.Init(src)
		}
		_ = d.ReadLiteral(8)
	}
}
