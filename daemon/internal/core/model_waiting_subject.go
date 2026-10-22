package core

// WaitingSubject is an issue or ticket a trigger listed but doesn't start a run
// on yet, and why. Kept with the model so the daemon's read projections can
// carry it without depending on the coordinator.
//
// Plain struct: callers write the literal.
type WaitingSubject struct {
	BeltID  string
	Subject Subject
	Reason  string
}
