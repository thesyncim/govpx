//go:build arm64 && !purego

package encoder

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/bitstream"
	vp9dec "github.com/thesyncim/govpx/internal/vp9/decoder"
	"github.com/thesyncim/govpx/internal/vp9/tables"
)

// packTokenKernelArgs is the single-pointer argument block for the arm64
// range-coder batch kernel. Field offsets are consumed by
// token_pack_kernel_arm64.s and must not change without updating it.
type packTokenKernelArgs struct {
	lo    uint32 // +0
	rng   uint32 // +4
	count int32  // +8
	pos   uint32 // +12

	buf  unsafe.Pointer // +16 output byte buffer base
	toks unsafe.Pointer // +24 TokenExtra base (6-byte stride)
	nTok uint64         // +32

	fc     unsafe.Pointer // +40 FrameCoefProbs base (ProbOff byte offsets)
	pareto unsafe.Pointer // +48 tables.Pareto8Full base ([255][8]uint8)
	cats   unsafe.Pointer // +56 packed Cat1..Cat6 probability bytes

	hasResidue uint64 // +64 out: 1 when a non-zero token was packed
	consumed   uint64 // +72 out: tokens consumed
	status     uint64 // +80 out: 0 = clean, 1 = bad stream (EOSB/EOB mid-run)
}

// packCatProbsPacked lays the category extra-bit probability rows out
// back-to-back so the kernel indexes them from one base pointer:
// cat1 at +0 (1B), cat2 at +1 (2B), cat3 at +3 (3B), cat4 at +6 (4B),
// cat5 at +10 (5B), cat6 at +15 (14B).
var packCatProbsPacked = func() [29]uint8 {
	var p [29]uint8
	copy(p[0:], tables.Cat1Prob[:])
	copy(p[1:], tables.Cat2Prob[:])
	copy(p[3:], tables.Cat3Prob[:])
	copy(p[6:], tables.Cat4Prob[:])
	copy(p[10:], tables.Cat5Prob[:])
	copy(p[15:], tables.Cat6Prob[:])
	return p
}()

//go:noescape
func packTokenWindowNEON(args *packTokenKernelArgs)

// packTokenWindowKernel packs one staged token window with the arithmetic
// coder state held in registers. handled=false means the caller must run the
// portable path (writer discarding/errored, empty window, or insufficient
// spare buffer capacity for the batch worst case).
func packTokenWindowKernel(
	bw *bitstream.Writer, tokens []TokenExtra, fc *vp9dec.FrameCoefProbs,
) (hasResidue bool, consumed int, ok bool, handled bool) {
	if len(tokens) == 0 {
		return false, 0, false, false
	}
	lo, rng, count, buf, pos, stateOK := bw.KernelState()
	if !stateOK {
		return false, 0, false, false
	}
	// Worst-case output for one token is under 3 bytes (Category6 head:
	// 1 not-EOB + 1 zero + 1 pivot + 4 tail + 14 extra + 1 sign = 22 bits).
	// Add slack for the byte in flight.
	worst := uint64(len(tokens))*3 + 8
	if uint64(len(buf))-uint64(pos) < worst {
		return false, 0, false, false
	}
	args := packTokenKernelArgs{
		lo:     lo,
		rng:    rng,
		count:  count,
		pos:    pos,
		buf:    unsafe.Pointer(unsafe.SliceData(buf)),
		toks:   unsafe.Pointer(unsafe.SliceData(tokens)),
		nTok:   uint64(len(tokens)),
		fc:     unsafe.Pointer(fc),
		pareto: unsafe.Pointer(&tables.Pareto8Full[0][0]),
		cats:   unsafe.Pointer(&packCatProbsPacked[0]),
	}
	packTokenWindowNEON(&args)
	bw.SetKernelState(args.lo, args.rng, args.count, args.pos)
	if args.status != 0 {
		return false, 0, false, true
	}
	return args.hasResidue != 0, int(args.consumed), true, true
}
