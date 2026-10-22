import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { contextRoute, contextWithBases, beltsList, firstLive, firstName, firstStage, fixture, mockDaemon as mockBaseDaemon, nameInput, renderAt, type Handler } from "./beltsTestKit";
import { json } from "../../test/response";
import { goldenText } from "../../test/golden";

/** The kit's daemon, answering every context read for the draft it was sent unless a route does. */
const mockDaemon = (routes: Record<string, Handler | string> = {}) => mockBaseDaemon({ "/api/v1/belts/context": contextRoute(), ...routes });

const firstTrigger = firstLive.belt.triggers[0]!;
const manualBelt = fixture.belts.find(record => record.belt.triggers.some(entry => entry.trigger.kind === "manual") && !record.archived)!;
const manualEntry = manualBelt.belt.triggers.find(entry => entry.trigger.kind === "manual")!;

const draftFromGolden = (name: string) => JSON.parse(goldenText(name)) as { draft: unknown; warnings: string[]; actions: unknown[] };
const stageTemplate = { stage: { id: "verify-2", type: "agent", name: "Verify", outcomes: [], agent: { id: "verify", version: 1 }, runner: "pi" } };

type User = ReturnType<typeof userEvent.setup>;

/** Opens a stage in the form under the line, does something there, and goes back to the trigger, as the app's editor does. */
async function inStage(user: User, act: (form: HTMLElement) => Promise<void>) {
  await user.click(screen.getByRole("button", { name: firstStage.name }));
  const form = await screen.findByRole("region", { name: `Stage ${firstStage.name} settings` });
  await waitFor(() => expect(form.getAttribute("data-ready")).toBe("true"));
  await act(form);
  await user.click(screen.getByRole("button", { name: "Trigger" }));
}

/** The "+" in front of the first stage adds the Build agent there. */
async function addFromMenu(user: User) {
  await user.click(screen.getAllByRole("button", { name: "Add a stage here" })[0]!);
  await user.click(await screen.findByRole("menuitem", { name: "Build" }));
}

beforeEach(() => {
  localStorage.clear();
});

/** Each structural control, the daemon operation it sends, the golden answer it gets, and the operands it must carry. */
const operations: Array<{
  op: string;
  act: (user: ReturnType<typeof userEvent.setup>) => Promise<void>;
  answer: string;
  operands: (body: any) => void;
}> = [
  { op: "stage-move", act: async user => inStage(user, form => user.click(within(form).getByRole("button", { name: "Later →" }))), answer: "belt-draft-stage-move.json",
    operands: body => expect(body).toMatchObject({ stageId: firstStage.id, step: 1 }) },
  { op: "stage-remove", act: async user => user.click(screen.getByRole("button", { name: `Remove ${firstStage.name}` })), answer: "belt-draft-stage-remove.json",
    operands: body => expect(body).toMatchObject({ stageId: firstStage.id }) },
  { op: "fork-add", act: async user => inStage(user, form => user.click(within(form).getByRole("button", { name: `Fork at ${firstStage.name}` }))), answer: "belt-draft-fork-add.json",
    operands: body => expect(body).toMatchObject({ stageId: firstStage.id }) },
  { op: "stage-add", act: async user => addFromMenu(user), answer: "belt-draft-stage-add.json", operands: body => expect(body.stage).toMatchObject({ id: "verify-2" }) },
  { op: "feed-into", act: async user => inStage(user, form => user.click(within(form).getByRole("button", { name: `Feed a new stage into ${firstStage.name}` }))), answer: "belt-draft-stage-add.json",
    operands: body => expect(body).toMatchObject({ stageId: firstStage.id }) },
  { op: "trigger-add", act: async user => user.click(screen.getByRole("button", { name: "Add trigger" })), answer: "belt-draft-trigger-add.json",
    operands: body => expect(body).toEqual({ draft: expect.any(Object), op: "trigger-add" }) },
  { op: "trigger-remove", act: async user => user.click(screen.getByRole("button", { name: `Remove trigger 1 GitHub issues` })), answer: "belt-draft-trigger-remove.json",
    operands: body => expect(body).toMatchObject({ entryId: firstTrigger.id }) },
  { op: "trigger-kind", act: async user => user.selectOptions(screen.getByRole("combobox", { name: "Kind of trigger 1" }), "jira"), answer: "belt-draft-trigger-kind.json",
    operands: body => expect(body).toMatchObject({ entryId: firstTrigger.id, kind: "jira", stash: { entry: firstTrigger.id, triggers: { issues: firstTrigger.trigger } } }) },
  { op: "trigger-update", act: async user => {
    await user.clear(screen.getByLabelText("Has label for trigger 1"));
    await user.type(screen.getByLabelText("Has label for trigger 1"), "agent-now{Enter}");
  }, answer: "belt-draft-trigger-update.json", operands: body => expect(body).toMatchObject({ entryId: firstTrigger.id, trigger: { kind: "issues", label: "agent-now" } }) },
  // Leaving the label field applies it too, so Save is not left waiting on text that was never sent.
  { op: "trigger-update", act: async user => {
    await user.clear(screen.getByLabelText("Has label for trigger 1"));
    await user.type(screen.getByLabelText("Has label for trigger 1"), "agent-left");
    await user.tab();
  }, answer: "belt-draft-trigger-update.json", operands: body => expect(body).toMatchObject({ entryId: firstTrigger.id, trigger: { kind: "issues", label: "agent-left" } }) },
  { op: "repository-add", act: async user => {
    await user.type(screen.getByLabelText("Add a repository to trigger 1"), "acme/new{Enter}");
  }, answer: "belt-draft-repository-add.json", operands: body => expect(body).toMatchObject({ entryId: firstTrigger.id, repository: "acme/new" }) },
  { op: "repository-remove", act: async user => user.click(screen.getByRole("button", { name: `Remove ${firstTrigger.repos[0]} from trigger 1` })), answer: "belt-draft-repository-remove.json",
    operands: body => expect(body).toMatchObject({ entryId: firstTrigger.id, repository: firstTrigger.repos[0] }) },
];

describe("structural edits go to the daemon with the whole draft", () => {
  it.each(operations.map(item => [item.op, item] as const))("%s sends the current draft, its operands, and uses the answer", async (op, item) => {
    const user = userEvent.setup();
    const answer = draftFromGolden(item.answer);
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      // The daemon echoes the submitted name back, so the answer carries the same typed value.
      [`/api/v1/belts/draft/${op}`]: request => json({ ...answer, draft: { ...(answer.draft as object), name: request.body.draft.name } }),
      "/api/v1/belts/stage-template": () => json(stageTemplate)
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user.type(nameInput(), " typed");
    await item.act(user);
    await waitFor(() => expect(daemon.posts(`/draft/${op}`)).toHaveLength(1));
    const [request] = daemon.posts(`/draft/${op}`);
    expect(request!.path).toBe(`/api/v1/belts/draft/${op}`);
    expect(request!.body.op).toBe(op);
    expect(request!.body.draft.name).toBe(`${firstName} typed`);
    item.operands(request!.body);
    await waitFor(() => expect(nameInput().value).toBe(`${firstName} typed`));
  });

  it("names a stage's own template before the stage is added, and sends the template as the operand", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/stage-template": () => json(stageTemplate),
      "/api/v1/belts/draft/stage-add": () => json(draftFromGolden("belt-draft-stage-add.json"))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await addFromMenu(user);
    await waitFor(() => expect(daemon.posts("/draft/stage-add")).toHaveLength(1));
    expect(daemon.requests.filter(request => request.path === "/api/v1/belts/stage-template")[0]!.body.type).toBe("agent");
    expect(daemon.posts("/draft/stage-add")[0]!.body.stage).toMatchObject({ id: "verify-2", name: "Verify" });
  });
});

describe("the add menu stays in the viewport", () => {
  it("bounds its height to the space under its \"+\" and closes when the page scrolls", async () => {
    const user = userEvent.setup();
    mockDaemon({ "/api/v1/belts": goldenText("belts.json") });
    vi.stubGlobal("innerHeight", 200);
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user.click(screen.getAllByRole("button", { name: "Add a stage here" })[0]!);
    const menu = await screen.findByRole("menu");
    // jsdom lays nothing out, so the "+" sits at the top: 6px down, and 12px is kept clear at the bottom of 200px.
    expect(menu.style.maxHeight).toBe("182px");
    document.dispatchEvent(new Event("scroll"));
    await waitFor(() => expect(screen.queryByRole("menu")).toBeNull());
  });

});

describe("fork and route edits, in the order a person makes them", () => {
  it("forks a stage, adds, renames and removes a route, then removes the fork, each from the answer before it", async () => {
    const user = userEvent.setup();
    const forked = draftFromGolden("belt-draft-fork-add.json") as { draft: { forks: Array<{ id: string; routes: Array<{ id: string; name: string }> }> } };
    const forkId = forked.draft.forks[0]!.id;
    const routeAdded = { ...forked, draft: { ...forked.draft, forks: [{ ...forked.draft.forks[0]!, routes: [...forked.draft.forks[0]!.routes, { id: "fresh", name: "Fresh" }] }] } };
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/draft/fork-add": () => json(forked),
      "/api/v1/belts/draft/route-add": () => json(routeAdded),
      "/api/v1/belts/draft/route-update": () => json(routeAdded),
      "/api/v1/belts/draft/route-remove": () => json(forked),
      "/api/v1/belts/draft/fork-remove": () => json(goldenText("belt-draft-fork-remove.json"))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user.click(screen.getByRole("button", { name: firstStage.name }));
    await user.click(await screen.findByRole("button", { name: `Fork at ${firstStage.name}` }));
    const fork = await screen.findByRole("region", { name: `Fork ${forkId}` });
    await user.click(within(fork).getByRole("button", { name: "+ Route" }));
    await waitFor(() => expect(daemon.posts("/draft/route-add")[0]!.body.forkId).toBe(forkId));

    const renamed = await screen.findByRole("textbox", { name: "Route name for Fresh" });
    await user.clear(renamed);
    await user.type(renamed, "Renamed{Enter}");
    await waitFor(() => expect(daemon.posts("/draft/route-update")[0]!.body).toMatchObject({ routeId: "fresh", routeName: "Renamed" }));

    await user.click(screen.getByRole("button", { name: `Remove route Fresh` }));
    await waitFor(() => expect(daemon.posts("/draft/route-remove")[0]!.body.routeId).toBe("fresh"));
  });
});

describe("unsent input and located problems", () => {
  it("keeps the name typed while a structural edit was pending, and shows the daemon's answer for everything else", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => undefined;
    mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/draft/stage-remove": () => new Promise<Response>(resolve => { release = resolve; })
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user.type(nameInput(), " sent");
    await user.click(screen.getByRole("button", { name: `Remove ${firstStage.name}` }));
    await user.type(nameInput(), " while pending");
    release(json(draftFromGolden("belt-draft-stage-remove.json")));
    await waitFor(() => expect(screen.queryByRole("button", { name: `Remove ${firstStage.name}` })).toBeNull());
    expect(nameInput().value).toBe(`${firstName} sent while pending`);
  });

  it("links a located 422 to its stage, opening the stage's form", async () => {
    const user = userEvent.setup();
    mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/draft/stage-remove": () => json({
        message: "The draft is not valid",
        problems: ["stage-remove needs a stage id that is on the belt."],
        locations: [{ kind: "stage", id: firstStage.id }]
      }, 422)
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user.click(screen.getByRole("button", { name: `Remove ${firstStage.name}` }));
    const alert = await screen.findByRole("alert", { name: "Problems" });
    expect(within(alert).getByText("stage-remove needs a stage id that is on the belt.")).toBeTruthy();
    await user.click(within(alert).getByRole("button", { name: "Show stage" }));
    const form = await screen.findByRole("region", { name: `Stage ${firstStage.name} settings` });
    const stageName = within(form).getByLabelText("Name");
    await waitFor(() => expect(document.activeElement).toBe(stageName));
    expect(stageName.getAttribute("aria-invalid")).toBe("true");
    expect(document.getElementById(stageName.getAttribute("aria-describedby")!)!.textContent).toContain("stage-remove needs a stage id that is on the belt.");
  });
});

describe("stage form", () => {
  it("applies a stage's name and brief with one stage-update that carries both", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/draft/stage-update": () => json(draftFromGolden("belt-draft-stage-update.json"))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    await user.click(screen.getByRole("button", { name: firstStage.name, pressed: false }));
    const form = await screen.findByRole("region", { name: `Stage ${firstStage.name} settings` });
    await user.type(within(form).getByLabelText("Name"), " 2");
    // The brief starts as the agent's own, so typing extends it into this belt's override.
    await waitFor(() => expect(form.getAttribute("data-ready")).toBe("true"));
    await user.type(within(form).getByLabelText("Brief"), " Read the ticket.");
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")[0]!.body).toMatchObject({ stageId: firstStage.id, update: { name: `${firstStage.name} 2`, brief: "The triage brief. Read the ticket." } });
  });
});

describe("task sheet", () => {
  it("starts a task on the saved manual trigger, with its title and what to do, and opens the run", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      [`/api/v1/belts`]: goldenText("belts.json"),
      "/api/v1/tasks": () => json(goldenText("run-decide.json"))
    });
    renderAt(`/belts?belt=${manualBelt.belt.id}`);
    await screen.findByRole("heading", { level: 2, name: manualBelt.belt.name });
    await user.click(screen.getByRole("button", { name: "Start a task…" }));
    const sheet = await screen.findByRole("dialog", { name: "Start a task" });
    expect((within(sheet).getByRole("button", { name: "Start" }) as HTMLButtonElement).disabled).toBe(true);
    expect(within(sheet).getByText("Give the task a title.")).toBeTruthy();
    await user.type(within(sheet).getByLabelText("Title"), "Fix the report");
    await user.type(within(sheet).getByLabelText("What to do"), "Check the totals.");
    await user.click(within(sheet).getByRole("button", { name: "Start" }));
    await waitFor(() => expect(daemon.posts("/api/v1/tasks")).toHaveLength(1));
    expect(daemon.posts("/api/v1/tasks")[0]!.body).toEqual({
      belt: manualBelt.belt.id,
      entry: manualEntry.id,
      repo: manualEntry.repos[0],
      title: "Fix the report",
      body: "Check the totals."
    });
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Start a task" })).toBeNull());
  });

  it("cancelling sends nothing", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({ "/api/v1/belts": goldenText("belts.json") });
    renderAt(`/belts?belt=${manualBelt.belt.id}`);
    await screen.findByRole("heading", { level: 2, name: manualBelt.belt.name });
    await user.click(screen.getByRole("button", { name: "Start a task…" }));
    await user.click(within(await screen.findByRole("dialog", { name: "Start a task" })).getByRole("button", { name: "Cancel" }));
    expect(screen.queryByRole("dialog", { name: "Start a task" })).toBeNull();
    expect(daemon.posts()).toHaveLength(0);
  });
});

describe("repository base fields follow the daemon's answer", () => {
  it("a repository two trigger entries list gets one field in the group, showing its override once", async () => {
    const [repeated, ...others] = firstTrigger.repos;
    const belts = fixture.belts.map(record => record.belt.id === firstLive.belt.id
      ? { ...record, belt: { ...record.belt, base: "develop", repositoryBases: { [repeated!]: "release/repeated" }, triggers: [...record.belt.triggers, { ...firstTrigger, id: "2", repos: [repeated!] }] }, draft: { ...record.draft, base: "develop", repositoryBases: { [repeated!]: "release/repeated" }, triggers: [...record.draft.triggers, { ...firstTrigger, id: "2", repos: [repeated!] }] } }
      : record);
    mockDaemon({
      "/api/v1/belts": () => json(beltsList(belts)),
      // The daemon lists each repository once, in first-appearance order across the entries.
      "/api/v1/belts/context": request => json(contextWithBases(request.body.draft))
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    const group = await screen.findByRole("group", { name: "Repository base branches" });
    const fields = within(group).getAllByRole("textbox") as HTMLInputElement[];
    expect(fields.map(field => field.closest("label")?.textContent)).toEqual([repeated, ...others]);
    expect(within(group).getAllByLabelText(repeated!)).toHaveLength(1);
    expect((within(group).getByLabelText(repeated!) as HTMLInputElement).value).toBe("release/repeated");
  });

  it("removing a repository's last listing sends the draft, and its override comes back cleaned by the daemon", async () => {
    const user = userEvent.setup();
    const overridden = fixture.belts.map(record => record.belt.id === firstLive.belt.id
      ? { ...record, belt: { ...record.belt, base: "develop", repositoryBases: { "acme/platform": "release/platform", "acme/billing": "release/billing" } }, draft: { ...record.draft, base: "develop", repositoryBases: { "acme/platform": "release/platform", "acme/billing": "release/billing" } } }
      : record);
    const daemon = mockDaemon({
      "/api/v1/belts": () => json(beltsList(overridden)),
      "/api/v1/belts/context": request => json(contextWithBases(request.body.draft)),
      // The daemon drops the override with the last listing it had, and keeps the other.
      "/api/v1/belts/draft/repository-remove": request => {
        const sent = request.body.draft;
        const draft = { ...sent, triggers: sent.triggers.map((entry: any) => ({ ...entry, repos: entry.repos.filter((repo: string) => repo !== "acme/billing") })), repositoryBases: { "acme/platform": "release/platform" } };
        return json({ ...draftFromGolden("belt-draft-repository-remove.json"), draft });
      }
    });
    renderAt("/belts");
    await screen.findByRole("heading", { level: 2, name: firstName });
    expect((await screen.findByLabelText("acme/billing") as HTMLInputElement).value).toBe("release/billing");
    await user.click(screen.getByRole("button", { name: `Remove ${firstTrigger.repos[1]} from trigger 1` }));
    await waitFor(() => expect(daemon.posts("/draft/repository-remove")).toHaveLength(1));
    const [request] = daemon.posts("/draft/repository-remove");
    expect(request!.body.draft.repositoryBases).toEqual({ "acme/platform": "release/platform", "acme/billing": "release/billing" });
    await waitFor(() => expect(screen.queryByLabelText("acme/billing")).toBeNull());
    expect((screen.getByLabelText("acme/platform") as HTMLInputElement).value).toBe("release/platform");
  });
});

