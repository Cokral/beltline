package runner

import (
	"math/big"
	"testing"

	"github.com/robbertmanders/beltline/daemon/internal/testkit"
)

// rat is an exact decimal, read from its text.
func rat(t testing.TB, text string) *big.Rat {
	t.Helper()
	value, ok := new(big.Rat).SetString(text)
	if !ok {
		t.Fatalf("rat %q", text)
	}
	return value
}

// ratEqual compares two optional decimals by value.
func ratEqual(a, b *big.Rat) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Cmp(b) == 0
}

// pricesEqual compares two price tiers by value.
func pricesEqual(a, b *Prices) bool {
	if a == nil || b == nil {
		return a == b
	}
	return ratEqual(a.Input, b.Input) && ratEqual(a.Output, b.Output) && ratEqual(a.CacheRead, b.CacheRead) &&
		ratEqual(a.CacheWrite, b.CacheWrite) && ratEqual(a.CacheWrite5m, b.CacheWrite5m) && ratEqual(a.CacheWrite1h, b.CacheWrite1h)
}

// fixtureText reads a usage fixture.
func fixtureText(t testing.TB, name string) string {
	t.Helper()
	return string(testkit.ReadFixture(t, "usage", name))
}

// withHome points the user's home folder at a fresh temporary folder for one test.
func withHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// mustEqual fails the test with a diff when got differs from want.
func mustEqual(t *testing.T, want, got any) {
	t.Helper()
	if d := testkit.Diff(want, got); d != "" {
		t.Fatalf("mismatch (-want +got):\n%s", d)
	}
}
