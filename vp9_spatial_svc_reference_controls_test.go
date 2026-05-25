package govpx

import (
	"errors"
	"github.com/thesyncim/govpx/internal/testutil/vp9test"
	"testing"
)

func TestVP9SpatialSVCEncoderLayerReferenceControls(t *testing.T) {
	const baseW, baseH = 32, 32
	const enhW, enhH = 64, 64
	svc, err := NewVP9SpatialSVCEncoder(VP9SpatialSVCEncoderOptions{
		LayerCount: 2,
		Layers: [VP9MaxSpatialLayers]VP9EncoderOptions{
			{Width: baseW, Height: baseH},
			{Width: enhW, Height: enhH},
		},
	})
	if err != nil {
		t.Fatalf("NewVP9SpatialSVCEncoder: %v", err)
	}

	baseRefYCbCr := vp9test.NewMotionYCbCr(baseW, baseH)
	baseRef := vp9ImageFromYCbCrForTest(baseRefYCbCr)
	baseWant := clonePublicImage(baseRef)
	if err := svc.SetLayerReferenceFrame(0, ReferenceGolden, baseRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame base: %v", err)
	}
	baseRef.Y[0] ^= 0xff
	baseDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(baseW, baseH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(0, ReferenceGolden, &baseDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame base: %v", err)
	}
	assertImagesEqual(t, "base layer copied GOLDEN reference", baseWant, baseDst)

	enhRefYCbCr := vp9test.NewMotionYCbCr(enhW, enhH)
	enhRef := vp9ImageFromYCbCrForTest(enhRefYCbCr)
	if err := svc.SetLayerReferenceFrame(1, ReferenceLast, enhRef); err != nil {
		t.Fatalf("SetLayerReferenceFrame enhancement: %v", err)
	}
	enhDst := vp9ImageFromYCbCrForTest(vp9test.NewYCbCr(enhW, enhH, 0, 0, 0))
	if err := svc.CopyLayerReferenceFrame(1, ReferenceLast, &enhDst); err != nil {
		t.Fatalf("CopyLayerReferenceFrame enhancement: %v", err)
	}
	assertImagesEqual(t, "enhancement layer copied LAST reference", enhRef, enhDst)

	if err := svc.SetLayerReferenceFrame(2, ReferenceLast, enhRef); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("SetLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.CopyLayerReferenceFrame(2, ReferenceLast, &enhDst); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("CopyLayerReferenceFrame invalid layer err = %v, want ErrInvalidConfig", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := svc.SetLayerReferenceFrame(0, ReferenceLast, baseWant); !errors.Is(err, ErrClosed) {
		t.Fatalf("SetLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
	if err := svc.CopyLayerReferenceFrame(0, ReferenceLast, &baseDst); !errors.Is(err, ErrClosed) {
		t.Fatalf("CopyLayerReferenceFrame after close err = %v, want ErrClosed", err)
	}
}
