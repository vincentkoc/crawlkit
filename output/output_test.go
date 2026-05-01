package output

import (
	"bytes"
	"testing"
)

func TestResolveJSONFlagWins(t *testing.T) {
	got, err := Resolve("text", true)
	if err != nil {
		t.Fatal(err)
	}
	if got != JSON {
		t.Fatalf("format = %q", got)
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, JSON, "status", map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"ok": true`)) {
		t.Fatalf("json output = %s", buf.String())
	}
}

func TestUsageError(t *testing.T) {
	_, err := Resolve("xml", false)
	if !IsUsage(err) {
		t.Fatalf("expected usage error, got %v", err)
	}
}
