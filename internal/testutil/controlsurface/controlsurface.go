package controlsurface

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type Mapping struct {
	Kind         string
	HelperTokens []string
}

func ExportedMethodSet(t *testing.T, sample any) map[string]struct{} {
	t.Helper()
	typ := reflect.TypeOf(sample)
	if typ.Kind() != reflect.Pointer {
		t.Fatalf("sample type = %s, want pointer", typ)
	}
	out := make(map[string]struct{}, typ.NumMethod())
	for method := range typ.Methods() {
		if method.PkgPath == "" {
			out[method.Name] = struct{}{}
		}
	}
	return out
}

func ExportedFieldSet(t *testing.T, sample any) map[string]struct{} {
	t.Helper()
	typ := reflect.TypeOf(sample)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		t.Fatalf("sample type = %s, want struct", typ)
	}
	out := make(map[string]struct{}, typ.NumField())
	for field := range typ.Fields() {
		if field.PkgPath == "" {
			out[field.Name] = struct{}{}
		}
	}
	return out
}

func AssertPublicMethodMappings(t *testing.T, typeName string, got map[string]struct{}, want map[string]Mapping) {
	t.Helper()
	for method := range got {
		if _, ok := want[method]; !ok {
			t.Fatalf("%s.%s has no parity/control mapping entry", typeName, method)
		}
	}
	for method, mapping := range want {
		if _, ok := got[method]; !ok {
			t.Fatalf("%s.%s mapping kind %q has no public method", typeName, method, mapping.Kind)
		}
		if mapping.Kind == "" {
			t.Fatalf("%s.%s has empty parity mapping kind", typeName, method)
		}
	}
}

func AssertOptionFieldMappings(t *testing.T, typeName string, got map[string]struct{}, want map[string]Mapping) {
	t.Helper()
	for field := range got {
		if _, ok := want[field]; !ok {
			t.Fatalf("%s.%s has no parity/options mapping entry", typeName, field)
		}
	}
	for field, mapping := range want {
		if _, ok := got[field]; !ok {
			t.Fatalf("%s.%s mapping kind %q has no exported field", typeName, field, mapping.Kind)
		}
		if mapping.Kind == "" {
			t.Fatalf("%s.%s has empty parity mapping kind", typeName, field)
		}
	}
}

func AssertFrameFlagsDriverTokens(t *testing.T, mappings map[string]Mapping) {
	t.Helper()
	assertFrameFlagsDriverTokensInFile(t, mappings, repoPath("internal/coracle/vpxenc_frameflags.c"))
}

func AssertVP9FrameFlagsDriverTokens(t *testing.T, mappings map[string]Mapping) {
	t.Helper()
	assertFrameFlagsDriverTokensInFiles(t, mappings,
		repoPath("internal/coracle/vpxenc_frameflags.c"),
		repoPath("internal/coracle/vpxenc_vp9_frameflags.c"))
}

func assertFrameFlagsDriverTokensInFile(t *testing.T, mappings map[string]Mapping, filename string) {
	t.Helper()
	assertFrameFlagsDriverTokensInFiles(t, mappings, filename)
}

func assertFrameFlagsDriverTokensInFiles(t *testing.T, mappings map[string]Mapping, filenames ...string) {
	t.Helper()
	var source strings.Builder
	for _, filename := range filenames {
		data, err := os.ReadFile(filename)
		if err != nil {
			t.Fatalf("read %s: %v", filename, err)
		}
		source.WriteString("\n" + string(data))
	}
	label := strings.Join(filenames, ", ")
	for method, mapping := range mappings {
		for _, token := range mapping.HelperTokens {
			if !strings.Contains(source.String(), `"`+token) {
				t.Fatalf("%s maps to frameflags token %q, but %s does not contain it", method, token, label)
			}
		}
	}
}

func AssertDecoderControlTokens(t *testing.T, mappings map[string]Mapping) {
	t.Helper()
	path := repoPath("internal/coracle/vpx_oracle.c")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	source := string(data)
	for method, mapping := range mappings {
		for _, token := range mapping.HelperTokens {
			if !strings.Contains(source, `"`+token) {
				t.Fatalf("%s maps to decoder oracle token %q, but %s does not contain it", method, token, path)
			}
		}
	}
}

func repoPath(elem string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return elem
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	return filepath.Join(root, elem)
}
