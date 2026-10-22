package coordinator

import (
	"context"
	"sync"
	"testing"
)

type turnsState struct {
	Turns
	runs, settings int
	seen           [][]Resource
}

func newTurnsState() *turnsState {
	s := &turnsState{}
	s.Project = func(r Resource) []byte {
		switch r {
		case ResourceRuns:
			return []byte{byte(s.runs)}
		case ResourceSettings:
			return []byte{byte(s.settings)}
		}
		return nil
	}
	s.OnChange = func(rs []Resource) { s.seen = append(s.seen, rs) }
	s.Start(context.Background())
	return s
}

func TestAChangeIsReportedOnceAtTheEndOfItsTurn(t *testing.T) {
	t.Parallel()
	s := newTurnsState()
	s.Lock()
	s.runs++
	s.settings++
	s.runs++
	s.EndTurn()
	s.Lock()
	s.EndTurn()
	if len(s.seen) != 1 || len(s.seen[0]) != 2 || s.seen[0][0] != ResourceRuns || s.seen[0][1] != ResourceSettings {
		t.Fatalf("seen = %v, want one turn with runs and settings", s.seen)
	}
}

func TestAChangeBackWithinATurnIsNoChange(t *testing.T) {
	t.Parallel()
	s := newTurnsState()
	s.Lock()
	s.runs++
	s.runs--
	s.EndTurn()
	if len(s.seen) != 0 {
		t.Fatalf("seen = %v, want nothing", s.seen)
	}
}

func TestSuspendEndsTheTurnAndLetsOthersRun(t *testing.T) {
	t.Parallel()
	s := newTurnsState()
	s.Lock()
	s.runs++
	s.Suspend(func() {
		s.Lock() // another caller gets in while the first is suspended
		s.settings++
		s.EndTurn()
	})
	s.EndTurn()
	if len(s.seen) != 2 || s.seen[0][0] != ResourceRuns || s.seen[1][0] != ResourceSettings {
		t.Fatalf("seen = %v, want runs then settings", s.seen)
	}
}

func TestIdleWaitsForSpawnedWorkIncludingWhatItSpawns(t *testing.T) {
	t.Parallel()
	s := newTurnsState()
	var mu sync.Mutex
	done := 0
	s.Spawn(func(ctx context.Context) {
		s.Spawn(func(ctx context.Context) {
			s.Lock()
			s.runs++
			s.EndTurn()
			mu.Lock()
			done++
			mu.Unlock()
		})
		mu.Lock()
		done++
		mu.Unlock()
	})
	s.Idle()
	if done != 2 {
		t.Fatalf("done = %d after Idle, want 2", done)
	}
	s.Close()
	if s.Context().Err() == nil {
		t.Fatal("Close did not cancel the context")
	}
}

func TestHeldWorkStartsOnlyOnReleaseOrIdle(t *testing.T) {
	t.Parallel()
	s := newTurnsState()
	s.Hold()
	ran := make(chan struct{}, 1)
	s.Spawn(func(ctx context.Context) { ran <- struct{}{} })
	select {
	case <-ran:
		t.Fatal("held work ran before Idle")
	default:
	}
	s.Idle()
	select {
	case <-ran:
	default:
		t.Fatal("Idle did not run the held work")
	}
	s.Close()
}
