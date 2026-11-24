package govpx

func (d *VP9Decoder) markVP9Unsupported() {
	d.unsupportedReconstruct = true
}
