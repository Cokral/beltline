import { useId, useRef } from "react";
import { machineComponents, machineFaces, machineIds, machineTints, type MachineFace, type MachineId, type MachineTint } from "../../theme/machines/Machines";
import type { RunMap, RunStage } from "../../api/wire.gen";
import { Caps, StatusPill, type Look } from "../../components/Paper";

import { trackData } from "../../theme/stages/tracks";
import { useCentreOnStage } from "./centerOnStage";

function asMachine(value: string | undefined): MachineId {
  return (value && (machineIds as readonly string[]).includes(value) ? value : "box") as MachineId;
}
function asTint(value: string | undefined): MachineTint {
  return (value && (machineTints as readonly string[]).includes(value) ? value : "grey") as MachineTint;
}
function asFace(value: string | undefined): MachineFace {
  return (value && (machineFaces as readonly string[]).includes(value) ? value : "idle") as MachineFace;
}

export interface RunMapProps {
  map: RunMap;
  metadata: string;
  stages: readonly RunStage[];
  selectedStageId: string | null;
  onSelectStage: (stageId: string) => void;
  /** The run and its current stage, which the line opens centred on. */
  opening?: { run: string; stage: string };
}

/**
 * The run's belt as machines on a track (RunView.line and BeltMap): the track fills up to the stage the run is
 * at, routes not taken are dotted, and the run rides along as a crate. Every coordinate comes from the daemon.
 */
export function RunMap({ map, metadata, stages, selectedStageId, onSelectStage, opening }: RunMapProps) {
  const clip = useId();
  const section = useRef<HTMLElement>(null);
  useCentreOnStage(section, opening);
  const byId = new Map(stages.map(stage => [stage.id, stage]));
  const filled = new Set(map.filledTrackPathIds);
  const width = Math.max(map.contentWidth, 1);
  const size = map.compact ? 70 : 96;
  const lanes = map.tracks.filter(track => track.kind === "route");
  const main = map.tracks.filter(track => track.kind === "main");
  const fillTracks = map.tracks.filter(track => filled.has(track.id));
  return <section ref={section} className="run-line paper-card paper-card--raised" aria-label="Stage line">
    <header className="run-line-header"><Caps>{metadata}</Caps><Caps>{map.caption}</Caps></header>
    <div className="run-map-scroll" data-scroll-viewport>
      <div className="run-map" data-ready="true" style={{ width, height: map.height }}>
        <svg className="run-map-tracks" width={width} height={map.height} aria-hidden="true">
          <defs><clipPath id={clip}><rect x={-20} y={-20} width={Math.max(0, map.fillUntilX) + 20} height={map.height + 40} /></clipPath></defs>
          {main.map(track => <path key={track.id} d={trackData(track)} className="run-track" data-track={track.id} />)}
          {lanes.map(track => <path key={track.id} d={trackData(track)} data-track={track.id}
            className={track.taken === false ? "run-track run-track-not-taken" : "run-track"} />)}
          {map.fillUntilX > 0 && <g clipPath={`url(#${clip})`}>
            {fillTracks.map(track => <path key={`fill:${track.id}`} d={trackData(track)} className="run-track-fill" data-filled="true" />)}
            {map.moving && fillTracks.map(track => <path key={`stripes:${track.id}`} d={trackData(track)} className="run-track-stripes" />)}
          </g>}
        </svg>
        {map.placements.map(placement => {
          const stage = byId.get(placement.stageId);
          const selected = placement.stageId === selectedStageId;
          const Machine = machineComponents[asMachine(stage?.machine)];
          const status = map.compact ? stage?.compactStatus : stage?.status;
          const label = stage ? `${stage.number}. ${stage.name} — ${stage.status.text}` : placement.stageId;
          return <button key={placement.stageId} type="button" className={`run-station${selected ? " is-selected" : ""}${map.compact ? " is-compact" : ""}`}
            data-stage={placement.stageId} data-selected={selected ? "true" : "false"} aria-label={label} aria-pressed={selected}
            style={{ left: placement.x, top: placement.y, width: placement.width, height: placement.height }}
            onClick={() => onSelectStage(placement.stageId)}>
            <StatusPill text={status?.text ?? ""} look={(status?.look ?? "plain") as Look} compact={map.compact} />
            <span className="run-station-machine">
              <Machine width={size} height={size * 0.9} face={asFace(stage?.status.face)} tint={asTint(stage?.tint)} dimmed={placement.dimmed} />
            </span>
            <span className="run-station-name">
              <span className="station-number">{String(placement.number).padStart(2, "0")}</span>
              <span className="station-title">{stage?.name ?? placement.stageId}</span>
            </span>
            {stage?.worker && !map.compact && <span className="run-station-worker">{stage.worker}</span>}
          </button>;
        })}
        {map.routeLabels.map(label => <span key={label.id} className={`run-route-tag${label.taken === false ? " is-not-taken" : ""}`}
          style={{ left: label.x, top: label.y, maxWidth: Math.max(label.room, 40) }}
          title={label.taken === false ? `The ${label.text} route: not taken` : `The ${label.text} route`}>{label.text}</span>)}
        {map.crates.map(crate => <span key={crate.id} className={`run-crate run-crate-${crate.state}`} data-crate={crate.id}
          style={{ left: crate.x, top: crate.y }} aria-hidden="true">{crate.reference}</span>)}
      </div>
    </div>
  </section>;
}
