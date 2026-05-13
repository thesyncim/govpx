package decoder

// VP9 frame-size + interpolation-filter parsing helpers used by the
// uncompressed header. Ported from libvpx v1.16.0
// vp9/decoder/vp9_decodeframe.c — read_interp_filter, setup_render_size,
// the frame-size-with-refs reference picker, and the inter-frame ref
// frame index block.

// InterpFilter mirrors the INTERP_FILTER enum in vp9/common/vp9_filter.h.
type InterpFilter uint8

const (
	InterpEighttap       InterpFilter = 0
	InterpEighttapSmooth InterpFilter = 1
	InterpEighttapSharp  InterpFilter = 2
	InterpBilinear       InterpFilter = 3
	InterpSwitchable     InterpFilter = 4
)

// literalToFilter maps the 2-bit literal in read_interp_filter to the
// matching INTERP_FILTER value. Order is fixed by the wire format.
var literalToFilter = [4]InterpFilter{
	InterpEighttapSmooth, InterpEighttap, InterpEighttapSharp, InterpBilinear,
}

// ReadInterpFilter mirrors read_interp_filter. A leading 1-bit selects
// SWITCHABLE (per-block); otherwise a 2-bit literal selects one of the
// four banks.
func ReadInterpFilter(r *BitReader) InterpFilter {
	if r.ReadBit() != 0 {
		return InterpSwitchable
	}
	return literalToFilter[r.ReadLiteral(2)]
}

// RenderSize holds the optional display dimensions a key/intra-only
// frame can carry after the coded dimensions.
type RenderSize struct {
	Width, Height uint32
}

// ReadRenderSize mirrors setup_render_size. If the leading bit is 0
// the render dimensions equal the coded (width, height); otherwise a
// fresh 32-bit (w-1, h-1) field follows.
func ReadRenderSize(r *BitReader, codedWidth, codedHeight uint32) RenderSize {
	out := RenderSize{Width: codedWidth, Height: codedHeight}
	if r.ReadBit() != 0 {
		out.Width, out.Height = ReadFrameSize(r)
	}
	return out
}

// FrameSizeWithRefs carries the state setup_frame_size_with_refs
// emits — either an inherited size from one of the three reference
// frames (Found = true with refer index FromRef) or a freshly read
// (Width, Height) pair (Found = false).
type FrameSizeWithRefs struct {
	Found   bool
	FromRef int
	Width   uint32
	Height  uint32
	Render  RenderSize
}

// ReadFrameSizeWithRefs mirrors the parser shape of
// setup_frame_size_with_refs without doing any buffer allocation. For
// each of the three reference slots a 1-bit flag says "inherit this
// ref's size"; the first set bit wins. If none is set, an explicit
// 32-bit (w-1, h-1) field is read. Then the render size follows.
//
// refWidths / refHeights carry the dimensions of the three references
// in the order LAST / GOLDEN / ALTREF. Callers wire this from the
// frame buffer pool.
func ReadFrameSizeWithRefs(r *BitReader, refWidths, refHeights [3]uint32) FrameSizeWithRefs {
	var out FrameSizeWithRefs
	for i := range 3 {
		if r.ReadBit() != 0 && !out.Found {
			out.Found = true
			out.FromRef = i
			out.Width = refWidths[i]
			out.Height = refHeights[i]
		}
	}
	if !out.Found {
		out.Width, out.Height = ReadFrameSize(r)
	}
	out.Render = ReadRenderSize(r, out.Width, out.Height)
	return out
}

// InterRefBlock carries the per-reference-frame state read for an
// inter frame: each of REFS_PER_FRAME slots picks a ring slot via 3
// bits and adds a sign-bias bit.
type InterRefBlock struct {
	RefIndex [3]uint8 // 0..7 index into the 8-slot ring
	SignBias [3]uint8 // 0 or 1
}

// ReadInterRefBlock mirrors the per-reference fragment of
// read_uncompressed_header's non-key, non-intra-only path: for each of
// 3 inter refs, read REF_FRAMES_LOG2 (= 3) bits for the ring slot and
// one bit for the sign bias.
func ReadInterRefBlock(r *BitReader) InterRefBlock {
	var out InterRefBlock
	for i := range 3 {
		out.RefIndex[i] = uint8(r.ReadLiteral(3))
		out.SignBias[i] = uint8(r.ReadBit())
	}
	return out
}
