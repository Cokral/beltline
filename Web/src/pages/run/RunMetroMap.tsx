import { useRef, type CSSProperties } from "react";
import type { RunMetro, RunMetroStation } from "../../api/wire.gen";
import { Caps, StatusPill, type Look } from "../../components/Paper";
import { MetroShape } from "../../theme/stages/metroShape";
import { trackData } from "../../theme/stages/tracks";
import { useCentreOnStage } from "./centerOnStage";
import "./runMetro.css";

export interface RunMetroMapProps {
  metro: RunMetro;
  metadata: string;
  caption?: string;
  selectedStageId: string | null;
  onSelectStage: (stageId: string) => void;
  /** The run and its current stage, which the line opens centred on. */
  opening?: { run: string; stage: string };
}

/** The current station's halo: its ring sits concentric with the glyph, clear of the glyph's corners and the name. */
const haloRadius = 10;

/**
 * The run's current station: a solid halo in its status colour, and a ring that pulses out from it. Both are
 * centred on the station's own point, so they are concentric with the glyph whatever its shape.
 */
function Halo({ station }: { station: RunMetroStation }) {
  const { x, y } = station;
  return <>
    <circle cx={x} cy={y} r={haloRadius} className={`run-metro-halo is-${station.look}`} />
    <circle cx={x} cy={y} r={haloRadius} className={`run-metro-pulse is-${station.look}`} aria-hidden="true" />
  </>;
}

/** A station's click area over its glyph and name, and its name under it. The area is the daemon's; a selected station's glyph is blue. */
function StationControl({ station, selected, onSelect }: { station: RunMetroStation; selected: boolean; onSelect: (stageId: string) => void }) {
  const label = `${String(station.number).padStart(2, "0")}. ${station.name} — ${station.status}`;
  const { hit, label: name } = station;
  return <>
    <button type="button" className="run-metro-hit" data-stage={station.id}
      data-selected={selected ? "true" : "false"} aria-label={label} aria-pressed={selected} title={label}
      style={{ left: hit.x, top: hit.y, width: hit.width, height: hit.height }} onClick={() => onSelect(station.id)} />
    <span className={`run-metro-name run-metro-name-${station.state}`} aria-hidden="true"
      style={{ left: name.x, top: name.y, width: name.width, height: name.height }}>
      {station.lines.map((line, index) => <span key={index}>{line}</span>)}
    </span>
  </>;
}

/** The status chip above a station that is done or current: the same text and look the run page's boxes show. */
function StatusTag({ station }: { station: RunMetroStation }) {
  const box = station.statusBox;
  return <span className="run-metro-status" data-status={station.id} aria-hidden="true"
    style={{ left: box.x, top: box.y, width: box.width, height: box.height }}>
    <StatusPill text={station.status} look={station.look as Look} compact />
  </span>;
}

/**
 * The run's belt as a metro line (RunMetro in BeltlineCore): the line fills behind the run, the
 * untaken routes are dashed and faded, the station the run is at is haloed in its status colour, and the
 * stations behind it and the current one carry their status. Every coordinate comes from the daemon; this
 * only draws them and reports the station picked.
 */
export function RunMetroMap({ metro, metadata, caption, selectedStageId, onSelectStage, opening }: RunMetroMapProps) {
  const section = useRef<HTMLElement>(null);
  useCentreOnStage(section, opening);
  const colour = `var(--metro-line-${metro.colour})`;
  const pill: CSSProperties = {
    left: metro.trigger.box.x, top: metro.trigger.box.y, width: metro.trigger.box.width, height: metro.trigger.box.height, background: colour
  };
  return <section ref={section} className="run-line run-metro paper-card paper-card--raised" aria-label="Stage line">
    <header className="run-line-header"><Caps>{metadata}</Caps>{caption && <Caps>{caption}</Caps>}</header>
    <div className="run-metro-scroll" data-scroll-viewport>
      <div className="run-metro-canvas" data-ready="true" style={{ width: metro.width, height: metro.height }}>
        <svg className="run-metro-lines" width={metro.width} height={metro.height} aria-hidden="true" focusable="false">
          {metro.segments.map(segment => <path key={segment.id} d={trackData(segment.path)} data-segment={segment.id} data-state={segment.state}
            className={`run-metro-line run-metro-line-${segment.state}`} style={{ "--line": colour } as CSSProperties} />)}
          {metro.ends.map((end, index) => <line key={`end:${index}`} className="run-metro-terminus"
            x1={end.x} x2={end.x} y1={end.y - 7.5} y2={end.y + 7.5} />)}
          {metro.stations.map(station => <g key={station.id} data-stop={station.id}
            className={`run-metro-stop run-metro-stop-${station.state}${station.state === "current" ? ` is-${station.look}`
              : station.id === selectedStageId ? " is-selected" : ""}`}>
            {station.state === "current" && <Halo station={station} />}
            <MetroShape shape={station.glyph} x={station.x} y={station.y} base="run-metro-glyph" />
          </g>)}
        </svg>
        <span className="run-metro-pill" style={pill}>{metro.trigger.text}</span>
        {metro.routes.map(route => <span key={route.id} data-route={route.id}
          className={`run-metro-route${route.taken === false ? " is-not-taken" : ""}`}
          style={{ left: route.x, top: route.y, width: route.width, height: route.height }}
          title={route.taken === false ? `The ${route.text} route: not taken` : `The ${route.text} route`}>{route.text}</span>)}
        {metro.stations.map(station => <StationControl key={station.id} station={station} selected={station.id === selectedStageId}
          onSelect={onSelectStage} />)}
        {metro.stations.filter(station => station.tagged).map(station => <StatusTag key={station.id} station={station} />)}
      </div>
    </div>
  </section>;
}
