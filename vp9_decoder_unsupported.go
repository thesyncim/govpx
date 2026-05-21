package govpx

func (d *VP9Decoder) markVP9Unsupported(reason string) {
	if vp9OracleTraceBuild {
		d.traceVP9Unsupported(reason)
	}
	d.unsupportedReconstruct = true
}
