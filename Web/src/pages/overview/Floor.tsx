import type { ReactNode } from "react";
import type { BeltLook, MetroLayout, OverviewFloor, OverviewFloorChip, OverviewRun } from "../../api/wire.gen";
import { FactoryMap } from "./FactoryMap";
import { BoxFloor } from "./BoxFloor";
import { MetroMap } from "./MetroMap";
import { Chip, NeedChip } from "../../components/Paper";

export interface FloorProps {
  /** The belt look the daemon says the Floor is drawn in. */
  look: BeltLook;
  floor: OverviewFloor;
  metroLayout?: MetroLayout;
  runs: ReadonlyMap<string, OverviewRun>;
  focus: string | null;
  pinned: string | null;
  onHover: (belt: string, pointing: boolean) => void;
  onPin: (belt: string) => void;
  onOpenRun: (chip: OverviewFloorChip) => void;
  onOpenRunById: (runId: string, stage?: string | null) => void;
  onOpenMachine: (machineId: string) => void;
  onOpenBelt: (beltId: string) => void;
}

/** The card the Floor and the Metro share: its title and the factory's totals, with a ring while anything needs you. */
export function FloorCard({ total, className = "", children }: { total: OverviewFloor["total"]; className?: string; children: ReactNode }) {
  return <section className={`overview-floor ${className}${total.needYou > 0 ? " needs-you" : ""}`} aria-label="Factory floor">
    <header className="overview-floor-header">
      <h2 className="overview-section-title">Factory floor</h2>
      <div className="overview-floor-totals">
        {total.needYou > 0 && <NeedChip>{total.needYou} need you</NeedChip>}
        <Chip look={total.running > 0 ? "running" : "plain"}>{total.running} RUNNING</Chip>
        <Chip tone="grey">{total.inProgress} IN PROGRESS</Chip>
        <Chip tone="good">{total.shippedToday} SHIPPED TODAY</Chip>
      </div>
    </header>
    {children}
  </section>;
}

/** The Floor in the belt look the daemon sent: the machines, the boxes or the metro, inside the same card. */
export function Floor({ look, floor, metroLayout, onOpenRunById, ...map }: FloorProps) {
  if (look === "metro") {
    return <FloorCard total={floor.total} className="overview-metro">
      {metroLayout ? <MetroMap layout={metroLayout} runs={map.runs} focus={map.focus} pinned={map.pinned} onHover={map.onHover}
        onPin={map.onPin} onOpenRun={onOpenRunById} onOpenMachine={map.onOpenMachine} /> : <Undrawn />}
    </FloorCard>;
  }
  if (!floor.layout) return <FloorCard total={floor.total}><Undrawn /></FloorCard>;
  if (look === "boxes") {
    return <FloorCard total={floor.total}>
      <BoxFloor layout={floor.layout} belts={floor.belts} boxes={floor.boxes ?? []} runs={map.runs} focus={map.focus} pinned={map.pinned}
        onHover={map.onHover} onPin={map.onPin} onOpenRun={map.onOpenRun} onOpenMachine={map.onOpenMachine} onOpenBelt={map.onOpenBelt} />
    </FloorCard>;
  }
  return <FloorCard total={floor.total}>
    <FactoryMap layout={floor.layout} belts={floor.belts} machines={floor.machines ?? []} runs={map.runs} focus={map.focus} pinned={map.pinned}
      onHover={map.onHover} onPin={map.onPin} onOpenRun={map.onOpenRun} onOpenMachine={map.onOpenMachine} onOpenBelt={map.onOpenBelt} />
  </FloorCard>;
}

/** Shown only if the daemon sent a look without its drawing, which the schema refuses; it never draws a guess. */
function Undrawn() {
  return <p className="empty-state" role="status">The Floor&apos;s drawing did not arrive.</p>;
}
