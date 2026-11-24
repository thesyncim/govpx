package testutil

// ByteCursor is a deterministic circular byte reader for fuzz tests.
type ByteCursor struct {
	data []byte
	pos  int
}

// NewByteCursor returns a cursor over data.
func NewByteCursor(data []byte) ByteCursor {
	return ByteCursor{data: data}
}

// Remaining returns the number of original input bytes not yet consumed.
func (c *ByteCursor) Remaining() int {
	return len(c.data) - c.pos
}

// Next returns the next byte, wrapping short inputs. Empty input yields zero.
func (c *ByteCursor) Next() byte {
	if len(c.data) == 0 {
		return 0
	}
	b := c.data[c.pos%len(c.data)]
	c.pos++
	return b
}

// U16LE reads two wrapped bytes as an unsigned little-endian integer.
func (c *ByteCursor) U16LE() uint16 {
	lo := c.Next()
	hi := c.Next()
	return uint16(lo) | uint16(hi)<<8
}

// Pick returns a bounded index selected by the next byte.
func (c *ByteCursor) Pick(n int) int {
	if n <= 1 {
		return 0
	}
	return int(c.Next()) % n
}
