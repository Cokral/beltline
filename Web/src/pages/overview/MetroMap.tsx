import { useState, type ButtonHTMLAttributes, type CSSProperties, type ReactNode } from "react";
import type { MetroLayout, MetroLayoutDot, MetroLayoutLabel, MetroLayoutStation, OverviewRun, RunMetroGlyph } from "../../api/wire.gen";
import { StatusPill, type Look } from "../../components/Paper";
import { MetroShape } from "../../theme/stages/metroShape";
import { focusOpacity } from "./focus";
import { runCardText } from "./Stations";

export interface MetroMapProps {
  layout: MetroLayout;
  runs: ReadonlyMap<string, OverviewRun>;
  focus: string | null;
  pinned: string | null;
  onHover: (belt: string, pointing: boolean) => void;
  onPin: (belt: string) => void;
  onOpenRun: (runId: string, stage?: string | null) => void;
  onOpenMachine: (machineId: string) => void;
}

/** The line width, an interchange's width and how far it reaches past its outer stops, as the daemon lays them out. */
const lineWidth = 6;
const interchangeWidth = 30;
const overhang = 7;

/** A button placed at a box the daemon sent: its left, top, width and height in points. */
function Box({ box, className, style, children, ...rest }: {
  box: { x: number; y: number; width: number; height: number };
  className: string;
  style?: CSSProperties;
  children?: ReactNode;
} & ButtonHTMLAttributes<HTMLButtonElement>) {
  return <button type="button" className={className} {...rest}
    style={{ left: box.x, top: box.y, width: box.width, height: box.height, ...style }}>{children}</button>;
}

function Glyph({ station }: { station: MetroLayoutStation }) {
  if (station.glyph === "interchange") {
    const x = station.x;
    return <>{station.pieces.map(piece => <rect key={`${piece.lower}-${piece.upper}`} className="metro-interchange"
      x={x - interchangeWidth / 2} y={piece.lower - overhang} width={interchangeWidth} height={piece.upper - piece.lower + overhang * 2}
      rx={station.kind === "square" ? 2 : 6} />)}</>;
  }
  return <MetroShape shape={station.glyph as RunMetroGlyph} x={station.x} y={station.top} base="metro-glyph" />;
}

/** The Metro as the daemon laid it out: lines, stations, dots and labels in its points, dimmed by the same focus as the Floor. */
export function MetroMap(props: MetroMapProps) {
  const { layout, runs, focus, pinned } = props;
  const [pointed, setPointed] = useState<string | null>(null);
  const pointedDot = pointed ? layout.dots.find(dot => dot.id === pointed) : undefined;
  // The focused belt's line is drawn last, so it runs over the others.
  const ordered = [...layout.lines.filter(line => line.beltId !== focus), ...layout.lines.filter(line => line.beltId === focus)];
  const hasReturnLane = layout.lines.some(line => line.returns.length > 0);

  return <div className="metro-ground">
    <div className="metro-scroll" data-testid="metro-scroll" data-scroll-viewport>
      <div className="metro-canvas" style={{ width: layout.width, height: layout.height }}>
        <svg className="metro-lines" width={layout.width} height={layout.height} aria-hidden="true" focusable="false">
          {layout.labels.map(label => {
            const edge = label.above ? label.box.y + label.box.height : label.box.y;
            if (Math.abs(edge - label.dot.y) <= 12) return null;
            return <line key={`leader:${label.runIds.join("+")}:${label.dot.x}:${label.dot.y}`} x1={label.dot.x} y1={edge} x2={label.dot.x} y2={label.dot.y}
              className="metro-leader" opacity={0.55 * focusOpacity([label.beltId], focus)} />;
          })}
          {layout.stations.filter(station => station.pieces.length > 1).map(station => <rect key={`link:${station.id}`} className="metro-link"
            x={station.x - 3.5} y={station.top} width={7} height={station.bottom - station.top} opacity={focusOpacity(station.belts, focus)} />)}
          {ordered.map(line => {
            const colour = `var(--metro-line-${line.colour})`;
            return <g key={`line:${line.id}`} data-belt={line.beltId} opacity={focusOpacity([line.beltId], focus)}>
              {line.paths.map((d, index) => <path key={index} d={d} className="metro-line"
                style={{ stroke: colour, strokeWidth: focus === line.beltId ? lineWidth + 1.5 : lineWidth }} />)}
              {line.returnPaths.map((d, index) => <path key={`return:${index}`} d={d} className="metro-return" style={{ stroke: colour }} />)}
              {line.endBars.map((bar, index) => <line key={`end:${index}`} className="metro-terminus" x1={bar.x} x2={bar.x} y1={bar.top} y2={bar.bottom} />)}
              {/* A wide transparent stroke over the line makes the whole track a hover target. */}
              {line.paths.map((d, index) => <path key={`hit:${index}`} d={d} className="metro-hit"
                onPointerEnter={() => props.onHover(line.beltId, true)} onPointerLeave={() => props.onHover(line.beltId, false)} />)}
            </g>;
          })}
          {layout.stations.map(station => <g key={`glyph:${station.id}`} opacity={focusOpacity(station.belts, focus)}><Glyph station={station} /></g>)}
        </svg>
        {layout.lines.flatMap(line => line.routes.map(route => <span key={`route:${line.beltId}:${route.id}`} data-route={route.id} data-belt={line.beltId}
          className="metro-route" style={{ left: route.box.x, top: route.box.y, width: route.box.width, height: route.box.height,
            borderColor: `var(--metro-line-${line.colour})`, opacity: focusOpacity([line.beltId], focus) }}>{route.text}</span>))}
        {layout.stations.map(station => <Station key={`station:${station.id}`} station={station} focus={focus} onOpen={() => props.onOpenMachine(station.id)} />)}
        {layout.stations.map(station => <span key={`status:${station.id}`} className="metro-status" data-status={station.id}
          style={{ left: station.status.box.x, top: station.status.box.y, width: station.status.box.width, height: station.status.box.height,
            opacity: focusOpacity(station.belts, focus) }}>
          <StatusPill text={station.status.text} look={station.status.look as Look} compact />
        </span>)}
        {layout.lines.map(line => <Box key={`pill:${line.id}`} box={line.nameBox} data-belt={line.beltId} className={`metro-pill${pinned === line.beltId ? " is-pinned" : ""}`}
          style={{ background: `var(--metro-line-${line.colour})`, opacity: focusOpacity([line.beltId], focus) }}
          title={pinned === line.beltId ? `Click to stop showing ${line.name}'s way through the factory` : `Click to keep ${line.name}'s way through the factory drawn`}
          onPointerEnter={() => props.onHover(line.beltId, true)} onPointerLeave={() => props.onHover(line.beltId, false)}
          onClick={() => props.onPin(line.beltId)}>{line.name}</Box>)}
        {layout.labels.map(label => <LabelButton key={`label:${label.runIds.join("+")}`} label={label} focus={focus}
          onOpen={runId => props.onOpenRun(runId, runs.get(runId)?.openStage)} />)}
        {layout.dots.map(dot => <DotButton key={dot.id} dot={dot} runs={runs} focus={focus} pointed={pointed === dot.id}
          onPoint={pointing => setPointed(current => pointing ? dot.id : current === dot.id ? null : current)}
          onOpen={() => {
            const runId = dot.runIds[0] ?? "";
            if (dot.overflow > 0) props.onOpenMachine(dot.stationId); else props.onOpenRun(runId, runs.get(runId)?.openStage);
          }} />)}
        {pointedDot && <DotCard dot={pointedDot} />}
      </div>
    </div>
    <MetroLegend returns={hasReturnLane} />
  </div>;
}

/** A station's name, set on one or two lines on a faint card, and its glyph's area, both opening its runs. */
function Station({ station, focus, onOpen }: { station: MetroLayoutStation; focus: string | null; onOpen: () => void }) {
  const opacity = focusOpacity(station.belts, focus);
  const title = `${station.name}: show its runs`;
  return <>
    <Box box={station.box} className="metro-station-hit" style={{ opacity }} title={title} aria-label={title} onClick={onOpen} />
    <Box box={station.nameBox} className="metro-station-name" style={{ opacity }} title={title} tabIndex={-1} onClick={onOpen}>
      {station.lines.map((line, index) => <span key={index}>{line}</span>)}
    </Box>
  </>;
}

function DotButton({ dot, runs, focus, pointed, onPoint, onOpen }: {
  dot: MetroLayoutDot; runs: ReadonlyMap<string, OverviewRun>; focus: string | null; pointed: boolean; onPoint: (pointing: boolean) => void; onOpen: () => void
}) {
  const overflow = dot.overflow > 0;
  const runId = dot.runIds[0] ?? "";
  return <button type="button" data-dot={dot.id} className={`metro-dot metro-dot-${dot.look}${dot.halo ? " is-halo" : ""}${overflow ? " is-overflow" : ""}${pointed ? " is-pointed" : ""}`}
    style={{ left: dot.at.x, top: dot.at.y, opacity: focusOpacity([dot.beltId], focus) }}
    aria-label={overflow ? `${dot.overflow} more runs at ${dot.stationId}` : runCardText(runs.get(runId), runId)}
    onPointerEnter={() => onPoint(true)} onPointerLeave={() => onPoint(false)} onFocus={() => onPoint(true)} onBlur={() => onPoint(false)} onClick={onOpen}>
    {overflow ? <span className="metro-dot-more">+{dot.overflow}</span> : <span className="metro-dot-mark" />}
  </button>;
}

/** What the pointed run is and where it stands, beside its dot: the daemon's card text and placement. */
function DotCard({ dot }: { dot: MetroLayoutDot }) {
  const { card } = dot;
  return <div className="metro-card" role="tooltip" style={{ left: card.left, top: card.top, width: card.width }}>
    <strong className={dot.overflow > 0 ? "metro-card-more" : "metro-card-title"}>{card.headline}</strong>
    {card.detail && <span className="metro-card-detail">{card.detail}</span>}
    <span className={dot.overflow > 0 ? "metro-card-detail" : "metro-card-hint"}>{card.hint}</span>
  </div>;
}

function LabelButton({ label, focus, onOpen }: {
  label: MetroLayoutLabel;
  focus: string | null;
  onOpen: (runId: string) => void;
}) {
  const runId = label.openRunId;
  return <Box box={label.box} className={`metro-label${label.needsYou ? " needs-you" : ""}${label.urgent ? ` is-${label.urgent}` : ""}`}
    style={{ opacity: focusOpacity([label.beltId], focus) }} title={`Open ${label.text.split(" ")[0] ?? "the run"}`}
    onClick={() => { if (runId) onOpen(runId); }}>{label.text}</Box>;
}

/** Station glyphs and the five run looks, plus return lanes when the map has them. */
function MetroLegend({ returns }: { returns: boolean }) {
  return <ul className="metro-legend" aria-label="Legend">
    <li><span className="legend-sample"><span className="legend-circle" /></span>agent</li>
    <li><span className="legend-sample"><span className="legend-diamond" /></span>approval</li>
    <li><span className="legend-sample"><span className="legend-square" /></span>watch</li>
    <li><span className="legend-sample"><span className="legend-shared" /></span>shared by several belts</li>
    {returns && <li><span className="legend-sample"><span className="legend-return" /></span>return lane</li>}
    <li className="legend-rule" aria-hidden="true" />
    <li><span className="legend-sample"><span className="legend-dot metro-dot-running" /></span>running</li>
    <li><span className="legend-sample"><span className="legend-dot metro-dot-waiting" /></span>needs you</li>
    <li><span className="legend-sample"><span className="legend-dot metro-dot-held" /></span>held</li>
    <li><span className="legend-sample"><span className="legend-dot metro-dot-queued" /></span>queued</li>
    <li><span className="legend-sample"><span className="legend-dot metro-dot-parked" /></span>parked</li>
  </ul>;
}
