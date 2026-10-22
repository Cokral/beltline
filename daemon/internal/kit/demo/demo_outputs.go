// The fixture model list and the Claude rate revision that the seeded catalogs carry. The simulated
// session output lives in internal/integrations/runner.

package demo

import (
	"context"
	"strings"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/runner"
)

// claudeRateRevision and claudeRateSource are RateTable.claude's checked date and
// source, read from Sources/BeltlineIntegrations/Resources/Rates/claude.json.
var (
	claudeRateRevision = runner.RateTableClaude().Checked
	claudeRateSource   = runner.RateTableClaude().Source
)

// fixtureModels is the model list every runner's fixture catalog reports, with
// illustrative sizes for the demo and snapshots.
func fixtureModels() []core.ModelChoice {
	models, _ := runner.FixtureModelCatalog{}.Discover(context.Background(), core.BackendPi, "")
	return models
}

// SimulatedOutput is the text a simulated agent session writes for its agent
// and outcome.
// seedLongMarkdownSample is the Markdown that BELTLINE_DEMO_LONG_MARKDOWN puts
// in place of the spec's output, so a snapshot can exercise wrapping.
func seedLongMarkdownSample() string {
	token := strings.Repeat("VeryLongIdentifier", 40)
	return "# Wrapping samples\n\n" +
		strings.Repeat("Agent output fills the available column and wraps when the column gets narrower. ", 12) + "\n\n" +
		"Agents often wrap their prose at a column of their own, as this paragraph\n" +
		"is. Those breaks give way, so the text still fills the page however wide\n" +
		"the window is.\n\n" +
		"## Section heading with a long tail " + token + "\n\n" +
		"- A list item holding one very long token " + token + " so the marker and the text have to share a row.\n\n" +
		"/Users/agent/work/" + token + "/output.md\n\n" +
		"https://example.com/" + token + "\n\n" +
		"```\n" + token + "\n```"
}
