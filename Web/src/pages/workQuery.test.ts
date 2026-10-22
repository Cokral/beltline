import { describe, expect, it } from "vitest";
import { foldedValues, readWorkQuery, toggleFolded, withWorkQuery, workRequestPath } from "./workQuery";

describe("Work URL parameters", () => {
  it("reads omitted parameters as empty, and the default query requests the bare view", () => {
    const query = readWorkQuery(new URLSearchParams());
    expect(query).toEqual({ filter: "", belt: "", stage: "", search: "", groupBy: "" });
    expect(workRequestPath(query)).toBe("/api/v1/views/work");
  });

  it("requests each nonempty parameter and leaves the others out", () => {
    const query = readWorkQuery(new URLSearchParams("filter=you&belt=issues&groupBy=epic"));
    expect(workRequestPath(query)).toBe("/api/v1/views/work?filter=you&belt=issues&groupBy=epic");
  });

  it("encodes spaces, #, &, Unicode and ids in search and stage values", () => {
    const params = new URLSearchParams();
    params.set("search", "  café #12 & more ");
    params.set("stage", "review/ops & qa");
    const path = workRequestPath(readWorkQuery(params));
    const sent = new URLSearchParams(path.split("?")[1]);
    expect(sent.get("search")).toBe("  café #12 & more ");
    expect(sent.get("stage")).toBe("review/ops & qa");
    expect(path).not.toContain(" ");
  });

  it("keeps an unsupported filter or groupBy as written, so the daemon rejects it", () => {
    const query = readWorkQuery(new URLSearchParams("filter=bogus&groupBy=owner"));
    expect(workRequestPath(query)).toBe("/api/v1/views/work?filter=bogus&groupBy=owner");
  });

  it("sets and clears one parameter while preserving the others", () => {
    const start = new URLSearchParams("filter=parked&belt=docs&search=api");
    const changed = withWorkQuery(start, { groupBy: "status" });
    expect(changed.get("filter")).toBe("parked");
    expect(changed.get("belt")).toBe("docs");
    expect(changed.get("search")).toBe("api");
    expect(changed.get("groupBy")).toBe("status");
    expect(withWorkQuery(changed, { search: "" }).has("search")).toBe(false);
  });

  it("drops a parameter set to its default so the default URL has no parameters", () => {
    const params = withWorkQuery(new URLSearchParams("filter=you&groupBy=repo&belt=docs"), { filter: "active", groupBy: "none", belt: "all" });
    expect(params.toString()).toBe("");
  });
});

describe("folds in preferences", () => {
  it("keeps Runs and Watching folds in their own records, per dimension", () => {
    const runs = toggleFolded({}, "repo", "acme/web");
    expect([...foldedValues(runs, "repo")]).toEqual(["acme/web"]);
    expect(foldedValues(runs, "status").size).toBe(0);
    expect(foldedValues({}, "repo").size).toBe(0);
  });

  it("unfolds a value it already holds and keeps the rest", () => {
    const record = toggleFolded(toggleFolded({}, "epic", "KAN-1"), "epic", "No epic");
    const reopened = toggleFolded(record, "epic", "KAN-1");
    expect([...foldedValues(reopened, "epic")]).toEqual(["No epic"]);
  });

  it("treats a value with the same text in another dimension as a different group", () => {
    const record = toggleFolded({}, "belt", "Docs");
    expect(foldedValues(record, "repo").has("Docs")).toBe(false);
    expect(foldedValues(record, "belt").has("Docs")).toBe(true);
  });
});
