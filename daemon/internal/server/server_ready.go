// The Ready one-shot
// result that a waiter may ask for before or after it arrives.
package server

import "sync"

// ready is a one-shot result: a waiter may ask before or after it arrives. It
// is guarded by a mutex, with one continuation.
type ready struct {
	done chan struct{}
	once sync.Once
	addr string
	err  error
}

// newReady builds the ready from its arguments.
func newReady() *ready {
	return &ready{done: make(chan struct{})}
}

// finish records the result once and wakes the waiter.
// The first finish wins, as the pending-to-done transition does.
func (r *ready) finish(addr string, err error) {
	r.once.Do(func() {
		r.addr = addr
		r.err = err
		close(r.done)
	})
}

// wait blocks until finish, returning the bound address or
// the error it was finished with.
func (r *ready) wait() (string, error) {
	<-r.done
	return r.addr, r.err
}
