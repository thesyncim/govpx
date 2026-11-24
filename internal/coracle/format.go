package coracle

import (
	"bytes"
	"encoding/json"
	"strconv"
)

// FormatDivergences renders comparator divergences in a compact, stable form
// for test failure messages.
func FormatDivergences(div []Divergence) string {
	var buf bytes.Buffer
	for _, d := range div {
		buf.WriteString("row=")
		buf.WriteString(strconv.Itoa(d.RowIndex))
		buf.WriteString(" kind=")
		buf.WriteString(d.RowKind)
		buf.WriteString(" frame=")
		buf.WriteString(strconv.FormatInt(d.FrameIndex, 10))
		if d.MBRow >= 0 || d.MBCol >= 0 {
			buf.WriteString(" mb=")
			buf.WriteString(strconv.Itoa(d.MBRow))
			buf.WriteByte(',')
			buf.WriteString(strconv.Itoa(d.MBCol))
		}
		buf.WriteString(" field=")
		buf.WriteString(d.Field)
		buf.WriteString(" govpx=")
		buf.WriteString(strconv.Quote(traceValueString(d.Govpx)))
		buf.WriteString(" libvpx=")
		buf.WriteString(strconv.Quote(traceValueString(d.Libvpx)))
		buf.WriteByte('\n')
	}
	return buf.String()
}

func traceValueString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "<invalid>"
	}
	return string(b)
}
