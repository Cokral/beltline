package core

import (
	"crypto/rand"
	"encoding/binary"
	"math/bits"
	"slices"
	"strings"
)

// ModelPoolEntries is the settings a stage's fresh sessions can start with: its
// own settings first, then its alternates, each once.
func ModelPoolEntries(runner Backend, model string, thinking *Thinking, alternates []SessionSettings) []SessionSettings {
	own := runner
	entries := []SessionSettings{{Runner: &own, Model: model, Thinking: CopyPointer(thinking)}}
	for _, alternate := range alternates {
		normalized := modelPoolNormalized(alternate)
		if !modelPoolContains(entries, normalized) {
			entries = append(entries, normalized)
		}
	}
	return entries
}

// ModelPoolRNG is the random source ModelPoolPick draws from: a one-method
// interface. *PoolRandom
// implements it.
type ModelPoolRNG interface {
	Next() uint64
}

// ModelPoolPick is the settings a fresh session at the stage starts with. running
// are the settings of the other sessions that take one of the stage's slots now;
// previous is what this run's last attempt at the stage used. Entries allowed
// turns down are skipped. A tie between equally busy entries is drawn with
// random. Nil when no entry is left.
func ModelPoolPick(from, running []SessionSettings, previous *SessionSettings, allowed func(SessionSettings) bool, random ModelPoolRNG) *SessionSettings {
	var candidates []SessionSettings
	for _, entry := range from {
		normalized := modelPoolNormalized(entry)
		if allowed(normalized) {
			candidates = append(candidates, normalized)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	if previous != nil {
		normalized := modelPoolNormalized(*previous)
		if modelPoolContains(candidates, normalized) {
			return &normalized
		}
	}
	var runningNormalized []SessionSettings
	for _, settings := range running {
		runningNormalized = append(runningNormalized, modelPoolNormalized(settings))
	}
	load := make([]int, len(candidates))
	fewest := 0
	for i, entry := range candidates {
		for _, settings := range runningNormalized {
			if modelPoolSame(settings, entry) {
				load[i]++
			}
		}
		if i == 0 || load[i] < fewest {
			fewest = load[i]
		}
	}
	var tied []SessionSettings
	for i, entry := range candidates {
		if load[i] == fewest {
			tied = append(tied, entry)
		}
	}
	chosen := tied[modelPoolRandomIndex(random, uint64(len(tied)))]
	return &chosen
}

// ModelPoolCurrent is the settings this run works stage with, as the run page
// shows them: what its last attempt there used while the pool still has them,
// else the stage's own first entry.
func ModelPoolCurrent(run Run, stage Stage) SessionSettings {
	pool := make([]SessionSettings, 0)
	for _, entry := range stage.Pool() {
		pool = append(pool, modelPoolNormalized(entry))
	}
	var previous *SessionSettings
	for i := len(run.Attempts) - 1; i >= 0; i-- {
		attempt := run.Attempts[i]
		if attempt.StageID == stage.ID && attempt.Settings != nil {
			normalized := modelPoolNormalized(*attempt.Settings)
			previous = &normalized
			break
		}
	}
	if previous == nil || !modelPoolContains(pool, *previous) {
		return pool[0]
	}
	return *previous
}

// modelPoolNormalized is the same settings with an unset runner read as Pi, as
// older stages did, and the model trimmed.
func modelPoolNormalized(settings SessionSettings) SessionSettings {
	runner := ValueOr(settings.Runner, BackendPi)
	model := strings.TrimSpace(settings.Model)
	return SessionSettings{Runner: &runner, Model: model, Thinking: CopyPointer(settings.Thinking)}
}

// modelPoolSame compares SessionSettings: runner, model
// and thinking all equal, a nil one equal only to a nil one.
func modelPoolSame(a, b SessionSettings) bool {
	return a.Model == b.Model && SamePointee(a.Runner, b.Runner) && SamePointee(a.Thinking, b.Thinking)
}

// modelPoolContains reports whether one of entries is the same settings as x.
func modelPoolContains(entries []SessionSettings, x SessionSettings) bool {
	return slices.ContainsFunc(entries, func(entry SessionSettings) bool { return modelPoolSame(entry, x) })
}

// modelPoolRandomIndex is a uniform draw from 0..<count:
// Lemire's multiply-and-reject method over the generator's 64-bit output.
func modelPoolRandomIndex(random ModelPoolRNG, count uint64) int {
	value := random.Next()
	high, low := bits.Mul64(value, count)
	if low < count {
		threshold := (0 - count) % count
		for low < threshold {
			value = random.Next()
			high, low = bits.Mul64(value, count)
		}
	}
	return int(high)
}

// PoolRandom is the random source that draws between equally busy models: the
// system's, or a seeded one that draws the same way each time, for tests.
// A nil state means the system's source.
//
// Value semantics: Next replaces the state rather than writing through it, so a
// copy of a PoolRandom draws on its own.
type PoolRandom struct {
	state *uint64
}

// PoolRandomSystem is the system's random source. The
// system source is OS randomness (crypto/rand), read only when Next is called
// on it.
func PoolRandomSystem() PoolRandom {
	return PoolRandom{state: nil}
}

// NewPoolRandom is a seeded random source.
func NewPoolRandom(seed uint64) PoolRandom {
	return PoolRandom{state: &seed}
}

// Next is the next value: SplitMix64 for a seeded source, the system's for the
// system one.
func (r *PoolRandom) Next() uint64 {
	if r.state == nil {
		var buffer [8]byte
		if _, err := rand.Read(buffer[:]); err != nil {
			panic("PoolRandom.Next: system randomness unavailable: " + err.Error())
		}
		return binary.LittleEndian.Uint64(buffer[:])
	}
	next := *r.state + 0x9E3779B97F4A7C15
	r.state = &next
	z := next
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}
