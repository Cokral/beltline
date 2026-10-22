package coordinator

import "github.com/robbertmanders/beltline/daemon/internal/core"

// stageLine is the stage line of a belt's draft: its blocks, fork boxes and where
// each "+" inserts a stage, read against the library's agents (core.StageLineOf).
// Lock: runs with c.mu held (it reads the library).
func (c *Coordinator) stageLine(belt core.Belt) core.StageLinePresentation {
	return core.StageLineOf(belt, c.library.agents)
}
