import { screen, waitFor, within, render } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { AppProviders } from "../../app/App";
import { json } from "../../test/response";
import { LibraryPage } from "./LibraryPage";
import { backupRepo, mockLibrary, sharedRepo } from "./libraryTestKit";

const repos = (...list: unknown[]) => JSON.stringify({ repos: list });

function renderLibrary() {
  render(<AppProviders><MemoryRouter initialEntries={["/library"]}><LibraryPage /></MemoryRouter></AppProviders>);
}

/** A control waits for the daemon connection before it can be used, as every command control does. */
async function usable(button: HTMLElement): Promise<HTMLElement> {
  await waitFor(() => expect((button as HTMLButtonElement).disabled).toBe(false));
  return button;
}

describe("Library page", () => {
  it("shows each repository with its role and the daemon's actions, and each item with its state and reason", async () => {
    mockLibrary({ "/api/v1/library/repos": repos(backupRepo, sharedRepo) });
    renderLibrary();
    const backup = await screen.findByRole("region", { name: "Library acme/beltline-backup" });
    expect(within(backup).getByText("Changed since backup")).toBeTruthy();
    expect((within(backup).getByRole("button", { name: "Back up" }) as HTMLButtonElement).disabled).toBe(false);
    const shared = screen.getByRole("region", { name: "Library acme/team-library" });
    expect(shared.querySelectorAll(".library-item")).toHaveLength(3);
    expect(shared.querySelector('.library-item[data-state="update"]')).toBeTruthy();
    expect(within(shared).getByText("Conflict")).toBeTruthy();
    expect(within(shared).getByText("You changed triage v1 locally; save it under a new id before updating.")).toBeTruthy();
    expect((within(shared).getAllByRole("button", { name: "Install" })[0] as HTMLButtonElement).disabled).toBe(true);
  });

  it("installs an update with the item's kind and id, and shows the repository as read again", async () => {
    const { posts, requests } = mockLibrary({
      "/api/v1/library/repos": repos(sharedRepo),
      "/api/v1/library/install": () => json({ written: ["belts/issue-flow/v3.json"] })
    });
    const user = userEvent.setup();
    renderLibrary();
    const row = (await screen.findByText("Issue flow")).closest("li")!;
    await user.click(await usable(within(row).getByRole("button", { name: "Update" })));
    await waitFor(() => expect(posts("/library/install")).toHaveLength(1));
    expect(posts("/library/install")[0]!.body).toEqual({ repository: "acme/team-library", path: "", kind: "belt", id: "issue-flow" });
    await waitFor(() => expect(requests.filter(request => request.path === "/api/v1/library/repos").length).toBeGreaterThan(1));
  });

  it("backs up and says whether anything changed", async () => {
    const { posts } = mockLibrary({
      "/api/v1/library/repos": repos(backupRepo),
      "/api/v1/library/backup": () => json({ commit: "7d1e0aa", changed: true })
    });
    const user = userEvent.setup();
    renderLibrary();
    await user.click(await usable(await screen.findByRole("button", { name: "Back up" })));
    expect(await screen.findByText("Backed up as 7d1e0aa.")).toBeTruthy();
    expect(posts("/library/backup")[0]!.body).toEqual({ repository: "acme/beltline-backup", path: "" });
  });

  it("asks before a restore and sends it only when confirmed", async () => {
    const { posts } = mockLibrary({
      "/api/v1/library/repos": repos(backupRepo),
      "/api/v1/library/restore": () => json({ savedTo: "library-backups/2026-10-15T10-00-00" })
    });
    const user = userEvent.setup();
    renderLibrary();
    await user.click(await usable(await screen.findByRole("button", { name: "Restore…" })));
    const confirm = await screen.findByRole("alertdialog", { name: "Confirm restore" });
    expect(posts("/library/restore")).toHaveLength(0);
    await user.click(await usable(within(confirm).getByRole("button", { name: "Restore" })));
    expect(await screen.findByText("The previous library was saved to library-backups/2026-10-15T10-00-00.")).toBeTruthy();
    expect(posts("/library/restore")[0]!.body).toEqual({ repository: "acme/beltline-backup", path: "" });
  });

  it("compares a shared item's files beside your own copy", async () => {
    const files = { files: [{ path: "belts/issue-flow/v3.json", repo: '{"version":3}', local: '{"version":2}' }] };
    const { requests } = mockLibrary({
      "/api/v1/library/repos": repos(sharedRepo),
      "/api/v1/library/item": () => json(files)
    });
    const user = userEvent.setup();
    renderLibrary();
    const row = (await screen.findByText("Issue flow")).closest("li")!;
    await user.click(within(row).getByRole("button", { name: "Compare" }));
    const sheet = await screen.findByRole("dialog", { name: "Compare Issue flow" });
    expect(await within(sheet).findByText("belts/issue-flow/v3.json")).toBeTruthy();
    expect(within(sheet).getByText('{"version":3}')).toBeTruthy();
    expect(within(sheet).getByText('{"version":2}')).toBeTruthy();
    const read = requests.find(request => request.path.startsWith("/api/v1/library/item?"))!;
    expect(new URLSearchParams(read.path.split("?")[1]).get("id")).toBe("issue-flow");
  });

  it("shows a daemon refusal of a command as its problems", async () => {
    mockLibrary({
      "/api/v1/library/repos": repos(backupRepo),
      "/api/v1/library/backup": () => json({ message: "Backup failed", problems: ["git push was rejected"] }, 422)
    });
    const user = userEvent.setup();
    renderLibrary();
    await user.click(await usable(await screen.findByRole("button", { name: "Back up" })));
    expect(await screen.findByText("Backup failed")).toBeTruthy();
    expect(screen.getByText("git push was rejected")).toBeTruthy();
  });

  it("shows a refresh that timed out in the repository's error area, and Refresh runs again", async () => {
    let refreshes = 0;
    const { posts } = mockLibrary({
      "/api/v1/library/repos": repos(sharedRepo),
      "/api/v1/library/refresh": () => {
        refreshes += 1;
        return refreshes === 1 ? json({ message: "git fetch failed: command timed out", problems: [] }, 422) : json(sharedRepo);
      }
    });
    const user = userEvent.setup();
    renderLibrary();
    const shared = await screen.findByRole("region", { name: "Library acme/team-library" });
    await user.click(await usable(within(shared).getByRole("button", { name: "Refresh" })));
    const alert = await within(shared).findByRole("alert");
    expect(alert.textContent).toContain("git fetch failed: command timed out");
    await user.click(await usable(within(shared).getByRole("button", { name: "Refresh" })));
    await waitFor(() => expect(posts("/library/refresh")).toHaveLength(2));
    await waitFor(() => expect(within(shared).queryByRole("alert")).toBeNull());
    expect((within(shared).getByRole("button", { name: "Refresh" }) as HTMLButtonElement).disabled).toBe(false);
  });

  it("points to Settings when no library repository is set up", async () => {
    mockLibrary({ "/api/v1/library/repos": repos() });
    renderLibrary();
    expect(await screen.findByRole("link", { name: "Add one in Settings → Library repositories" })).toBeTruthy();
  });
});
