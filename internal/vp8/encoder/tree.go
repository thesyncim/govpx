package encoder

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vp8/common/treecoder.h token representation and
// vp8/encoder/bitstream.c tree token writers.

type TreeToken struct {
	Value uint32
	Len   uint8
}

func BuildTreeToken(tree []int16, token int, out *TreeToken) bool {
	if out == nil || token < 0 {
		return false
	}
	value, length, ok := findTreeToken(tree, 0, token, 0, 0)
	if !ok {
		return false
	}
	out.Value = value
	out.Len = uint8(length)
	return true
}

func WriteTreeToken(w *BoolWriter, tree []int16, probs []uint8, token TreeToken) bool {
	probsLen := len(probs)
	treeLen := len(tree)
	tokenLen := int(token.Len)
	if probsLen == 0 || treeLen < 2 || tokenLen == 0 || tokenLen > 32 || w.err != 0 {
		return false
	}
	low := w.low
	rng := w.rng
	count := w.count
	pos := w.pos
	buf := w.buf

	node := int16(0)
	value := token.Value
	for bitIndex := tokenLen - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		nodeIdx := int(node)
		if uint(probIndex) >= uint(probsLen) || nodeIdx+1 >= treeLen {
			w.low = low
			w.rng = rng
			w.count = count
			w.pos = pos
			return false
		}
		bit := uint8((value >> uint(bitIndex)) & 1)
		split := uint32(1 + (((rng - 1) * uint32(probs[probIndex])) >> 8))
		if bit == 0 {
			rng = split
		} else {
			low += split
			rng -= split
		}

		shift := uint(tables.BoolNorm[byte(rng)] & 7)
		rng <<= shift
		count += int(shift)
		if count >= 0 {
			offset := int(shift) - count
			if ((low << uint(offset-1)) & 0x80000000) != 0 {
				w.pos = pos
				w.propagateCarry()
				if w.err != 0 {
					return false
				}
			}
			if pos >= len(buf) {
				w.err = boolWriterErrBufferTooSmall
				w.pos = pos
				return false
			}
			boolWriterStoreByte(buf, pos, byte((low>>uint(24-offset))&0xff))
			pos++
			tailShift := uint(count)
			low = (low << uint(offset)) & 0xffffff
			count -= 8
			low <<= tailShift
		} else {
			low <<= shift
		}

		next := tree[nodeIdx+int(bit)]
		if next <= 0 {
			w.low = low
			w.rng = rng
			w.count = count
			w.pos = pos
			return bitIndex == 0
		}
		node = next
	}
	w.low = low
	w.rng = rng
	w.count = count
	w.pos = pos
	return false
}

// findTreeToken walks the encoder token tree to find the (value, length)
// pair that decodes to `token`. The Go inliner unrolls the small recursion
// here for the fixed-shape VP8 trees we ship; benchmarks (BenchmarkFindTreeToken)
// show this beats an explicit iterative-stack rewrite, so we leave it
// recursive. Mode decision avoids re-walking the tree at runtime by using
// the precomputed token-cost paths in tree_cost.go;
// the recursive walker only runs at startup or when a non-fixed tree is
// supplied.
func findTreeToken(tree []int16, node int16, token int, value uint32, depth int) (uint32, int, bool) {
	if depth >= 32 || int(node)+1 >= len(tree) {
		return 0, 0, false
	}
	for bit := range int16(2) {
		next := tree[int(node)+int(bit)]
		nextValue := (value << 1) | uint32(bit)
		if next <= 0 {
			if int(-next) == token {
				return nextValue, depth + 1, true
			}
			continue
		}
		if foundValue, foundDepth, ok := findTreeToken(tree, next, token, nextValue, depth+1); ok {
			return foundValue, foundDepth, true
		}
	}
	return 0, 0, false
}
