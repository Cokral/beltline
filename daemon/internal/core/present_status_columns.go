// The status reads for a stage across its runs: the lane and column counts above a
// machine on the Floor, and the durations and clock a run shows. The types are in
// present_status.go.

package core

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// StatusColumn is the counts at a merged station, with pause state kept per belt. The
// global pause stands in for every belt, so a queued run reads `PAUSED` anywhere.
// pausedBelts is the set of paused belt ids.
func StatusColumn(here []Run, pausedBelts map[string]struct{}, pause Pause) StageStatus {
	var yours []Run
	var others []Run
	for _, run := range here {
		if RunPauseStatusCounts(run) {
			yours = append(yours, run)
		} else {
			others = append(others, run)
		}
	}
	here = others
	belts := map[string]struct{}{}
	for belt := range pausedBelts {
		belts[belt] = struct{}{}
	}
	if pause.IsPaused {
		for _, run := range here {
			belts[run.BeltID] = struct{}{}
		}
	}
	paused := len(here) > 0
	for _, run := range here {
		if _, ok := belts[run.BeltID]; !ok {
			paused = false
			break
		}
	}
	waiting, held, queued := 0, 0, 0
	var running []Run
	for _, run := range here {
		switch run.State {
		case RunStateWaiting:
			waiting++
		case RunStateHeld:
			held++
		case RunStateRunning:
			running = append(running, run)
		case RunStateQueued:
			queued++
		}
	}
	if len(here) == 0 {
		if len(yours) == 0 {
			if paused {
				return StageStatus{Text: "PAUSED", Look: StageStatusLookPlain, Face: StageStatusFacePaused}
			}
			return StageStatus{Text: "IDLE", Look: StageStatusLookPlain, Face: StageStatusFaceTodo}
		}
		return StageStatus{Text: strconv.Itoa(len(yours)) + " PAUSED", Look: StageStatusLookPlain, Face: StageStatusFacePaused}
	}
	var parts []string
	if waiting > 0 {
		parts = append(parts, strconv.Itoa(waiting)+" WAITING FOR YOU")
	}
	if held > 0 {
		parts = append(parts, strconv.Itoa(held)+" HELD")
	}
	if len(running) == 1 && len(parts) == 0 && queued == 0 {
		parts = append(parts, "RUNNING · "+running[0].Subject.Ref())
	} else if len(running) > 0 {
		parts = append(parts, strconv.Itoa(len(running))+" RUNNING")
	}
	pausedCount := 0
	for _, run := range here {
		if _, ok := belts[run.BeltID]; run.State == RunStateQueued && ok {
			pausedCount++
		}
	}
	if queued > pausedCount {
		parts = append(parts, strconv.Itoa(queued-pausedCount)+" QUEUED")
	}
	if pausedCount+len(yours) > 0 {
		parts = append(parts, strconv.Itoa(pausedCount+len(yours))+" PAUSED")
	}
	parked := len(here) - waiting - held - len(running) - queued
	if len(parts) == 0 && parked > 0 {
		parts = append(parts, strconv.Itoa(parked)+" PARKED")
	}
	look, face := StageStatusLookPlain, StageStatusFaceIdle
	if paused {
		face = StageStatusFacePaused
	}
	switch {
	case waiting > 0:
		look, face = StageStatusLookWaiting, StageStatusFaceWait
	case held > 0:
		look, face = StageStatusLookHeld, StageStatusFaceHeld
	case len(running) > 0:
		look, face = StageStatusLookRunning, StageStatusFaceRun
	}
	return StageStatus{Text: strings.Join(parts, " · "), Look: look, Face: face}
}

// StatusDuration formats seconds as "Xh Ym", "Ym Zs" or "Zs".
func StatusDuration(seconds float64) string {
	total := max(0, int(math.Round(seconds)))
	m := total / 60
	if m >= 60 {
		return strconv.Itoa(m/60) + "h " + strconv.Itoa(m%60) + "m"
	}
	if m > 0 {
		return strconv.Itoa(m) + "m " + strconv.Itoa(total%60) + "s"
	}
	return strconv.Itoa(total) + "s"
}

// StatusClock formats seconds as "MM:SS".
func StatusClock(seconds float64) string {
	total := max(0, int(seconds))
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}
