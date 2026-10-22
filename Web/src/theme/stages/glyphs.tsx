import type { StageIcon } from "../../api/wire.gen";

/** The stage glyphs, on a 24 × 24 grid, drawn as strokes in the box's ink. */
export const glyphPaths: Record<StageIcon, string> = {
  funnel: "M4 5h16l-6 7v6l-4 2v-8z",
  doc: "M7 3h7l4 4v14H7zM14 3v4h4M10 12h5M10 16h5",
  approve: "M9 4.5a3.5 3.5 0 1 1 0 7a3.5 3.5 0 1 1 0-7M3 20c.8-3.5 3.2-5 6-5s5.2 1.5 6 5M15.5 11l2 2 4-4",
  wrench: "M14.7 6.3a4 4 0 0 0-5.4 5.4L4 17l3 3 5.3-5.3a4 4 0 0 0 5.4-5.4l-2.4 2.4-2.6-.6-.6-2.6z",
  scope: "M11 4.5a6.5 6.5 0 1 1 0 13a6.5 6.5 0 1 1 0-13M20 20l-4.3-4.3M8.5 11l2 2 3.5-3.5",
  ship: "M12 15V3M7 8l5-5 5 5M5 14v6h14v-6",
  merge: "M6 3a2 2 0 1 1 0 4a2 2 0 1 1 0-4M6 17a2 2 0 1 1 0 4a2 2 0 1 1 0-4M18 11a2 2 0 1 1 0 4a2 2 0 1 1 0-4M6 7v10M6 8c0 3.5 4 5 10 5",
  flag: "M5 21V4M5 4h11l-2 4 2 4H5",
  eye: "M2 12s3.5-6.5 10-6.5S22 12 22 12s-3.5 6.5-10 6.5S2 12 2 12zM12 9.5a2.5 2.5 0 1 1 0 5a2.5 2.5 0 1 1 0-5",
  send: "M4 12l16-8-6 16-3-7z",
  list: "M9 6h11M9 12h11M9 18h11M4.5 6h.01M4.5 12h.01M4.5 18h.01",
  pen: "M4 20h4L19 9l-4-4L4 16zM14 6l4 4",
  cycle: "M20 11a8 8 0 0 0-14.6-4.5M4 4v3h3M4 13a8 8 0 0 0 14.6 4.5M20 20v-3h-3",
  branch: "M6 4a2 2 0 1 1 0 4a2 2 0 1 1 0-4M6 16a2 2 0 1 1 0 4a2 2 0 1 1 0-4M18 6a2 2 0 1 1 0 4a2 2 0 1 1 0-4M6 8v8M18 10c0 4-6 3-10 7",
  gauge: "M4 17a8 8 0 1 1 16 0M12 17l4-5",
  chip: "M8 6h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2zM9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3",
  bolt: "M13 3L5 13h6l-1 8 8-10h-6z",
  jira: "M12 3l8 8-8 8-8-8zM12 8l3 3-3 3-3-3z",
  clock: "M12 4a8 8 0 1 1 0 16a8 8 0 1 1 0-16M12 8v4l3 2",
  hand: "M8 13V6a1.5 1.5 0 0 1 3 0v5M11 11V4.5a1.5 1.5 0 0 1 3 0V11M14 11V6a1.5 1.5 0 0 1 3 0v8a6 6 0 0 1-6 6h-1a5 5 0 0 1-4.3-2.5L4 14a1.5 1.5 0 0 1 2.5-1.6L8 14"
};

/** One glyph as an inline SVG. */
export function Glyph({ icon, size = 15 }: { icon: StageIcon; size?: number }) {
  return <svg className="stage-glyph" viewBox="0 0 24 24" width={size} height={size} aria-hidden="true"><path d={glyphPaths[icon]} /></svg>;
}
