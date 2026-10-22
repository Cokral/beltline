import { Fragment, useState } from "react";
import { Caps, Chip } from "../../components/Paper";
import type { InsightsBeltRow, InsightsFigures, InsightsRate, InsightsStage } from "../../api/wire.gen";
import { DeltaBadge, Sparkline } from "./Sparkline";

/** A share as a short bar with its label. */
function ShareBar({ rate, low }: { rate: InsightsRate; low?: boolean }) {
  return <span className={`insights-share ${low ? "is-low" : ""}`} title={`${rate.count} of ${rate.of}`}>
    <span className="insights-share-track" aria-hidden="true"><span style={{ width: `${rate.of ? (100 * rate.count) / rate.of : 0}%` }} /></span>
    <span className="insights-share-label">{rate.percent}</span>
  </span>;
}

/** Every belt at a glance; a row opens its belt. */
export function BeltList({ rows, onOpen }: { rows: InsightsBeltRow[]; onOpen: (id: string) => void }) {
  return <table className="work-table insights-table" aria-label="Belts">
    <colgroup><col /><col style={{ width: 70 }} /><col style={{ width: 120 }} /><col style={{ width: 120 }} /><col style={{ width: 100 }} /><col style={{ width: 104 }} /><col style={{ width: 196 }} /></colgroup>
    <thead><tr>{["Belt", "Runs", "Ship rate", "First try", "Lead time", "Cost per run", "First try · 7-day"].map(name => <th key={name} scope="col"><Caps>{name}</Caps></th>)}</tr></thead>
    <tbody>{rows.map(row => <tr key={row.id} className="work-row" tabIndex={0} onClick={() => onOpen(row.id)} onKeyDown={event => { if (event.key === "Enter") onOpen(row.id); }}>
      <td><span className="work-title">{row.name}</span></td>
      <td>{row.runs}</td>
      <td><ShareBar rate={row.shipRate} /></td>
      <td><ShareBar rate={row.firstTry} /></td>
      <td>{row.leadTime}</td>
      <td className={row.costPerRun === "–" ? "insights-muted" : ""}>{row.costPerRun}</td>
      <td><span className="insights-trend-cell"><Sparkline series={row.trend} /><DeltaBadge delta={row.trend.delta} /></span></td>
    </tr>)}</tbody>
  </table>;
}

/** The figure cells a stage row and a model row share. */
function FigureCells({ figures }: { figures: InsightsFigures }) {
  const unknown = figures.cost === "–" || figures.cost === "not tracked";
  return <>
    <td>{figures.visits}</td>
    <td>{figures.visitsPerRun}</td>
    <td>{figures.tries}</td>
    <td><ShareBar rate={figures.firstTry} low={figures.lowSample} /></td>
    <td>{figures.medianTime}</td>
    <td className={unknown ? "insights-muted" : ""} title={figures.costCaption}>{figures.cost === "not tracked" ? "–" : figures.cost}</td>
  </>;
}

const columns = ["Stage", "Visits", "Visits / run", "Tries / visit", "First try", "Median time", "Cost"];

/** The belt's stages in order; a stage whose sessions ran on a model opens to compare its models. */
export function Stages({ stages }: { stages: InsightsStage[] }) {
  const [open, setOpen] = useState<ReadonlySet<string>>(new Set());
  const toggle = (key: string) => setOpen(current => {
    const next = new Set(current);
    if (next.has(key)) next.delete(key); else next.add(key);
    return next;
  });
  return <section className="insights-section" aria-labelledby="insights-stages">
    <div className="insights-section-head">
      <h2 id="insights-stages" className="paper-tag paper-tag--strong">Stages</h2>
      <span className="insights-hint">A visit is one entry of a run into the stage; a run sent back to it visits again. Tries and cost are per visit, all its attempts together. Open a stage to compare its models.</span>
    </div>
    <table className="work-table insights-table" aria-label="Stages">
      <colgroup><col /><col style={{ width: 64 }} /><col style={{ width: 84 }} /><col style={{ width: 84 }} /><col style={{ width: 150 }} /><col style={{ width: 110 }} /><col style={{ width: 96 }} /></colgroup>
      <thead><tr>{columns.map(name => <th key={name} scope="col"><Caps>{name}</Caps></th>)}</tr></thead>
      <tbody>
        {stages.length === 0 && <tr><td className="empty-state" colSpan={columns.length} role="status">No attempts ended in this period.</td></tr>}
        {stages.map((stage, index) => {
          const expandable = stage.models.length > 0;
          const isOpen = expandable && open.has(stage.key);
          return <Fragment key={stage.key}>
            <tr className={`work-row ${isOpen ? "is-open" : ""} ${stage.figures.lowSample ? "is-low" : ""} ${expandable ? "" : "is-fixed"}`}
              tabIndex={expandable ? 0 : undefined} aria-expanded={expandable ? isOpen : undefined} data-stage={stage.key}
              onClick={() => { if (expandable) toggle(stage.key); }}
              onKeyDown={event => { if (expandable && (event.key === "Enter" || event.key === " ")) { event.preventDefault(); toggle(stage.key); } }}>
              <td className="insights-name">
                <span className="insights-name-line">
                  <svg className={`work-fold-chevron ${expandable ? "" : "is-hidden"}`} width="10" height="10" viewBox="0 0 10 10" aria-hidden="true"><path d={isOpen ? "M2 3.5L5 6.5L8 3.5" : "M3.5 2L6.5 5L3.5 8"} fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" /></svg>
                  <span className="insights-order">{String(index + 1).padStart(2, "0")}</span>
                  <span className="work-title">{stage.name}</span>
                  {stage.kindLabel && <Chip tone="grey">{stage.kindLabel}</Chip>}
                  {stage.models.length > 1 && <span className="insights-n">{stage.models.length} models</span>}
                </span>
                {stage.caption && <span className="insights-caption">{stage.caption}</span>}
              </td>
              <FigureCells figures={stage.figures} />
            </tr>
            {isOpen && stage.models.map(model => <tr key={model.model} className={`insights-model-row ${model.figures.lowSample ? "is-low" : ""}`}>
              <td><span className="insights-model-line">
                <span className="insights-model-name" title={model.model}>{model.model}</span>
                <span className="insights-n">{model.runner}</span>
                {model.best && <Chip tone="good">BEST</Chip>}
              </span></td>
              <FigureCells figures={model.figures} />
            </tr>)}
          </Fragment>;
        })}
      </tbody>
    </table>
  </section>;
}
