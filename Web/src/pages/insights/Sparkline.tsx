// Small lines over time, drawn from the daemon's 7-day rolling series. A day with nothing in its window leaves a gap.
import type { InsightsDelta, InsightsPoint, InsightsSeries } from "../../api/wire.gen";

function linePath(points: InsightsPoint[], x: (i: number) => number, y: (v: number) => number): string {
  let path = "";
  let drawing = false;
  points.forEach((point, i) => {
    if (point.value == null) { drawing = false; return; }
    path += `${drawing ? "L" : "M"}${x(i).toFixed(1)} ${y(point.value).toFixed(1)}`;
    drawing = true;
  });
  return path;
}

function areaPath(points: InsightsPoint[], x: (i: number) => number, y: (v: number) => number, base: number): string {
  const parts: string[] = [];
  let run: number[] = [];
  const close = () => {
    if (run.length > 1) parts.push(`M${x(run[0]!)} ${base}${run.map(i => `L${x(i).toFixed(1)} ${y(points[i]!.value!).toFixed(1)}`).join("")}L${x(run[run.length - 1]!)} ${base}Z`);
    run = [];
  };
  points.forEach((point, i) => { if (point.value == null) close(); else run.push(i); });
  close();
  return parts.join("");
}

/** A line with its area and its last value marked. */
export function Sparkline({ series, width = 88, height = 26, low, label }: {
  series: InsightsSeries; width?: number; height?: number; low?: boolean; label?: string;
}) {
  const pad = 3;
  const max = series.max;
  const count = series.points.length;
  const x = (i: number) => pad + (i * (width - 2 * pad)) / Math.max(1, count - 1);
  const y = (v: number) => height - pad - (v / max) * (height - 2 * pad);
  const last = series.points.map((point, i) => ({ point, i })).reverse().find(item => item.point.value != null);
  const title = label ?? series.points.filter(point => point.value != null).slice(-1).map(point => `${point.label}: ${point.display}`)[0];
  return <svg className={`insights-spark ${low ? "is-low" : ""}`} width={width} height={height} viewBox={`0 0 ${width} ${height}`} role="img" aria-label={title ?? "No data"}>
    {title && <title>{title}</title>}
    <path d={areaPath(series.points, x, y, height - pad)} className="insights-spark-area" />
    <path d={linePath(series.points, x, y)} className="insights-spark-line" />
    {last && <circle cx={x(last.i)} cy={y(last.point.value!)} r={2.4} className="insights-spark-dot" />}
  </svg>;
}

/** The change over the last 7 days, green when better and red when worse. */
export function DeltaBadge({ delta }: { delta: InsightsDelta }) {
  if (delta.tone === "none") return <span className="insights-delta is-none" title={delta.caption}>–</span>;
  const arrow = delta.tone === "flat" ? "→" : delta.label.startsWith("+") ? "↑" : "↓";
  return <span className={`insights-delta is-${delta.tone}`} title={delta.caption}>{arrow} {delta.label}</span>;
}
