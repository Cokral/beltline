/** The radius of a circle station, and the half-size of the diamond and square ones, as the metro maps draw them. */
const circleRadius = 7;

/**
 * A metro station's shape at the daemon's point (x, y): a circle for an agent, a diamond for an approval, a square for a
 * watch. `base` is the class the map styles its stations by; the diamond also gets `${base}-diamond`.
 */
export function MetroShape({ shape, x, y, base }: {
  shape: "circle" | "diamond" | "square";
  x: number;
  y: number;
  base: string;
}) {
  switch (shape) {
    case "circle": return <circle cx={x} cy={y} r={circleRadius} className={base} />;
    case "diamond": return <path d={`M${x} ${y - 8.5} L${x + 8.5} ${y} L${x} ${y + 8.5} L${x - 8.5} ${y} Z`} className={`${base} ${base}-diamond`} />;
    case "square": return <rect x={x - 6.5} y={y - 6.5} width={13} height={13} rx={1.5} className={base} />;
  }
}
