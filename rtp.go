package govpx

import vpxrtp "github.com/thesyncim/govpx/internal/vpx/rtp"

// RTPPayloadFragment is one RTP payload body plus the RTP marker-bit value
// the caller should put in the RTP header for that body.
//
// Payload contains codec-specific payload-descriptor bytes followed by the
// codec payload fragment. It does not include an RTP header.
type RTPPayloadFragment = vpxrtp.PayloadFragment
