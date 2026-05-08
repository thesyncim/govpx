package encoder

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
	if probsLen == 0 || treeLen < 2 || tokenLen == 0 || tokenLen > 32 {
		return false
	}
	node := int16(0)
	value := token.Value
	for bitIndex := tokenLen - 1; bitIndex >= 0; bitIndex-- {
		probIndex := int(node >> 1)
		nodeIdx := int(node)
		if probIndex < 0 || probIndex >= probsLen || nodeIdx+1 >= treeLen {
			return false
		}
		bit := uint8((value >> uint(bitIndex)) & 1)
		w.WriteBool(bit, probs[probIndex])
		next := tree[nodeIdx+int(bit)]
		if next <= 0 {
			if w.err != nil {
				return false
			}
			return bitIndex == 0
		}
		node = next
	}
	return false
}

// findTreeToken walks the encoder token tree to find the (value, length)
// pair that decodes to `token`. The Go inliner unrolls the small recursion
// here for the fixed-shape VP8 trees we ship; benchmarks (BenchmarkFindTreeToken)
// show this beats an explicit iterative-stack rewrite, so we leave it
// recursive. Mode decision avoids re-walking the tree at runtime by using
// the precomputed token-cost paths in encoder_tree_costs.go (root package);
// the recursive walker only runs at startup or when a non-fixed tree is
// supplied.
func findTreeToken(tree []int16, node int16, token int, value uint32, depth int) (uint32, int, bool) {
	if depth >= 32 || int(node)+1 >= len(tree) {
		return 0, 0, false
	}
	for bit := int16(0); bit < 2; bit++ {
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
