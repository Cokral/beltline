import { ApiError, ContractError, NetworkError, NotFoundError, SignInRequiredError, ValidationError } from "./errors";
import type { ErrorResponse } from "./wire.gen";

export interface ApiRequestOptions {
  method?: "GET" | "POST";
  body?: unknown;
  signal?: AbortSignal;
  onUnauthorized?: () => void;
  fetcher?: typeof fetch;
}

async function readBody(response: Response): Promise<unknown> {
  const text = await response.text();
  if (!text) return undefined;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    throw new ContractError(`The daemon returned invalid JSON (HTTP ${response.status}).`);
  }
}

/** The shared error body of a non-2xx response, or null when the body does not have its shape. */
function errorBody(body: unknown): ErrorResponse | null {
  const candidate = body as Partial<ErrorResponse> | null | undefined;
  return candidate && typeof candidate.message === "string" && Array.isArray(candidate.problems) ? candidate as ErrorResponse : null;
}

/**
 * Sends one request and returns the daemon's JSON as T. The types are generated from the Go wire types
 * (wire.gen.ts), so a response is trusted to have them; only the error bodies are inspected.
 */
export async function apiRequest<T>(path: string, options: ApiRequestOptions = {}): Promise<T> {
  const fetcher = options.fetcher ?? fetch;
  let response: Response;
  try {
    response = await fetcher(path, {
      method: options.method ?? "GET",
      credentials: "same-origin",
      headers: options.body === undefined ? undefined : { "content-type": "application/json" },
      body: options.body === undefined ? undefined : JSON.stringify(options.body),
      signal: options.signal
    });
  } catch (error) {
    if (error instanceof DOMException && error.name === "AbortError") throw error;
    throw new NetworkError();
  }

  if (response.status === 401) {
    options.onUnauthorized?.();
    throw new SignInRequiredError();
  }
  if (response.status === 404) {
    let message = "Not found";
    try {
      message = errorBody(await readBody(response))?.message ?? message;
    } catch { /* A 404 still has a useful typed result when its body is malformed. */ }
    throw new NotFoundError(message);
  }
  if (response.status === 422) {
    let body: unknown;
    try {
      body = await readBody(response);
    } catch {
      throw new ValidationError("The daemon rejected this request.", []);
    }
    const parsed = errorBody(body);
    // A refusal may carry more than the shared shape, such as a changed preview; that rest travels in `body`.
    if (!parsed) throw new ValidationError("The daemon rejected this request.", []);
    throw new ValidationError(parsed.message, parsed.problems, parsed.locations ?? [], body);
  }
  if (!response.ok) {
    let message = `The daemon returned HTTP ${response.status}.`;
    let problems: string[] = [];
    let locations: NonNullable<ErrorResponse["locations"]> = [];
    try {
      const parsed = errorBody(await readBody(response));
      if (parsed) {
        message = parsed.message;
        problems = parsed.problems;
        locations = parsed.locations ?? [];
      }
    } catch { /* Keep the status-based message for non-JSON failures. */ }
    throw new ApiError(message, response.status, problems, "http", locations);
  }

  return await readBody(response) as T;
}
