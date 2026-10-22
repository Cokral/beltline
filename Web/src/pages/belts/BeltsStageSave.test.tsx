import { fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { contextRoute, firstLive, firstName, firstStage, library, mockDaemon, renderAt } from "./beltsTestKit";
import { json } from "../../test/response";
import { goldenText } from "../../test/golden";

beforeEach(() => localStorage.clear());

async function openStage(user: ReturnType<typeof userEvent.setup>) {
  await screen.findByRole("heading", { level: 2, name: firstName });
  await user.click(screen.getByRole("button", { name: firstStage.name, pressed: false }));
  const form = await screen.findByRole("region", { name: `Stage ${firstStage.name} settings` });
  await waitFor(() => expect(form.getAttribute("data-ready")).toBe("true"));
  return form;
}

describe("saving stage input", () => {
  it("enables Save immediately and saves the daemon's accepted prompt and model without Apply", async () => {
    const user = userEvent.setup();
    const saved = library();
    const daemon = mockDaemon({
      "/api/v1/belts": saved.read,
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/models/pi": goldenText("models.json"),
      "/api/v1/belts/draft/stage-update": request => json({
        draft: { ...request.body.draft, stages: request.body.draft.stages.map((stage: any) => stage.id === request.body.stageId ? { ...stage, ...request.body.update } : stage) }, warnings: [], actions: []
      }),
      "/api/v1/belts/save": request => saved.change(firstLive.belt.id, {
        belt: { ...request.body.belt, version: firstLive.belt.version + 1 },
        draft: { ...request.body.belt, version: firstLive.belt.version + 1 }
      })
    });
    renderAt("/belts");
    const form = await openStage(user);
    expect(within(form).queryByRole("button", { name: "Apply stage" })).toBeNull();
    fireEvent.change(within(form).getByLabelText("Brief"), { target: { value: "Save this prompt." } });
    const save = screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
    expect(save.disabled).toBe(false);
    await waitFor(() => expect((within(form).getByLabelText("Model") as HTMLSelectElement).disabled).toBe(false));
    const choices = within(form).getByLabelText("Model") as HTMLSelectElement;
    const option = Array.from(choices.options).find(item => !item.disabled && item.value !== choices.value && item.value !== "")!;
    await user.selectOptions(choices, option.value);
    await user.click(save);
    await waitFor(() => expect(daemon.posts("/save")).toHaveLength(1));
    expect(daemon.posts("/draft/stage-update")).toHaveLength(1);
    expect(daemon.posts("/save")[0]!.body.belt.stages.find((stage: any) => stage.id === firstStage.id)).toMatchObject({ brief: "Save this prompt.", model: option.value });
    await waitFor(() => expect(screen.queryByRole("button", { name: "Save" })).toBeNull());
  });

  it("keeps pending edits and never saves when the daemon rejects the stage update", async () => {
    const user = userEvent.setup();
    const daemon = mockDaemon({
      "/api/v1/belts": goldenText("belts.json"),
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/belts/draft/stage-update": () => json(goldenText("error-not-found.json"), 422)
    });
    renderAt("/belts");
    const form = await openStage(user);
    fireEvent.change(within(form).getByLabelText("Brief"), { target: { value: "Keep this unsaved prompt." } });
    await user.click(screen.getByRole("button", { name: "Save" }));
    await screen.findByRole("alert", { name: "Problems" });
    expect(daemon.posts("/save")).toHaveLength(0);
    expect((within(form).getByLabelText("Brief") as HTMLTextAreaElement).value).toBe("Keep this unsaved prompt.");
    expect((screen.getByRole("button", { name: "Save" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it.each(["Newer unsaved prompt.", `The ${firstStage.agent!.id} brief.`])("keeps text entered during Save and blocks duplicate submissions: %s", async newer => {
    const user = userEvent.setup();
    const saved = library();
    let release!: () => void;
    const pending = new Promise<void>(resolve => { release = resolve; });
    const daemon = mockDaemon({
      "/api/v1/belts": saved.read,
      "/api/v1/belts/context": contextRoute(),
      "/api/v1/models/pi": goldenText("models.json"),
      "/api/v1/belts/draft/stage-update": async request => {
        await pending;
        return json({ draft: { ...request.body.draft, stages: request.body.draft.stages.map((stage: any) => stage.id === request.body.stageId ? { ...stage, ...request.body.update } : stage) }, warnings: [], actions: [] });
      },
      "/api/v1/belts/save": request => saved.change(firstLive.belt.id, { belt: request.body.belt, draft: request.body.belt })
    });
    renderAt("/belts");
    const form = await openStage(user);
    const brief = within(form).getByLabelText("Brief");
    fireEvent.change(brief, { target: { value: "Submitted prompt." } });
    const save = screen.getByRole("button", { name: "Save" }) as HTMLButtonElement;
    await user.click(save);
    await waitFor(() => expect(daemon.posts("/draft/stage-update")).toHaveLength(1));
    expect(save.textContent).toBe("Saving…");
    await user.click(save);
    fireEvent.change(brief, { target: { value: newer } });
    release();
    await waitFor(() => expect(daemon.posts("/save")).toHaveLength(1));
    await waitFor(() => expect(screen.queryByRole("button", { name: "Saving…" })).toBeNull());
    expect((within(form).getByLabelText("Brief") as HTMLTextAreaElement).value).toBe(newer);
    expect(daemon.posts("/save")[0]!.body.belt.stages.find((stage: any) => stage.id === firstStage.id).brief).toBe("Submitted prompt.");
    expect(daemon.posts("/draft/stage-update")).toHaveLength(1);
  });
});
