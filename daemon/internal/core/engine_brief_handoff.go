package core

import (
	"path/filepath"
	"strings"
)

// BriefHandoffFile is the record in an attempt's folder an interactive session
// writes its launch's token to when it hands the work back.
const BriefHandoffFile = "handoff"

// BriefHandoff is the section an interactive stage's prompt ends with: the
// runner's interface stays open after a turn, so the agent says when its output
// is final by writing token to the attempt folder's handoff record, renamed into
// place so the record is never read half written. The Outcome line still decides
// where the run goes.
func BriefHandoff(attemptFolder, token string) string {
	record := filepath.Join(attemptFolder, BriefHandoffFile)
	partial := record + ".tmp"
	quote := func(text string) string { return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'" }
	return "\n## Handing the work back\n\n" +
		"This session stays open after you reply, and a person may watch or type to you here. Beltline only reads your output once you hand the work back, and nothing ends the stage before that.\n\n" +
		"When your output file is final, Outcome line included, hand the work back as your very last step by running exactly:\n\n" +
		"```sh\nprintf '%s\\n' " + quote(token) + " > " + quote(partial) + " && mv " + quote(partial) + " " + quote(record) + "\n```\n\n" +
		"Don't run it while work is left or while you are waiting for an answer in this session. A question for the person who started the run belongs in your output, with the outcome that holds the run for them, before you hand back. After handing back, stop: don't wait for a reply.\n"
}
