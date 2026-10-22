// The help text of the AFK button of a run, given the run and the global switch.

package core

// RunAfkButtonHelp is the hover help of a run's AFK button: what its mode means now
// (on is whether AFK is on for it, which the global mode decides) and where a click goes.
func RunAfkButtonHelp(mode AfkMode, on bool) string {
	var explanation string
	switch mode {
	case AfkModeAlways:
		explanation = "This run is in AFK mode, whatever the global switch."
	case AfkModeNever:
		explanation = "This run waits for you, whatever the global switch."
	default:
		state := "off"
		if on {
			state = "on"
		}
		explanation = "This run follows the global AFK switch, which is " + state + "."
	}
	return explanation + " Click for " + mode.Next().Label() + "."
}
