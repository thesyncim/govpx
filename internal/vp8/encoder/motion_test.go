package encoder

import (
	"errors"
	"testing"

	"github.com/thesyncim/libgopx/internal/vp8/boolcoder"
	vp8dec "github.com/thesyncim/libgopx/internal/vp8/decoder"
	"github.com/thesyncim/libgopx/internal/vp8/tables"
)

func TestWriteMotionVectorRoundTripsSmall(t *testing.T) {
	probs := tables.DefaultMVContext
	payload := motionVectorPayload(t, &probs, MotionVector{Row: -6, Col: 0})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	got := vp8dec.DecodeMotionVector(&br, &probs)

	if got != (vp8dec.MotionVector{Row: -6, Col: 0}) {
		t.Fatalf("mv = %+v, want {-6,0}", got)
	}
}

func TestWriteMotionVectorRoundTripsLarge(t *testing.T) {
	probs := tables.DefaultMVContext
	payload := motionVectorPayload(t, &probs, MotionVector{Row: 40, Col: -24})
	var br boolcoder.Decoder
	if err := br.Init(payload); err != nil {
		t.Fatalf("Init returned error: %v", err)
	}

	got := vp8dec.DecodeMotionVector(&br, &probs)

	if got != (vp8dec.MotionVector{Row: 40, Col: -24}) {
		t.Fatalf("mv = %+v, want {40,-24}", got)
	}
}

func TestWriteMotionVectorRejectsOddComponent(t *testing.T) {
	var w BoolWriter
	w.Init(make([]byte, 64))
	probs := tables.DefaultMVContext

	err := WriteMotionVector(&w, &probs, MotionVector{Row: 1})

	if !errors.Is(err, ErrInvalidPacketConfig) {
		t.Fatalf("error = %v, want ErrInvalidPacketConfig", err)
	}
}

func TestWriteMotionVectorAllocatesZero(t *testing.T) {
	buf := make([]byte, 64)
	probs := tables.DefaultMVContext
	var w BoolWriter
	allocs := testing.AllocsPerRun(1000, func() {
		w.Init(buf)
		_ = WriteMotionVector(&w, &probs, MotionVector{Row: -6, Col: 16})
		w.Finish()
	})
	if allocs != 0 {
		t.Fatalf("allocs = %v, want 0", allocs)
	}
}

func motionVectorPayload(t *testing.T, probs *[2][tables.MVPCount]uint8, mv MotionVector) []byte {
	t.Helper()
	var w BoolWriter
	buf := make([]byte, 64)
	w.Init(buf)
	if err := WriteMotionVector(&w, probs, mv); err != nil {
		t.Fatalf("WriteMotionVector returned error: %v", err)
	}
	w.Finish()
	if err := w.Err(); err != nil {
		t.Fatalf("BoolWriter error = %v", err)
	}
	return w.Bytes()
}
