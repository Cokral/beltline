package core

// erasureCopyRemoved says what erasure removes. Copy from ErasureCopy.message.
const erasureCopyRemoved = "Saved history, outputs, artifacts, AFK decisions, drafts and local checkouts will be permanently removed."

// erasureCopyExternal says that external work is untouched. Copy from ErasureCopy.message.
const erasureCopyExternal = "External work is unchanged."

// erasureCopyReplayNote is told of manual and scheduled runs: the task is not replayed. Copy from ErasureCopy.message.
const erasureCopyReplayNote = "Erasure does not replay the task."

// erasureCopyReturnNote is told of polled work: it may return on the next check. Copy from ErasureCopy.message.
const erasureCopyReturnNote = "Eligible polled work may return on the next check."

// ErasureCopyMessage is the confirmation shown before a finished or dropped run is
// erased. It names the subject and belt, and says what erasure removes. Manual and
// scheduled runs are told that the task is not replayed; polled work is told that
// it may return on the next check.
func ErasureCopyMessage(run Run) string {
	who := run.Subject.Ref() + " " + run.Subject.Title + " on the " + run.BeltName + " belt"
	removed := erasureCopyRemoved
	external := erasureCopyExternal
	switch run.Subject.Kind {
	case SubjectKindManual, SubjectKindScheduled:
		return who + ". " + removed + " " + erasureCopyReplayNote + " " + external
	default:
		return who + ". " + removed + " " + erasureCopyReturnNote + " " + external
	}
}
