package govpx

import (
	"errors"
	"testing"
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

func TestNewVP9DecoderRejectsPartialExternalFrameBufferCallbacks(t *testing.T) {
	pool := newVP9ExternalFrameBufferPoolForTest()
	for _, opts := range []VP9DecoderOptions{
		{GetFrameBuffer: pool.get},
		{ReleaseFrameBuffer: pool.release},
	} {
		_, err := NewVP9Decoder(opts)
		if !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("NewVP9Decoder(%+v) err = %v, want ErrInvalidConfig",
				opts, err)
		}
	}
}

func TestVP9DecoderSetFrameBufferFunctionsValidation(t *testing.T) {
	pool := newVP9ExternalFrameBufferPoolForTest()
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	if err := d.SetFrameBufferFunctions(nil, pool.release); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil get err = %v, want ErrInvalidConfig", err)
	}
	if err := d.SetFrameBufferFunctions(pool.get, nil); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("nil release err = %v, want ErrInvalidConfig", err)
	}
	if err := d.SetFrameBufferFunctions(pool.get, pool.release); err != nil {
		t.Fatalf("SetFrameBufferFunctions: %v", err)
	}

	packet := vp9StubPacketForTest(t, 96, 80, 0, common.DcPred)
	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if _, ok := d.NextFrame(); !ok {
		t.Fatal("NextFrame returned no visible frame")
	}
	if err := d.SetFrameBufferFunctions(pool.get, pool.release); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("post-init SetFrameBufferFunctions err = %v, want ErrInvalidConfig", err)
	}
	d.Reset()
	if err := d.SetFrameBufferFunctions(pool.get, pool.release); err != nil {
		t.Fatalf("SetFrameBufferFunctions after Reset: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := d.SetFrameBufferFunctions(pool.get, pool.release); !errors.Is(err, ErrClosed) {
		t.Fatalf("closed SetFrameBufferFunctions err = %v, want ErrClosed", err)
	}
	var nilDecoder *VP9Decoder
	if err := nilDecoder.SetFrameBufferFunctions(pool.get, pool.release); !errors.Is(err, ErrClosed) {
		t.Fatalf("nil SetFrameBufferFunctions err = %v, want ErrClosed", err)
	}
}

func TestVP9DecoderExternalFrameBufferPublishesAlignedPixelsAndReleases(t *testing.T) {
	pool := newVP9ExternalFrameBufferPoolForTest()
	packet := vp9StubPacketForTest(t, 96, 80, 0, common.DcPred)
	d, err := NewVP9Decoder(VP9DecoderOptions{
		ByteAlignment:      128,
		GetFrameBuffer:     pool.get,
		ReleaseFrameBuffer: pool.release,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	frame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no visible frame")
	}
	assertVP9NeutralFrame(t, frame, 96, 80)
	assertVP9PlaneAligned(t, "Y", frame.Y, 128)
	assertVP9PlaneAligned(t, "U", frame.U, 128)
	assertVP9PlaneAligned(t, "V", frame.V, 128)
	id := pool.assertOwnsImage(t, frame)
	if !pool.inUse[id] {
		t.Fatalf("frame buffer %d released while frame is still current", id)
	}
	if got, want := len(pool.gets), 1; got != want {
		t.Fatalf("get callbacks = %d, want %d", got, want)
	}
	if got, want := pool.gets[0], vp9ExternalFrameMinSizeForTest(96, 80, 128); got != want {
		t.Fatalf("min_size = %d, want %d", got, want)
	}
	if got := len(pool.releases); got != 0 {
		t.Fatalf("release callbacks before Reset = %d, want 0", got)
	}

	d.Reset()
	if got, want := len(pool.releases), 1; got != want {
		t.Fatalf("release callbacks after Reset = %d, want %d", got, want)
	}
	pool.assertAllReleased(t)
}

func TestVP9DecoderExternalFrameBufferReleasesPreviousKeyOnRefresh(t *testing.T) {
	pool := newVP9ExternalFrameBufferPoolForTest()
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	d, err := NewVP9Decoder(VP9DecoderOptions{
		GetFrameBuffer:     pool.get,
		ReleaseFrameBuffer: pool.release,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	if err := d.Decode(packet); err != nil {
		t.Fatalf("first Decode: %v", err)
	}
	first, ok := d.NextFrame()
	if !ok {
		t.Fatal("first NextFrame returned no visible frame")
	}
	firstID := pool.assertOwnsImage(t, first)

	if err := d.Decode(packet); err != nil {
		t.Fatalf("second Decode: %v", err)
	}
	if got, want := len(pool.releases), 1; got != want {
		t.Fatalf("release callbacks after second key = %d, want %d", got, want)
	}
	if got := pool.releases[0]; got != firstID {
		t.Fatalf("released buffer id = %d, want first buffer %d", got, firstID)
	}
	second, ok := d.NextFrame()
	if !ok {
		t.Fatal("second NextFrame returned no visible frame")
	}
	secondID := pool.assertOwnsImage(t, second)
	if secondID == firstID {
		t.Fatalf("second frame reused released id %d", secondID)
	}

	d.Reset()
	if got, want := len(pool.releases), 2; got != want {
		t.Fatalf("release callbacks after Reset = %d, want %d", got, want)
	}
	pool.assertAllReleased(t)
}

func TestVP9DecoderExternalFrameBufferRejectsShortBuffer(t *testing.T) {
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	releaseCalled := false
	d, err := NewVP9Decoder(VP9DecoderOptions{
		GetFrameBuffer: func(minSize int) (VP9ExternalFrameBuffer, error) {
			return VP9ExternalFrameBuffer{
				Data:    make([]byte, minSize-1),
				Private: 1,
			}, nil
		},
		ReleaseFrameBuffer: func(VP9ExternalFrameBuffer) {
			releaseCalled = true
		},
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()
	if err := d.Decode(packet); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Decode err = %v, want ErrInvalidConfig", err)
	}
	if releaseCalled {
		t.Fatal("release called for rejected short external buffer")
	}
}

func TestVP9DecoderExternalFrameBufferShowExistingUsesReferenceBuffer(t *testing.T) {
	pool := newVP9ExternalFrameBufferPoolForTest()
	packet := vp9StubPacketForTest(t, 64, 64, 0, common.DcPred)
	d, err := NewVP9Decoder(VP9DecoderOptions{
		GetFrameBuffer:     pool.get,
		ReleaseFrameBuffer: pool.release,
	})
	if err != nil {
		t.Fatalf("NewVP9Decoder: %v", err)
	}
	defer d.Close()

	if err := d.Decode(packet); err != nil {
		t.Fatalf("Decode keyframe: %v", err)
	}
	keyFrame, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no keyframe")
	}
	keyID := pool.assertOwnsImage(t, keyFrame)
	if err := d.Decode(vp9ShowExistingFramePacketForTest(5)); err != nil {
		t.Fatalf("Decode show-existing: %v", err)
	}
	show, ok := d.NextFrame()
	if !ok {
		t.Fatal("NextFrame returned no show-existing frame")
	}
	showID := pool.assertOwnsImage(t, show)
	if showID != keyID {
		t.Fatalf("show-existing buffer id = %d, want key buffer %d", showID, keyID)
	}
	if got, want := len(pool.gets), 1; got != want {
		t.Fatalf("get callbacks after show-existing = %d, want %d", got, want)
	}
	assertVP9ImagesEqual(t, keyFrame, show)

	d.Reset()
	if got, want := len(pool.releases), 1; got != want {
		t.Fatalf("release callbacks after Reset = %d, want %d", got, want)
	}
	pool.assertAllReleased(t)
}

type vp9ExternalFrameBufferPoolForTest struct {
	nextID        int
	gets          []int
	releases      []int
	buffers       map[int][]byte
	inUse         map[int]bool
	doubleRelease bool
}

func newVP9ExternalFrameBufferPoolForTest() *vp9ExternalFrameBufferPoolForTest {
	return &vp9ExternalFrameBufferPoolForTest{
		buffers: make(map[int][]byte),
		inUse:   make(map[int]bool),
	}
}

func (p *vp9ExternalFrameBufferPoolForTest) get(minSize int) (VP9ExternalFrameBuffer, error) {
	id := p.nextID
	p.nextID++
	p.gets = append(p.gets, minSize)
	data := make([]byte, minSize+1)
	data = data[1:]
	p.buffers[id] = data
	p.inUse[id] = true
	return VP9ExternalFrameBuffer{Data: data, Private: id}, nil
}

func (p *vp9ExternalFrameBufferPoolForTest) release(buffer VP9ExternalFrameBuffer) {
	id, ok := buffer.Private.(int)
	if !ok {
		p.doubleRelease = true
		return
	}
	if !p.inUse[id] {
		p.doubleRelease = true
	}
	p.inUse[id] = false
	p.releases = append(p.releases, id)
}

func (p *vp9ExternalFrameBufferPoolForTest) assertOwnsImage(t *testing.T, img Image) int {
	t.Helper()
	yID, ok := p.ownerID(img.Y)
	if !ok {
		t.Fatal("Y plane does not point into an external frame buffer")
	}
	if uID, ok := p.ownerID(img.U); !ok || uID != yID {
		t.Fatalf("U plane owner = %d/%v, want Y owner %d", uID, ok, yID)
	}
	if vID, ok := p.ownerID(img.V); !ok || vID != yID {
		t.Fatalf("V plane owner = %d/%v, want Y owner %d", vID, ok, yID)
	}
	return yID
}

func (p *vp9ExternalFrameBufferPoolForTest) ownerID(plane []byte) (int, bool) {
	if len(plane) == 0 {
		return 0, false
	}
	ptr := uintptr(unsafe.Pointer(&plane[0]))
	for id, data := range p.buffers {
		if len(data) == 0 {
			continue
		}
		start := uintptr(unsafe.Pointer(&data[0]))
		end := start + uintptr(len(data))
		if ptr >= start && ptr < end {
			return id, true
		}
	}
	return 0, false
}

func (p *vp9ExternalFrameBufferPoolForTest) assertAllReleased(t *testing.T) {
	t.Helper()
	if p.doubleRelease {
		t.Fatal("external frame buffer released twice or with invalid private id")
	}
	for id, inUse := range p.inUse {
		if inUse {
			t.Fatalf("external frame buffer %d still in use", id)
		}
	}
}

func vp9ExternalFrameMinSizeForTest(width, height, alignment int) int {
	layout := vp9DecoderFrameBufferLayout(width, height, alignment)
	return layout.yFullLen + 2*layout.uvFullLen + 31
}
