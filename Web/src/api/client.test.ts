import { describe, expect, it, vi } from "vitest";
import { apiRequest } from "./client";
import { ContractError, NetworkError, NotFoundError, SignInRequiredError, ValidationError } from "./errors";
import type { VersionResponse } from "./wire.gen";

const response = (status: number, body: string, headers?: HeadersInit) => new Response(body, { status, headers });

describe("same-origin daemon API client", () => {
  it("uses same-origin credentials and validates a successful JSON response", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response(200, '{"apiVersion":1,"version":"dev"}'));
    await expect(apiRequest<VersionResponse>("/api/v1/version", { fetcher })).resolves.toEqual({ apiVersion: 1, version: "dev" });
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[0]).toBe("/api/v1/version");
    expect(fetcher.mock.calls[0]?.[1]?.credentials).toBe("same-origin");
  });

  it("maps 401 to sign-in and never replays a command", async () => {
    const unauthorized = vi.fn();
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response(401, "{}"));
    await expect(apiRequest<VersionResponse>("/api/v1/pause", { method: "POST", body: { paused: true }, fetcher, onUnauthorized: unauthorized }))
      .rejects.toBeInstanceOf(SignInRequiredError);
    expect(unauthorized).toHaveBeenCalledTimes(1);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0]?.[1]?.method).toBe("POST");
  });

  it("maps 404 and preserves the daemon's message", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response(404, '{"message":"Run missing","problems":[]}'));
    await expect(apiRequest<VersionResponse>("/api/v1/runs/missing", { fetcher })).rejects.toMatchObject<Partial<NotFoundError>>({
      name: "NotFoundError", status: 404, message: "Run missing"
    });
  });

  it("returns a typed 422 with ordered problems", async () => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValue(response(422, '{"message":"Invalid request","problems":["first","second"]}'));
    await expect(apiRequest<VersionResponse>("/api/v1/pause", { method: "POST", body: {}, fetcher })).rejects.toMatchObject<Partial<ValidationError>>({
      name: "ValidationError", message: "Invalid request", problems: ["first", "second"]
    });
  });

  it("keeps other HTTP, invalid JSON, and network failures visible", async () => {
    const serverFailure = vi.fn<typeof fetch>().mockResolvedValue(response(500, '{"message":"Daemon failed","problems":[]}'));
    await expect(apiRequest<VersionResponse>("/api/v1/version", { fetcher: serverFailure })).rejects.toThrow("Daemon failed");

    const malformed = vi.fn<typeof fetch>().mockResolvedValue(response(200, "<html>bad</html>"));
    await expect(apiRequest<VersionResponse>("/api/v1/version", { fetcher: malformed })).rejects.toBeInstanceOf(ContractError);

    const disconnected = vi.fn<typeof fetch>().mockRejectedValue(new TypeError("fetch failed"));
    await expect(apiRequest<VersionResponse>("/api/v1/version", { fetcher: disconnected })).rejects.toBeInstanceOf(NetworkError);
  });
});
