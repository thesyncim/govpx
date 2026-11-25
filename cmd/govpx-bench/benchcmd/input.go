package benchcmd

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	govpx "github.com/thesyncim/govpx"
)

// resolveFrameSource returns the synthetic generator matching cfg.Content.
func resolveFrameSource(content string) (func(int, int, int) govpx.Image, error) {
	switch strings.ToLower(strings.TrimSpace(content)) {
	case "", "ramp":
		return makeBenchmarkFrame, nil
	case "panning":
		return makePanningFrame, nil
	case "checker":
		return makeCheckerFrame, nil
	default:
		return nil, fmt.Errorf("unsupported -content %q (want ramp, panning, or checker)", content)
	}
}

// prepareEncodeFrames loads frames from cfg.InputPath (.y4m) when set,
// otherwise generates cfg.Frames synthetic frames in parallel using source.
// When loading a Y4M file, cfg.Width, cfg.Height, cfg.Frames, and cfg.FPS are
// updated to match the loaded frames.
func prepareEncodeFrames(cfg *benchConfig, source func(int, int, int) govpx.Image) ([]govpx.Image, error) {
	if len(cfg.preloadedFrames) > 0 {
		return cfg.preloadedFrames, nil
	}
	if cfg.InputPath != "" {
		frames, fps, err := loadY4MFrames(cfg.InputPath, cfg.Frames)
		if err != nil {
			return nil, err
		}
		cfg.Width = frames[0].Width
		cfg.Height = frames[0].Height
		cfg.Frames = len(frames)
		if fps > 0 {
			cfg.FPS = fps
		}
		return frames, nil
	}
	if source == nil {
		var err error
		source, err = resolveFrameSource(cfg.Content)
		if err != nil {
			return nil, err
		}
	}
	return generateFramesParallel(cfg.Width, cfg.Height, cfg.Frames, source), nil
}

func generateFramesParallel(width, height, count int, source func(int, int, int) govpx.Image) []govpx.Image {
	frames := make([]govpx.Image, count)
	if count <= 1 || width*height < 640*360 {
		for i := range count {
			frames[i] = source(width, height, i)
		}
		return frames
	}
	workers := min(runtime.GOMAXPROCS(0), count)
	if workers <= 1 {
		for i := range count {
			frames[i] = source(width, height, i)
		}
		return frames
	}
	var wg sync.WaitGroup
	wg.Add(workers)
	for w := range workers {
		go func(worker int) {
			defer wg.Done()
			for i := worker; i < count; i += workers {
				frames[i] = source(width, height, i)
			}
		}(w)
	}
	wg.Wait()
	return frames
}

// loadY4MFrames reads up to maxFrames 8-bit 4:2:0 frames from a YUV4MPEG2 file.
func loadY4MFrames(path string, maxFrames int) ([]govpx.Image, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, fmt.Errorf("open y4m %q: %w", path, err)
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, 256*1024)
	headerLine, err := r.ReadString('\n')
	if err != nil {
		return nil, 0, fmt.Errorf("read y4m header %q: %w", path, err)
	}
	headerLine = strings.TrimRight(headerLine, "\r\n")
	if !strings.HasPrefix(headerLine, "YUV4MPEG2 ") {
		return nil, 0, fmt.Errorf("file %q is not a YUV4MPEG2 (.y4m) stream", path)
	}
	width := 0
	height := 0
	fps := 0
	chroma := "420"
	for field := range strings.FieldsSeq(headerLine[len("YUV4MPEG2 "):]) {
		switch {
		case strings.HasPrefix(field, "W"):
			width, _ = strconv.Atoi(field[1:])
		case strings.HasPrefix(field, "H"):
			height, _ = strconv.Atoi(field[1:])
		case strings.HasPrefix(field, "F"):
			numStr, denStr, ok := strings.Cut(field[1:], ":")
			if ok {
				num, err1 := strconv.Atoi(numStr)
				den, err2 := strconv.Atoi(denStr)
				if err1 == nil && err2 == nil && num > 0 && den > 0 {
					fps = (num + den/2) / den
				}
			}
		case strings.HasPrefix(field, "C"):
			chroma = field[1:]
		}
	}
	if width <= 0 || height <= 0 {
		return nil, 0, fmt.Errorf("y4m %q missing valid W/H header fields", path)
	}
	if !strings.HasPrefix(chroma, "420") || strings.Contains(chroma, "p10") || strings.Contains(chroma, "p12") {
		return nil, 0, fmt.Errorf("y4m %q chroma %q unsupported (need 8-bit 4:2:0)", path, chroma)
	}

	uvWidth := (width + 1) >> 1
	uvHeight := (height + 1) >> 1
	ySize := width * height
	uvSize := uvWidth * uvHeight
	var frames []govpx.Image
	for maxFrames <= 0 || len(frames) < maxFrames {
		line, err := r.ReadString('\n')
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, 0, fmt.Errorf("read y4m frame header %d: %w", len(frames), err)
		}
		line = strings.TrimRight(line, "\r\n")
		if !strings.HasPrefix(line, "FRAME") {
			return nil, 0, fmt.Errorf("y4m %q frame %d: expected FRAME marker, got %q", path, len(frames), line)
		}
		yPlane := make([]byte, ySize)
		uPlane := make([]byte, uvSize)
		vPlane := make([]byte, uvSize)
		if _, err := io.ReadFull(r, yPlane); err != nil {
			return nil, 0, fmt.Errorf("read y4m frame %d Y plane: %w", len(frames), err)
		}
		if _, err := io.ReadFull(r, uPlane); err != nil {
			return nil, 0, fmt.Errorf("read y4m frame %d U plane: %w", len(frames), err)
		}
		if _, err := io.ReadFull(r, vPlane); err != nil {
			return nil, 0, fmt.Errorf("read y4m frame %d V plane: %w", len(frames), err)
		}
		frames = append(frames, govpx.Image{
			Width:   width,
			Height:  height,
			Y:       yPlane,
			U:       uPlane,
			V:       vPlane,
			YStride: width,
			UStride: uvWidth,
			VStride: uvWidth,
		})
	}
	if len(frames) == 0 {
		return nil, 0, fmt.Errorf("y4m %q contained no frames", path)
	}
	return frames, fps, nil
}

type ivfInputStream struct {
	codec   string
	width   int
	height  int
	fps     int
	raw     []byte
	packets [][]byte
}

// loadIVFInput reads an IVF bitstream for -decode -input=<file.ivf>.
func loadIVFInput(path string, maxFrames int) (ivfInputStream, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ivfInputStream{}, fmt.Errorf("read ivf %q: %w", path, err)
	}
	const (
		fileHeaderSize  = 32
		frameHeaderSize = 12
	)
	if len(raw) < fileHeaderSize || string(raw[:4]) != "DKIF" {
		return ivfInputStream{}, fmt.Errorf("file %q is not a valid IVF stream", path)
	}
	fourcc := string(raw[8:12])
	codec := ""
	switch fourcc {
	case "VP80":
		codec = codecVP8
	case "VP90":
		codec = codecVP9
	default:
		return ivfInputStream{}, fmt.Errorf("ivf %q unsupported FourCC %q (want VP80 or VP90)", path, fourcc)
	}
	width := int(binary.LittleEndian.Uint16(raw[12:14]))
	height := int(binary.LittleEndian.Uint16(raw[14:16]))
	rate := binary.LittleEndian.Uint32(raw[16:20])
	scale := binary.LittleEndian.Uint32(raw[20:24])
	fps := 30
	if rate > 0 && scale > 0 {
		fps = max(1, int((rate+scale/2)/scale))
	}
	offset := fileHeaderSize
	var packets [][]byte
	for offset < len(raw) && (maxFrames <= 0 || len(packets) < maxFrames) {
		if offset+frameHeaderSize > len(raw) {
			return ivfInputStream{}, fmt.Errorf("ivf %q truncated frame header", path)
		}
		size := int(binary.LittleEndian.Uint32(raw[offset:]))
		offset += frameHeaderSize
		if size < 0 || offset+size > len(raw) {
			return ivfInputStream{}, fmt.Errorf("ivf %q truncated frame payload", path)
		}
		packets = append(packets, raw[offset:offset+size])
		offset += size
	}
	if len(packets) == 0 {
		return ivfInputStream{}, fmt.Errorf("ivf %q contained no frames", path)
	}
	// If truncated by maxFrames, rebuild raw IVF so libvpx decodes the exact same frames.
	if offset < len(raw) {
		raw = makeBenchmarkIVFForCodec(codec, width, height, fps, packets)
	}
	return ivfInputStream{
		codec:   codec,
		width:   width,
		height:  height,
		fps:     fps,
		raw:     raw,
		packets: packets,
	}, nil
}

// writeTempI420File writes frames once to a temp file and returns its path plus
// a cleanup function so repeated runs can reuse the same on-disk input.
func writeTempI420File(frames []govpx.Image) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "govpx-bench-raw-*")
	if err != nil {
		return "", func() {}, err
	}
	rawPath := tempDir + string(os.PathSeparator) + "input.i420"
	raw, err := os.Create(rawPath)
	if err != nil {
		os.RemoveAll(tempDir)
		return "", func() {}, err
	}
	bw := bufio.NewWriterSize(raw, 512*1024)
	for _, frame := range frames {
		if err := writeI420FrameBuf(bw, frame); err != nil {
			raw.Close()
			os.RemoveAll(tempDir)
			return "", func() {}, err
		}
	}
	if err := bw.Flush(); err != nil {
		raw.Close()
		os.RemoveAll(tempDir)
		return "", func() {}, err
	}
	if err := raw.Close(); err != nil {
		os.RemoveAll(tempDir)
		return "", func() {}, err
	}
	return rawPath, func() { _ = os.RemoveAll(tempDir) }, nil
}

func writeI420FrameBuf(dst io.Writer, frame govpx.Image) error {
	for row := range frame.Height {
		if _, err := dst.Write(frame.Y[row*frame.YStride : row*frame.YStride+frame.Width]); err != nil {
			return err
		}
	}
	uvWidth := (frame.Width + 1) >> 1
	uvHeight := (frame.Height + 1) >> 1
	for row := range uvHeight {
		if _, err := dst.Write(frame.U[row*frame.UStride : row*frame.UStride+uvWidth]); err != nil {
			return err
		}
	}
	for row := range uvHeight {
		if _, err := dst.Write(frame.V[row*frame.VStride : row*frame.VStride+uvWidth]); err != nil {
			return err
		}
	}
	return nil
}
