package govpx

import (
	vp8codec "github.com/thesyncim/govpx/internal/vp8/codec"
)

// VP8 decoder and encoder public types (zero-overhead aliases).
type (
	DecoderOptions             = vp8codec.DecoderOptions
	VP8Decoder                 = vp8codec.VP8Decoder
	EncoderOptions             = vp8codec.EncoderOptions
	EncodeResult               = vp8codec.EncodeResult
	VP8Encoder                 = vp8codec.VP8Encoder
	VP8RTPPayloadDescriptor    = vp8codec.VP8RTPPayloadDescriptor
	VP8SDPReceiverCapabilities = vp8codec.VP8SDPReceiverCapabilities
)

// VP8 RTP and SDP constants.
const (
	VP8RTPMediaType          = vp8codec.VP8RTPMediaType
	VP8RTPEncodingName       = vp8codec.VP8RTPEncodingName
	VP8RTPClockRate          = vp8codec.VP8RTPClockRate
	VP8SDPFmtpMaxFrameRate   = vp8codec.VP8SDPFmtpMaxFrameRate
	VP8SDPFmtpMaxFrameSize   = vp8codec.VP8SDPFmtpMaxFrameSize
	VP8RTPPictureID7BitMask  = vp8codec.VP8RTPPictureID7BitMask
	VP8RTPPictureID15BitMask = vp8codec.VP8RTPPictureID15BitMask
	VP8RTPKeyIndexMask       = vp8codec.VP8RTPKeyIndexMask
)

// NewVP8Decoder creates a new VP8 decoder with the given options.
func NewVP8Decoder(opts DecoderOptions) (*VP8Decoder, error) {
	return vp8codec.NewVP8Decoder(opts)
}

// NewVP8Encoder creates a new VP8 encoder with the given options.
func NewVP8Encoder(opts EncoderOptions) (*VP8Encoder, error) {
	return vp8codec.NewVP8Encoder(opts)
}

// VP8 RTP packetization / depacketization helpers.
func ParseVP8RTPPayloadDescriptor(packet []byte) (VP8RTPPayloadDescriptor, []byte, error) {
	return vp8codec.ParseVP8RTPPayloadDescriptor(packet)
}

func VP8RTPPayloadSize(desc VP8RTPPayloadDescriptor, payload []byte) (int, error) {
	return vp8codec.VP8RTPPayloadSize(desc, payload)
}

func PackVP8RTPPayloadInto(dst []byte, desc VP8RTPPayloadDescriptor, payload []byte) (int, error) {
	return vp8codec.PackVP8RTPPayloadInto(dst, desc, payload)
}

func PackVP8RTPPayload(desc VP8RTPPayloadDescriptor, payload []byte) ([]byte, error) {
	return vp8codec.PackVP8RTPPayload(desc, payload)
}

func VP8RTPFramePacketizationSize(desc VP8RTPPayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	return vp8codec.VP8RTPFramePacketizationSize(desc, frame, mtu)
}

func PacketizeVP8RTPFrameInto(dst []RTPPayloadFragment, payloadBuf []byte, desc VP8RTPPayloadDescriptor, frame []byte, mtu int) (int, int, error) {
	return vp8codec.PacketizeVP8RTPFrameInto(dst, payloadBuf, desc, frame, mtu)
}

func PacketizeVP8RTPFrame(desc VP8RTPPayloadDescriptor, frame []byte, mtu int) ([]RTPPayloadFragment, error) {
	return vp8codec.PacketizeVP8RTPFrame(desc, frame, mtu)
}

func VP8RTPFrameAssemblySize(payloads []RTPPayloadFragment) (int, error) {
	return vp8codec.VP8RTPFrameAssemblySize(payloads)
}

func AssembleVP8RTPFrameInto(dst []byte, payloads []RTPPayloadFragment) (int, error) {
	return vp8codec.AssembleVP8RTPFrameInto(dst, payloads)
}

func AssembleVP8RTPFrame(payloads []RTPPayloadFragment) ([]byte, error) {
	return vp8codec.AssembleVP8RTPFrame(payloads)
}

// VP8 SDP helpers.
func VP8SDPFrameSizeMacroblocks(width int, height int) (int, error) {
	return vp8codec.VP8SDPFrameSizeMacroblocks(width, height)
}

func ParseVP8SDPFmtp(fmtp string) (VP8SDPReceiverCapabilities, error) {
	return vp8codec.ParseVP8SDPFmtp(fmtp)
}

func VP8SDPNegotiates(sdp string) bool {
	return vp8codec.VP8SDPNegotiates(sdp)
}

func VP8SDPOffersReceive(sdp string) bool {
	return vp8codec.VP8SDPOffersReceive(sdp)
}

func VP8SDPOffersReceiveFrame(sdp string, width int, height int, fps int) bool {
	return vp8codec.VP8SDPOffersReceiveFrame(sdp, width, height, fps)
}

func VP8SDPAnswersSend(sdp string) bool {
	return vp8codec.VP8SDPAnswersSend(sdp)
}

// VP8 WebRTC RTP header helpers.
func NextVP8RTPPictureID(id uint16) uint16 {
	return vp8codec.NextVP8RTPPictureID(id)
}

func NextVP8RTPPictureID7Bit(id uint8) uint8 {
	return vp8codec.NextVP8RTPPictureID7Bit(id)
}

func NextVP8RTPPictureID15Bit(id uint16) uint16 {
	return vp8codec.NextVP8RTPPictureID15Bit(id)
}

func NextVP8RTPTL0PICIDX(id uint8) uint8 {
	return vp8codec.NextVP8RTPTL0PICIDX(id)
}

func NextVP8RTPKeyIndex(index uint8) uint8 {
	return vp8codec.NextVP8RTPKeyIndex(index)
}
