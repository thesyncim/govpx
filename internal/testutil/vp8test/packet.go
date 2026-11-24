package vp8test

import (
	vp8common "github.com/thesyncim/govpx/internal/vp8/common"
	vp8tables "github.com/thesyncim/govpx/internal/vp8/tables"
)

// KeyFramePacket builds a VP8 keyframe tag and keyframe header.
func KeyFramePacket(width int, height int, firstPartitionSize int,
	profile int, showFrame bool,
) []byte {
	packet := make([]byte, 10)
	tag := uint32(profile&7) << 1
	if showFrame {
		tag |= 1 << 4
	}
	tag |= uint32(firstPartitionSize) << 5
	packet[0] = byte(tag)
	packet[1] = byte(tag >> 8)
	packet[2] = byte(tag >> 16)
	packet[3] = 0x9d
	packet[4] = 0x01
	packet[5] = 0x2a
	packet[6] = byte(width)
	packet[7] = byte(width >> 8)
	packet[8] = byte(height)
	packet[9] = byte(height >> 8)
	return packet
}

// KeyFramePacketWithPayload pads a VP8 keyframe packet with a synthetic first
// partition and token bytes.
func KeyFramePacketWithPayload(width int, height int, firstPartitionSize int,
	profile int, showFrame bool,
) []byte {
	packet := KeyFramePacket(width, height, firstPartitionSize, profile,
		showFrame)
	packet = append(packet, make([]byte, firstPartitionSize)...)
	return append(packet, make([]byte, 10000)...)
}

// InterFramePacket builds a VP8 interframe tag.
func InterFramePacket(firstPartitionSize int, profile int, showFrame bool) []byte {
	packet := make([]byte, 3)
	tag := uint32(1) | uint32(profile&7)<<1
	if showFrame {
		tag |= 1 << 4
	}
	tag |= uint32(firstPartitionSize) << 5
	packet[0] = byte(tag)
	packet[1] = byte(tag >> 8)
	packet[2] = byte(tag >> 16)
	return packet
}

// KeyFramePacketWithFirstPartition builds a keyframe packet whose first
// partition is supplied by the caller.
func KeyFramePacketWithFirstPartition(width int, height int, first []byte) []byte {
	packet := KeyFramePacket(width, height, len(first), 0, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

// KeyFramePacketWithFirstPartitionProfile builds a profiled keyframe packet
// whose first partition is supplied by the caller.
func KeyFramePacketWithFirstPartitionProfile(width int, height int,
	profile int, first []byte,
) []byte {
	packet := KeyFramePacket(width, height, len(first), profile, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

// InterFramePacketWithFirstPartition builds an interframe packet whose first
// partition is supplied by the caller.
func InterFramePacketWithFirstPartition(first []byte) []byte {
	packet := InterFramePacket(len(first), 0, true)
	packet = append(packet, first...)
	return append(packet, make([]byte, 10000)...)
}

// InterFramePacketWithTokenPartitions builds an interframe packet with an
// explicit first token partition size and token payload.
func InterFramePacketWithTokenPartitions(first []byte, firstTokenSize int,
	tokens []byte,
) []byte {
	packet := InterFramePacket(len(first), 0, true)
	packet = append(packet, first...)
	packet = append(packet, byte(firstTokenSize), byte(firstTokenSize>>8),
		byte(firstTokenSize>>16))
	return append(packet, tokens...)
}

// InterFirstPartitionLastZeroMVWithConfig builds an interframe first partition
// with explicit token-partition, skip-coeff, and base-q settings.
func InterFirstPartitionLastZeroMVWithConfig(
	tokenPartition vp8common.TokenPartition, skipCoeff bool, baseQIndex uint8,
) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(uint32(tokenPartition), 2)
	w.writeLiteral(uint32(baseQIndex&0x7f), 7)
	for range 5 {
		w.writeBool(0, 128)
	}

	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)

	writeNoCoefficientProbabilityUpdates(&w)
	if skipCoeff {
		w.writeBool(1, 128)
		w.writeLiteral(128, 8)
	} else {
		w.writeBool(0, 128)
	}
	w.writeLiteral(128, 8)
	w.writeLiteral(128, 8)
	w.writeLiteral(128, 8)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	for component := range 2 {
		for i := range vp8tables.MVPCount {
			w.writeBool(0, vp8tables.MVUpdateProbs[component][i])
		}
	}

	if skipCoeff {
		w.writeBool(1, 128)
	}
	w.writeBool(1, 128)
	w.writeBool(0, 128)
	w.writeBool(0, vp8tables.InterModeContexts[0][0])

	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

// FirstPartitionWithBaseQIndex builds a keyframe first partition with the
// supplied base quantizer.
func FirstPartitionWithBaseQIndex(baseQIndex uint8) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(uint32(baseQIndex&0x7f), 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

// FirstPartitionWithSingleCoefProbabilityUpdate builds a keyframe first
// partition that updates one coefficient probability.
func FirstPartitionWithSingleCoefProbabilityUpdate(refreshEntropy bool,
	value uint8,
) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	if refreshEntropy {
		w.writeBool(1, 128)
	} else {
		w.writeBool(0, 128)
	}

	first := true
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					if first {
						w.writeBool(1,
							vp8tables.CoefUpdateProbs[block][band][ctx][node])
						w.writeLiteral(uint32(value), 8)
						first = false
					} else {
						w.writeBool(0,
							vp8tables.CoefUpdateProbs[block][band][ctx][node])
					}
				}
			}
		}
	}

	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

// FirstPartitionWithLoopFilterLevel builds a keyframe first partition with a
// supplied loop-filter level.
func FirstPartitionWithLoopFilterLevel(level uint8) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(uint32(level), 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	w.writeBool(0, 128)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

// FirstPartitionWithMacroblockSkip builds a keyframe first partition with a
// macroblock skip probability and one skipped macroblock.
func FirstPartitionWithMacroblockSkip(probSkipFalse uint8) []byte {
	var w testBoolWriter
	w.init()
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeBool(0, 128)
	w.writeLiteral(0, 6)
	w.writeLiteral(0, 3)
	w.writeBool(0, 128)
	w.writeLiteral(0, 2)
	w.writeLiteral(0, 7)
	for range 5 {
		w.writeBool(0, 128)
	}
	w.writeBool(0, 128)
	writeNoCoefficientProbabilityUpdates(&w)
	w.writeBool(1, 128)
	w.writeLiteral(uint32(probSkipFalse), 8)
	w.writeBool(1, probSkipFalse)
	payload := w.finish()
	return append(payload, make([]byte, 200)...)
}

func writeNoCoefficientProbabilityUpdates(w *testBoolWriter) {
	for block := range vp8tables.BlockTypes {
		for band := range vp8tables.CoefBands {
			for ctx := range vp8tables.PrevCoefContexts {
				for node := range vp8tables.EntropyNodes {
					w.writeBool(0, vp8tables.CoefUpdateProbs[block][band][ctx][node])
				}
			}
		}
	}
}

type testBoolWriter struct {
	low   uint32
	rng   uint32
	count int
	buf   []byte
}

func (w *testBoolWriter) init() {
	w.low = 0
	w.rng = 255
	w.count = -24
	w.buf = w.buf[:0]
}

func (w *testBoolWriter) writeLiteral(value uint32, bits int) {
	for bit := bits - 1; bit >= 0; bit-- {
		w.writeBool(uint8((value>>uint(bit))&1), 128)
	}
}

func (w *testBoolWriter) finish() []byte {
	for range 32 {
		w.writeBool(0, 128)
	}
	return w.buf
}

func (w *testBoolWriter) writeBool(bit uint8, probability uint8) {
	split := uint32(1 + (((w.rng - 1) * uint32(probability)) >> 8))

	rng := split
	low := w.low
	if bit != 0 {
		low += split
		rng = w.rng - split
	}

	shift := int(vp8tables.BoolNorm[byte(rng)])
	rng <<= uint(shift)
	count := w.count + shift

	if count >= 0 {
		offset := shift - count
		if ((low << uint(offset-1)) & 0x80000000) != 0 {
			for i := len(w.buf) - 1; i >= 0; i-- {
				if w.buf[i] != 0xff {
					w.buf[i]++
					break
				}
				w.buf[i] = 0
			}
		}

		w.buf = append(w.buf, byte((low>>uint(24-offset))&0xff))
		shift = count
		low = uint32((uint64(low) << uint(offset)) & 0xffffff)
		count -= 8
	}

	low <<= uint(shift)
	w.low = low
	w.rng = rng
	w.count = count
}
