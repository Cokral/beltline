// The heading of the run view in the run view response.

package core

// RunPresentationHeading is the run page's heading: its belt's name, then the repository's
// display name (with the owner when ambiguous).
func RunPresentationHeading(run Run, ambiguous map[string]struct{}) string {
	return run.BeltName + runPresentationHeadingSeparator + RepoLabelShort(run.Subject.Repo, ambiguous)
}

// runPresentationHeadingSeparator sits between the belt's name and the repository. Copy from the run heading.
const runPresentationHeadingSeparator = " · "
