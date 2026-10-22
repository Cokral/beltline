// Tests of readOptionalText's UTF-8 rule: a session log that is not valid UTF-8 is treated as missing,
// as reading it with UTF-8 decoding gives nil for those bytes.

package coordinator

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadOptionalTextTreatsInvalidUTF8AsMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	invalid := filepath.Join(dir, "invalid.log")
	if err := os.WriteFile(invalid, []byte("ok\xff\xfe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readOptionalText(invalid); got != nil {
		t.Fatalf("invalid UTF-8 read as %q, want nil", *got)
	}

	valid := filepath.Join(dir, "valid.log")
	if err := os.WriteFile(valid, []byte("héllo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readOptionalText(valid); got == nil || *got != "héllo\n" {
		t.Fatalf("valid UTF-8 read as %v, want the text", got)
	}

	if got := readOptionalText(filepath.Join(dir, "missing.log")); got != nil {
		t.Fatalf("a missing file read as %q, want nil", *got)
	}
}
