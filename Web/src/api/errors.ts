import type { ProblemLocation } from "./wire.gen";

export class ApiError extends Error {
  constructor(
    message: string,
    readonly status: number,
    readonly problems: string[] = [],
    readonly kind: "http" | "network" | "contract" = "http",
    /** One entry per problem, in order, when the daemon located them; empty otherwise. */
    readonly locations: ReadonlyArray<ProblemLocation | null> = []
  ) {
    super(message);
    this.name = "ApiError";
  }
}

export class SignInRequiredError extends ApiError {
  constructor() {
    super("Sign in to the local Beltline daemon.", 401);
    this.name = "SignInRequiredError";
  }
}

export class NotFoundError extends ApiError {
  constructor(message = "Not found") {
    super(message, 404);
    this.name = "NotFoundError";
  }
}

export class ValidationError extends ApiError {
  /** The daemon's whole 422 body, such as a changed preview, when it sent one. */
  readonly body: unknown;
  constructor(message: string, problems: string[], locations: ReadonlyArray<ProblemLocation | null> = [], body: unknown = null) {
    super(message, 422, problems, "http", locations);
    this.name = "ValidationError";
    this.body = body;
  }
}

export class NetworkError extends ApiError {
  constructor(message = "The daemon could not be reached.") {
    super(message, 0, [], "network");
    this.name = "NetworkError";
  }
}

export class ContractError extends ApiError {
  constructor(message: string) {
    super(message, 200, [], "contract");
    this.name = "ContractError";
  }
}

/** The message and ordered problems a user sees for any failed request. Problems are only ever daemon-supplied. */
export function describeError(error: unknown): { message: string; problems: string[] } {
  if (error instanceof ApiError) return { message: error.message, problems: [...error.problems] };
  if (error instanceof Error) return { message: error.message, problems: [] };
  return { message: "The request failed.", problems: [] };
}
