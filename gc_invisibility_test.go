package govpx

import (
	"fmt"
	"image"
	"reflect"
	"testing"
)

func typeHasPointers(t reflect.Type) bool {
	if t == nil {
		return false
	}
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func, reflect.Interface, reflect.String, reflect.UnsafePointer:
		return true
	case reflect.Array:
		return typeHasPointers(t.Elem())
	case reflect.Struct:
		for i := range t.NumField() {
			if typeHasPointers(t.Field(i).Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// typePtrBytes returns the prefix byte length that Go's GC actually scans
// for a value of type t (matching runtime abi.Type.PtrBytes): trailing
// pointer-free fields in a struct are never scanned by the GC.
func typePtrBytes(t reflect.Type) int64 {
	if !typeHasPointers(t) {
		return 0
	}
	switch t.Kind() {
	case reflect.Struct:
		var last int64
		for i := range t.NumField() {
			f := t.Field(i)
			if pb := typePtrBytes(f.Type); pb > 0 {
				if end := int64(f.Offset) + pb; end > last {
					last = end
				}
			}
		}
		return last
	case reflect.Array:
		if t.Len() == 0 {
			return 0
		}
		pb := typePtrBytes(t.Elem())
		if pb == 0 {
			return 0
		}
		return int64(t.Len()-1)*int64(t.Elem().Size()) + pb
	default:
		return int64(t.Size())
	}
}

type gcMemRange struct {
	start uintptr
	end   uintptr
}

type gcScanReport struct {
	noscanBytes int64
	scanBytes   int64
	scanSlices  []string
	ranges      []gcMemRange
}

func (r *gcScanReport) overlapsRange(start uintptr) bool {
	for _, mr := range r.ranges {
		if start >= mr.start && start < mr.end {
			return true
		}
	}
	return false
}

func auditValueGCScan(v reflect.Value, path string, seen map[uintptr]bool, rep *gcScanReport) {
	if !v.IsValid() {
		return
	}
	t := v.Type()
	switch t.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return
		}
		ptr := v.Pointer()
		if seen[ptr] {
			return
		}
		seen[ptr] = true
		elem := v.Elem()
		sz := int64(elem.Type().Size())
		pb := typePtrBytes(elem.Type())
		rep.scanBytes += pb
		rep.noscanBytes += sz - pb
		auditValueGCScan(elem, path, seen, rep)
	case reflect.Slice:
		if v.IsNil() || v.Cap() == 0 {
			return
		}
		ptr := v.Pointer()
		if seen[ptr] || rep.overlapsRange(ptr) {
			return
		}
		seen[ptr] = true
		elemType := t.Elem()
		totalBytes := int64(v.Cap()) * int64(elemType.Size())
		rep.ranges = append(rep.ranges, gcMemRange{start: ptr, end: ptr + uintptr(totalBytes)})
		if typeHasPointers(elemType) {
			elemPB := typePtrBytes(elemType)
			scanTotal := int64(v.Cap()-1)*int64(elemType.Size()) + elemPB
			rep.scanBytes += scanTotal
			rep.noscanBytes += totalBytes - scanTotal
			if scanTotal >= 1024 {
				rep.scanSlices = append(rep.scanSlices, fmt.Sprintf("%s (%s, scan=%d KiB / total=%d KiB)", path, t.String(), scanTotal>>10, totalBytes>>10))
			}
			for i := range v.Len() {
				auditValueGCScan(v.Index(i), path+"[]", seen, rep)
			}
		} else {
			rep.noscanBytes += totalBytes
		}
	case reflect.Array:
		if typeHasPointers(t.Elem()) {
			for i := range v.Len() {
				auditValueGCScan(v.Index(i), path+"[]", seen, rep)
			}
		}
	case reflect.Struct:
		for i := range v.NumField() {
			f := t.Field(i)
			auditValueGCScan(v.Field(i), path+"."+f.Name, seen, rep)
		}
	}
}

func TestGCInvisibilityAudit(t *testing.T) {
	const w, h = 1280, 720

	img := Image{
		Width:   w,
		Height:  h,
		Y:       make([]byte, w*h),
		U:       make([]byte, (w/2)*(h/2)),
		V:       make([]byte, (w/2)*(h/2)),
		YStride: w,
		UStride: w / 2,
		VStride: w / 2,
	}
	ycbcr := image.NewYCbCr(image.Rect(0, 0, w, h), image.YCbCrSubsampleRatio420)
	pkt := make([]byte, w*h)

	logReport := func(name string, obj any, minNoscanPct float64, maxScanKiB int64) {
		var rep gcScanReport
		rv := reflect.ValueOf(obj)
		auditValueGCScan(rv, name, map[uintptr]bool{}, &rep)
		total := rep.noscanBytes + rep.scanBytes
		pct := 100 * float64(rep.noscanBytes) / float64(total)
		elemT := rv.Type().Elem()
		scanKiB := rep.scanBytes >> 10
		t.Logf("%-22s noscan=%6d KiB  scan=%5d KiB  (%.2f%% noscan)  structPtrBytes=%d/%d B",
			name+":", rep.noscanBytes>>10, scanKiB, pct, typePtrBytes(elemT), elemT.Size())
		for _, s := range rep.scanSlices {
			t.Logf("  [SCAN] %s", s)
		}
		if pct < minNoscanPct {
			t.Errorf("%s: noscan %.2f%% < required %.2f%% (scan=%d KiB)", name, pct, minNoscanPct, scanKiB)
		}
		if scanKiB > maxScanKiB {
			t.Errorf("%s: scan %d KiB > max %d KiB", name, scanKiB, maxScanKiB)
		}
	}

	if typeHasPointers(reflect.TypeFor[vp9MLPartitionContext]()) {
		t.Fatal("vp9MLPartitionContext must contain zero pointers (GC noscan)")
	}

	// 1. VP8Encoder (1T & 4T)
	vp8Enc, err := NewVP8Encoder(EncoderOptions{
		Width:             w,
		Height:            h,
		FPS:               30,
		TargetBitrateKbps: 2000,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -8,
		Threads:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vp8Enc.Close()
	res8, err := vp8Enc.EncodeInto(pkt, img, 0, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	kf8 := append([]byte(nil), res8.Data...)
	if _, err := vp8Enc.EncodeInto(pkt, img, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	logReport("VP8Encoder 720p 1T", vp8Enc, 99.85, 32)

	vp8Enc4, err := NewVP8Encoder(EncoderOptions{
		Width:             w,
		Height:            h,
		FPS:               30,
		TargetBitrateKbps: 2000,
		Deadline:          DeadlineRealtime,
		CpuUsed:           -8,
		Threads:           4,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vp8Enc4.Close()
	if _, err := vp8Enc4.EncodeInto(pkt, img, 0, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := vp8Enc4.EncodeInto(pkt, img, 1, 1, 0); err != nil {
		t.Fatal(err)
	}
	logReport("VP8Encoder 720p 4T", vp8Enc4, 99.50, 160)

	// 2. VP8Decoder
	vp8Dec, err := NewVP8Decoder(DecoderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer vp8Dec.Close()
	if err := vp8Dec.Decode(kf8); err != nil {
		t.Fatal(err)
	}
	logReport("VP8Decoder 720p", vp8Dec, 99.90, 16)

	// 3. VP9Encoder (1T & 4T RowMT)
	vp9Enc, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             w,
		Height:            h,
		TargetBitrateKbps: 2000,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		Threads:           1,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vp9Enc.Close()
	res9, err := vp9Enc.EncodeIntoWithResult(ycbcr, pkt)
	if err != nil {
		t.Fatal(err)
	}
	kf9 := append([]byte(nil), res9.Data...)
	if _, err := vp9Enc.EncodeIntoWithResult(ycbcr, pkt); err != nil {
		t.Fatal(err)
	}
	logReport("VP9Encoder 720p 1T", vp9Enc, 99.80, 100)

	vp9Enc4, err := NewVP9Encoder(VP9EncoderOptions{
		Width:             w,
		Height:            h,
		TargetBitrateKbps: 2000,
		Deadline:          DeadlineRealtime,
		CpuUsed:           8,
		Threads:           4,
		RowMT:             true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer vp9Enc4.Close()
	if _, err := vp9Enc4.EncodeIntoWithResult(ycbcr, pkt); err != nil {
		t.Fatal(err)
	}
	if _, err := vp9Enc4.EncodeIntoWithResult(ycbcr, pkt); err != nil {
		t.Fatal(err)
	}
	logReport("VP9Encoder 720p 4T", vp9Enc4, 99.45, 560)

	// 4. VP9Decoder
	vp9Dec, err := NewVP9Decoder(VP9DecoderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer vp9Dec.Close()
	if err := vp9Dec.Decode(kf9); err != nil {
		t.Fatal(err)
	}
	logReport("VP9Decoder 720p", vp9Dec, 99.85, 8)
}
