package bitstream

import (
	"math/rand"
	"testing"
)

// TestRoundTripFixedProb writes a stream of bits against a fixed
// probability and checks the reader recovers the same sequence. This
// exercises the carry-propagation, normalization, and fill paths in
// isolation from any libvpx-specific framing.
func TestRoundTripFixedProb(t *testing.T) {
	cases := []struct {
		name string
		prob uint32
	}{
		{"prob1", 1},
		{"prob32", 32},
		{"prob128", 128},
		{"prob200", 200},
		{"prob255", 255},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const n = 4096
			rng := rand.New(rand.NewSource(0x9e3779b9))
			bits := make([]uint32, n)
			for i := range bits {
				bits[i] = uint32(rng.Intn(2))
			}

			buf := make([]byte, 8192)
			var w Writer
			w.Start(buf)
			for _, b := range bits {
				w.Write(b, tc.prob)
			}
			size, err := w.Stop()
			if err != nil {
				t.Fatalf("Stop: %v", err)
			}

			var r Reader
			if err := r.Init(buf[:size]); err != nil {
				t.Fatalf("Init: %v", err)
			}
			for i, want := range bits {
				if got := r.Read(tc.prob); got != want {
					t.Fatalf("bit %d: got %d, want %d", i, got, want)
				}
			}
		})
	}
}

// TestRoundTripMixedProb varies the probability per bit so we exercise
// the writer's split-recompute / normalization shift / fill paths over a
// wider range of (range, prob) inputs than the fixed-probability case.
func TestRoundTripMixedProb(t *testing.T) {
	const n = 8192
	rng := rand.New(rand.NewSource(0xdeadbeef))
	bits := make([]uint32, n)
	probs := make([]uint32, n)
	for i := range bits {
		bits[i] = uint32(rng.Intn(2))
		probs[i] = uint32(1 + rng.Intn(255))
		if i&7 == 0 {
			probs[i] = 128
		}
	}

	buf := make([]byte, 16384)
	var w Writer
	w.Start(buf)
	for i := range bits {
		w.Write(bits[i], probs[i])
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for i := range bits {
		if got := r.Read(probs[i]); got != bits[i] {
			t.Fatalf("bit %d (prob %d): got %d, want %d", i, probs[i], got, bits[i])
		}
	}
}

func TestWriteBitMatchesGenericProb128(t *testing.T) {
	const n = 8192
	rng := rand.New(rand.NewSource(0x128128))
	bits := make([]uint32, n)
	for i := range bits {
		bits[i] = uint32(rng.Intn(2))
	}

	genericBuf := make([]byte, 4096)
	var generic Writer
	generic.Start(genericBuf)
	for _, bit := range bits {
		generic.Write(bit, 128)
	}
	genericSize, err := generic.Stop()
	if err != nil {
		t.Fatalf("generic Stop: %v", err)
	}

	bitBuf := make([]byte, 4096)
	var bitWriter Writer
	bitWriter.Start(bitBuf)
	for _, bit := range bits {
		bitWriter.WriteBit(bit)
	}
	bitSize, err := bitWriter.Stop()
	if err != nil {
		t.Fatalf("WriteBit Stop: %v", err)
	}
	if bitSize != genericSize {
		t.Fatalf("WriteBit size = %d, want generic size %d", bitSize, genericSize)
	}
	for i := range bitSize {
		if bitBuf[i] != genericBuf[i] {
			t.Fatalf("byte %d: WriteBit = 0x%02x, generic = 0x%02x",
				i, bitBuf[i], genericBuf[i])
		}
	}
}

func TestWritePackedMatchesSequentialWrites(t *testing.T) {
	type fragment struct {
		bits  uint32
		probs uint32
		n     int
	}
	fragments := []fragment{
		{bits: 0b00, probs: 17<<8 | 229, n: 2},
		{bits: 0b101, probs: 33<<16 | 128<<8 | 244, n: 3},
		{bits: 0b1110, probs: 201<<24 | 19<<16 | 88<<8 | 177, n: 4},
		{bits: 0b0101, probs: 91<<24 | 92<<16 | 93<<8 | 94, n: 4},
	}

	sequentialBuf := make([]byte, 256)
	var sequential Writer
	sequential.Start(sequentialBuf)
	for _, f := range fragments {
		for i := f.n - 1; i >= 0; i-- {
			bit := (f.bits >> uint(i)) & 1
			prob := (f.probs >> uint(i*8)) & 0xff
			sequential.Write(bit, prob)
		}
	}
	sequentialSize, err := sequential.Stop()
	if err != nil {
		t.Fatalf("sequential Stop: %v", err)
	}

	packedBuf := make([]byte, 256)
	var packed Writer
	packed.Start(packedBuf)
	for _, f := range fragments {
		packed.WritePacked(f.bits, f.probs, f.n)
	}
	packedSize, err := packed.Stop()
	if err != nil {
		t.Fatalf("packed Stop: %v", err)
	}
	if packedSize != sequentialSize {
		t.Fatalf("packed size = %d, want sequential size %d", packedSize, sequentialSize)
	}
	for i := range packedSize {
		if packedBuf[i] != sequentialBuf[i] {
			t.Fatalf("byte %d: packed = 0x%02x, sequential = 0x%02x",
				i, packedBuf[i], sequentialBuf[i])
		}
	}
}

func TestWritePacked64MatchesSequentialWrites(t *testing.T) {
	type fragment struct {
		bits  uint32
		probs uint64
		n     int
	}
	fragments := []fragment{
		{bits: 0b10010, probs: 17<<32 | 229<<24 | 33<<16 | 128<<8 | 244, n: 5},
		{bits: 0b111010, probs: 201<<40 | 19<<32 | 88<<24 | 177<<16 | 91<<8 | 92, n: 6},
		{bits: 0b0101101, probs: 93<<48 | 94<<40 | 95<<32 | 96<<24 | 97<<16 | 98<<8 | 99, n: 7},
		{bits: 0b10110111, probs: 101<<56 | 102<<48 | 103<<40 | 104<<32 | 105<<24 | 106<<16 | 107<<8 | 108, n: 8},
	}

	sequentialBuf := make([]byte, 256)
	var sequential Writer
	sequential.Start(sequentialBuf)
	for _, f := range fragments {
		for i := f.n - 1; i >= 0; i-- {
			bit := (f.bits >> uint(i)) & 1
			prob := uint32((f.probs >> uint(i*8)) & 0xff)
			sequential.Write(bit, prob)
		}
	}
	sequentialSize, err := sequential.Stop()
	if err != nil {
		t.Fatalf("sequential Stop: %v", err)
	}

	packedBuf := make([]byte, 256)
	var packed Writer
	packed.Start(packedBuf)
	for _, f := range fragments {
		packed.WritePacked64(f.bits, f.probs, f.n)
	}
	packedSize, err := packed.Stop()
	if err != nil {
		t.Fatalf("packed Stop: %v", err)
	}
	if packedSize != sequentialSize {
		t.Fatalf("packed size = %d, want sequential size %d", packedSize, sequentialSize)
	}
	for i := range packedSize {
		if packedBuf[i] != sequentialBuf[i] {
			t.Fatalf("byte %d: packed = 0x%02x, sequential = 0x%02x",
				i, packedBuf[i], sequentialBuf[i])
		}
	}
}

func TestReaderStateMatchesReader(t *testing.T) {
	const n = 8192
	rng := rand.New(rand.NewSource(0x51504c4f43414c))
	bits := make([]uint32, n)
	probs := make([]uint32, n)
	for i := range bits {
		bits[i] = uint32(rng.Intn(2))
		probs[i] = uint32(1 + rng.Intn(255))
	}

	buf := make([]byte, 16384)
	var w Writer
	w.Start(buf)
	for i := range bits {
		w.Write(bits[i], probs[i])
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var direct Reader
	if err := direct.Init(buf[:size]); err != nil {
		t.Fatalf("direct Init: %v", err)
	}
	var local Reader
	if err := local.Init(buf[:size]); err != nil {
		t.Fatalf("local Init: %v", err)
	}
	state := local.LocalState()
	for i := range bits {
		var gotLocal uint32
		if i&7 == 0 && probs[i] == 128 {
			gotLocal = state.ReadBit()
		} else {
			gotLocal = state.Read(probs[i])
		}
		gotDirect := direct.Read(probs[i])
		if gotLocal != gotDirect || gotDirect != bits[i] {
			t.Fatalf("bit %d: local=%d direct=%d want=%d prob=%d",
				i, gotLocal, gotDirect, bits[i], probs[i])
		}
	}
	state.Commit()
	if local.HasError() != direct.HasError() {
		t.Fatalf("HasError local=%v direct=%v", local.HasError(), direct.HasError())
	}
}

func TestWriterDiscardProducesNoBytesAndResets(t *testing.T) {
	var discard Writer
	discard.StartDiscard()
	for i := range 4096 {
		discard.Write(uint32(i&1), uint32(100+(i%150)))
	}
	discard.WriteLiteral(0xdeadbeef, 32)
	size, err := discard.Stop()
	if err != nil {
		t.Fatalf("discard Stop: %v", err)
	}
	if size != 0 {
		t.Fatalf("discard size = %d, want 0", size)
	}

	wantBuf := make([]byte, 1024)
	var want Writer
	want.Start(wantBuf)
	want.Write(1, 137)
	want.WriteLiteral(0x35, 6)
	wantSize, err := want.Stop()
	if err != nil {
		t.Fatalf("want Stop: %v", err)
	}

	gotBuf := make([]byte, 1024)
	discard.Start(gotBuf)
	discard.Write(1, 137)
	discard.WriteLiteral(0x35, 6)
	gotSize, err := discard.Stop()
	if err != nil {
		t.Fatalf("got Stop: %v", err)
	}
	if gotSize != wantSize {
		t.Fatalf("size after reset = %d, want %d", gotSize, wantSize)
	}
	for i := range gotSize {
		if gotBuf[i] != wantBuf[i] {
			t.Fatalf("byte %d after reset = 0x%02x, want 0x%02x",
				i, gotBuf[i], wantBuf[i])
		}
	}
}

// TestRoundTripLiterals exercises the multi-bit literal helpers used by
// the VP9 uncompressed header parser.
func TestRoundTripLiterals(t *testing.T) {
	values := []struct {
		data uint32
		bits uint32
	}{
		{0x00, 1},
		{0x1, 1},
		{0x5, 3},
		{0xff, 8},
		{0x1234, 16},
		{0xa5a5a5, 24},
		{0xdeadbeef, 32},
	}

	buf := make([]byte, 1024)
	var w Writer
	w.Start(buf)
	for _, v := range values {
		w.WriteLiteral(v.data, v.bits)
	}
	size, err := w.Stop()
	if err != nil {
		t.Fatalf("Stop: %v", err)
	}

	var r Reader
	if err := r.Init(buf[:size]); err != nil {
		t.Fatalf("Init: %v", err)
	}
	for i, v := range values {
		mask := uint32(1)<<uint(v.bits) - 1
		want := v.data & mask
		if got := r.ReadLiteral(int(v.bits)); got != want {
			t.Fatalf("literal %d: got %x, want %x", i, got, want)
		}
	}
}

// TestReaderRejectsBadMarker verifies that an Init against a buffer whose
// very first decoded bit is nonzero is rejected, matching the upstream
// vpx_reader_init contract.
func TestReaderRejectsBadMarker(t *testing.T) {
	src := []byte{0xff, 0x00, 0x00, 0x00}
	var r Reader
	if err := r.Init(src); err == nil {
		t.Fatal("expected ErrInvalidInput on bad marker bit, got nil")
	}
}

var benchmarkWriterBytes int

func BenchmarkWriterWriteBit(b *testing.B) {
	const n = 8192
	bits := make([]uint32, n)
	for i := range bits {
		bits[i] = uint32((i*1103515245 + 12345) >> 31)
	}
	buf := make([]byte, n/8+128)
	b.ReportAllocs()
	for b.Loop() {
		var w Writer
		w.Start(buf)
		for _, bit := range bits {
			w.WriteBit(bit)
		}
		size, err := w.Stop()
		if err != nil {
			b.Fatalf("Stop: %v", err)
		}
		benchmarkWriterBytes = size
	}
}

func BenchmarkWriterWriteMixedProb(b *testing.B) {
	const n = 8192
	bits := make([]uint32, n)
	probs := make([]uint32, n)
	for i := range bits {
		x := uint32(i*1664525 + 1013904223)
		bits[i] = x >> 31
		probs[i] = 1 + ((x >> 16) % 255)
		if i&7 == 0 {
			probs[i] = 128
		}
	}
	buf := make([]byte, n*2)
	b.ReportAllocs()
	for b.Loop() {
		var w Writer
		w.Start(buf)
		for i, bit := range bits {
			w.Write(bit, probs[i])
		}
		size, err := w.Stop()
		if err != nil {
			b.Fatalf("Stop: %v", err)
		}
		benchmarkWriterBytes = size
	}
}
