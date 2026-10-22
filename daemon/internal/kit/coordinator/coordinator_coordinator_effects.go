// The effects the engine asks
// for after an event, the merges of legs and the comments on a run's subject.

package coordinator

import (
	"context"
	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
	"strings"
)

// perform carries out what the engine asked for after an event on run.
// Lock: runs with c.mu held. Spawns: a comment task, and each stop, cleanup and
// merge through trackEffect.
func (c *Coordinator) perform(effects []core.Effect, run core.Run, session *string) {
	id := run.ID
	for _, effect := range effects {
		switch effect.Kind {
		case core.EffectKindComment:
			text := effect.Text
			subject := run.Subject.Clone()
			c.Spawn(func(ctx context.Context) {
				c.Lock()
				defer c.EndTurn()
				if err := c.comment(ctx, subject, text); err != nil {
					runID := id
					c.book.note("Could not comment on "+subject.Ref()+": "+err.Error(), &runID)
				}
			})
		case core.EffectKindNotify:
			if c.settings.Notifications && c.notifier != nil {
				c.notifier(effect.Title, effect.Body)
			}
		case core.EffectKindStopSession:
			if session != nil {
				name := *session
				c.trackStop(id, func(ctx context.Context) { c.Suspend(func() { c.runner.Stop(ctx, name) }) })
			}
		case core.EffectKindStop:
			name := effect.Session
			c.trackStop(id, func(ctx context.Context) { c.Suspend(func() { c.runner.Stop(ctx, name) }) })
		case core.EffectKindStopForPark:
			c.stopForPark(id, effect.Session)
		case core.EffectKindCleanUp:
			repos := run.Repos
			folder := c.paths.WorkFolder(run)
			c.trackEffect(id, func(ctx context.Context) { c.Suspend(func() { c.workspaces.CleanUp(ctx, repos, folder) }) })
		case core.EffectKindMergeLegs:
			branches := effect.Legs
			c.trackEffect(id, func(ctx context.Context) { c.mergeLegs(ctx, branches, id) })
		case core.EffectKindInterrupt, core.EffectKindCancelAfk:
			// 			// The perform breaks for these: the pause (RunPause) interrupts the
			// session itself, and the AFK cancel is handled where it is raised.
		}
	}
}

// mergeLegs merges the branches a run's legs worked on into the run's own
// branch, before the stage where the routes meet, and reports what it did
// .
// Lock: runs with c.mu held (it runs inside trackEffect's main-actor work).
// Suspends: sessions.base and workspaces.mergeLegs. Spawns: kick.
func (c *Coordinator) mergeLegs(ctx context.Context, branches []string, id string) {
	run := c.book.Run(id)
	if run == nil || len(branches) == 0 {
		c.book.apply(id, core.Event{Kind: core.EventKindMergedLegs, Legs: []string{}}, nil, nil, nil)
		c.kick()
		return
	}
	var merged, conflict []string
	destination := mergeDestination(*run)
	for _, repo := range run.Repos {
		base, err := c.sessions.base(ctx, *run, repo)
		if err != nil {
			base = ""
			if requested := run.RequestedBase(repo); requested != nil {
				base = *requested
			}
		}
		spec := runner.WorkspaceSpec{
			Repo: repo, Base: base, Branch: destination, Checkout: runner.WorkspaceCheckout{Kind: runner.WorkspaceCheckoutKindWorktree},
			Folder: c.paths.WorkFolderLeg(*run, run.MergeCheckout), Repos: run.Repos,
		}
		var result runner.MergeResult
		c.Suspend(func() { result = c.workspaces.MergeLegs(ctx, branches, spec) })
		switch result.Kind {
		case runner.MergeResultMerged:
			merged = append(merged, result.Names...)
		case runner.MergeResultConflict:
			conflict = append(conflict, result.Names...)
		}
	}
	delete(c.book.busy, id)
	// The leg that carries on works on the branch it was merged into, so
	// an outer join merges that branch in turn.
	leg := run.MergeLeg
	record := func(r *core.Run) {
		if leg == nil {
			return
		}
		at := r.LegIndexOf(*leg)
		if at < 0 || r.Legs[at].Branch != nil {
			return
		}
		r.Legs[at].Branch = &destination
		if core.LegFromID(leg) == nil {
			if r.Branch == nil {
				r.Branch = &destination
			}
		}
	}
	if len(conflict) == 0 {
		c.book.apply(id, core.Event{Kind: core.EventKindMergedLegs, Legs: merged}, nil, nil, record)
	} else {
		c.book.apply(id, core.Event{Kind: core.EventKindMergeConflict, Files: conflict}, nil, nil, record)
		runID := id
		c.book.note("Merging the routes of "+run.Subject.Ref()+" conflicted: "+strings.Join(conflict, ", "), &runID)
	}
	c.kick()
}

// mergeDestination is the branch a join's merge goes into: the one the join
// named, or the branch of the leg whose checkout the merge happens in (
// `Coordinator.mergeDestination`, static).
// Lock: none (pure).
func mergeDestination(run core.Run) string {
	if run.MergeInto != nil {
		return *run.MergeInto
	}
	var leg *core.Leg
	if run.MergeCheckout != nil {
		id := *run.MergeCheckout
		if found := run.LegByID(&id); found != nil {
			leg = found
		} else {
			leg = &core.Leg{ID: id, State: core.RunStateQueued}
		}
	}
	return sessionsBranch(run, leg)
}

// comment comments on the issue, pull request or ticket the run is about, when it is held.
// Lock: runs with c.mu held. Suspends: github.comment, github.commentOnPullRequest or jira.comment.
func (c *Coordinator) comment(ctx context.Context, subject core.Subject, body string) error {
	about := subject.About()
	if about == nil {
		return core.NewFailure("a " + subject.Kind.Noun() + " has nowhere to comment")
	}
	var err error
	switch about.Kind {
	case core.ResourceRefKindIssue:
		c.Suspend(func() { err = c.github.Comment(ctx, about.Repo, about.Number, body) })
	case core.ResourceRefKindPullRequest:
		c.Suspend(func() { err = c.github.CommentOnPullRequest(ctx, about.Repo, about.Number, body) })
	case core.ResourceRefKindTicket:
		c.Suspend(func() { err = c.jira.Comment(ctx, about.Key, body) })
	}
	return err
}
