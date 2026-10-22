type Point = { x: number; y: number };

/** A line through the points that turns each corner on an arc, as the app's tracks do. */
export function roundedTrack(points: readonly Point[], radius: number): string {
  if (points.length === 0) return "";
  let d = `M${points[0]!.x} ${points[0]!.y}`;
  for (let index = 1; index < points.length; index += 1) {
    const b = points[index]!;
    if (index === points.length - 1 || radius <= 0) { d += ` L${b.x} ${b.y}`; continue; }
    const a = points[index - 1]!, c = points[index + 1]!;
    const ab = Math.hypot(b.x - a.x, b.y - a.y), bc = Math.hypot(c.x - b.x, c.y - b.y);
    const r = Math.min(radius, ab / 2, bc / 2);
    if (r <= 0) { d += ` L${b.x} ${b.y}`; continue; }
    const start = { x: b.x - ((b.x - a.x) / ab) * r, y: b.y - ((b.y - a.y) / ab) * r };
    const end = { x: b.x + ((c.x - b.x) / bc) * r, y: b.y + ((c.y - b.y) / bc) * r };
    d += ` L${start.x} ${start.y} Q${b.x} ${b.y} ${end.x} ${end.y}`;
  }
  return d;
}

/** Every piece of a MapPath as one SVG path's data. */
export function trackData(path: { subpaths: readonly (readonly Point[])[]; cornerRadius: number }): string {
  return path.subpaths.map(points => roundedTrack(points, path.cornerRadius)).join(" ");
}
