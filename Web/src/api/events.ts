import { eventResourceValues, type EventMessage } from "./wire.gen";

export { eventResourceValues, type EventResource, type EventMessage, type VersionResponse } from "./wire.gen";

export const supportedApiVersion = 1;

/** Whether a decoded WebSocket frame is one of the event messages the daemon sends. Only the fields the client reads are checked. */
export function isEventMessage(value: unknown): value is EventMessage {
  if (typeof value !== "object" || value === null) return false;
  const message = value as Record<string, unknown>;
  switch (message.type) {
    case "hello": return typeof message.apiVersion === "number";
    case "changed": return (eventResourceValues as readonly unknown[]).includes(message.resource) && typeof message.revision === "number";
    case "notification": return typeof message.title === "string" && typeof message.body === "string";
    default: return false;
  }
}
