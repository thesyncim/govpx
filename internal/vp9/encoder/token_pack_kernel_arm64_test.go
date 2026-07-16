//go:build arm64 && !purego

package encoder

import (
	"bytes"
	"math/rand"
	"testing"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
)

func TestPackTokenKernelLayoutAssumptions(t *testing.T) {
	if unsafe.Sizeof(TokenExtra{}) != 6 {
		t.Fatalf("TokenExtra stride = %d, kernel assumes 6", unsafe.Sizeof(TokenExtra{}))
	}
	var a packTokenKernelArgs
	for _, c := range []struct {
		name string
		off  uintptr
		want uintptr
	}{
		{"lo", unsafe.Offsetof(a.lo), 0},
		{"rng", unsafe.Offsetof(a.rng), 4},
		{"count", unsafe.Offsetof(a.count), 8},
		{"pos", unsafe.Offsetof(a.pos), 12},
		{"buf", unsafe.Offsetof(a.buf), 16},
		{"toks", unsafe.Offsetof(a.toks), 24},
		{"nTok", unsafe.Offsetof(a.nTok), 32},
		{"fc", unsafe.Offsetof(a.fc), 40},
		{"pareto", unsafe.Offsetof(a.pareto), 48},
		{"cats", unsafe.Offsetof(a.cats), 56},
		{"hasResidue", unsafe.Offsetof(a.hasResidue), 64},
		{"consumed", unsafe.Offsetof(a.consumed), 72},
		{"status", unsafe.Offsetof(a.status), 80},
	} {
		if c.off != c.want {
			t.Fatalf("packTokenKernelArgs.%s offset = %d, kernel assumes %d", c.name, c.off, c.want)
		}
	}
}

func TestPackTokenKernelMatchesPerBitReference(t *testing.T) {
	rng := rand.New(rand.NewSource(0x51ac0b5f))
	var fc vp9dec.FrameCoefProbs
	for trial := range 6000 {
		randCoefProbs(rng, &fc)
		tokens := randStagedTokens(rng, &fc, 1+rng.Intn(80))
		// Occasionally corrupt the stream to exercise the bail paths.
		if trial%17 == 0 && len(tokens) > 1 {
			i := 1 + rng.Intn(len(tokens)-1)
			if rng.Intn(2) == 0 {
				tokens[i].Token = EOSBToken
			} else {
				tokens[i].Token = EobToken
			}
		}

		refBuf := make([]byte, 4096)
		gotBuf := make([]byte, 4096)
		var refW, gotW bitstream.Writer
		refW.Start(refBuf)
		gotW.Start(gotBuf)
		prefix := rng.Intn(48)
		for range prefix {
			bit := uint32(rng.Intn(2))
			prob := uint32(1 + rng.Intn(255))
			refW.Write(bit, prob)
			gotW.Write(bit, prob)
		}

		// Reference = the portable Go window loop (itself pinned against the
		// per-bit reference by TestPackTokenWindowFusedMatchesPerBitReference).
		refRes, refN, refOK := packTokenBlockAndHasResidueWindow(&refW, tokens, &fc)
		gotRes, gotN, gotOK, handled := packTokenWindowKernel(&gotW, tokens, &fc)
		if !handled {
			t.Fatalf("trial %d: kernel refused a viable window", trial)
		}
		if refRes != gotRes || refN != gotN || refOK != gotOK {
			t.Fatalf("trial %d: result mismatch ref=(%v,%d,%v) got=(%v,%d,%v)",
				trial, refRes, refN, refOK, gotRes, gotN, gotOK)
		}
		refLen, refErr := refW.Stop()
		gotLen, gotErr := gotW.Stop()
		if (refErr == nil) != (gotErr == nil) || refLen != gotLen ||
			!bytes.Equal(refBuf[:refLen], gotBuf[:gotLen]) {
			t.Fatalf("trial %d: byte mismatch len ref=%d got=%d", trial, refLen, gotLen)
		}
	}
}

func FuzzPackTokenKernel(f *testing.F) {
	f.Add(int64(1), 16)
	f.Add(int64(42), 64)
	f.Fuzz(func(t *testing.T, seed int64, n int) {
		if n <= 0 || n > 512 {
			t.Skip()
		}
		rng := rand.New(rand.NewSource(seed))
		var fc vp9dec.FrameCoefProbs
		randCoefProbs(rng, &fc)
		tokens := randStagedTokens(rng, &fc, n)
		refBuf := make([]byte, 8192)
		gotBuf := make([]byte, 8192)
		var refW, gotW bitstream.Writer
		refW.Start(refBuf)
		gotW.Start(gotBuf)
		refRes, refN, refOK := packTokenBlockAndHasResidueWindow(&refW, tokens, &fc)
		gotRes, gotN, gotOK, handled := packTokenWindowKernel(&gotW, tokens, &fc)
		if !handled {
			t.Skip()
		}
		if refRes != gotRes || refN != gotN || refOK != gotOK {
			t.Fatalf("result mismatch")
		}
		refLen, _ := refW.Stop()
		gotLen, _ := gotW.Stop()
		if refLen != gotLen || !bytes.Equal(refBuf[:refLen], gotBuf[:gotLen]) {
			t.Fatalf("byte mismatch")
		}
	})
}
