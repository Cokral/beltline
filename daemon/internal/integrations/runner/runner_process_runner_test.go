package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/integrations/shell"
)

// The process runner drains both outputs while it sends input, and the native
// install folder comes before Homebrew on the PATH.
func TestDrainsBothOutputsWhileSendingLargeInput(t *testing.T) {
	t.Parallel()
	// The alarm bounds a regression: the old runner blocks writing stdin
	// while this child blocks writing stdout, then times out with 124.
	program := `$SIG{ALRM} = sub { exit 124 }; alarm 5;
$| = 1;
print STDOUT 'o' x 262144;
print STDERR 'e' x 262144;
local $/; my $input = <STDIN>;
print STDOUT length($input);
alarm 0;`
	input := strings.Repeat("i", 262144)
	result := shell.ProcessRunner{}.Run(context.Background(), "/usr/bin/perl", []string{"-e", program}, "", &input)
	if result.Status != 0 {
		t.Fatalf("status = %d: %s", result.Status, result.Stderr)
	}
	if result.Stdout != strings.Repeat("o", 262144)+"262144" {
		t.Errorf("stdout is %d bytes", len(result.Stdout))
	}
	if result.Stderr != strings.Repeat("e", 262144) {
		t.Errorf("stderr is %d bytes", len(result.Stderr))
	}
}

func TestTheNativeInstallFolderComesBeforeHomebrew(t *testing.T) {
	t.Parallel()
	// A login shell lists Homebrew first, and may list ~/.local/bin after it or not at all.
	if got, want := shell.PathFrom([]string{"/opt/homebrew/bin", "/usr/bin", "/h/.local/bin", "/bin"}, "/h"),
		"/h/.local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/local/bin:/usr/sbin:/sbin:/home/linuxbrew/.linuxbrew/bin"; got != want {
		t.Errorf("path = %q, want %q", got, want)
	}
	if got, want := shell.PathFrom(nil, "/h"),
		"/h/.local/bin:/opt/homebrew/bin:/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin:/home/linuxbrew/.linuxbrew/bin"; got != want {
		t.Errorf("empty login path = %q, want %q", got, want)
	}
}
