import { machineComponents, machineFaces, machineIds, machineTints, type MachineFace, type MachineId, type MachineTint } from "../../theme/machines/Machines";
import { Caps, Chip, NeedChip, StatusPill, type Look } from "../../components/Paper";
import type { OverviewFloorBelt, OverviewFloorChip, OverviewFloorMachine, OverviewRun } from "../../api/wire.gen";

/** A machine's drawing, by the identifiers the daemon sends; an unknown one draws as the box. */
export function machineDrawing(machine: string): (typeof machineComponents)[MachineId] {
  return machineComponents[(machineIds as readonly string[]).includes(machine) ? machine as MachineId : "box"];
}

export function machineTint(tint: string): MachineTint {
  return (machineTints as readonly string[]).includes(tint) ? tint as MachineTint : "grey";
}

export function machineFace(face: string): MachineFace {
  return (machineFaces as readonly string[]).includes(face) ? face as MachineFace : "todo";
}

/** A run chip's name: its reference, and its belt's short name where the machine is shared. */
export function chipLabel(chip: OverviewFloorChip): string {
  return [chip.ref, chip.belt].filter(Boolean).join(" · ");
}

/** The hover text for a run: its subject, repository, and where it is. */
export function runCardText(run: OverviewRun | undefined, fallback: string): string {
  if (!run) return fallback;
  return `${run.ref} ${run.title} · ${run.repository} · ${run.location.text}`;
}

/** One machine on the Floor and Metro: its status pill above, its drawing, and its numbered name below. */
export function Station({ machine, onOpen, dimmed }: { machine: OverviewFloorMachine; onOpen: () => void; dimmed: boolean }) {
  const Drawing = machineDrawing(machine.machine);
  return <button type="button" className="overview-station" onClick={onOpen}
    title={`${machine.name}, on ${machine.beltNames.join(", ")}: show its runs`}
    aria-label={`${machine.name}: ${machine.status.text.toLowerCase()}`}>
    <StatusPill text={machine.status.text} look={machine.status.look as Look} />
    <span className="overview-machine" aria-hidden="true">
      <Drawing face={machineFace(machine.status.face)} tint={machineTint(machine.tint)} width={80} height={72} dimmed={dimmed} />
    </span>
    <span className="station-name">
      <span className="station-number">{String(machine.number).padStart(2, "0")}</span>
      <span className="station-title">{machine.name}</span>
    </span>
  </button>;
}

/** A run as a small numbered chip below its machine. */
export function RunChip({ chip, run, onOpen }: { chip: OverviewFloorChip; run: OverviewRun | undefined; onOpen: () => void }) {
  return <button type="button" className={`overview-chip overview-chip-${chip.state}`}
    title={runCardText(run, chip.ref)} onClick={onOpen}>{chipLabel(chip)}</button>;
}

/** Where a belt comes onto the Floor: its name, trigger and counts. A click pins it; Runs opens its runs. */
export function EntranceCard({ belt, pinned, focused, dimmed, onPin, onHover, onRuns }: {
  belt: OverviewFloorBelt;
  pinned: boolean;
  focused: boolean;
  dimmed: boolean;
  onPin: () => void;
  onHover: (pointing: boolean) => void;
  onRuns: () => void;
}) {
  const help = pinned ? `Click to stop showing ${belt.name}'s way through the factory` : `Click to keep ${belt.name}'s way through the factory drawn`;
  return <div className={`overview-entrance${pinned ? " is-pinned" : ""}${focused ? " is-focused" : ""}${dimmed ? " is-dimmed" : ""}`}
    onPointerEnter={() => onHover(true)} onPointerLeave={() => onHover(false)} onClick={onPin} title={help}
    data-belt={belt.id}>
    <button type="button" className="overview-entrance-pin" aria-pressed={pinned} aria-label={help} onClick={event => { event.stopPropagation(); onPin(); }}>
      <span className="overview-entrance-name">{belt.name}</span>
      {belt.paused && <Chip tone="grey">PAUSED</Chip>}
    </button>
    <span className="overview-entrance-summary">{belt.summary}</span>
    <span className="overview-entrance-counts">
      {belt.counts.needYou > 0 && <NeedChip>{belt.counts.needYou} need you</NeedChip>}
      <Caps>{belt.counts.running} running · {belt.counts.inProgress} active</Caps>
    </span>
    <button type="button" className="action-button small overview-entrance-runs" onClick={event => { event.stopPropagation(); onRuns(); }}>Runs →</button>
  </div>;
}
