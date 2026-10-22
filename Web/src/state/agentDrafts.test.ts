import { describe, expect, it } from "vitest";
import type { Agent } from "../api/wire.gen";
import { runnerSwitchDraft } from "./agentDrafts";

function agent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "build", version: 2, name: "Build", runner: "claude", model: "opus", thinking: "high", alternates: [],
    ...overrides
  } as Agent;
}

describe("runner switch draft", () => {
  it("applies the switched runner, model and thinking while keeping unrelated typing", () => {
    const base = agent();
    const latest = agent({ name: "Build, renamed" });
    const switched = agent({ runner: "codex", model: "gpt", thinking: "low" });
    expect(runnerSwitchDraft(latest, base, switched)).toEqual({ ...latest, runner: "codex", model: "gpt", thinking: "low" });
  });

  it("leaves the draft alone when its runner settings changed while the switch ran", () => {
    const base = agent();
    expect(runnerSwitchDraft(agent({ model: "sonnet" }), base, agent({ model: "gpt" }))).toBeUndefined();
  });

  it("applies only the alternates for a switch on an alternate slot", () => {
    const base = agent();
    const latest = agent({ name: "Edited" });
    const switched = agent({ alternates: [{ runner: "codex", model: "gpt" } as Agent["alternates"][number]] });
    expect(runnerSwitchDraft(latest, base, switched, 0)).toEqual({ ...latest, alternates: switched.alternates });
  });

  it("refuses when the draft is gone, is a different version, or its alternates changed", () => {
    const base = agent();
    expect(runnerSwitchDraft(undefined, base, agent())).toBeUndefined();
    expect(runnerSwitchDraft(agent({ version: 3 }), base, agent())).toBeUndefined();
    expect(runnerSwitchDraft(agent({ alternates: [{ runner: "codex" } as Agent["alternates"][number]] }), base, agent(), 0)).toBeUndefined();
  });
});
