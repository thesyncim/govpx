package govpx

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

type controlParityMapping struct {
	kind         string
	helperTokens []string
}

func exportedMethodSet(t *testing.T, sample any) map[string]struct{} {
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

func exportedFieldSet(t *testing.T, sample any) map[string]struct{} {
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

func assertPublicMethodMappings(t *testing.T, typeName string, got map[string]struct{}, want map[string]controlParityMapping) {
	t.Helper()
	for method := range got {
		if _, ok := want[method]; !ok {
			t.Fatalf("%s.%s has no parity/control mapping entry", typeName, method)
		}
	}
	for method, mapping := range want {
		if _, ok := got[method]; !ok {
			t.Fatalf("%s.%s mapping kind %q has no public method", typeName, method, mapping.kind)
		}
		if mapping.kind == "" {
			t.Fatalf("%s.%s has empty parity mapping kind", typeName, method)
		}
	}
}

func assertOptionFieldMappings(t *testing.T, typeName string, got map[string]struct{}, want map[string]controlParityMapping) {
	t.Helper()
	for field := range got {
		if _, ok := want[field]; !ok {
			t.Fatalf("%s.%s has no parity/options mapping entry", typeName, field)
		}
	}
	for field, mapping := range want {
		if _, ok := got[field]; !ok {
			t.Fatalf("%s.%s mapping kind %q has no exported field", typeName, field, mapping.kind)
		}
		if mapping.kind == "" {
			t.Fatalf("%s.%s has empty parity mapping kind", typeName, field)
		}
	}
}

func assertFrameFlagsDriverTokens(t *testing.T, mappings map[string]controlParityMapping) {
	t.Helper()
	assertFrameFlagsDriverTokensInFile(t, mappings, "internal/coracle/vpxenc_frameflags.c")
}

func assertVP9FrameFlagsDriverTokens(t *testing.T, mappings map[string]controlParityMapping) {
	t.Helper()
	assertFrameFlagsDriverTokensInFiles(t, mappings,
		"internal/coracle/vpxenc_frameflags.c",
		"internal/coracle/vpxenc_vp9_frameflags.c")
}

func assertFrameFlagsDriverTokensInFile(t *testing.T, mappings map[string]controlParityMapping, filename string) {
	t.Helper()
	assertFrameFlagsDriverTokensInFiles(t, mappings, filename)
}

func assertFrameFlagsDriverTokensInFiles(t *testing.T, mappings map[string]controlParityMapping, filenames ...string) {
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
		for _, token := range mapping.helperTokens {
			if !strings.Contains(source.String(), `"`+token) {
				t.Fatalf("%s maps to frameflags token %q, but %s does not contain it", method, token, label)
			}
		}
	}
}

func assertDecoderControlTokens(t *testing.T, mappings map[string]controlParityMapping) {
	t.Helper()
	data, err := os.ReadFile("internal/coracle/vpx_oracle.c")
	if err != nil {
		t.Fatalf("read vpx_oracle.c: %v", err)
	}
	source := string(data)
	for method, mapping := range mappings {
		for _, token := range mapping.helperTokens {
			if !strings.Contains(source, `"`+token) {
				t.Fatalf("%s maps to decoder oracle token %q, but internal/coracle/vpx_oracle.c does not contain it", method, token)
			}
		}
	}
}
