// MultiRepositoryBriefTests,
// and the brief test of MultiRepositoryBaseTests.

package core

import (
	"strings"
	"testing"
)

// TestEveryRepositoryFolderAndBranchIsInTheBrief
func TestEveryRepositoryFolderAndBranchIsInTheBrief(t *testing.T) {
	t.Parallel()
	belt := triggerTestBelt(TriggerKindJira, nil)
	belt.SetRepos([]string{"acme/billing", "acme/infra"})
	run := NewRun(RunArgs{Belt: belt, Subject: triggerTestTicket(), Now: engineT0})
	context := BriefContext{RunFolder: "/run", AttemptFolder: "/attempt", Base: "main", Branch: "beltline/jira-PAY-31",
		Checkouts: map[string]string{"acme/billing": "/work/worktree/billing", "acme/infra": "/work/worktree/infra"}}
	checked := 0
	for _, stage := range run.Stages {
		if stage.Kind() != StageKindAgent {
			continue
		}
		checked++
		text := BriefRender(run, stage, context)
		for _, want := range []string{"acme/billing: /work/worktree/billing", "acme/infra: /work/worktree/infra",
			"run branch `beltline/jira-PAY-31`", "You choose which repositories to work in",
			"open their pull requests yourself", "The code is spread over several repositories."} {
			if !strings.Contains(text, want) {
				t.Fatalf("%s brief lacks %q:\n%s", stage.ID, want, text)
			}
		}
		if strings.Contains(text, "The code is in acme/billing.") {
			t.Fatalf("%s brief names one repository:\n%s", stage.ID, text)
		}
	}
	if checked == 0 {
		t.Fatal("the jira belt has no agent stages")
	}
}

// TestTheBriefSaysWhereEachRepositoryMerges
func TestTheBriefSaysWhereEachRepositoryMerges(t *testing.T) {
	t.Parallel()
	epicTicket := Subject{Kind: SubjectKindJira, Repo: "acme/billing", Key: "PAY-42", Title: "Part",
		Labels: []string{"agent", "base:beltline/PAY-40"}, IsOpen: true, Blockers: []string{}}
	belt := triggerTestBelt(TriggerKindJira, nil)
	belt.SetRepos([]string{"acme/billing", "acme/web"})
	belt.Base = ""
	run := NewRun(RunArgs{Belt: belt, Subject: epicTicket, Now: engineT0})
	context := BriefContext{RunFolder: "/run", AttemptFolder: "/attempt", Base: "beltline/PAY-40", Branch: "beltline/jira-PAY-42",
		Checkouts: map[string]string{"acme/billing": "/work/worktree/billing", "acme/web": "/work/worktree/web"},
		Bases:     map[string]string{"acme/billing": "beltline/PAY-40", "acme/web": "main"}}
	var stage Stage
	for _, candidate := range run.Stages {
		if candidate.Kind() == StageKindAgent {
			stage = candidate
			break
		}
	}
	text := BriefRender(run, stage, context)
	for _, want := range []string{
		"acme/billing: /work/worktree/billing, run branch `beltline/jira-PAY-42`, merges into `beltline/PAY-40`",
		"acme/web: /work/worktree/web, run branch `beltline/jira-PAY-42`, merges into `main`",
		"Its work starts from beltline/PAY-40 where a repository has that branch",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("brief lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "its pull requests merge into beltline/PAY-40.") {
		t.Fatalf("brief says one base for every repository:\n%s", text)
	}
}
