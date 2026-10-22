import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { answer, contextFor, contextRoute, outdatedLine, docs, echo, firstLive, firstName, firstStage, fixture, heading, library, mockDaemon, nameInput, renderAt, resource, selectBelt, beltsList } from "./beltsTestKit";
import { history } from "../../test/daemon";
import { json } from "../../test/response";
import { goldenText } from "../../test/golden";

/** The Belts page's behaviour rules 6 to 11: saved belt commands, availability, located problems, the task sheet, entry points and the wire contract. */

beforeEach(() => {
  localStorage.clear();
});

afterEach(() => {
  Object.defineProperty(window.navigator, "onLine", { configurable: true, value: true });
  window.dispatchEvent(new Event("online"));
});

/** A disabled action's reason, which the editor gives as the button's accessible description. */
const reasonOf = (button: HTMLElement) => document.getElementById(button.getAttribute("aria-describedby") ?? "")?.textContent ?? null;

describe("rule 6: saved belt commands", () => {
  it("opens the share sheet for the immutable saved belt and blocks dirty drafts", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({ "/api/v1/belts": goldenText("belts.json"), "/api/v1/belts/context": contextRoute() });
    renderAt("/belts");
    await heading(firstName);
    await user.type(nameInput(), " changed");
    const share = screen.getByRole("button", { name: "Share…" }) as HTMLButtonElement;
    expect(share.disabled).toBe(true);
    expect(reasonOf(share)).toBe("Save changes before sharing");
    await user.click(share);
    expect(daemon.posts("/library/share")).toHaveLength(0);
    await user.type(nameInput(), "{Backspace}".repeat(" changed".length));
    await waitFor(() => expect((screen.getByRole("button", { name: "Share…" }) as HTMLButtonElement).disabled).toBe(false));
    await user.click(screen.getByRole("button", { name: "Share…" }));
    const sheet = await screen.findByRole("dialog", { name: `Share ${firstLive.belt.name}` });
    expect(within(sheet).getByText(`${firstLive.belt.id} · v${firstLive.belt.version}`)).toBeTruthy();
    expect(daemon.posts("/library/share")).toHaveLength(0);
  });

  it("updates every stage in the draft through update-all-stages, and saves nothing", async () => {
    const user = userEvent.setup();
    const withUpdates = fixture.belts.map(record => record.belt.id === docs.belt.id
      ? { ...record, actions: record.actions.map(action => action.id === "update-all-stages" ? { id: action.id, enabled: true } : action) } : record);
    const daemon = mockDaemon({
      "/api/v1/belts": () => json(beltsList(withUpdates)),
      "/api/v1/belts/context": request => json({ ...contextFor(request.body.draft), actions: [{ id: "save", enabled: true }, { id: "update-all-stages", enabled: true }], warnings: [], placeholders: null, routes: [], stages: [], outcomes: [], schedules: [], policies: [], line: outdatedLine(request.body.draft), outdatedStages: request.body.draft.stages.length }),
      "/api/v1/belts/update-all-stages": echo("belt-update-all-stages.json")
    });
    renderAt(`/belts?belt=${docs.belt.id}`);
    await heading(docs.belt.name);
    await user.type(nameInput(), " typed");
    await user.click(await screen.findByRole("button", { name: /^Update (1 stage|all \d+ stages)$/ }));
    await waitFor(() => expect(daemon.posts("/update-all-stages")).toHaveLength(1));
    expect(daemon.posts("/update-all-stages")[0]!.body.draft.name).toBe(`${docs.belt.name} typed`);
    expect(daemon.posts("/save")).toHaveLength(0);
    await waitFor(() => expect(screen.getByText(/Unsaved changes/)).toBeTruthy());
  });

  it("pauses a running belt and resumes a paused one with the daemon's answer, without saving typed edits", async () => {
    const user = userEvent.setup();
    const belts = library();
    const daemon = mockDaemon({
      "/api/v1/belts": belts.read,
      "/api/v1/belts/context": contextRoute(),
      [`/api/v1/belts/${docs.belt.id}/pause`]: request => belts.change(docs.belt.id, { paused: request.body.paused })
    });
    renderAt(`/belts?belt=${docs.belt.id}`);
    await heading(docs.belt.name);
    await user.type(nameInput(), " typed");
    await user.click(screen.getByRole("button", { name: "Pause belt" }));
    await waitFor(() => expect(daemon.posts("/pause")).toHaveLength(1));
    expect(daemon.posts("/pause")[0]!.body).toEqual({ paused: true });
    await waitFor(() => expect(screen.getByRole("button", { name: "Resume belt" })).toBeTruthy());
    expect(screen.getByText(/· paused$/, { selector: ".paper-tag" })).toBeTruthy();
    expect(nameInput().value).toBe(`${docs.belt.name} typed`);
    expect(daemon.posts("/save")).toHaveLength(0);
  });

  it("copies a dirty source with its unsent edits, and the source keeps them", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/belts/copy": request => json({ ...answer("belt-copy.json"), draft: { ...request.body.draft, id: "", version: 0, name: `${request.body.draft.name} copy` } })
    });
    renderAt("/belts");
    await heading(firstName);
    await user.type(nameInput(), " edited");
    await user.click(screen.getByRole("button", { name: "Copy belt" }));
    await waitFor(() => expect(daemon.posts("/copy")).toHaveLength(1));
    expect(daemon.posts("/copy")[0]!.body.draft.name).toBe(`${firstName} edited`);
    await heading(`${firstName} edited copy`);
    await selectBelt(user, firstName);
    await heading(`${firstName} edited`);
  });
});

describe("rule 7: availability and its reasons", () => {
  it("shows the daemon's reason beside an unavailable action and links it for assistive technology", async () => {
    mockDaemon({ "/api/v1/belts": goldenText("belts.json"), "/api/v1/belts/context": request => json({ ...JSON.parse(JSON.stringify({})), ...contextFor(request.body.draft), actions: docs.actions, line: outdatedLine(request.body.draft), outdatedStages: request.body.draft.stages.length }) });
    renderAt(`/belts?belt=${docs.belt.id}`);
    await heading(docs.belt.name);
    const update = await screen.findByRole("button", { name: /^Update (1 stage|all \d+ stages)$/ }) as HTMLButtonElement;
    expect(update.disabled).toBe(true);
    expect(update.getAttribute("aria-describedby")).not.toBeNull();
    expect(document.getElementById(update.getAttribute("aria-describedby")!)!.textContent).toBe("Every stage already uses its agent’s latest version.");
  });

  it("hides Save while the daemon is offline and sends nothing until it is back", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({ "/api/v1/belts": goldenText("belts.json"), "/api/v1/belts/context": contextRoute() });
    renderAt("/belts");
    await heading(firstName);
    await user.type(nameInput(), " typed");
    Object.defineProperty(window.navigator, "onLine", { configurable: true, value: false });
    window.dispatchEvent(new Event("offline"));
    // Without the daemon there is no Save to press; the offline banner says why.
    await waitFor(() => expect(screen.queryByRole("button", { name: "Save" })).toBeNull());
    // Offline, the line is read only: its blocks offer no remove.
    expect(screen.queryByRole("button", { name: `Remove ${firstStage.name}` })).toBeNull();
    expect(nameInput().value).toBe(`${firstName} typed`);
    expect(daemon.posts()).toHaveLength(0);
  });
});

describe("rule 8: a rejected save names its fields", () => {
  it("lists the problems in order, links each located one to its field, and keeps unlocated ones in the summary", async () => {
    const user = userEvent.setup();
    const trigger = firstLive.belt.triggers[0]!;
    const outcome = firstStage.outcomes[1]!;
    let refuse = true;
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/models/pi": goldenText("models.json"),
      "/api/v1/belts/save": request => refuse ? json({
        message: "The belt was not saved.",
        problems: ["Give the belt a name.", "Trigger id 1 is used more than once.", `Repository ${trigger.repos[0]} is listed more than once.`, "Outcome goes to a missing stage.", "Something about the whole belt <b>"],
        locations: [{ kind: "belt", field: "name" }, { kind: "trigger", id: trigger.id }, { kind: "repository", id: trigger.id, field: trigger.repos[0] },
          { kind: "outcome", id: firstStage.id, field: outcome.id }, null]
      }, 422) : resource(firstLive, { belt: request.body.belt })
    });
    renderAt("/belts");
    await heading(firstName);
    await user.clear(nameInput());
    await user.click(screen.getByRole("button", { name: "Save" }));
    const alert = await screen.findByRole("alert", { name: "Problems" });
    expect(within(alert).getAllByRole("listitem").map(item => item.querySelector("span")!.textContent)).toEqual([
      "Give the belt a name.", "Trigger id 1 is used more than once.", `Repository ${trigger.repos[0]} is listed more than once.`, "Outcome goes to a missing stage.", "Something about the whole belt <b>"
    ]);
    expect(within(alert).getAllByRole("listitem")[4]!.textContent).toContain("(not about one field)");
    expect(nameInput().getAttribute("aria-invalid")).toBe("true");
    expect(screen.getByRole("combobox", { name: "Kind of trigger 1" }).getAttribute("aria-invalid")).toBe("true");

    await user.click(within(alert).getByRole("button", { name: "Show trigger" }));
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("combobox", { name: "Kind of trigger 1" })));
    await user.click(within(alert).getByRole("button", { name: `Show repository ${trigger.repos[0]}` }));
    await waitFor(() => expect(document.activeElement).toBe(screen.getByRole("button", { name: `Remove ${trigger.repos[0]} from trigger 1` })));
    await user.click(within(alert).getByRole("button", { name: "Show outcome" }));
    const form = await screen.findByRole("region", { name: `Stage ${firstStage.name} settings` });
    await waitFor(() => expect(document.activeElement).toBe(within(form).getByLabelText("Outcome 2 name")));
    expect(within(form).getByLabelText("Outcome 2 name").getAttribute("aria-invalid")).toBe("true");
    await user.click(screen.getByRole("button", { name: "Trigger" }));

    await user.click(within(screen.getByRole("alert", { name: "Problems" })).getByRole("button", { name: "Show belt name" }));
    await waitFor(() => expect(document.activeElement).toBe(nameInput()));
    await user.type(nameInput(), "Corrected");
    refuse = false;
    await user.click(screen.getByRole("button", { name: "Save" }));
    await waitFor(() => expect(daemon.posts("/save")).toHaveLength(2));
    expect(daemon.posts("/save")[1]!.body.belt.name).toBe("Corrected");
  });
});

describe("rule 9: the task sheet", () => {
  const tasks = fixture.belts.find(record => record.belt.id === "tasks")!;

  it("sends one start however often Start is clicked, and the belt's unsent edits are there on return", async () => {
    const user = userEvent.setup();
    let release: () => void = () => undefined;
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/tasks": () => new Promise<Response>(resolve => { release = () => resolve(json(goldenText("tasks.json"))); })
    });
    renderAt(`/belts?belt=${tasks.belt.id}`);
    await heading(tasks.belt.name);
    await user.type(nameInput(), " typed");
    await user.click(screen.getByRole("button", { name: "Start a task…" }));
    const sheet = await screen.findByRole("dialog", { name: /Start a task/ });
    await user.type(within(sheet).getByLabelText("Title"), "Fix the thing");
    const start = within(sheet).getByRole("button", { name: "Start" });
    await user.click(start);
    await user.click(start);
    expect(daemon.posts("/tasks")).toHaveLength(1);
    release();
    await waitFor(() => expect(screen.getByLabelText("Location").textContent).toMatch(/^\/runs\//));
    await history(user, "Back");
    await heading(`${tasks.belt.name} typed`);
    expect(daemon.posts("/tasks")).toHaveLength(1);
  });
});

describe("rule 10 and 11: entry points and the wire contract", () => {
  it("returns from a pinned agent to the same editor and draft", async () => {
    const user = userEvent.setup();
    mockDaemon({ "/api/v1/belts": goldenText("belts.json"), "/api/v1/belts/context": contextRoute(), "/api/v1/models/pi": goldenText("models.json") });
    renderAt("/belts");
    await heading(firstName);
    await user.type(nameInput(), " typed");
    await user.click(screen.getByRole("button", { name: firstStage.name, pressed: false }));
    const form = await screen.findByRole("region", { name: `Stage ${firstStage.name} settings` });
    await waitFor(() => expect(form.getAttribute("data-ready")).toBe("true"));
    await user.click(within(form).getByRole("button", { name: "Edit" }));
    const sheet = await screen.findByRole("dialog", { name: `Open agent ${firstStage.agent!.id}` });
    await user.click(within(sheet).getByRole("button", { name: "Open without saving" }));
    await waitFor(() => expect(screen.getByLabelText("Location").textContent).toMatch(/^\/agents\?agent=/));
    await history(user, "Back");
    await heading(`${firstName} typed`);
  });

});
