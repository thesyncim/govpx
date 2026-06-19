package govpx

import (
	"math"
	"strconv"
	"strings"
)

const (
	// VP9RTPMediaType is the RTP media type used by WebRTC VP9 senders.
	VP9RTPMediaType = "video/VP9"
	// VP9RTPEncodingName is the SDP rtpmap encoding name for VP9.
	VP9RTPEncodingName = "VP9"
	// VP9RTPClockRate is the fixed RTP clock rate for VP9 video.
	VP9RTPClockRate = 90000

	// VP9SDPFmtpProfileID is the VP9 SDP fmtp key for bitstream profile.
	VP9SDPFmtpProfileID = "profile-id"
	// VP9SDPFmtpMaxFrameRate is the RFC 9628 fmtp key for max-fr.
	VP9SDPFmtpMaxFrameRate = "max-fr"
	// VP9SDPFmtpMaxFrameSize is the RFC 9628 fmtp key for max-fs.
	VP9SDPFmtpMaxFrameSize = "max-fs"
	// VP9SDPFmtpProfile0 is the fmtp parameter for VP9 Profile 0.
	VP9SDPFmtpProfile0 = "profile-id=0"
)

// VP9SDPReceiverCapabilities holds VP9 SDP fmtp receiver-capability
// parameters from RFC 9628. Zero means the corresponding optional parameter
// was not declared by the peer.
type VP9SDPReceiverCapabilities struct {
	// MaxFrameRate is max-fr: maximum decodable frame rate in frames per
	// second. Zero means the parameter was not declared.
	MaxFrameRate int
	// MaxFrameSizeMacroblocks is max-fs: maximum decodable frame size in
	// 16x16 macroblocks. Zero means the parameter was not declared.
	MaxFrameSizeMacroblocks int
}

// AppendVP9SDPFmtpProfile0 appends the fmtp parameter for VP9 Profile 0.
func AppendVP9SDPFmtpProfile0(dst []byte) []byte {
	return append(dst, VP9SDPFmtpProfile0...)
}

// VP9SDPFrameSizeMacroblocks returns the max-fs value needed to advertise a
// VP9 frame of width x height pixels.
func VP9SDPFrameSizeMacroblocks(width int, height int) (int, error) {
	if !validVP9Dimension(width) || !validVP9Dimension(height) {
		return 0, ErrInvalidConfig
	}
	mbWidth := (width + 15) / 16
	mbHeight := (height + 15) / 16
	return mbWidth * mbHeight, nil
}

// Validate rejects nonsensical VP9 SDP receiver capabilities. Zero means the
// optional parameter was not declared; negative values are invalid.
func (c VP9SDPReceiverCapabilities) Validate() error {
	if c.MaxFrameRate < 0 || c.MaxFrameSizeMacroblocks < 0 {
		return ErrInvalidConfig
	}
	return nil
}

// AllowsFrame reports whether width x height at fps fits these VP9 SDP
// receiver capabilities. Undeclared max-fr or max-fs values are treated as
// unconstrained so common browser SDP that omits them remains acceptable.
func (c VP9SDPReceiverCapabilities) AllowsFrame(width int, height int, fps int) (bool, error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	if fps <= 0 {
		return false, ErrInvalidConfig
	}
	frameSize, err := VP9SDPFrameSizeMacroblocks(width, height)
	if err != nil {
		return false, err
	}
	if c.MaxFrameRate > 0 && fps > c.MaxFrameRate {
		return false, nil
	}
	if c.MaxFrameSizeMacroblocks <= 0 {
		return true, nil
	}
	if frameSize > c.MaxFrameSizeMacroblocks {
		return false, nil
	}
	limit := int(math.Sqrt(float64(c.MaxFrameSizeMacroblocks) * 8))
	mbWidth := (width + 15) / 16
	mbHeight := (height + 15) / 16
	return mbWidth <= limit && mbHeight <= limit, nil
}

// ParseVP9SDPFmtp parses RFC 9628 VP9 fmtp receiver-capability parameters.
// Unknown parameters are ignored so callers can pass complete fmtp attribute
// values from peers. The profile-id parameter is intentionally ignored here;
// use [VP9SDPFmtpContainsProfile0] for profile matching.
func ParseVP9SDPFmtp(fmtp string) (VP9SDPReceiverCapabilities, error) {
	var out VP9SDPReceiverCapabilities
	var sawMaxFR bool
	var sawMaxFS bool
	for _, part := range strings.Split(fmtp, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return VP9SDPReceiverCapabilities{}, ErrInvalidConfig
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		switch key {
		case VP9SDPFmtpMaxFrameRate:
			if sawMaxFR {
				return VP9SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return VP9SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			out.MaxFrameRate = n
			sawMaxFR = true
		case VP9SDPFmtpMaxFrameSize:
			if sawMaxFS {
				return VP9SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			n, err := strconv.Atoi(value)
			if err != nil || n <= 0 {
				return VP9SDPReceiverCapabilities{}, ErrInvalidConfig
			}
			out.MaxFrameSizeMacroblocks = n
			sawMaxFS = true
		}
	}
	return out, nil
}

// VP9SDPFmtpContainsProfile0 reports whether fmtp parameters include
// profile-id=0, or omit profile-id entirely, which RFC 9628 defines as
// Profile 0. Unknown parameters are ignored so callers can pass complete fmtp
// attribute values from peers.
func VP9SDPFmtpContainsProfile0(params string) bool {
	sawProfileID := false
	for _, rawParam := range strings.Split(params, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(rawParam), "=")
		if !ok {
			continue
		}
		if strings.ToLower(strings.TrimSpace(key)) == VP9SDPFmtpProfileID {
			sawProfileID = true
			if strings.TrimSpace(value) == "0" {
				return true
			}
		}
	}
	return !sawProfileID
}

// VP9SDPNegotiatesProfile0 reports whether an SDP blob contains an active
// video section that binds a VP9/90000 payload type to profile-id=0.
func VP9SDPNegotiatesProfile0(sdp string) bool {
	return vp9SDPHasProfile0(sdp, vp9SDPDirectionIsActive)
}

// VP9SDPOffersProfile0Receive reports whether an SDP offer contains a video
// section that can receive VP9 Profile 0.
func VP9SDPOffersProfile0Receive(sdp string) bool {
	return vp9SDPHasProfile0(sdp, vp9SDPDirectionAllowsReceive)
}

// VP9SDPOffersProfile0ReceiveFrame reports whether an SDP offer contains a
// video section that can receive VP9 Profile 0 and does not declare max-fr or
// max-fs receiver-capability limits below width x height at fps.
func VP9SDPOffersProfile0ReceiveFrame(
	sdp string,
	width int,
	height int,
	fps int,
) bool {
	return vp9SDPHasProfile0Frame(sdp, vp9SDPDirectionAllowsReceive,
		width, height, fps)
}

// VP9SDPAnswersProfile0Send reports whether an SDP answer contains a video
// section that can send VP9 Profile 0.
func VP9SDPAnswersProfile0Send(sdp string) bool {
	return vp9SDPHasProfile0(sdp, vp9SDPDirectionAllowsSend)
}

func vp9SDPHasProfile0(sdp string, directionOK func(string) bool) bool {
	return vp9SDPHasProfile0Matching(sdp, directionOK, nil)
}

func vp9SDPHasProfile0Frame(
	sdp string,
	directionOK func(string) bool,
	width int,
	height int,
	fps int,
) bool {
	return vp9SDPHasProfile0Matching(sdp, directionOK,
		func(section vp9SDPMediaSection, payloadType string) bool {
			params, ok := section.fmtpParams[payloadType]
			if !ok {
				return true
			}
			caps, err := ParseVP9SDPFmtp(params)
			if err != nil {
				return false
			}
			allowed, err := caps.AllowsFrame(width, height, fps)
			return err == nil && allowed
		})
}

func vp9SDPHasProfile0Matching(
	sdp string,
	directionOK func(string) bool,
	payloadOK func(vp9SDPMediaSection, string) bool,
) bool {
	sessionDirection := "sendrecv"
	section := vp9SDPMediaSection{direction: sessionDirection}
	haveSection := false
	for _, raw := range strings.Split(sdp, "\n") {
		line := strings.TrimSpace(strings.ToLower(raw))
		if strings.HasPrefix(line, "m=") {
			if haveSection && section.hasVP9Profile0(directionOK, payloadOK) {
				return true
			}
			media, active, payloadTypes := vp9SDPMediaPayloadTypes(line)
			section = vp9SDPMediaSection{
				media:                media,
				portActive:           active,
				payloadTypes:         payloadTypes,
				direction:            sessionDirection,
				vp9PayloadTypes:      make(map[string]bool),
				profile0PayloadTypes: make(map[string]bool),
				fmtpParams:           make(map[string]string),
			}
			haveSection = true
			continue
		}
		if direction, ok := vp9SDPDirection(line); ok {
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
			if len(fields) >= 2 && fields[1] == "vp9/90000" &&
				section.payloadTypes[fields[0]] {
				section.vp9PayloadTypes[fields[0]] = true
			}
		case strings.HasPrefix(line, "a=fmtp:"):
			fields := strings.Fields(strings.TrimPrefix(line, "a=fmtp:"))
			if len(fields) >= 2 && section.payloadTypes[fields[0]] {
				params := strings.Join(fields[1:], " ")
				section.fmtpParams[fields[0]] = params
				if VP9SDPFmtpContainsProfile0(params) {
					section.profile0PayloadTypes[fields[0]] = true
				}
			}
		}
	}
	return haveSection && section.hasVP9Profile0(directionOK, payloadOK)
}

type vp9SDPMediaSection struct {
	media                string
	portActive           bool
	payloadTypes         map[string]bool
	direction            string
	vp9PayloadTypes      map[string]bool
	profile0PayloadTypes map[string]bool
	fmtpParams           map[string]string
}

func (s vp9SDPMediaSection) parsesVideoPayloadAttributes() bool {
	return s.media == "video" && s.portActive
}

func (s vp9SDPMediaSection) hasVP9Profile0(
	directionOK func(string) bool,
	payloadOK func(vp9SDPMediaSection, string) bool,
) bool {
	if !s.parsesVideoPayloadAttributes() || !directionOK(s.direction) {
		return false
	}
	for payloadType := range s.vp9PayloadTypes {
		_, hasFmtp := s.fmtpParams[payloadType]
		profile0 := !hasFmtp || s.profile0PayloadTypes[payloadType]
		if profile0 && (payloadOK == nil || payloadOK(s, payloadType)) {
			return true
		}
	}
	return false
}

func vp9SDPMediaPayloadTypes(line string) (string, bool, map[string]bool) {
	fields := strings.Fields(strings.TrimPrefix(line, "m="))
	if len(fields) < 4 {
		return "", false, nil
	}
	payloadTypes := make(map[string]bool, len(fields)-3)
	for _, payloadType := range fields[3:] {
		payloadTypes[payloadType] = true
	}
	return fields[0], !vp9SDPMediaPortIsZero(fields[1]), payloadTypes
}

func vp9SDPMediaPortIsZero(port string) bool {
	first, _, _ := strings.Cut(port, "/")
	first = strings.TrimLeft(first, "0")
	return first == ""
}

func vp9SDPDirection(line string) (string, bool) {
	switch line {
	case "a=sendrecv", "a=sendonly", "a=recvonly", "a=inactive":
		return strings.TrimPrefix(line, "a="), true
	default:
		return "", false
	}
}

func vp9SDPDirectionIsActive(direction string) bool {
	return direction != "inactive"
}

func vp9SDPDirectionAllowsReceive(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "recvonly"
}

func vp9SDPDirectionAllowsSend(direction string) bool {
	return direction == "" || direction == "sendrecv" || direction == "sendonly"
}
