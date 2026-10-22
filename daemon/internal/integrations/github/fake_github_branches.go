// The branch side of FakeGitHub: branch existence and how far branches are ahead of their base.

package github

import (
	"context"
)

// SetAhead sets how many commits branch has that its base doesn't.
func (f *FakeGitHub) SetAhead(repo, branch string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ahead[repo+"|"+branch] = count
}

// CommitsAhead is how many commits head has that base doesn't; nil when the branch is not set.
func (f *FakeGitHub) CommitsAhead(_ context.Context, repo, _, head string) (*int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.ahead[repo+"|"+head]; ok {
		return &n, nil
	}
	return nil, nil
}

// SetBranch sets whether branch exists in repo; every branch exists unless set missing.
func (f *FakeGitHub) SetBranch(repo, branch string, exists bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if exists {
		delete(f.missing, repo+"|"+branch)
	} else {
		f.missing[repo+"|"+branch] = true
	}
}

// BranchLookups is how many BranchExists calls were made.
func (f *FakeGitHub) BranchLookups() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lookups
}

// BranchExists reports whether branch is on repo; every branch exists unless set missing.
func (f *FakeGitHub) BranchExists(_ context.Context, repo, branch string) (*bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	exists := !f.missing[repo+"|"+branch]
	return &exists, nil
}
