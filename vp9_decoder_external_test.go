package govpx

import (
	"bytes"
	"crypto/md5"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/thesyncim/govpx/internal/testutil"
	"github.com/thesyncim/govpx/internal/testutil/vp9corpus"
)

func TestVP9DecoderDefaultProfile0WebMCorpusMinimumMatchesList(t *testing.T) {
	if got := testutil.DefaultVP9Profile0WebMTestNameCount(); got != testutil.DefaultVP9Profile0WebMTestMinimum {
		t.Fatalf("default VP9 Profile 0 WebM corpus list = %d, minimum = %d",
			got, testutil.DefaultVP9Profile0WebMTestMinimum)
	}
}

func TestVP9DecoderDefaultIVFCorpusMinimumMatchesList(t *testing.T) {
	if got := testutil.DefaultVP9IVFTestNameCount(); got != testutil.DefaultVP9IVFTestDataMinimum {
		t.Fatalf("default VP90 IVF corpus list = %d, minimum = %d",
			got, testutil.DefaultVP9IVFTestDataMinimum)
	}
}

func TestVP9DecoderOfficialIVFTestDataThreadedMatchesSerial(t *testing.T) {
	root, ok := vp9corpus.IVFRoot(t)
	if !ok {
		return
	}
	paths := vp9corpus.FindIVF(t, root, false)
	if len(paths) == 0 {
		t.Fatalf("no VP90 IVF files found under %s", root)
	}
	vp9corpus.AssertIVFMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			ivf, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			want, err := decodeVP9IVFVisibleI420WithOptions(ivf,
				VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("serial Decode VP90 IVF returned error: %v", err)
			}
			got, err := decodeVP9IVFVisibleI420WithOptions(ivf,
				VP9DecoderOptions{Threads: 3})
			if err != nil {
				t.Fatalf("threaded Decode VP90 IVF returned error: %v", err)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("threaded VP90 IVF I420 mismatch for %s\nserial=%s\nthreaded=%s",
					filepath.Base(path),
					testutil.MD5Hex(md5.Sum(want)),
					testutil.MD5Hex(md5.Sum(got)))
			}
		})
	}
}

func TestVP9DecoderOfficialProfileWebMTestDataReturnsUnsupported(t *testing.T) {
	root, ok := vp9corpus.ProfileWebMRoot(t)
	if !ok {
		return
	}
	paths := vp9corpus.FindProfileWebM(t, root)
	if len(paths) == 0 {
		if os.Getenv("GOVPX_VP9_PROFILE_TEST_DATA_REQUIRED") == "1" ||
			vp9corpus.ProfileWebMMinimum(t, root) > 0 {
			t.Fatalf("no official VP9 profile WebM files found under %s", root)
		}
		t.Skipf("no official VP9 profile WebM files found under %s", root)
	}
	vp9corpus.AssertProfileWebMMinimum(t, root, paths)

	for _, path := range paths {
		t.Run(testutil.SafeCorpusTestName(root, path), func(t *testing.T) {
			webm, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("ReadFile returned error: %v", err)
			}
			packets, err := testutil.ExtractVP9WebMPackets(webm)
			if err != nil {
				t.Fatalf("extract VP9 WebM packets returned error: %v", err)
			}
			if len(packets) == 0 {
				t.Fatalf("official VP9 profile WebM contained no VP9 packets")
			}

			d, err := NewVP9Decoder(VP9DecoderOptions{})
			if err != nil {
				t.Fatalf("NewVP9Decoder returned error: %v", err)
			}
			for i, packet := range packets {
				err := d.Decode(packet)
				if errors.Is(err, ErrVP9NotImplemented) {
					return
				}
				if err != nil {
					t.Fatalf("Decode official unsupported-profile WebM packet %d returned %v, want ErrVP9NotImplemented", i, err)
				}
				if img, ok := d.NextFrame(); ok {
					t.Fatalf("Decode official unsupported-profile WebM packet %d produced %dx%d I420 output", i, img.Width, img.Height)
				}
			}
			t.Fatalf("Decode accepted %d official unsupported-profile VP9 WebM packets without ErrVP9NotImplemented", len(packets))
		})
	}
}

func decodeVP9IVFVisibleI420(ivf []byte) ([]byte, error) {
	return decodeVP9IVFVisibleI420WithOptions(ivf, VP9DecoderOptions{})
}

func decodeVP9IVFVisibleI420WithOptions(ivf []byte, opts VP9DecoderOptions) (out []byte, err error) {
	d, err := NewVP9Decoder(opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := d.Close(); err == nil && closeErr != nil {
			err = closeErr
		}
	}()
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
		return nil, testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return nil, err
		}
		if err := d.Decode(frame.Data); err != nil {
			return nil, err
		}
		if img, ok := d.NextFrame(); ok {
			out = appendVP9I420(out, img)
		}
		offset = next
	}
	return out, nil
}

func decodeVP9WebMVisibleI420(webm []byte) ([]byte, error) {
	packets, err := testutil.ExtractVP9WebMPackets(webm)
	if err != nil {
		return nil, err
	}
	if len(packets) == 0 {
		return nil, ErrInvalidVP9Data
	}
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		return nil, err
	}
	var out []byte
	for _, packet := range packets {
		if err := d.Decode(packet); err != nil {
			return nil, err
		}
		if img, ok := d.NextFrame(); ok {
			out = appendVP9I420(out, img)
		}
	}
	return out, nil
}

func decodeVP9IVFExpectErrorForTest(ivf []byte) error {
	d, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		return err
	}
	if !testutil.VP9IVFHeaderLooksValid(ivf) {
		return testutil.ErrInvalidIVF
	}
	offset := testutil.IVFFileHeaderSize
	var firstErr error
	for inputIndex := 0; offset < len(ivf); inputIndex++ {
		frame, next, err := testutil.NextIVFFrame(ivf, offset, inputIndex)
		if err != nil {
			return err
		}
		if err := d.Decode(frame.Data); err != nil {
			firstErr = err
			break
		}
		offset = next
	}
	return firstErr
}
