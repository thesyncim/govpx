package govpx

import (
	"strconv"
	"strings"
)

const (
	// VP8RTPMediaType is the RTP media type registered by RFC 7741.
	VP8RTPMediaType = "video/VP8"
	// VP8RTPEncodingName is the SDP rtpmap encoding name for VP8.
	VP8RTPEncodingName = "VP8"
	// VP8RTPClockRate is the fixed RTP clock rate for VP8 video.
	VP8RTPClockRate = 90000

	// VP8SDPFmtpMaxFrameRate is the RFC 7741 fmtp key for max-fr.
	VP8SDPFmtpMaxFrameRate = "max-fr"
	// VP8SDPFmtpMaxFrameSize is the RFC 7741 fmtp key for max-fs.
	VP8SDPFmtpMaxFrameSize = "max-fs"
)

// VP8SDPReceiverCapabilities holds the two VP8 SDP fmtp receiver-capability
// parameters from RFC 7741. Both parameters are required when SDP declares VP8
// receive capability.
type VP8SDPReceiverCapabilities struct {
	// MaxFrameRate is max-fr: maximum decodable frame rate in frames per
	// second.
	MaxFrameRate int
	// MaxFrameSizeMacroblocks is max-fs: maximum decodable frame size in
	// 16x16 macroblocks.
	MaxFrameSizeMacroblocks int
}

// VP8SDPFrameSizeMacroblocks returns the max-fs value needed to advertise a
// VP8 frame of width x height pixels.
func VP8SDPFrameSizeMacroblocks(width int, height int) (int, error) {
	if width <= 0 || height <= 0 || width > maxVP8Dimension || height > maxVP8Dimension {
		return 0, ErrInvalidConfig
	}
	mbWidth := (width + 15) / 16
	mbHeight := (height + 15) / 16
	return mbWidth * mbHeight, nil
}

// Validate rejects incomplete or nonsensical VP8 SDP receiver capabilities.
func (c VP8SDPReceiverCapabilities) Validate() error {
	if c.MaxFrameRate <= 0 || c.MaxFrameSizeMacroblocks <= 0 {
		return ErrInvalidConfig
	}
	return nil
}

// AppendFmtp appends a semicolon-separated fmtp parameter string in RFC 7741
// order: max-fr first, then max-fs.
func (c VP8SDPReceiverCapabilities) AppendFmtp(dst []byte) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return dst, err
	}
	dst = append(dst, VP8SDPFmtpMaxFrameRate...)
	dst = append(dst, '=')
	dst = strconv.AppendInt(dst, int64(c.MaxFrameRate), 10)
	dst = append(dst, ';', ' ')
	dst = append(dst, VP8SDPFmtpMaxFrameSize...)
	dst = append(dst, '=')
	dst = strconv.AppendInt(dst, int64(c.MaxFrameSizeMacroblocks), 10)
	return dst, nil
}

// Fmtp returns a semicolon-separated VP8 fmtp parameter string.
func (c VP8SDPReceiverCapabilities) Fmtp() (string, error) {
	buf, err := c.AppendFmtp(nil)
	if err != nil {
		return "", err
	}
	return string(buf), nil
}

// ParseVP8SDPFmtp parses RFC 7741 VP8 fmtp parameters. Unknown parameters are
// ignored so callers can pass complete fmtp attribute values from peers.
func ParseVP8SDPFmtp(fmtp string) (VP8SDPReceiverCapabilities, error) {
	var out VP8SDPReceiverCapabilities
	var sawMaxFR bool
	var sawMaxFS bool
	for _, part := range strings.Split(fmtp, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return VP8SDPReceiverCapabilities{}, ErrInvalidConfig
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case VP8SDPFmtpMaxFrameRate:
			if sawMaxFR {
				return VP8SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return VP8SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			out.MaxFrameRate = n
			sawMaxFR = true
		case VP8SDPFmtpMaxFrameSize:
			if sawMaxFS {
				return VP8SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return VP8SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			out.MaxFrameSizeMacroblocks = n
			sawMaxFS = true
		}
	}
	if !sawMaxFR || !sawMaxFS {
		return VP8SDPReceiverCapabilities{}, ErrInvalidConfig
	}
	return out, nil
}
