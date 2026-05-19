//go:build !darwin

package gpuanalysis

import "errors"

// newBackend reports that no GPU backend is implemented on this
// platform yet. Linux Vulkan and Windows D3D12 paths can land here
// later; the analysis.Analyzer construction surfaces the error to
// the caller via NewVP8Encoder / NewVP8Decoder, so the failure mode
// is clear at config time rather than silent at run time.
func newBackend() (Backend, error) {
	return nil, errors.New("gpuanalysis: no GPU backend implemented for this platform (darwin/Metal only at this revision)")
}

// stubBackendUploadReconstructedRef is here only to satisfy the
// Backend interface signature on non-darwin platforms. Real backends
// override UploadReconstructedRef. The stub is unreachable because
// newBackend always errors out on non-darwin.
var _ = stubBackendUploadReconstructedRef

func stubBackendUploadReconstructedRef(_ []byte, _, _ int) error { return nil }
