// Branch(for:): the
// branch a run's code work goes on. It is here, below the coordinator, so the
// demo's seed and the coordinator's sessions share the one rule.

package core

// RunBranch is the branch a run's code work goes on, such as beltline/issue-142.
// Belts other than the first put their id in it.
func RunBranch(run Run) string {
	if run.BeltID == "issues" {
		return "beltline/" + run.Subject.Slug()
	}
	return "beltline/" + run.BeltID + "-" + run.Subject.Slug()
}
