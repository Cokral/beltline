// The change projections' memo. Turns compares every resource's projection after
// each turn, so a projection that
// re-encodes its state every time costs a full encoding per turn, idle or not.
// A projection that reads state with a revision (a counter every write to that
// state bumps) keeps its encoding with the revision, and re-encodes only when the
// revision moved.

package coordinator

import "sync/atomic"

// projectionMemo keeps a projection's encoding with the key it was made at: the
// revision (or the pair of revisions) of every piece of state the projection
// reads. Lock: runs with c.mu held, like the state it reads.
type projectionMemo[K comparable] struct {
	key  K
	data []byte
	ok   bool
}

// of returns the encoding at key. It encodes again when key moved since the
// last call, or on the first call. A test build checks some memo hits against a
// fresh encoding (see shouldVerifyMemo), so a write that forgot its bump panics
// there instead of hiding a change.
func (m *projectionMemo[K]) of(name string, key K, encode func() []byte) []byte {
	if !m.ok || m.key != key {
		m.data = encode()
		m.key = key
		m.ok = true
	} else if shouldVerifyMemo() {
		verifyMemo(name, m.data, encode())
	}
	return m.data
}

// verifyProjectionMemos makes every memoised projection check itself against a
// fresh encoding (tests set it in TestMain): a write that forgot to bump its
// revision panics instead of leaving a browser stale.
var verifyProjectionMemos bool

// verifyCalls counts memo hits; one in verifyEvery is checked. A memo left
// stale by a missed bump stays stale until the next bump, so a sampled check
// still finds it, at a quarter of the cost.
var verifyCalls atomic.Uint64

// verifyEvery is how many memo hits pass between two checks.
const verifyEvery = 4

// verifyAllHits makes every memo hit a check, for tests that must see each
// write's memo verified (see TestEveryProjectedWriteIsReported).
var verifyAllHits atomic.Bool

func shouldVerifyMemo() bool {
	if !verifyProjectionMemos {
		return false
	}
	return verifyAllHits.Load() || verifyCalls.Add(1)%verifyEvery == 0
}

func verifyMemo(name string, memo, fresh []byte) {
	if string(memo) != string(fresh) {
		panic("coordinator: the " + name + " projection changed without a revision bump")
	}
}
