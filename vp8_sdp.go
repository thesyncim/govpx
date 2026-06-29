package govpx

import (
	"math"
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

// AllowsFrame reports whether width x height at fps fits these VP8 SDP
// receiver capabilities.
func (c VP8SDPReceiverCapabilities) AllowsFrame(width int, height int, fps int) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	if fps <= 0 {
		return false, ErrInvalidConfig
	}
	frameSize, err := VP8SDPFrameSizeMacroblocks(width, height)
	if err != nil {
		return false, err
	}
	if fps > c.MaxFrameRate || frameSize > c.MaxFrameSizeMacroblocks {
		return false, nil
	}
	limit := int(math.Sqrt(float64(c.MaxFrameSizeMacroblocks) * 8))
	mbWidth := (width + 15) / 16
	mbHeight := (height + 15) / 16
	return mbWidth <= limit && mbHeight <= limit, nil
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

// VP8SDPNegotiates reports whether an SDP blob contains an active video
// section that binds a VP8/90000 payload type.
func VP8SDPNegotiates(sdp string) bool {
	return vp8SDPHasPayload(sdp, vp8SDPDirectionIsActive)
}

// VP8SDPOffersReceive reports whether an SDP offer contains a video section
// that can receive VP8.
func VP8SDPOffersReceive(sdp string) bool {
	return vp8SDPHasPayload(sdp, vp8SDPDirectionAllowsReceive)
}

// VP8SDPOffersReceiveFrame reports whether an SDP offer contains a video
// section that can receive VP8 and does not declare max-fr or max-fs
// receiver-capability limits below width x height at fps.
func VP8SDPOffersReceiveFrame(sdp string, width int, height int, fps int) bool {
	return vp8SDPHasPayloadMatching(sdp, vp8SDPDirectionAllowsReceive,
		func(section vp8SDPMediaSection, payloadType string) bool {
			params, ok := section.fmtpParams[payloadType]
			if !ok || !vp8SDPFmtpDeclaresReceiverCaps(params) {
				return true
			}
			caps, err := ParseVP8SDPFmtp(params)
			if err != nil {
				return false
			}
			allowed, err := caps.AllowsFrame(width, height, fps)
			return err == nil && allowed
		})
}

// VP8SDPAnswersSend reports whether an SDP answer contains a video section
// that can send VP8.
func VP8SDPAnswersSend(sdp string) bool {
	return vp8SDPHasPayload(sdp, vp8SDPDirectionAllowsSend)
}

func vp8SDPFmtpDeclaresReceiverCaps(fmtp string) bool {
	for _, part := range strings.Split(fmtp, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, _, _ := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		if key == VP8SDPFmtpMaxFrameRate || key == VP8SDPFmtpMaxFrameSize {
			return true
		}
	}
	return false
}

func vp8SDPHasPayload(sdp string, directionOK func(string) bool) bool {
	return vp8SDPHasPayloadMatching(sdp, directionOK, nil)
}

func vp8SDPHasPayloadMatching(
	sdp string,
	directionOK func(string) bool,
	payloadOK func(vp8SDPMediaSection, string) bool,
) bool {
	sessionDirection := "sendrecv"
	section := vp8SDPMediaSection{direction: sessionDirection}
	haveSection := false
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(strings.ToLower(raw))
		if strings.HasPrefix(line, "m=") {
			if haveSection && section.hasVP8(directionOK, payloadOK) {
				return true
			}
			media, active, payloadTypes := vp8SDPMediaPayloadTypes(line)
			section = vp8SDPMediaSection{
				media:           media,
				portActive:      active,
				payloadTypes:    payloadTypes,
				direction:       sessionDirection,
				vp8PayloadTypes: make(map[string]bool),
				fmtpParams:      make(map[string]string),
			}
			haveSection = true
			continue
		}
		if direction, ok := vp8SDPDirection(line); ok {
			if haveSection {
				section.direction = direction
			} else {
				sessionDirection = direction
			}
			continue
		}
		if !haveSection || !section.parsesVideoPayloadAttributes() {
			continue
		}
		switch {
		case strings.HasPrefix(line, "a=rtpmap:"):
			fields := strings.Fields(strings.TrimPrefix(line, "a=rtpmap:"))
			if len(fields) >= 2 && fields[1] == "vp8/90000" &&
				section.payloadTypes[fields[0]] {
				section.vp8PayloadTypes[fields[0]] = true
			}
		case strings.HasPrefix(line, "a=fmtp:"):
			fields := strings.Fields(strings.TrimPrefix(line, "a=fmtp:"))
			if len(fields) >= 2 && section.payloadTypes[fields[0]] {
				section.fmtpParams[fields[0]] = strings.Join(fields[1:], " ")
			}
		}
	}
	return haveSection && section.hasVP8(directionOK, payloadOK)
}

type vp8SDPMediaSection struct {
	media           string
	portActive      bool
	payloadTypes    map[string]bool
	direction       string
	vp8PayloadTypes map[string]bool
	fmtpParams      map[string]string
}

func (s vp8SDPMediaSection) parsesVideoPayloadAttributes() bool {
	return s.media == "video" && s.portActive
}

func (s vp8SDPMediaSection) hasVP8(
	directionOK func(string) bool,
	payloadOK func(vp8SDPMediaSection, string) bool,
) bool {
	if !s.parsesVideoPayloadAttributes() || !directionOK(s.direction) {
		return false
	}
	for payloadType := range s.vp8PayloadTypes {
		if payloadOK == nil || payloadOK(s, payloadType) {
			return true
		}
	}
	return false
}

func vp8SDPMediaPayloadTypes(line string) (string, bool, map[string]bool) {
	fields := strings.Fields(strings.TrimPrefix(line, "m="))
	if len(fields) < 4 {
		return "", false, nil
	}
	payloadTypes := make(map[string]bool, len(fields)-3)
	for _, payloadType := range fields[3:] {
		payloadTypes[payloadType] = true
	}
	return fields[0], !vp8SDPMediaPortIsZero(fields[1]), payloadTypes
}

func vp8SDPMediaPortIsZero(port string) bool {
	first, _, _ := strings.Cut(port, "/")
	first = strings.TrimLeft(first, "0")
	return first == ""
}

func vp8SDPDirection(line string) (string, bool) {
	switch line {
	case "a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive":
		return strings.TrimPrefix(line, "a="), true
	default:
		return "", false
	}
}

func vp8SDPDirectionIsActive(direction string) bool {
	return direction != "inactive"
}

func vp8SDPDirectionAllowsReceive(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "recvonly"
}

func vp8SDPDirectionAllowsSend(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "sendonly"
}
