import type { Backend } from "../api/wire.gen";

/** A harness's name as the app writes it. */
export const backendLabels: Record<Backend, string> = {
  copilot: "Copilot CLI", pi: "Pi", claude: "Claude Code", codex: "Codex CLI", cursor: "Cursor CLI"
};
