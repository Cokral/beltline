import { screen, waitFor, within } from "@testing-library/react";
import type { Thinking } from "../../api/wire.gen";
import type { BeltRecord } from "./beltRecords";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { contextFor, contextRoute, firstLive, firstName, firstStage, fixture, goldenContext, mockDaemon as mockBaseDaemon, renderAt, type Handler, beltsList } from "./beltsTestKit";
import { json } from "../../test/response";
import { goldenText } from "../../test/golden";

beforeEach(() => {
  localStorage.clear();
});

const belts = (records: BeltRecord[] = fixture.belts) => () => json(beltsList(records));
const answer = (name: string) => JSON.parse(goldenText(name));
const draftAnswer = (name: string): Handler => request => json({ ...answer(name), draft: { ...answer(name).draft, name: request.body.draft.name } });
const models = { "/api/v1/models/pi": goldenText("models.json") };

function mockDaemon(routes: Record<string, Handler | string>) {
  return mockBaseDaemon({
    "/api/v1/belts/save": request => json({ resource: { ...firstLive, belt: request.body.belt, draft: request.body.belt }, warnings: [] }),
    ...routes
  });
}

/** The fixture with one belt replaced, selected by the URL. */
function withBelt(belt: BeltRecord["belt"]) {
  // The editor opens the daemon's draft, so a changed belt changes its draft too.
  return fixture.belts.map(record => record.belt.id === belt.id ? { ...record, belt, draft: belt } : record);
}

/** The stage form's cards, by their names, in the order they are drawn. */
const cardNames = (form: HTMLElement) => [...form.querySelectorAll<HTMLElement>(":scope > section")].map(card => card.getAttribute("aria-label"));
/** The preview's title and text, as the daemon supplied them. */
const previewOf = (form: HTMLElement) => ({ title: form.querySelector(".outcome-preview .paper-tag")!.textContent, text: form.querySelector(".outcome-preview pre")!.textContent });
const expectedPreview = { title: goldenContext.stage.preview.title, text: goldenContext.stage.preview.lines.join("\n") };

async function openStage(user: ReturnType<typeof userEvent.setup>, name: string) {
  await user.click(screen.getByRole("button", { name, pressed: false }));
  const form = await screen.findByRole("region", { name: `Stage ${name} settings` });
  await waitFor(() => expect(form.getAttribute("data-ready")).toBe("true"));
  return form;
}

/** Renders the belt at `path` and opens its first stage, with the stage menu and models answered. */
async function openFirst(user: ReturnType<typeof userEvent.setup>, routes: Record<string, Handler | string>, path = "/belts", belt = firstName) {
  const daemon = mockDaemon({ "/api/v1/belts": belts(), "/api/v1/belts/context": contextRoute(), ...models, ...routes });
  renderAt(path);
  await screen.findByRole("heading", { level: 2, name: belt });
  return { daemon, form: await openStage(user, firstStage.name) };
}

describe("agent stage settings", () => {
  it("shows the pinned agent and its inherited brief, and sends an edited brief or Use the agent's brief as the override", async () => {
    const user = userEvent.setup();
    const stage = firstStage.agent!;
    const { daemon, form } = await openFirst(user, { "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json") });
    const pin = form.querySelector("[data-agent-pin]") as HTMLElement;
    expect(within(pin).getByText(`Agent ${stage.id}`)).toBeTruthy();
    expect(within(pin).getByText(`v${stage.version}`)).toBeTruthy();
    const brief = within(form).getByLabelText("Brief") as HTMLTextAreaElement;
    expect(brief.value).toBe(`The ${stage.id} brief.`);
    expect(within(form).getByText(/Edit it to change it for this belt only/)).toBeTruthy();
    await user.type(brief, " Be brief.");
    expect(within(form).getByText(/Changed for this belt/)).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")[0]!.body.update.brief).toBe(`The ${stage.id} brief. Be brief.`);
  });

  it("sends Use the agent's brief as an empty brief override", async () => {
    const user = userEvent.setup();
    const overridden = { ...firstLive.belt, stages: firstLive.belt.stages.map((stage, index) => index === 0 ? { ...stage, brief: "My own brief" } : stage) };
    const daemon = mockDaemon({ "/api/v1/belts": belts(withBelt(overridden)), "/api/v1/belts/context": contextRoute(), ...models, "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json") });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    const form = await openStage(user, firstStage.name);
    expect((within(form).getByLabelText("Brief") as HTMLTextAreaElement).value).toBe("My own brief");
    await user.click(within(form).getByRole("button", { name: "Use the agent's brief" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")[0]!.body.update.brief).toBe("");
  });

  it("sends changed settings with Save: runner, slots, workspace, key, model, thinking, fallback and alternates", async () => {
    const user = userEvent.setup();
    const { daemon, form } = await openFirst(user, { "/api/v1/models/claude": goldenText("models.json"), "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json") });
    const runner = within(form).getByLabelText("Harness") as HTMLSelectElement;
    expect([...runner.options].map(option => option.value)).toEqual(["copilot", "pi", "claude", "codex", "cursor"]);
    await user.selectOptions(runner, "claude");
    await user.clear(within(form).getByLabelText("Slots"));
    await user.type(within(form).getByLabelText("Slots"), "3");
    await user.selectOptions(within(form).getByLabelText("Workspace"), "worktree");
    await user.clear(within(form).getByLabelText("Placeholder key"));
    await user.type(within(form).getByLabelText("Placeholder key"), "notes");
    await waitFor(() => expect((within(form).getByLabelText("Model") as HTMLSelectElement).disabled).toBe(false));
    await user.selectOptions(within(form).getByLabelText("Model"), "opus-5.5-1m");
    await user.selectOptions(within(form).getByLabelText("Thinking"), "high");
    await user.selectOptions(within(form).getByLabelText("When the stage fails"), "retry");
    await user.click(within(form).getByRole("button", { name: "Add a model" }));
    await user.selectOptions(within(form).getByLabelText("Alternate 1 harness"), "claude");
    await waitFor(() => expect((within(form).getByLabelText("Alternate 1 model") as HTMLSelectElement).disabled).toBe(false));
    await user.selectOptions(within(form).getByLabelText("Alternate 1 model"), "opus-5.5-250k");
    await user.selectOptions(within(form).getByLabelText("Alternate 1 thinking"), "low");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")[0]!.body.update).toMatchObject({
      runner: "claude", slots: 3, workspace: "worktree", key: "notes", model: "opus-5.5-1m", thinking: "high", fallback: "retry",
      alternates: [{ runner: "claude", model: "opus-5.5-250k", thinking: "low" }]
    });
  });

  it("clears a thinking level the runner does not take with the default, and shows a level the runner does not list", async () => {
    const user = userEvent.setup();
    const belt = { ...firstLive.belt, stages: [{ ...firstStage, thinking: "ultra" as Thinking }, ...firstLive.belt.stages.slice(1)] };
    const { daemon, form } = await openFirst(user, { "/api/v1/belts": belts(withBelt(belt)), "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json") });
    const thinking = within(form).getByLabelText("Thinking") as HTMLSelectElement;
    expect(thinking.value).toBe("ultra");
    expect(within(thinking).getByRole("option", { name: "ultra (not taken by Pi)" })).toBeTruthy();
    await user.selectOptions(thinking, "");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    const update = daemon.posts("/draft/stage-update")[0]!.body.update;
    expect(update.clearThinking).toBe(true);
    expect(update.thinking).toBeUndefined();
  });

  it("keeps a model the runner does not allow, labelled, and keeps the thinking level when the models cannot be read", async () => {
    const user = userEvent.setup();
    const belt = { ...firstLive.belt, stages: [{ ...firstStage, model: "gone-model", thinking: "high" as Thinking }, ...firstLive.belt.stages.slice(1)] };
    const { form } = await openFirst(user, { "/api/v1/belts": belts(withBelt(belt)), "/api/v1/models/codex": () => json(goldenText("error-failure.json"), 500) });
    const model = within(form).getByLabelText("Model") as HTMLSelectElement;
    await waitFor(() => expect(model.value).toBe("gone-model"));
    expect(within(model).getByRole("option", { name: "gone-model (not allowed)" })).toBeTruthy();
    await user.selectOptions(within(form).getByLabelText("Harness"), "codex");
    expect(await within(form).findByRole("alert")).toBeTruthy();
    expect((within(form).getByLabelText("Model") as HTMLSelectElement).value).toBe("gone-model");
    expect((within(form).getByLabelText("Thinking") as HTMLSelectElement).value).toBe("high");
  });

  it("says when the pinned version is archived and offers a newer one through update-stage", async () => {
    const user = userEvent.setup();
    const { daemon, form } = await openFirst(user, {
      "/api/v1/belts/context": request => {
        const context = contextFor(request.body.draft, undefined, request.body.stageId);
        return json(context.agent ? { ...context, agent: { ...context.agent, archived: true, latestVersion: 9 } } : context);
      },
      "/api/v1/belts/update-stage": () => json(answer("belt-update-stage.json"))
    });
    expect(within(form).getByText("Archived: kept for this stage, not offered for new ones.")).toBeTruthy();
    await user.click(within(form).getByRole("button", { name: "Update to v9" }));
    await waitFor(() => expect(daemon.posts("/update-stage")).toHaveLength(1));
    expect(daemon.posts("/update-stage")[0]!.body.stageId).toBe(firstStage.id);
  });

  it("adds, edits and removes an alternate, and leaves the primary model alone", async () => {
    const user = userEvent.setup();
    const { form } = await openFirst(user, { "/api/v1/models/claude": goldenText("models.json") });
    const primaryModel = within(form).getByRole("combobox", { name: "Model" }) as HTMLSelectElement;
    await waitFor(() => expect(primaryModel.disabled).toBe(false));
    const primary = () => [within(form).getByRole("combobox", { name: "Harness" }), primaryModel, within(form).getByRole("combobox", { name: "Thinking" })].map(select => (select as HTMLSelectElement).value);
    const before = primary();
    await user.click(within(form).getByRole("button", { name: "Add a model" }));
    await user.selectOptions(within(form).getByRole("combobox", { name: "Alternate 1 harness" }), "claude");
    await waitFor(() => expect((within(form).getByRole("combobox", { name: "Alternate 1 model" }) as HTMLSelectElement).disabled).toBe(false));
    const alternateModel = within(form).getByRole("combobox", { name: "Alternate 1 model" }) as HTMLSelectElement;
    const other = Array.from(alternateModel.options).find(option => !option.disabled && option.value !== "" && option.value !== alternateModel.value)!;
    await user.selectOptions(alternateModel, other.value);
    expect(alternateModel.value).toBe(other.value);
    expect(within(form).getByRole("combobox", { name: "Alternate 1 thinking" })).toBeTruthy();
    expect(primary()).toEqual(before);
    await user.click(within(form).getByRole("button", { name: "Remove alternate 1" }));
    expect(within(form).queryByRole("combobox", { name: "Alternate 1 model" })).toBeNull();
    expect(primary()).toEqual(before);
  });

  it("sends edited outcomes and the failure fallback with Save", async () => {
    const user = userEvent.setup();
    const { daemon, form } = await openFirst(user, { "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json") });
    const outcome = within(form).getByLabelText("Outcome 1 name");
    await user.clear(outcome);
    await user.type(outcome, "Ready to ship");
    await user.selectOptions(within(form).getByLabelText("When the stage fails"), "drop");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    const update = daemon.posts("/draft/stage-update")[0]!.body.update;
    expect(update.fallback).toBe("drop");
    expect(update.outcomes[0].name).toBe("Ready to ship");
  });

  it("shows the placeholder help, the daemon's preview, and a pinned version that is missing", async () => {
    const user = userEvent.setup();
    const { form } = await openFirst(user, {
      "/api/v1/belts/context": request => {
        const context = contextFor(request.body.draft, undefined, request.body.stageId);
        return json({ ...context, placeholders: goldenContext.placeholders, agent: context.agent && { ...context.agent, missing: true } });
      }
    });
    expect(within(form).getByText("This version is missing from the library.")).toBeTruthy();
    const brief = form.querySelector<HTMLElement>(":scope > section[aria-label='Stage brief']")!;
    const chips = within(brief).getByRole("list", { name: "Placeholders" });
    expect(within(chips).getByText(`{${goldenContext.placeholders.entries[0].name}}`)).toBeTruthy();
    expect(within(brief).getByText(goldenContext.placeholders.prose.introduction)).toBeTruthy();
    expect(previewOf(form)).toEqual(expectedPreview);
  });

  it("opens the agent from the pin through the agent entry", async () => {
    const user = userEvent.setup();
    const { form } = await openFirst(user, {});
    await user.click(within(form).getByRole("button", { name: "Edit" }));
    const sheet = await screen.findByRole("dialog", { name: `Open agent ${firstStage.agent!.id}` });
    await user.click(within(sheet).getByRole("button", { name: "Open agent" }));
    // The agents page loads when it is first opened, so the location moves on once it has.
    await waitFor(() => expect(screen.getByLabelText("Location").textContent).toBe(`/agents?agent=${encodeURIComponent(`${firstStage.agent!.id}@${firstStage.agent!.version}`)}`));
  });
});

describe("approval and watch stages", () => {
  const tasks = fixture.belts.find(record => record.belt.id === "tasks")!;
  const approval = tasks.belt.stages.find(stage => stage.type === "approve")!;
  const watch = tasks.belt.stages.find(stage => stage.type === "watch")!;

  it("shows an approval's decider and button outcomes, without runner, model or brief", async () => {
    const user = userEvent.setup();
    mockDaemon({ "/api/v1/belts": belts(), "/api/v1/belts/context": contextRoute() });
    renderAt("/belts?belt=tasks");
    await screen.findByRole("heading", { level: 2, name: tasks.belt.name });
    const form = await openStage(user, approval.name);
    expect((within(form).getByLabelText("Decided by") as HTMLSelectElement).value).toBe("you");
    expect(within(form).queryByLabelText("Harness")).toBeNull();
    expect(within(form).queryByLabelText("Model")).toBeNull();
    expect(within(form).queryByLabelText("Brief")).toBeNull();
    expect((within(form).getByLabelText("Outcome 1 when") as HTMLTextAreaElement).placeholder).toBe("Button label");
    expect(cardNames(form)).toEqual(["Stage settings", "Stage outcomes"]);
    expect(previewOf(form)).toEqual(expectedPreview);
  });

  it("changes what a watch waits on at once, and offers the target's events as outcomes", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({ "/api/v1/belts": belts(), "/api/v1/belts/context": contextRoute(), "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json") });
    renderAt("/belts?belt=tasks");
    await screen.findByRole("heading", { level: 2, name: tasks.belt.name });
    const form = await openStage(user, watch.name);
    expect(within(form).getByText("Watches the run's pull request.")).toBeTruthy();
    expect(within(form).queryByLabelText("Harness")).toBeNull();
    const when = within(form).getByLabelText("Outcome 1 when") as HTMLSelectElement;
    expect([...when.options].map(option => option.textContent)).toContain("the pull request was merged");
    await user.selectOptions(within(form).getByLabelText("Waits on"), "ticket");
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")[0]!.body).toMatchObject({ stageId: watch.id, update: { watch: "ticket" } });
  });
});

describe("outcome destinations and placement", () => {
  const nested = fixture.belts.find(record => record.belt.id === "nested")!;
  const verify = nested.belt.stages.find(stage => stage.id === "verify")!;
  const assess = nested.belt.stages.find(stage => stage.id === "assess")!;
  const placement = { off: "None: on the line after the fork", routes: [{ id: "build", label: "Build" }, { id: "skip", label: "Skip" }] };

  it("sends a goto pick and a route's stage to the daemon's draft ops, which add the counterparts and clear a route to none", async () => {
    const user = userEvent.setup();
    // The daemon answers each draft op with the draft it made; the browser keeps only what it sent.
    const echo: Handler = request => json({ draft: request.body.draft, warnings: [] });
    const daemon = mockDaemon({
      "/api/v1/belts": belts(),
      "/api/v1/belts/context": contextRoute(undefined, {
        gotoTargets: [
          { id: "deep-build", label: "Build · Build / Build", counterparts: ["quick-build"], others: [{ route: { id: "quick", label: "Build / Quick" }, stages: [{ id: "quick-build", label: "Build / Quick: Quick build" }] }] },
          { id: "triage", label: "Triage", counterparts: [], others: [] }
        ]
      }),
      ...models,
      "/api/v1/belts/draft/goto-pick": echo,
      "/api/v1/belts/draft/goto-route": echo
    });
    renderAt("/belts?belt=nested");
    await screen.findByRole("heading", { level: 2, name: nested.belt.name });
    const form = await openStage(user, verify.name);
    // Outcome 2 already goes to deep-build with its quick-build counterpart, as the draft holds it.
    expect((within(form).getByLabelText("Outcome 2 stage") as HTMLSelectElement).value).toBe("deep-build");
    expect((within(form).getByLabelText("Outcome 2 on Build / Quick") as HTMLSelectElement).value).toBe("quick-build");
    await user.selectOptions(within(form).getByLabelText("Outcome 2 on Build / Quick"), "");
    await waitFor(() => expect(daemon.posts("/draft/goto-route")).toHaveLength(1));
    expect(daemon.posts("/draft/goto-route")[0]!.body).toMatchObject({ stageId: verify.id, outcomeId: verify.outcomes[1]!.id, routeId: "quick", gotoTarget: "" });
    await user.selectOptions(within(form).getByLabelText("Outcome 1 then"), "goto");
    await user.selectOptions(within(form).getByLabelText("Outcome 1 stage"), "deep-build");
    await waitFor(() => expect(daemon.posts("/draft/goto-pick")).toHaveLength(1));
    expect(daemon.posts("/draft/goto-pick")[0]!.body).toMatchObject({ stageId: verify.id, outcomeId: verify.outcomes[0]!.id, gotoTarget: "deep-build" });
  });

  it("takes several routes one by one, keeps a saved route that is unavailable here, and falls back to Next stage when none is left", async () => {
    const user = userEvent.setup();
    const triage = nested.belt.stages.find(stage => stage.id === "triage")!;
    const daemon = mockDaemon({
      "/api/v1/belts": belts(),
      "/api/v1/belts/context": contextRoute(undefined, {
        actions: ["next", "goto", "route", "retry", "hold", "park", "done", "ship", "drop"].map(id => ({ id, label: id })),
        routesAfter: [{ id: "build", label: "Build" }],
        routePaths: [{ id: "build", label: "Build" }, { id: "skip", label: "Skip" }]
      }),
      ...models,
      "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json")
    });
    renderAt("/belts?belt=nested");
    await screen.findByRole("heading", { level: 2, name: nested.belt.name });
    const form = await openStage(user, triage.name);
    const routes = within(form).getByRole("group", { name: "Outcome 1 routes" });
    expect(within(routes).getByRole("checkbox", { name: "Skip (unavailable here)" })).toBeTruthy();
    await user.click(within(routes).getByRole("checkbox", { name: "Skip (unavailable here)" }));
    expect((within(form).getByLabelText("Outcome 1 then") as HTMLSelectElement).value).toBe("next");
    await user.selectOptions(within(form).getByLabelText("Outcome 1 then"), "route");
    const again = within(form).getByRole("group", { name: "Outcome 1 routes" });
    expect(within(again).queryByRole("checkbox", { name: /unavailable/ })).toBeNull();
    await user.click(within(again).getByRole("checkbox", { name: "Build" }));
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")[0]!.body.update.outcomes[0].then).toMatchObject({ do: "route", route: ["build"] });
  });

  it("offers Ship beside Done for an outcome and saves it as the ship action", async () => {
    const user = userEvent.setup();
    const triage = nested.belt.stages.find(stage => stage.id === "triage")!;
    const daemon = mockDaemon({
      "/api/v1/belts": belts(),
      "/api/v1/belts/context": contextRoute(),
      ...models,
      "/api/v1/belts/draft/stage-update": draftAnswer("belt-draft-stage-update.json")
    });
    renderAt("/belts?belt=nested");
    await screen.findByRole("heading", { level: 2, name: nested.belt.name });
    const form = await openStage(user, triage.name);
    const then = within(form).getByLabelText("Outcome 1 then") as HTMLSelectElement;
    expect(within(then).getByRole("option", { name: "Done" })).toBeTruthy();
    expect(within(then).getByRole("option", { name: "Ship" })).toBeTruthy();
    await user.selectOptions(then, "ship");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")[0]!.body.update.outcomes[0].then).toMatchObject({ do: "ship" });
  });

  it("moves a stage to another route of its fork", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": belts(),
      "/api/v1/belts/context": contextRoute(undefined, { placement }),
      ...models,
      "/api/v1/belts/draft/stage-move": draftAnswer("belt-draft-stage-move.json")
    });
    renderAt("/belts?belt=nested");
    await screen.findByRole("heading", { level: 2, name: nested.belt.name });
    const form = await openStage(user, assess.name);
    await user.selectOptions(within(form).getByLabelText("Route"), "skip");
    await waitFor(() => expect(daemon.posts("/draft/stage-move")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-move")[0]!.body).toMatchObject({ stageId: "assess", route: "skip" });
  });

  it("detaches a stage from its fork with the off choice", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": belts(),
      "/api/v1/belts/context": contextRoute(undefined, { placement }),
      ...models,
      "/api/v1/belts/draft/stage-move": draftAnswer("belt-draft-stage-move.json")
    });
    renderAt("/belts?belt=nested");
    await screen.findByRole("heading", { level: 2, name: nested.belt.name });
    const form = await openStage(user, assess.name);
    await user.selectOptions(within(form).getByLabelText("Route"), "");
    await waitFor(() => expect(daemon.posts("/draft/stage-move")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-move")[0]!.body).toMatchObject({ stageId: "assess", detach: true });
    expect(daemon.posts("/draft/stage-move")[0]!.body.route).toBeUndefined();
  });
});

describe("stage moves and removal", () => {
  it("moves a stage later, and says it cannot move earlier from the first", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({ "/api/v1/belts": belts(), "/api/v1/belts/context": contextRoute(), ...models, "/api/v1/belts/draft/stage-move": draftAnswer("belt-draft-stage-move.json") });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    const first = await openStage(user, firstStage.name);
    expect((within(first).getByRole("button", { name: "← Earlier" }) as HTMLButtonElement).disabled).toBe(true);
    await user.click(within(first).getByRole("button", { name: "Later →" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-move")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-move")[0]!.body).toMatchObject({ stageId: firstStage.id, step: 1 });
  });

  it("moves a stage earlier when an earlier stage exists", async () => {
    const user = userEvent.setup();
    const second = firstLive.belt.stages[1]!;
    const daemon = mockDaemon({ "/api/v1/belts": belts(), "/api/v1/belts/context": contextRoute(), ...models, "/api/v1/belts/draft/stage-move": draftAnswer("belt-draft-stage-move.json") });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    const form = await openStage(user, second.name);
    await user.click(within(form).getByRole("button", { name: "← Earlier" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-move")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-move")[0]!.body).toMatchObject({ stageId: second.id, step: -1 });
  });

  it("removes the stage on the form", async () => {
    const user = userEvent.setup();
    const second = firstLive.belt.stages[1]!;
    const daemon = mockDaemon({ "/api/v1/belts": belts(), "/api/v1/belts/context": contextRoute(), ...models, "/api/v1/belts/draft/stage-remove": draftAnswer("belt-draft-stage-move.json") });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    const form = await openStage(user, second.name);
    await user.click(within(form).getByRole("button", { name: "Remove stage" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-remove")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-remove")[0]!.body).toMatchObject({ stageId: second.id });
  });
});
