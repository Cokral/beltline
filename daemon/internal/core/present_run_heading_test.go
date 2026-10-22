package core

import "testing"

// TestRunPresentationHeadingIsTheBeltThenTheRepository: the owner shows only when the repository name is ambiguous.
func TestRunPresentationHeadingIsTheBeltThenTheRepository(t *testing.T) {
	t.Parallel()
	run := Run{BeltName: "Issues", Subject: Subject{Repo: "acme/tools"}}
	if got := RunPresentationHeading(run, nil); got != "Issues · tools" {
		t.Fatalf("heading = %q", got)
	}
	ambiguous := map[string]struct{}{"tools": {}}
	if got := RunPresentationHeading(run, ambiguous); got != "Issues · tools [acme]" {
		t.Fatalf("ambiguous heading = %q", got)
	}
}
