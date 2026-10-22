package coordinator

import (
	"context"
	"testing"
	"time"
)

// Spawn before Start hands its work a background context, never nil.
func TestSpawnBeforeStartGetsABackgroundContext(t *testing.T) {
	t.Parallel()
	var s Turns
	got := make(chan context.Context, 1)
	s.Spawn(func(ctx context.Context) { got <- ctx })
	s.Idle()
	ctx := <-got
	if ctx == nil {
		t.Fatal("spawned work received a nil context")
	}
	if ctx.Err() != nil {
		t.Fatalf("spawned work before Start got a cancelled context: %v", ctx.Err())
	}
}

// Close cancels the context, waits for spawned work to finish, and releases held work,
// which then runs with the cancelled context.
func TestCloseCancelsWaitsAndReleasesHeldWork(t *testing.T) {
	t.Parallel()
	s := newTurnsState()
	release := make(chan struct{})
	finished := make(chan error, 1)
	s.Spawn(func(ctx context.Context) {
		<-release
		finished <- ctx.Err()
	})
	s.Hold()
	heldRan := make(chan error, 1)
	s.Spawn(func(ctx context.Context) { heldRan <- ctx.Err() })
	close(release)
	s.Close()
	if err := <-finished; err == nil {
		t.Fatal("running work did not see the cancelled context")
	}
	select {
	case err := <-heldRan:
		if err == nil {
			t.Fatal("held work ran without a cancelled context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not release held work")
	}
	if s.Context().Err() == nil {
		t.Fatal("Close did not cancel the context")
	}
}

// Work spawned after Close still runs (so its main-actor part is not lost), with the cancelled context,
// and Close can be called again.
func TestSpawnAfterCloseRunsWithACancelledContext(t *testing.T) {
	t.Parallel()
	s := newTurnsState()
	s.Close()
	ran := make(chan error, 1)
	s.Spawn(func(ctx context.Context) { ran <- ctx.Err() })
	s.Idle()
	if err := <-ran; err == nil {
		t.Fatal("work spawned after Close did not see a cancelled context")
	}
	s.Close()
}
