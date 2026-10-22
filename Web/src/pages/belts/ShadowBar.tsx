import { Link } from "react-router-dom";
import type { ShadowSummary } from "../../api/wire.gen";
import { Caps } from "../../components/Paper";
import { useSettingsQuery } from "../../api/useSettingsQuery";

export interface ShadowBarProps {
  beltId: string;
  liveVersion: number;
  /** The running test, or null while a new shadow waits for its first save. */
  summary: ShadowSummary | null;
  shadowMode: boolean;
  onMode: (shadow: boolean) => void;
}

/** The switch between a belt's live and shadow versions, with what the test is doing. The words and counts are the daemon's. */
export function ShadowBar({ beltId, liveVersion, summary, shadowMode, onMode }: ShadowBarProps) {
  const shadowLabel = summary ? `Shadow · v${summary.version}` : "New shadow";
  const showInsights = useSettingsQuery().data?.showInsights !== false;
  return <div className="shadow-bar" role="group" aria-label="Shadow test">
    <div className="paper-segmented" role="group" aria-label="Version">
      <button type="button" className="paper-segmented__option" aria-pressed={!shadowMode} onClick={() => onMode(false)}>{`Live · v${liveVersion}`}</button>
      <button type="button" className="paper-segmented__option is-shadow" aria-pressed={shadowMode} onClick={() => onMode(true)}>
        <span className="shadow-dot" aria-hidden="true" />{shadowLabel}
      </button>
    </div>
    <p className="shadow-bar__text">{summary ? summary.caption
      : "A draft copy of the live version. Save it to start a shadow test: new runs then alternate between live and the shadow until you accept or discard it."}</p>
    {summary && <Caps className="shadow-bar__count">{`Live ${summary.runs.live} · Shadow ${summary.runs.shadow}`}</Caps>}
    {summary && showInsights && <Link className="shadow-bar__link" to={`/insights?belt=${encodeURIComponent(beltId)}&version=${encodeURIComponent(`shadow:${summary.test}`)}`}>Compare on Insights →</Link>}
  </div>;
}
