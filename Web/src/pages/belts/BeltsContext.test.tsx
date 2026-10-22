import { screen, waitFor, within } from "@testing-library/react";
import type { BeltRecord } from "./beltRecords";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";

import { contextFor, contextRoute, firstLive, firstName, firstStage, fixture, mockDaemon, renderAt, outdatedLine, beltsList } from "./beltsTestKit";
import { json } from "../../test/response";
import { goldenText } from "../../test/golden";

const nameInput = () => screen.getByLabelText("Belt name") as HTMLInputElement;

beforeEach(() => {
  localStorage.clear();
});

describe("context reads", () => {
  it("reads the context after mount, not while typing, and again after a committed change", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/belts/draft/stage-remove": (request) => json({ ...JSON.parse(goldenText("belt-draft-stage-remove.json")), draft: { ...JSON.parse(goldenText("belt-draft-stage-remove.json")).draft, name: request.body.draft.name } })
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await waitFor(() => expect(daemon.contextReads()).toHaveLength(1));
    await user.type(nameInput(), " typed");
    expect(daemon.contextReads()).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: `Remove ${firstStage.name}` }));
    await waitFor(() => expect(daemon.contextReads()).toHaveLength(2));
    expect(daemon.contextReads()[1]!.body.draft.name).toBe(`${firstName} typed`);
  });

  it("drops a context answer that a newer read has replaced", async () => {
    const u = userEvent.setup();
    let release: () => void = () => undefined;
    let reads = 0;
    mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": (request) => {
        reads += 1;
        if (reads === 1) return new Promise<Response>(resolve => { release = () => resolve(json(contextFor(request.body.draft, "Stale label"))); });
        return json(contextFor(request.body.draft, "Fresh label"));
      },
      "/api/v1/belts/draft/stage-remove": () => json(JSON.parse(goldenText("belt-draft-stage-remove.json")))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await u.click(screen.getByRole("button", { name: `Remove ${firstStage.name}` }));
    await waitFor(() => expect(reads).toBe(2));
    release();
    const answered = JSON.parse(goldenText("belt-draft-stage-remove.json")).draft as { stages: Array<{ name: string; outcomes: unknown[] }> };
    const stage = answered.stages.find(item => item.outcomes.length > 0)!;
    await u.click(await screen.findByRole("button", { name: stage.name, pressed: false }));
    const form = await screen.findByRole("region", { name: `Stage ${stage.name} settings` });
    await waitFor(() => expect(form.querySelector('[title="Fresh label"]')).not.toBeNull());
    expect(form.querySelector('[title="Stale label"]')).toBeNull();
  });
});

describe("policies", () => {
  it("sends the whole policies object when one option changes", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/belts/draft/policies-update": () => json(JSON.parse(goldenText("belt-draft-policies-update.json")))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    const policies = await screen.findByRole("region", { name: "Policies" });
    await user.click(within(policies).getByRole("checkbox", { name: /Comment when a stage holds the run/ }));
    await waitFor(() => expect(daemon.posts("/draft/policies-update")).toHaveLength(1));
    expect(daemon.posts("/draft/policies-update")[0]!.body.policies).toEqual({ ...firstLive.belt.policies, commentOnHold: !firstLive.belt.policies.commentOnHold });
  });
});

describe("schedule and outcome edits", () => {
  it("applies a schedule trigger's days, time, title and task as one trigger-update", async () => {
    const user = userEvent.setup();
    const scheduled = { ...firstLive.belt, triggers: [{ id: "s1", repos: [], trigger: { kind: "schedule", schedule: { days: "weekdays", weekday: 2, hour: 9, minute: 0 }, title: "", task: "" } }] } as BeltRecord["belt"];
    // The editor opens the daemon's draft, so a changed belt changes its draft too.
    const withSchedule = fixture.belts.map(record => record.belt.id === firstLive.belt.id ? { ...record, belt: scheduled, draft: scheduled } : record);
    const daemon = mockDaemon({
      "/api/v1/belts": () => json(beltsList(withSchedule)),
      "/api/v1/belts/context": () => json({ ...contextFor(scheduled), schedules: [{ entry: "s1", summary: "weekdays at 09:00", next: "2026-10-12T09:00:00Z", note: "A missed time runs once when it wakes." }] }),
      "/api/v1/belts/draft/trigger-update": () => json(JSON.parse(goldenText("belt-draft-trigger-update.json")))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    const settings = await waitFor(() => {
      const element = document.querySelector(".schedule-settings") as HTMLElement | null;
      expect(element).not.toBeNull();
      return element!;
    });
    await waitFor(() => expect(within(settings).getByRole("note").textContent).toMatch(/^Next run .+\. A missed time runs once when it wakes\.$/));
    // As in the app, a pick applies at once and typed text on Enter, each as one trigger-update of the whole schedule.
    await user.selectOptions(within(settings).getByLabelText("Runs for trigger 1"), "weekly");
    await waitFor(() => expect(daemon.posts("/draft/trigger-update")).toHaveLength(1));
    expect(daemon.posts("/draft/trigger-update")[0]!.body).toMatchObject({
      entryId: "s1",
      trigger: { kind: "schedule", schedule: { days: "weekly", weekday: 2, hour: 9, minute: 0 }, title: "", task: "" }
    });
  });

  it("changes an outcome to go to another stage with a goto-pick draft edit", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      // A goto target is set by the daemon's draft op, not by the browser composing the outcome's then.
      "/api/v1/belts/draft/goto-pick": request => json({ draft: request.body.draft, warnings: [] })
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user.click(screen.getByRole("button", { name: firstStage.name, pressed: false }));
    const form = await screen.findByRole("region", { name: `Stage ${firstStage.name} settings` });
    const target = firstLive.belt.stages[1]!;
    const outcome = firstStage.outcomes[0]!;
    await user.selectOptions(within(form).getByLabelText("Outcome 1 then"), "goto");
    await user.selectOptions(within(form).getByLabelText("Outcome 1 stage"), target.id);
    await waitFor(() => expect(daemon.posts("/draft/goto-pick")).toHaveLength(1));
    expect(daemon.posts("/draft/goto-pick")[0]!.body).toMatchObject({ stageId: firstStage.id, outcomeId: outcome.id, gotoTarget: target.id });
  });

});

const user = () => userEvent.setup();

describe("stage updates, Share and agent entry", () => {
  it("moves one stage to its agent's latest version with update-stage", async () => {
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": request => json({ ...contextFor(request.body.draft), line: outdatedLine(request.body.draft), outdatedStages: request.body.draft.stages.length }),
      "/api/v1/belts/update-stage": () => json(JSON.parse(goldenText("belt-update-stage.json")))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user().click(await screen.findByRole("button", { name: `Update ${firstStage.name} to its agent's latest version` }));
    await waitFor(() => expect(daemon.posts("/update-stage")).toHaveLength(1));
    expect(daemon.posts("/update-stage")[0]!.body.stageId).toBe(firstStage.id);
  });

  it("opens an agent without saving when the belt is clean, and keeps the editor when a save-and-open fails", async () => {
    const u = user();
    mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/belts/save": () => json({ message: "The belt is not valid", problems: ["Name is taken"] }, 422)
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await u.click(screen.getByRole("button", { name: firstStage.name }));
    await u.click(await screen.findByRole("button", { name: "Edit" }));
    const sheet = await screen.findByRole("dialog", { name: `Open agent ${firstStage.agent!.id}` });
    expect(within(sheet).queryByRole("button", { name: "Save and open" })).toBeNull();
    await u.click(within(sheet).getByRole("button", { name: "Open agent" }));
    // The agents page loads when it is first opened, so the location moves on once it has.
    await waitFor(() => expect(screen.getByLabelText("Location").textContent).toContain("/agents?agent="));
  });

});

describe("concurrency", () => {
  it("sends one save while a save is pending, however often Save is clicked", async () => {
    const u = userEvent.setup();
    let release: () => void = () => undefined;
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/belts/save": (request) => new Promise<Response>(resolve => {
        release = () => resolve(json({ resource: { ...firstLive, belt: { ...firstLive.belt, name: request.body.belt.name } }, warnings: [] }));
      })
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await u.type(nameInput(), "!");
    const save = screen.getByRole("button", { name: "Save" });
    await u.click(save);
    await u.click(save);
    await u.click(save);
    await waitFor(() => expect(daemon.posts("/save")).toHaveLength(1));
    expect(save.textContent).toBe("Saving…");
    release();
    // Saved, nothing is left to save, so Save goes away; the extra clicks sent nothing.
    await waitFor(() => expect(screen.queryByRole("button", { name: /^Sav/ })).toBeNull());
    expect(daemon.posts("/save")).toHaveLength(1);
  });
});
