package coracle

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/thesyncim/govpx/internal/testutil"
)

const (
	DefaultVP9ExternalTestDataDir       = "internal/coracle/build/test-data/vp9"
	DefaultVP9IVFTestDataMinimum        = 7
	DefaultVP9InvalidIVFTestDataMinimum = 17
	DefaultVP9Profile0WebMTestMinimum   = 101
	DefaultVP9ProfileWebMTestMinimum    = 11
)

var defaultVP9IVFTestNames = map[string]struct{}{
	"vp90-2-05-resize.ivf":         {},
	"vp90-2-09-subpixel-00.ivf":    {},
	"vp90-2-12-droppable_1.ivf":    {},
	"vp90-2-12-droppable_2.ivf":    {},
	"vp90-2-12-droppable_3.ivf":    {},
	"vp90-2-18-resize.ivf":         {},
	"vp90-2-22-svc_1280x720_3.ivf": {},
}

var defaultVP9Profile0WebMTestNames = map[string]struct{}{
	"vp90-2-01-sharpness-1.webm":                  {},
	"vp90-2-01-sharpness-2.webm":                  {},
	"vp90-2-01-sharpness-3.webm":                  {},
	"vp90-2-01-sharpness-4.webm":                  {},
	"vp90-2-01-sharpness-5.webm":                  {},
	"vp90-2-01-sharpness-6.webm":                  {},
	"vp90-2-01-sharpness-7.webm":                  {},
	"vp90-2-02-size-08x08.webm":                   {},
	"vp90-2-02-size-08x10.webm":                   {},
	"vp90-2-02-size-10x08.webm":                   {},
	"vp90-2-02-size-16x16.webm":                   {},
	"vp90-2-02-size-16x18.webm":                   {},
	"vp90-2-02-size-18x16.webm":                   {},
	"vp90-2-02-size-32x32.webm":                   {},
	"vp90-2-02-size-32x34.webm":                   {},
	"vp90-2-02-size-34x32.webm":                   {},
	"vp90-2-02-size-64x64.webm":                   {},
	"vp90-2-02-size-64x66.webm":                   {},
	"vp90-2-02-size-66x64.webm":                   {},
	"vp90-2-02-size-130x132.webm":                 {},
	"vp90-2-02-size-132x130.webm":                 {},
	"vp90-2-02-size-180x180.webm":                 {},
	"vp90-2-03-deltaq.webm":                       {},
	"vp90-2-06-bilinear.webm":                     {},
	"vp90-2-07-frame_parallel.webm":               {},
	"vp90-2-08-tile_1x4.webm":                     {},
	"vp90-2-08-tile_1x8.webm":                     {},
	"vp90-2-08-tile_1x2_frame_parallel.webm":      {},
	"vp90-2-09-aq2.webm":                          {},
	"vp90-2-09-lf_deltas.webm":                    {},
	"vp90-2-10-show-existing-frame.webm":          {},
	"vp90-2-11-size-351x287.webm":                 {},
	"vp90-2-14-resize-10frames-fp-tiles-1-2.webm": {},
	"vp90-2-14-resize-10frames-fp-tiles-1-4.webm": {},
	"vp90-2-15-segkey.webm":                       {},
	"vp90-2-16-intra-only.webm":                   {},
	"vp90-2-19-skip.webm":                         {},
}

func init() {
	for q := range 64 {
		defaultVP9Profile0WebMTestNames[fmt.Sprintf("vp90-2-00-quantizer-%02d.webm", q)] = struct{}{}
	}
}

func DefaultVP9IVFTestNameCount() int {
	return len(defaultVP9IVFTestNames)
}

func DefaultVP9Profile0WebMTestNameCount() int {
	return len(defaultVP9Profile0WebMTestNames)
}

func DefaultVP9TestDataExists() bool {
	info, err := os.Stat(DefaultVP9ExternalTestDataDir)
	return err == nil && info.IsDir()
}

func NonNegativeEnvInt(name string) (int, bool, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, true, fmt.Errorf("%s = %q, want a non-negative integer", name, raw)
	}
	return value, true, nil
}

func SafeCorpusTestName(root string, path string) string {
	name, err := filepath.Rel(root, path)
	if err != nil || name == "." {
		name = filepath.Base(path)
	}
	name = strings.TrimSuffix(name, filepath.Ext(name))
	name = strings.ReplaceAll(name, string(os.PathSeparator), "_")
	if name == "" {
		return "corpus"
	}
	return name
}

func FindVP8IVFTestData(root string, limit int, invalid bool) ([]string, error) {
	return findIVFTestData(root, limit, func(path string) (bool, error) {
		if IsInvalidIVFTestDataName(path) != invalid {
			return false, nil
		}
		return IsVP8IVFTestData(path)
	})
}

func FindVP9IVFTestData(root string, limit int, invalid bool) ([]string, error) {
	return findIVFTestData(root, limit, func(path string) (bool, error) {
		if IsInvalidIVFTestDataName(path) != invalid {
			return false, nil
		}
		if invalid {
			return true, nil
		}
		return IsVP9IVFTestData(path)
	})
}

func FindVP9Profile0WebMTestData(root string, limit int) ([]string, error) {
	return findCorpusFiles(root, limit, func(path string) (bool, error) {
		name := filepath.Base(path)
		if !strings.EqualFold(filepath.Ext(name), ".webm") {
			return false, nil
		}
		_, ok := defaultVP9Profile0WebMTestNames[name]
		return ok, nil
	})
}

func FindVP9ProfileWebMTestData(root string, limit int) ([]string, error) {
	return findCorpusFiles(root, limit, func(path string) (bool, error) {
		name := strings.ToLower(filepath.Base(path))
		return strings.EqualFold(filepath.Ext(path), ".webm") &&
			(strings.HasPrefix(name, "vp91-") ||
				strings.HasPrefix(name, "vp92-") ||
				strings.HasPrefix(name, "vp93-")), nil
	})
}

func findIVFTestData(root string, limit int, accept func(string) (bool, error)) ([]string, error) {
	return findCorpusFiles(root, limit, func(path string) (bool, error) {
		if !strings.EqualFold(filepath.Ext(path), ".ivf") {
			return false, nil
		}
		return accept(path)
	})
}

func findCorpusFiles(root string, limit int, accept func(string) (bool, error)) ([]string, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	var paths []string
	if info.Mode().IsRegular() {
		ok, err := accept(root)
		if err != nil {
			return nil, err
		}
		if ok {
			paths = append(paths, root)
		}
		return limitPaths(paths, limit), nil
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a regular file or directory", root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		ok, err := accept(path)
		if err != nil {
			return err
		}
		if ok {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", root, err)
	}
	sort.Strings(paths)
	return limitPaths(paths, limit), nil
}

func limitPaths(paths []string, limit int) []string {
	if limit > 0 && len(paths) > limit {
		return paths[:limit]
	}
	return paths
}

func IsInvalidIVFTestDataName(path string) bool {
	return strings.HasPrefix(strings.ToLower(filepath.Base(path)), "invalid-")
}

func IsVP8IVFTestData(path string) (bool, error) {
	header, err := readIVFHeader(path)
	if err != nil {
		return false, err
	}
	hdr, err := testutil.ParseIVFHeader(header)
	if err == nil {
		return hdr.FourCC == testutil.IVFFourCCVP8, nil
	}
	if errors.Is(err, testutil.ErrUnsupportedFourCC) {
		return false, nil
	}
	return false, fmt.Errorf("%s is not valid VP8 IVF data: %w", path, err)
}

func IsVP9IVFTestData(path string) (bool, error) {
	header, err := readIVFHeader(path)
	if err != nil {
		return false, err
	}
	return VP9IVFHeaderLooksValid(header), nil
}

func VP9IVFHeaderLooksValid(data []byte) bool {
	return len(data) >= testutil.IVFFileHeaderSize &&
		data[0] == 'D' && data[1] == 'K' && data[2] == 'I' && data[3] == 'F' &&
		data[6] == byte(testutil.IVFFileHeaderSize) && data[7] == 0 &&
		data[8] == 'V' && data[9] == 'P' && data[10] == '9' && data[11] == '0'
}

func readIVFHeader(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	header := make([]byte, testutil.IVFFileHeaderSize)
	if _, err := io.ReadFull(file, header); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s is not valid IVF data: %w", path, testutil.ErrInvalidIVF)
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return header, nil
}
