import { Sheet } from "../components/Sheet";

export interface AgentResetSheetProps {
  /** The agent being reset, or null when none is open. */
  agent: { name: string } | null;
  open: boolean;
  onClose: () => void;
  onConfirm: () => void;
}

/** The confirmation before an agent is reset to the version the app ships. */
export function AgentResetSheet({ agent, open, onClose, onConfirm }: AgentResetSheetProps) {
  return <Sheet title="Reset to default" open={open} onClose={onClose} footer={<>
    <button type="button" className="paper-button" onClick={onClose}>Cancel</button>
    <button type="button" className="paper-button paper-button--primary" onClick={onConfirm}>Confirm</button>
  </>}>
    <p>{agent ? `Reset ${agent.name} to the version the app ships?` : "Reset this agent to the version the app ships?"}</p>
    <p>Beltline saves the shipped content as a new version. Belts that use this agent keep the version they pin until you move them.</p>
  </Sheet>;
}
