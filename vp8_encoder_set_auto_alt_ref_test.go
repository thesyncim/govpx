package govpx

import "testing"

// TestSetAutoAltRefMutatesOption asserts the runtime setter flips
// EncoderOptions.AutoAltRef the same way libvpx's
// VP8E_SET_ENABLEAUTOALTREF / set_enable_auto_alt_ref overwrites
// extra_cfg.enable_auto_alt_ref (vp8/vp8_cx_iface.c:545-549). libvpx's
// downstream altref scheduling reads play_alternate on demand, so the
// setter mirrors that by mutating the option directly and letting later
// frame decisions pick up the new value.
func TestSetAutoAltRefMutatesOption(t *testing.T) {
	e := newTestEncoder(t)
	if e.opts.AutoAltRef {
		t.Fatalf("default AutoAltRef = true, want false on test encoder")
	}
	if err := e.SetAutoAltRef(true); err != nil {
		t.Fatalf("SetAutoAltRef(true) returned error: %v", err)
	}
	if !e.opts.AutoAltRef {
		t.Fatalf("AutoAltRef = false after SetAutoAltRef(true), want true")
	}
	if err := e.SetAutoAltRef(false); err != nil {
		t.Fatalf("SetAutoAltRef(false) returned error: %v", err)
	}
	if e.opts.AutoAltRef {
		t.Fatalf("AutoAltRef = true after SetAutoAltRef(false), want false")
	}
}
