package encoder

import (
	"testing"

	"github.com/thesyncim/govpx/internal/vp8/boolcoder"
	"github.com/thesyncim/govpx/internal/vp8/common"
	vp8dec "github.com/thesyncim/govpx/internal/vp8/decoder"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

func TestWriteTreeTokenMatchesDecoderTree(t *testing.T) {
	var token TreeToken
	if !BuildTreeToken(tables.KeyFrameYModeTree[:], int(common.DCPred), &token) {
		t.Fatalf("BuildTreeToken returned false")
	}

	var w BoolWriter
	buf := make([]byte, 16)
	w.Init(buf)
	if !WriteTreeToken(&w, tables.KeyFrameYModeTree[:], tables.KeyFrameYModeProbs[:], token) {
		t.Fatalf("WriteTreeToken returned false")
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v, want nil", err)
	}

	var br boolcoder.Decoder
	if err := br.Init(w.Bytes()); err != nil {
		t.Fatalf("Decoder Init returned error: %v", err)
	}
	if got := vp8dec.ReadKeyFrameYMode(&br, tables.KeyFrameYModeProbs[:]); got != int(common.DCPred) {
		t.Fatalf("ReadKeyFrameYMode = %d, want %d", got, common.DCPred)
	}
}

func TestBuildTreeTokenRejectsMissingToken(t *testing.T) {
	var token TreeToken
	if BuildTreeToken(tables.UVModeTree[:], 9, &token) {
		t.Fatalf("BuildTreeToken returned true for missing token")
	}
	if BuildTreeToken(tables.UVModeTree[:], int(common.DCPred), nil) {
		t.Fatalf("BuildTreeToken returned true with nil output")
	}
}

func TestWriteTreeTokenRejectsShortProbabilities(t *testing.T) {
	var token TreeToken
	if !BuildTreeToken(tables.UVModeTree[:], int(common.TMPred), &token) {
		t.Fatalf("BuildTreeToken returned false")
	}
	var w BoolWriter
	w.Init(make([]byte, 16))
	if WriteTreeToken(&w, tables.UVModeTree[:], tables.KeyFrameUVModeProbs[:1], token) {
		t.Fatalf("WriteTreeToken returned true with short probabilities")
	}
}

func TestTreeTokenWriterAllocatesZero(t *testing.T) {
	var token TreeToken
	buf := make([]byte, 16)
	var w BoolWriter
	allocs := testing.AllocsPerRun(1000, func() {
		_ = BuildTreeToken(tables.KeyFrameYModeTree[:], int(common.DCPred), &token)
		w.Init(buf)
		_ = WriteTreeToken(&w, tables.KeyFrameYModeTree[:], tables.KeyFrameYModeProbs[:], token)
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func BenchmarkWriteTreeToken(b *testing.B) {
	var token TreeToken
	_ = BuildTreeToken(tables.KeyFrameYModeTree[:], int(common.DCPred), &token)
	buf := make([]byte, 4096)
	var w BoolWriter
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		w.Init(buf)
		for n := 0; n < 512; n++ {
			_ = WriteTreeToken(&w, tables.KeyFrameYModeTree[:], tables.KeyFrameYModeProbs[:], token)
		}
		w.Finish()
	}
}
