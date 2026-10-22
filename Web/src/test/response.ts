/**
 * A JSON response for a stubbed daemon. A string is sent as it is, so a test can send a body that is not valid JSON;
 * anything else is serialised.
 */
export const json = (body: unknown, status = 200): Response =>
  new Response(typeof body === "string" ? body : JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
