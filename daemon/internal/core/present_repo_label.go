package core

import "strconv"

// RepoLabelAmbiguousNames is the set of repository names used by more than one owner in
// runs. Display-only: identities and stored values stay owner/repo.
//
// Use every known run, including repositories used by multi-repo work and
// its pull requests, so filtering never changes a label's disambiguation.
func RepoLabelAmbiguousNames(runs []Run) map[string]struct{} {
	// The first repository seen with each name; a second, different one makes the name ambiguous.
	first := map[string]string{}
	ambiguous := map[string]struct{}{}
	add := func(repo string) {
		name, ok := repoLabelName(repo)
		if !ok {
			return
		}
		if seen, ok := first[name]; !ok {
			first[name] = repo
		} else if seen != repo {
			ambiguous[name] = struct{}{}
		}
	}
	for i := range runs {
		run := &runs[i]
		add(run.Subject.Repo)
		for _, repo := range run.Repos {
			add(repo)
		}
		for _, pull := range run.PullRequests {
			add(pull.Repo)
		}
	}
	return ambiguous
}

// repoLabelName is the part of repo after its first slash, and whether there is one.
func repoLabelName(repo string) (string, bool) {
	for i := 0; i < len(repo); i++ {
		if repo[i] == '/' {
			return repo[i+1:], true
		}
	}
	return "", false
}

// RepoLabelShort is the display name of repo: its name, with the owner in brackets when ambiguous.
func RepoLabelShort(repo string, ambiguous map[string]struct{}) string {
	name, ok := repoLabelName(repo)
	if !ok {
		return repo
	}
	if _, isAmbiguous := ambiguous[name]; isAmbiguous {
		owner := repo[:len(repo)-len(name)-1]
		return name + " [" + owner + "]"
	}
	return name
}

// RepoLabelPullRequest is the display reference of a pull request, "name#number".
func RepoLabelPullRequest(repo string, number int, ambiguous map[string]struct{}) string {
	return RepoLabelShort(repo, ambiguous) + "#" + strconv.Itoa(number)
}
