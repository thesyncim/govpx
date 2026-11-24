package decoder

import "testing"

func TestReadInterpFilter(t *testing.T) {
	cases := []struct {
		name string
		bits []uint32
		want InterpFilter
	}{
		{"switchable", []uint32{1}, InterpSwitchable},
		{"eighttap_smooth", []uint32{0, 0, 0}, InterpEighttapSmooth},
		{"eighttap", []uint32{0, 0, 1}, InterpEighttap},
		{"eighttap_sharp", []uint32{0, 1, 0}, InterpEighttapSharp},
		{"bilinear", []uint32{0, 1, 1}, InterpBilinear},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var pk bitPacker
			for _, b := range c.bits {
				pk.writeBit(b)
			}
			for pk.bitPos&7 != 0 {
				pk.writeBit(0)
			}
			var r BitReader
			r.Init(pk.buf)
			if got := ReadInterpFilter(&r); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestReadRenderSizeInherit(t *testing.T) {
	// flag = 0 → render = (codedWidth, codedHeight)
	var pk bitPacker
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadRenderSize(&r, 1280, 720)
	if got.Width != 1280 || got.Height != 720 {
		t.Errorf("inherit: got %+v", got)
	}
}

func TestReadRenderSizeExplicit(t *testing.T) {
	// flag = 1 then (640-1, 360-1) as two 16-bit literals.
	var pk bitPacker
	pk.writeBit(1)
	pk.writeLiteral(639, 16)
	pk.writeLiteral(359, 16)
	var r BitReader
	r.Init(pk.buf)
	got := ReadRenderSize(&r, 1280, 720)
	if got.Width != 640 || got.Height != 360 {
		t.Errorf("explicit: got %+v", got)
	}
}

func TestReadFrameSizeWithRefsInheritFromGolden(t *testing.T) {
	// last_flag=0, golden_flag=1, altref_flag=0 — Golden's size wins.
	// render_flag=0 — render = (golden's w, h).
	var pk bitPacker
	pk.writeBit(0)
	pk.writeBit(1)
	pk.writeBit(0)
	pk.writeBit(0) // render flag inherit
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadFrameSizeWithRefs(&r,
		[3]uint32{1920, 1280, 960},
		[3]uint32{1080, 720, 540},
	)
	if !got.Found || got.FromRef != 1 || got.Width != 1280 || got.Height != 720 {
		t.Errorf("expected inherit-from-1 to (1280,720); got %+v", got)
	}
	if got.Render.Width != 1280 || got.Render.Height != 720 {
		t.Errorf("render = %+v, want (1280,720)", got.Render)
	}
}

func TestReadFrameSizeWithRefsExplicit(t *testing.T) {
	// All three flags = 0; then explicit 1024x768; render flag = 0.
	var pk bitPacker
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeBit(0)
	pk.writeLiteral(1023, 16)
	pk.writeLiteral(767, 16)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadFrameSizeWithRefs(&r,
		[3]uint32{1920, 1280, 960},
		[3]uint32{1080, 720, 540},
	)
	if got.Found {
		t.Errorf("expected !Found, got %+v", got)
	}
	if got.Width != 1024 || got.Height != 768 {
		t.Errorf("expected (1024,768), got (%d,%d)", got.Width, got.Height)
	}
}

func TestReadInterRefBlock(t *testing.T) {
	// Three refs: (idx=5, bias=0), (idx=2, bias=1), (idx=7, bias=0).
	var pk bitPacker
	pk.writeLiteral(5, 3)
	pk.writeBit(0)
	pk.writeLiteral(2, 3)
	pk.writeBit(1)
	pk.writeLiteral(7, 3)
	pk.writeBit(0)
	for pk.bitPos&7 != 0 {
		pk.writeBit(0)
	}
	var r BitReader
	r.Init(pk.buf)
	got := ReadInterRefBlock(&r)
	wantIdx := [3]uint8{5, 2, 7}
	wantBias := [3]uint8{0, 1, 0}
	if got.RefIndex != wantIdx || got.SignBias != wantBias {
		t.Errorf("got %+v, want RefIndex=%v SignBias=%v", got, wantIdx, wantBias)
	}
}
