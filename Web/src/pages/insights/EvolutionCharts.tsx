// The Evolution view's charts, drawn from the daemon's columns, statistics and ticks. Every chart shares
// one frame of columns, so a version sits at the same x in each, and clicking a column starts recent there.
import type { ReactNode } from "react";
import type { InsightsColumn, InsightsEvolutionRun, InsightsFigure, InsightsGrid, InsightsSpread, InsightsStack, InsightsTick } from "../../api/wire.gen";

export const chartWidth = 1100;
const margin = { top: 22, right: 12, bottom: 44, left: 56 };

/** A scale from values to pixels, linear from zero or logarithmic from low. */
export function scaleOf(low: number, high: number, from: number, to: number, log: boolean): (value: number) => number {
  if (log) {
    const a = Math.log10(low), b = Math.log10(Math.max(high, low * 1.0001));
    return value => from + (Math.log10(Math.max(value, low)) - a) / (b - a) * (to - from);
  }
  return value => from + (value - low) / ((high - low) || 1) * (to - from);
}

/** A string's hash, for a run's steady place inside its column. */
function hash(text: string): number {
  let h = 2166136261;
  for (let i = 0; i < text.length; i++) h = Math.imul(h ^ text.charCodeAt(i), 16777619);
  return (h >>> 0) / 4294967296;
}

interface Frame { width: number; x: (column: number) => number; height: number; left: number }

function frameOf(count: number, height: number, left = margin.left): Frame {
  const width = (chartWidth - left - margin.right) / Math.max(count, 1);
  return { width, x: column => left + width * (column + 0.5), height, left };
}

/** The columns' backdrop: recent shaded from `from`, and a target per column that starts recent there. */
function Backdrop({ frame, columns, from, onFrom, bottom }: { frame: Frame; columns: InsightsColumn[]; from: number; onFrom: (column: number) => void; bottom: number }) {
  const top = margin.top;
  const split = from > 0 ? frame.x(from) - frame.width / 2 : null;
  return <>
    {split !== null && <rect className="evolution-recent" x={split} y={top} width={chartWidth - margin.right - split} height={bottom - top} />}
    {columns.map((column, i) => <rect key={i} className={`evolution-column ${i > 0 ? "is-pickable" : ""}`} x={frame.x(i) - frame.width / 2} y={top} width={frame.width} height={bottom - top}
      onClick={() => { if (i > 0) onFrom(i); }}><title>{i > 0 ? `${column.label}: start recent here` : column.label}</title></rect>)}
    {split !== null && <>
      <line className="evolution-split" x1={split} x2={split} y1={top - 8} y2={bottom} />
      <text className="evolution-split-label" x={split + 6} y={top - 2}>RECENT →</text>
    </>}
  </>;
}

/** The column labels under a chart, thinned to fit, with a mark under each column whose belt changed. */
function ColumnAxis({ frame, columns, bottom, marks = true }: { frame: Frame; columns: InsightsColumn[]; bottom: number; marks?: boolean }) {
  const every = Math.max(1, Math.ceil(columns.length / Math.floor((chartWidth - frame.left - margin.right) / 48)));
  return <>
    <line className="evolution-axis" x1={frame.left} x2={chartWidth - margin.right} y1={bottom} y2={bottom} />
    {columns.map((column, i) => <g key={i}>
      {i % every === 0 && <text x={frame.x(i)} y={bottom + 16} textAnchor="middle">{column.label}</text>}
      {marks && column.changes > 0 && <rect className="evolution-change" x={frame.x(i) - 3.5} y={bottom + 24} width={7} height={7} rx={1}
        transform={`rotate(45 ${frame.x(i)} ${bottom + 27.5})`}><title>{column.changeCaption}</title></rect>}
    </g>)}
  </>;
}

function Grid({ ticks, y, frame }: { ticks: InsightsTick[]; y: (value: number) => number; frame: Frame }) {
  return <>{ticks.map(tick => <g key={tick.value}>
    <line className="evolution-grid" x1={frame.left} x2={chartWidth - margin.right} y1={y(tick.value)} y2={y(tick.value)} />
    <text x={frame.left - 8} y={y(tick.value) + 3} textAnchor="end">{tick.label}</text>
  </g>)}</>;
}

function Chart({ height, label, children }: { height: number; label: string; children: ReactNode }) {
  return <div className="evolution-chart"><svg viewBox={`0 0 ${chartWidth} ${height}`} role="img" aria-label={label}>{children}</svg></div>;
}

interface ColumnsProps { columns: InsightsColumn[]; from: number; onFrom: (column: number) => void }

/** A figure by column: one dot per run, the middle half of each column and its median, or for a share a bar. */
export function FigureChart({ figure, runs, log, ...props }: ColumnsProps & { figure: InsightsFigure; runs: InsightsEvolutionRun[]; log: boolean }) {
  const height = 330, bottom = height - margin.bottom;
  const frame = frameOf(props.columns.length, height);
  const useLog = log && figure.scalable;
  // The log scale starts a little under the smallest value, so its dots sit clear of the axis.
  const low = useLog ? figure.logMin / 1.15 : 0;
  const y = scaleOf(low, figure.max * (useLog ? 1.2 : figure.share ? 1 : 1.06), bottom, margin.top, useLog);
  const ticks = useLog ? figure.logTicks : figure.ticks;
  const band = Math.min(frame.width * 0.68, 32);
  const medians = figure.columns.map((stat, i) => stat.value == null ? null : [frame.x(i), y(stat.value)] as const);
  const line = medians.filter(point => point !== null).map((point, i) => `${i ? "L" : "M"}${point[0].toFixed(1)} ${point[1].toFixed(1)}`).join("");
  return <Chart height={height} label={`${figure.label} by column`}>
    <Backdrop frame={frame} bottom={bottom} {...props} />
    <Grid ticks={ticks} y={y} frame={frame} />
    {figure.share ? figure.columns.map((stat, i) => stat.value == null ? null : <g key={i}>
      <rect className="evolution-bar-rest" x={frame.x(i) - band / 2} y={y(1)} width={band} height={y(0) - y(1)} rx={3} />
      <rect className="evolution-bar" x={frame.x(i) - band / 2} y={y(stat.value)} width={band} height={y(0) - y(stat.value)} rx={3}><title>{`${props.columns[i]?.label}: ${stat.display}`}</title></rect>
    </g>) : <>
      {figure.columns.map((stat, i) => stat.low == null || stat.high == null ? null :
        <rect key={i} className="evolution-middle" x={frame.x(i) - band / 2} y={y(stat.high)} width={band} height={Math.max(1, y(stat.low) - y(stat.high))} rx={4} />)}
      {figure.dots.map(dot => {
        const run = runs[dot.run];
        if (!run) return null;
        const x = frame.x(run.column) + (hash(run.id) - 0.5) * Math.min(frame.width * 0.6, 28);
        return <circle key={run.id} className={`evolution-dot is-${run.outcome} ${dot.partial ? "is-partial" : ""}`} cx={x} cy={y(Math.max(dot.value, low))} r={3.4}>
          <title>{`${run.ref} ${run.title}\n${dot.display} · v${run.version} · ${run.outcome}${dot.partial ? " · partly priced" : ""}\nFinished ${run.finished}`}</title>
        </circle>;
      })}
      <path className="evolution-median" d={line} />
      {figure.columns.map((stat, i) => stat.value == null ? null : <circle key={i} className="evolution-median-dot" cx={frame.x(i)} cy={y(stat.value)} r={3.6}><title>{`${props.columns[i]?.label}: ${stat.display}`}</title></circle>)}
    </>}
    <ColumnAxis frame={frame} columns={props.columns} bottom={bottom} />
  </Chart>;
}

/** A stacked chart: each series' mean per run in each column, with a share drawn over it when there is one. */
export function StackChart({ stack, label, colors, ...props }: ColumnsProps & { stack: InsightsStack; label: string; colors: readonly string[] }) {
  const height = 260, bottom = height - margin.bottom;
  const frame = frameOf(props.columns.length, height);
  const y = scaleOf(0, stack.max * 1.08, bottom, margin.top, false);
  const band = Math.min(frame.width * 0.62, 30);
  const share = (value: number) => margin.top + (1 - value) * (bottom - margin.top);
  const line = stack.line;
  return <Chart height={height} label={label}>
    <Backdrop frame={frame} bottom={bottom} {...props} />
    <Grid ticks={stack.ticks} y={y} frame={frame} />
    {props.columns.map((column, i) => {
      let base = 0;
      const tip = `${column.label} · ${stack.runs[i]}\n${stack.series.map(series => `${series.label}: ${series.displays[i]}`).join("\n")}`;
      return <g key={i}>{stack.series.map((series, s) => {
        const value = series.values[i] ?? 0;
        if (!value) return null;
        const rect = <rect key={s} x={frame.x(i) - band / 2} y={y(base + value)} width={band} height={Math.max(0.6, y(base) - y(base + value))} style={{ fill: colors[s % colors.length] }}><title>{tip}</title></rect>;
        base += value;
        return rect;
      })}</g>;
    })}
    {line && <>
      <path className="evolution-share-line" d={line.values.map((value, i) => `${i ? "L" : "M"}${frame.x(i)} ${share(value)}`).join("")} />
      {line.values.map((value, i) => <circle key={i} className="evolution-share-dot" cx={frame.x(i)} cy={share(value)} r={3}><title>{`${props.columns[i]?.label}: ${line.displays[i]}`}</title></circle>)}
    </>}
    <ColumnAxis frame={frame} columns={props.columns} bottom={bottom} />
  </Chart>;
}

/** The sends back as a grid: one row each, one cell per column, darker for more per run. */
export function SendsBackGrid({ grid, ...props }: ColumnsProps & { grid: InsightsGrid }) {
  const left = 290, rowHeight = 30, top = 18;
  const height = top + grid.rows.length * rowHeight + 30, bottom = top + grid.rows.length * rowHeight;
  const frame = frameOf(props.columns.length, height, left);
  const split = props.from > 0 ? frame.x(props.from) - frame.width / 2 : null;
  const short = (value: number) => value >= 10 ? String(Math.round(value)) : value >= 1 ? value.toFixed(1) : value.toFixed(2).replace(/^0/, "");
  return <Chart height={height} label="Sends back per run">
    {split !== null && <rect className="evolution-recent" x={split - 1} y={top - 4} width={chartWidth - margin.right - split + 1} height={bottom - top + 4} />}
    {grid.rows.map((row, r) => {
      const rowTop = top + r * rowHeight;
      return <g key={row.label}>
        <text className="evolution-row-label" x={left - 12} y={rowTop + rowHeight / 2 + 4} textAnchor="end">{row.label}</text>
        {row.values.map((value, i) => {
          const strength = grid.max > 0 ? Math.round(Math.min(1, value / grid.max) * 88) : 0;
          const width = Math.max(4, frame.width - 4);
          return <g key={i} onClick={() => { if (i > 0) props.onFrom(i); }} className={i > 0 ? "is-pickable" : ""}>
            <rect x={frame.x(i) - width / 2} y={rowTop + 2} width={width} height={rowHeight - 4} rx={4}
              style={{ fill: `color-mix(in srgb, var(--red) ${strength}%, var(--paper))` }}><title>{`${row.label}\n${props.columns[i]?.label}: ${row.displays[i]}`}</title></rect>
            {value >= 0.005 && frame.width >= 22 && <text className={`evolution-cell ${strength > 50 ? "is-strong" : ""}`} x={frame.x(i)} y={rowTop + rowHeight / 2 + 3.5} textAnchor="middle">{short(value)}</text>}
          </g>;
        })}
      </g>;
    })}
    {split !== null && <line className="evolution-split" x1={split - 1} x2={split - 1} y1={top - 8} y2={bottom} />}
    <ColumnAxis frame={frame} columns={props.columns} bottom={bottom} marks={false} />
  </Chart>;
}

/** How before and recent are spread: the share of each group's runs in each range. */
export function Histogram({ figure, spread, log }: { figure: InsightsFigure; spread: InsightsSpread; log: boolean }) {
  const width = 620, height = 230, m = { top: 12, right: 12, bottom: 30, left: 44 };
  const useLog = log && figure.scalable && spread.logBins.length > 0;
  const bins = useLog ? spread.logBins : spread.bins;
  if (bins.length === 0) return null;
  const first = bins[0]!, last = bins[bins.length - 1]!;
  const x = scaleOf(first.from, last.to, m.left, width - m.right, useLog);
  const top = Math.max(0.01, ...bins.map(bin => Math.max(bin.before, bin.recent)));
  const y = scaleOf(0, top * 1.12, height - m.bottom, m.top, false);
  const shares = [0, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.4, 0.5, 0.6, 0.8, 1].filter(value => value <= top * 1.12).filter((_, i, all) => all.length <= 6 || i % 2 === 0);
  const ticks = (useLog ? figure.logTicks : figure.ticks).filter(tick => tick.value >= first.from && tick.value <= last.to);
  return <div className="evolution-chart"><svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Runs per range">
    {shares.map(share => <g key={share}>
      <line className="evolution-grid" x1={m.left} x2={width - m.right} y1={y(share)} y2={y(share)} />
      <text x={m.left - 6} y={y(share) + 3} textAnchor="end">{`${Math.round(share * 100)}%`}</text>
    </g>)}
    {figure.scalable ? ticks.map(tick => <text key={tick.value} x={x(tick.value)} y={height - m.bottom + 16} textAnchor="middle">{tick.label}</text>)
      : bins.filter((_, i) => i % Math.ceil(bins.length / 8) === 0).map(bin => <text key={bin.from} x={(x(bin.from) + x(bin.to)) / 2} y={height - m.bottom + 16} textAnchor="middle">{bin.label}</text>)}
    {bins.map(bin => {
      const left = x(bin.from) + 1.5, w = Math.max(1, x(bin.to) - x(bin.from) - 3);
      return <g key={bin.from}>
        {bin.before > 0 && <rect className="evolution-before" x={left} y={y(bin.before)} width={w} height={y(0) - y(bin.before)} rx={2}><title>{`${bin.label}\nBefore: ${Math.round(bin.before * 100)}% of runs`}</title></rect>}
        {bin.recent > 0 && <rect className="evolution-recent-bar" x={left + w * 0.22} y={y(bin.recent)} width={w * 0.56} height={y(0) - y(bin.recent)} rx={2}><title>{`${bin.label}\nRecent: ${Math.round(bin.recent * 100)}% of runs`}</title></rect>}
      </g>;
    })}
    <line className="evolution-axis" x1={m.left} x2={width - m.right} y1={height - m.bottom} y2={height - m.bottom} />
  </svg></div>;
}

/** The share of each group's runs at or under each value, as steps. */
export function Cumulative({ figure, spread, log }: { figure: InsightsFigure; spread: InsightsSpread; log: boolean }) {
  const width = 460, height = 230, m = { top: 12, right: 12, bottom: 30, left: 44 };
  const useLog = log && figure.scalable;
  const low = useLog ? figure.logMin : Math.min(spread.before[0] ?? 0, spread.recent[0] ?? 0, 0);
  const x = scaleOf(low, figure.max, m.left, width - m.right, useLog);
  const y = scaleOf(0, 1, height - m.bottom, m.top, false);
  const steps = (values: number[]) => values.length === 0 ? "" :
    `M${m.left} ${y(0)}${values.map((value, i) => ` H${x(Math.max(value, low)).toFixed(1)} V${y((i + 1) / values.length).toFixed(1)}`).join("")} H${width - m.right}`;
  const ticks = useLog ? figure.logTicks : figure.ticks;
  return <div className="evolution-chart"><svg viewBox={`0 0 ${width} ${height}`} role="img" aria-label="Share of runs at or under a value">
    {[0, 0.5, 0.8, 1].map(share => <g key={share}>
      <line className="evolution-grid" x1={m.left} x2={width - m.right} y1={y(share)} y2={y(share)} />
      <text x={m.left - 6} y={y(share) + 3} textAnchor="end">{`${Math.round(share * 100)}%`}</text>
    </g>)}
    {ticks.map(tick => <text key={tick.value} x={x(tick.value)} y={height - m.bottom + 16} textAnchor="middle">{tick.label}</text>)}
    <path className="evolution-step-before" d={steps(spread.before)} />
    <path className="evolution-step-recent" d={steps(spread.recent)} />
    <line className="evolution-axis" x1={m.left} x2={width - m.right} y1={height - m.bottom} y2={height - m.bottom} />
  </svg></div>;
}
