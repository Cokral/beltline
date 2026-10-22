import { useEffect, useState, type ChangeEvent, type FocusEvent } from "react";
import {
  machineComponents, machineFaces, machineIds, machineTints,
  type MachineFace, type MachineId, type MachineTint
} from "../theme/machines/Machines";
import "../theme/gallery.css";
import archivoLicense from "../theme/fonts/OFL-Archivo.txt?url";
import interLicense from "../theme/fonts/OFL-InterTight.txt?url";
import plexLicense from "../theme/fonts/OFL-IBMPlexMono.txt?url";
import { GallerySharedComponents } from "./GallerySharedComponents";

const tintForNativeIndex: MachineTint[] = ["blue", "sand", "gold", "green", "violet", "rose", "teal", "coral", "grey", "blue", "sand", "gold", "green"];
const machinesByState = machineIds.flatMap(machine => machineFaces.map(face => ({ machine, face })));

const paletteNames = [
  "--desk", "--paper", "--side", "--card", "--ink", "--ink2", "--ink-soft", "--muted", "--rule", "--slate", "--slate-deep", "--slate-wash",
  "--green", "--green-wash", "--gold", "--gold-wash", "--gold-lamp", "--red", "--red-wash", "--screen", "--screen-text", "--screen-bar",
  "--grey-wash", "--segment", "--track", "--bar", "--hover",
  ...Array.from({ length: 10 }, (_, index) => `--metro-line-${index}`),
  ...machineTints.map(tint => `--tint-${tint}`),
  "--tone-good-fg", "--tone-good-bg", "--tone-you-fg", "--tone-you-bg", "--tone-problem-fg", "--tone-problem-bg", "--tone-neutral-fg", "--tone-neutral-bg",
  "--status-done-fg", "--status-done-bg", "--status-running-fg", "--status-running-bg", "--status-waiting-fg", "--status-waiting-bg", "--status-held-fg", "--status-held-bg", "--status-plain-fg", "--status-plain-bg"
];
const typeNames = ["--font-display", "--font-body", "--font-mono", "--font-size-body", "--font-size-mono", "--font-size-tag", "--font-size-caps", "--font-size-chip", "--font-size-field", "--font-size-button", "--font-size-button-small", "--font-size-segment", "--font-size-select", "--font-size-select-compact", "--font-size-status", "--font-size-status-compact", "--font-weight-display", "--font-weight-medium", "--tracking-tag", "--tracking-caps", "--tracking-chip", "--tracking-status", "--tracking-status-compact"];
const measureNames = ["--hairline-width", "--focus-width", "--focus-offset", "--field-radius", "--field-padding-inline", "--field-padding-block", "--select-radius", "--select-radius-compact", "--select-radius-pill", "--select-padding-inline", "--select-padding-inline-compact", "--select-padding-inline-pill", "--select-padding-block", "--select-padding-block-compact", "--select-padding-block-pill", "--select-accent-width", "--select-chevron-size", "--button-padding-inline", "--button-padding-block", "--button-small-padding-inline", "--button-small-padding-block", "--button-border-width", "--button-opacity-disabled", "--button-opacity-pressed", "--segment-track-padding", "--segment-gap", "--segment-padding-inline", "--segment-padding-block", "--tag-padding-inline", "--chip-padding-inline", "--chip-padding-inline-need", "--chip-padding-block", "--chip-padding-block-need", "--card-padding", "--card-radius", "--status-height", "--status-padding-inline", "--status-padding-inline-compact"];

function tokenValue(name: string) { return getComputedStyle(document.documentElement).getPropertyValue(name).trim(); }

function TokenCards({ names, colors = false }: { names: string[]; colors?: boolean }) {
  return <div className="gallery-token-grid">{names.map(name => <div className="gallery-token" key={name}>
    {colors && <span className="gallery-token__swatch" style={{ background: `var(${name})` }} aria-hidden="true" />}
    <div><code>{name}</code><span className="gallery-token__value">{tokenValue(name)}</span></div>
  </div>)}</div>;
}

function SelectSample({ label, variant = "", disabled = false, accent }: { label: string; variant?: string; disabled?: boolean; accent?: string }) {
  return <label className={`paper-select ${variant} ${accent ? "paper-select--accent" : ""}`}>
    {accent && <span className="paper-select__accent" style={{ background: accent }} aria-hidden="true" />}
    <span className="paper-select__value">{label}</span><span className="paper-select__chevron" aria-hidden="true">⌄</span>
    <select aria-label={`${label} sample`} defaultValue="one" disabled={disabled}>
      <option value="one">{label}</option><option value="two">Another option</option>
    </select>
  </label>;
}

function SectionTitle({ number, title, note }: { number: string; title: string; note: string }) {
  return <header className="gallery-section__heading"><span className="paper-tag">{number}</span><div><h2>{title}</h2><p>{note}</p></div></header>;
}

function SamplesGrid({ list, tint, size, dimmed, frozen, paused }: { list: Array<{ machine: MachineId; face: MachineFace }>; tint: MachineTint; size: { width: number; height: number }; dimmed: boolean; frozen: boolean; paused: boolean }) {
  return <div className="gallery-machine-grid">{list.map(({ machine, face: sampleFace }) => {
    const Machine = machineComponents[machine];
    return <article className="gallery-machine-card" key={`${machine}-${sampleFace}`}>
      <Machine face={sampleFace} tint={tint} width={size.width} height={size.height} dimmed={dimmed} frozen={frozen} paused={paused} />
      <span className="gallery-machine-card__name">{machine}</span><span className="gallery-machine-card__state">{sampleFace}</span>
    </article>;
  })}</div>;
}

const sizeOptions = [
  { label: "120 × 108", width: 120, height: 108 },
  { label: "60 × 54", width: 60, height: 54 },
  { label: "240 × 216", width: 240, height: 216 },
  { label: "160 × 108 · letterboxed", width: 160, height: 108 }
];

export default function Gallery() {
  const [tint, setTint] = useState<MachineTint>("blue");
  const [sizeIndex, setSizeIndex] = useState(0);
  const [dimmed, setDimmed] = useState(false);
  const [frozen, setFrozen] = useState(true);
  const [pausedByTextInput, setPausedByTextInput] = useState(false);
  const [segment, setSegment] = useState("Native");
    const currentSize = sizeOptions[sizeIndex]!;

  useEffect(() => { document.title = "Beltline — Theme gallery"; }, []);

  function focusCapture(event: FocusEvent<HTMLElement>) {
    if (event.target instanceof HTMLInputElement && ["text", "search", "email", "url", "tel", "number"].includes(event.target.type)) setPausedByTextInput(true);
  }
  function blurCapture(event: FocusEvent<HTMLElement>) {
    const next = event.relatedTarget;
    if (!(next instanceof HTMLInputElement) || !["text", "search", "email", "url", "tel", "number"].includes(next.type)) setPausedByTextInput(false);
  }

  return <main id="top" data-column="gallery" className={`theme-gallery ${pausedByTextInput ? "theme-gallery--input-focused" : ""}`} onFocusCapture={focusCapture} onBlurCapture={blurCapture}>
    <header className="gallery-header">
      <div><p className="gallery-eyebrow">BELTLINE / DESIGN REVIEW</p><h1>Theme gallery</h1><p className="gallery-intro">Paper-and-slate tokens, shared controls and native machine drawings. This development route works without a daemon.</p></div>
      <nav className="gallery-jumps" aria-label="Gallery sections"><a href="#tokens">Tokens</a><a href="#fonts">Fonts</a><a href="#fields">Fields</a><a href="#machines">Machines</a><a href="#components">Components</a></nav>
    </header>

    <section className="gallery-section" id="tokens" aria-labelledby="tokens-heading">
      <SectionTitle number="01" title="Tokens" note="Palette, metro lines, machine tints, tone/status pairs, typography and shared measurements." />
      <h3 id="tokens-heading">Colour samples</h3><TokenCards names={paletteNames} colors />
      <h3>Type and size</h3><TokenCards names={typeNames} />
      <h3>Control measurements</h3><TokenCards names={measureNames} />
    </section>

    <section className="gallery-section" id="fonts">
      <SectionTitle number="02" title="Fonts" note="Every face is bundled with the web app and loaded from same-origin TTF files." />
      <div className="gallery-font-grid">
        <article className="gallery-font-sample"><span className="paper-caps">Archivo · variable 100–900</span><p className="gallery-font-display">Belts move work forward</p><span>Archivo display sample · regular, bold and heavy weights</span><div className="gallery-font-weights" style={{ fontFamily: "var(--font-display)" }}><b style={{ fontWeight: 400 }}>Regular</b><b style={{ fontWeight: 700 }}>Bold</b><b style={{ fontWeight: 800 }}>Heavy</b></div></article>
        <article className="gallery-font-sample"><span className="paper-caps">Inter Tight · variable 100–900</span><p className="gallery-font-body">A clear text face for pages, fields and longer descriptions.</p><span>Inter Tight body sample · 14px default</span><div className="gallery-font-weights" style={{ fontFamily: "var(--font-body)" }}><b style={{ fontWeight: 400 }}>Regular</b><b style={{ fontWeight: 600 }}>Semibold</b><b style={{ fontWeight: 700 }}>Bold</b></div></article>
        <article className="gallery-font-sample"><span className="paper-caps">IBM Plex Mono · local weights</span><p className="gallery-font-mono">[ REVIEW ] 0123456789</p><span>Plex Mono regular, medium and semibold</span><div className="gallery-font-weights" style={{ fontFamily: "var(--font-mono)" }}><b style={{ fontWeight: 400 }}>Regular</b><b style={{ fontWeight: 500 }}>Medium</b><b style={{ fontWeight: 600 }}>Semibold</b></div></article>
      </div>
      <div className="gallery-licences"><span className="paper-caps">Open Font License texts</span><a href={archivoLicense}>Archivo OFL</a><a href={interLicense}>Inter Tight OFL</a><a href={plexLicense}>IBM Plex Mono OFL</a></div>
    </section>

    <section className="gallery-section" id="fields">
      <SectionTitle number="03" title="Fields" note="Shared controls follow the palette: paper fields, selects, pill buttons, segments, tags, chips, caps and statuses." />
      <div className="gallery-field-columns" data-column="gallery-fields">
        <article data-column="gallery-field-paper" className="paper-card gallery-field-card"><h3>Paper fields and selects</h3><div className="gallery-field-row"><label className="gallery-field-label">Editable<input className="paper-field" defaultValue="A paper field" aria-label="Editable paper field" /></label><label className="gallery-field-label">Read only<input className="paper-field" value="Read-only sample" readOnly aria-label="Read-only paper field" /></label><label className="gallery-field-label">Disabled<input className="paper-field" value="Disabled field" disabled aria-label="Disabled paper field" /></label></div>
          <div className="gallery-field-row gallery-selects"><SelectSample label="Regular select" /><SelectSample label="Compact select" variant="paper-select--compact" /><SelectSample label="Pill select" variant="paper-select--pill" /><SelectSample label="Accented select" accent="var(--gold)" /><SelectSample label="Read-only select" disabled /></div></article>
        <article data-column="gallery-field-buttons" className="paper-card gallery-field-card"><h3>Pill buttons</h3><div className="gallery-field-row">{["plain", "primary", "slate", "danger"].map(kind => <button className={`paper-button ${kind === "plain" ? "" : `paper-button--${kind}`}`} key={kind} type="button">{kind}</button>)}</div>
          <div className="gallery-field-row">{["plain", "primary", "slate", "danger"].map(kind => <button className={`paper-button paper-button--small ${kind === "plain" ? "" : `paper-button--${kind}`}`} key={kind} type="button">Small {kind}</button>)}</div>
          <button className="paper-button paper-button--primary paper-button--wide" type="button">Wide primary button</button><div className="gallery-field-row"><button className="paper-button" type="button" disabled>Disabled · 40%</button><button className="paper-button" type="button" aria-label="Hold to inspect pressed opacity" onPointerDown={event => event.currentTarget.classList.add("is-pressed")} onPointerUp={event => event.currentTarget.classList.remove("is-pressed")} onPointerLeave={event => event.currentTarget.classList.remove("is-pressed")}>Pressed · 85%</button></div></article>
        <article data-column="gallery-field-segments" className="paper-card gallery-field-card"><h3>Segments, tags and chips</h3><div className="paper-segmented" role="group" aria-label="Segmented selection">{["Native", "Web", "Compare"].map(option => <button type="button" className="paper-segmented__option" aria-pressed={segment === option} key={option} onClick={() => setSegment(option)}>{option}</button>)}</div><span className="paper-tag">In review</span><span className="paper-caps">Local workspace</span><span className="paper-chip">Running</span><span className="paper-chip paper-chip--grey">Queued</span><span className="paper-chip paper-chip--need">Needs you</span><span className="paper-chip paper-chip--need paper-chip--held">Held</span></article>
        <article data-column="gallery-field-status" className="paper-card gallery-field-card"><h3>Status pills</h3><div className="gallery-field-row">{["done", "running", "waiting", "held", "plain"].map(status => <span className={`paper-status paper-status--${status}`} key={status}>{status}</span>)}<span className="paper-status paper-status--done paper-status--compact">✓ DONE</span></div><p className="gallery-muted">Segment selection: <strong>{segment}</strong></p></article>
      </div>
    </section>

    <section className="gallery-section" id="machines">
      <SectionTitle number="04" title="Machines" note="Static SVG paths use the 120 × 108 native drawing space. State cues and motion stay explicit and labelled." />
      <div className="gallery-machine-controls" aria-label="Machine gallery controls">
        <label>Tint<select aria-label="Machine tint" value={tint} onChange={(event: ChangeEvent<HTMLSelectElement>) => setTint(event.target.value as MachineTint)}>{machineTints.map(value => <option key={value} value={value}>{value}</option>)}</select></label>
        <label>Size<select aria-label="Machine size" value={sizeIndex} onChange={event => setSizeIndex(Number(event.target.value))}>{sizeOptions.map((item, index) => <option key={item.label} value={index}>{item.label}</option>)}</select></label>
        <label className="gallery-toggle"><input type="checkbox" checked={dimmed} onChange={event => setDimmed(event.target.checked)} />Dimmed</label>
        <label className="gallery-toggle"><input type="checkbox" checked={frozen} onChange={event => setFrozen(event.target.checked)} />Freeze animation</label>
      </div>
      <div className="gallery-native-comparison">
        <h3>Native comparison · idle · frozen · 120 × 108</h3>
        <div className="gallery-machine-grid gallery-machine-grid--native">{machineIds.map((machine, index) => {
          const Machine = machineComponents[machine];
          return <article className="gallery-machine-card" key={machine}>
            <Machine face="idle" tint={tintForNativeIndex[index]!} width={120} height={108} frozen />
            <span className="gallery-machine-card__name">{machine}</span><span className="gallery-machine-card__state">idle</span>
          </article>;
        })}</div>
      </div>
      <div className="gallery-state-heading"><h3>All 91 machine states</h3><span className="paper-caps">{currentSize.label} · {tint} tint · {dimmed ? "dimmed" : "full opacity"} · {frozen ? "frozen" : "animated"}{pausedByTextInput ? " · paused while typing" : ""}</span></div>
      <SamplesGrid list={machinesByState} tint={tint} size={currentSize} dimmed={dimmed} frozen={frozen} paused={pausedByTextInput} />
    </section>
    <GallerySharedComponents />
    <footer className="gallery-footer"><span>Theme tokens · paper palette in Light and Dark · development route only</span><a href="#top" onClick={event => { event.preventDefault(); window.scrollTo({ top: 0, behavior: "smooth" }); }}>Back to top ↑</a></footer>
  </main>;
}
