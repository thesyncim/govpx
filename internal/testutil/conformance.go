package testutil

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
)

var ErrInvalidOracleOutput = errString("libgopx: invalid oracle output")

type FrameChecksum struct {
	Index int

	Width  int
	Height int

	KeyFrame  bool
	ShowFrame bool

	MD5 PlaneMD5
}

func SameFrameChecksum(a FrameChecksum, b FrameChecksum) bool {
	return a.Index == b.Index &&
		a.Width == b.Width &&
		a.Height == b.Height &&
		a.KeyFrame == b.KeyFrame &&
		a.ShowFrame == b.ShowFrame &&
		a.MD5 == b.MD5
}

func ParseFrameChecksumJSONLines(data []byte) ([]FrameChecksum, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var checksums []FrameChecksum
	for {
		var frame frameChecksumJSON
		err := decoder.Decode(&frame)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, ErrInvalidOracleOutput
		}
		checksum, err := frame.toFrameChecksum()
		if err != nil {
			return nil, err
		}
		checksums = append(checksums, checksum)
	}
	return checksums, nil
}

type frameChecksumJSON struct {
	Frame     int    `json:"frame"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	KeyFrame  bool   `json:"keyframe"`
	ShowFrame bool   `json:"show_frame"`
	YMD5      string `json:"y_md5"`
	UMD5      string `json:"u_md5"`
	VMD5      string `json:"v_md5"`
	FullMD5   string `json:"full_md5"`
}

func (f frameChecksumJSON) toFrameChecksum() (FrameChecksum, error) {
	y, err := parseMD5Hex(f.YMD5)
	if err != nil {
		return FrameChecksum{}, err
	}
	u, err := parseMD5Hex(f.UMD5)
	if err != nil {
		return FrameChecksum{}, err
	}
	v, err := parseMD5Hex(f.VMD5)
	if err != nil {
		return FrameChecksum{}, err
	}
	full, err := parseMD5Hex(f.FullMD5)
	if err != nil {
		return FrameChecksum{}, err
	}
	return FrameChecksum{
		Index:     f.Frame,
		Width:     f.Width,
		Height:    f.Height,
		KeyFrame:  f.KeyFrame,
		ShowFrame: f.ShowFrame,
		MD5: PlaneMD5{
			Y:    y,
			U:    u,
			V:    v,
			Full: full,
		},
	}, nil
}

func parseMD5Hex(s string) ([16]byte, error) {
	var sum [16]byte
	if len(s) != 32 {
		return sum, ErrInvalidOracleOutput
	}
	decoded, err := hex.DecodeString(s)
	if err != nil || len(decoded) != len(sum) {
		return sum, ErrInvalidOracleOutput
	}
	copy(sum[:], decoded)
	return sum, nil
}
