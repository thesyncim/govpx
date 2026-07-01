package govpx

import (
	"testing"

	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestVP9DecoderInternalRefreshAliasesDecodedFrame(t *testing.T) {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	key := vp9test.StubPacket(t, 64, 64, 0, common.DcPred)
	if err := d.Decode(key); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no keyframe")
	}

	lease := d.refFrames[0].internal
	if lease == nil {
		t.Fatal("reference slot 0 did not retain an internal frame lease")
	}
	for slot := range d.refFrames {
		if !d.refFrames[slot].valid {
			t.Fatalf("reference slot %d invalid after keyframe refresh-all", slot)
		}
		if got := d.refFrames[slot].internal; got != lease {
			t.Fatalf("reference slot %d internal lease = %p, want %p",
				slot, got, lease)
		}
	}
	if got, want := lease.refs, common.RefFrames+1; got != want {
		t.Fatalf("internal lease refs = %d, want %d", got, want)
	}

	replacement := vp9SolidImageForTest(64, 64, 32, 128, 128)
	if err := d.SetReferenceFrame(ReferenceLast, replacement); err != nil {
		t.Fatalf("SetReferenceFrame LAST: %v", err)
	}
	if d.refFrames[0].internal != nil {
		t.Fatal("SetReferenceFrame LAST left an internal lease on slot 0")
	}

	if err := d.Decode(vp9test.ShowExistingFramePacket(5)); err != nil {
		t.Fatalf("Decode show-existing slot 5: %v", err)
	}
	show, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no show-existing frame")
	}
	if !vp9ImagesEqualForTest(keyFrame, show) {
		t.Fatal("show-existing slot 5 changed after replacing LAST")
	}
	if vp9ImagesEqualForTest(replacement, show) {
		t.Fatal("show-existing slot 5 unexpectedly matched replacement LAST")
	}
}

func vp9SolidImageForTest(width, height int, y, u, v byte) Image {
	img := Image{
		Width:   width,
		Height:  height,
		Y:       make([]byte, width*height),
		U:       make([]byte, ((width+1)>>1)*((height+1)>>1)),
		V:       make([]byte, ((width+1)>>1)*((height+1)>>1)),
		YStride: width,
		UStride: (width + 1) >> 1,
		VStride: (width + 1) >> 1,
	}
	for i := range img.Y {
		img.Y[i] = y
	}
	for i := range img.U {
		img.U[i] = u
		img.V[i] = v
	}
	return img
}

func vp9ImagesEqualForTest(a, b Image) bool {
	if a.Width != b.Width || a.Height != b.Height {
		return false
	}
	uvWidth := (a.Width + 1) >> 1
	uvHeight := (a.Height + 1) >> 1
	return vp9PlaneEqualForTest(a.Y, a.YStride, b.Y, b.YStride, a.Width, a.Height) &&
		vp9PlaneEqualForTest(a.U, a.UStride, b.U, b.UStride, uvWidth, uvHeight) &&
		vp9PlaneEqualForTest(a.V, a.VStride, b.V, b.VStride, uvWidth, uvHeight)
}

func vp9PlaneEqualForTest(a []byte, aStride int, b []byte, bStride int,
	width, height int,
) bool {
	for row := range height {
		ar := a[row*aStride : row*aStride+width]
		br := b[row*bStride : row*bStride+width]
		for col := range width {
			if ar[col] != br[col] {
				return false
			}
		}
	}
	return true
}
