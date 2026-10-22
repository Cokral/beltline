import type { CSSProperties, ComponentType } from "react";

export const machineIds = ["sorter", "typewriter", "mailbox", "robot", "microscope", "reader", "van", "box", "telescope", "stamp", "crane", "drill", "radar"] as const;
export type MachineId = typeof machineIds[number];
export const machineFaces = ["idle", "run", "wait", "happy", "held", "paused", "todo"] as const;
export type MachineFace = typeof machineFaces[number];
export const machineTints = ["blue", "sand", "gold", "green", "violet", "rose", "teal", "coral", "grey"] as const;
export type MachineTint = typeof machineTints[number];

export interface MachineProps {
  face?: MachineFace;
  tint?: MachineTint;
  width?: number | string;
  height?: number | string;
  dimmed?: boolean;
  frozen?: boolean;
  paused?: boolean;
}

type ShapeKind = "body" | "part" | "ink" | "knob" | "line" | "gold" | "glass" | "screen" | "shadow" | "fill" | "stroke";
interface ShapeProps {
  kind: ShapeKind;
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  radius?: number;
  cx?: number;
  cy?: number;
  rx?: number;
  ry?: number;
  d?: string;
  strokeWidth?: number;
  color?: string;
  fillColor?: string;
  fillStrokeWidth?: number;
}

function shapeStyle(kind: ShapeKind, strokeWidth = 2.2, color?: string, fillColor?: string, fillStrokeWidth = 0) {
  switch (kind) {
    case "body": return { fill: "var(--machine-tint)", stroke: "var(--machine-outline)", strokeWidth: 2.2, strokeLinejoin: "round" as const };
    case "part":
    case "knob": return { fill: "var(--card)", stroke: "var(--machine-outline)", strokeWidth: 2, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
    case "ink": return { fill: "var(--machine-outline)" };
    case "line": return { fill: "none", stroke: "var(--machine-outline)", strokeWidth, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
    case "gold": return { fill: "var(--machine-gold)", stroke: "var(--machine-outline)", strokeWidth: 2, strokeLinecap: "round" as const, strokeLinejoin: "round" as const };
    case "glass": return { fill: "var(--machine-glass)", stroke: "var(--machine-outline)", strokeWidth: 2.2 };
    case "screen": return { fill: "var(--machine-screen)" };
    case "shadow": return { fill: "var(--machine-shadow)", opacity: 0.09 };
    case "fill": return { fill: fillColor ?? color ?? "none", ...(fillStrokeWidth > 0 ? { stroke: "var(--machine-outline)", strokeWidth: fillStrokeWidth } : {}) };
    case "stroke": return { fill: "none", stroke: color ?? "var(--card)", strokeWidth, strokeLinecap: "round" as const };
  }
}

function Shape(props: ShapeProps) {
  const style = shapeStyle(props.kind, props.strokeWidth, props.color, props.fillColor, props.fillStrokeWidth);
  if (props.d !== undefined) return <path d={props.d} {...style} />;
  if (props.cx !== undefined && props.cy !== undefined && props.rx !== undefined && props.ry !== undefined) {
    return <ellipse cx={props.cx} cy={props.cy} rx={props.rx} ry={props.ry} {...style} />;
  }
  if (props.cx !== undefined && props.cy !== undefined && props.radius !== undefined) {
    return <circle cx={props.cx} cy={props.cy} r={props.radius} {...style} />;
  }
  return <rect x={props.x ?? 0} y={props.y ?? 0} width={props.width ?? 0} height={props.height ?? 0} rx={props.radius ?? 0} {...style} />;
}

function Face({ x, y, face }: { x: number; y: number; face: MachineFace }) {
  const top = y + 7;
  return <g className="machine-face" data-face={face}>
    <Shape kind="screen" x={x} y={y} width={40} height={23} radius={8} />
    {face === "happy" ? <path d={`M${x + 9},${y + 14} q4,-6 8,0 M${x + 23},${y + 14} q4,-6 8,0`} fill="none" stroke="#CFE0F7" strokeWidth={2} strokeLinecap="round" />
      : face === "paused" || face === "todo" ? <path d={`M${x + 9},${y + 12} h8 M${x + 23},${y + 12} h8`} fill="none" stroke="#CFE0F7" strokeWidth={2} strokeLinecap="round" />
        : <g className={face === "run" ? "machine-eyes machine-eyes--run" : "machine-eyes"}>
          {[x + 10, x + 24].map(ex => <rect key={ex} className={face === "run" ? "machine-eye machine-eye--run" : "machine-eye"} x={ex} y={top + (face === "wait" ? -0.675 : 0)} width={face === "wait" ? 6.9 : 6} height={face === "wait" ? 10.35 : 9} rx={face === "wait" ? 3.45 : 3} fill="#CFE0F7" />)}
        </g>}
  </g>;
}

function Shadow({ width }: { width: number }) { return <Shape kind="shadow" cx={60} cy={102} rx={width} ry={4} />; }
function Feet({ a, b, width = 12 }: { a: number; b: number; width?: number }) {
  return <><Shape kind="ink" x={a} y={90} width={width} height={10} radius={4} /><Shape kind="ink" x={b} y={90} width={width} height={10} radius={4} /></>;
}

function MachineDrawing({ machine, face: faceValue, tint, width, height, dimmed, frozen, paused }: MachineProps & { machine: MachineId }) {
  const face = faceValue ?? "idle";
  const rootStyle = { width, height, "--machine-tint": `var(--tint-${tint})` } as CSSProperties;
  const classes = ["machine-svg", frozen && "machine-frozen", paused && "machine-paused", dimmed && "machine-dimmed"].filter(Boolean).join(" ");
  const running = face === "run";
  const art = (() => {
    switch (machine) {
      case "sorter": return <>
        <Shadow width={34} /><Feet a={36} b={72} />
        <Shape kind="glass" cx={60} cy={29} radius={23} />
        <g className="machine-motion machine-motion--spin" style={{ transformOrigin: "60px 29px" }}>
          <Shape kind="fill" cx={51} cy={35} radius={5.5} fillColor="#F2A99B" fillStrokeWidth={1.5} />
          <Shape kind="fill" cx={64} cy={39} radius={5.5} fillColor="#7FA3D6" fillStrokeWidth={1.5} />
          <Shape kind="fill" cx={59} cy={24} radius={5.5} fillColor="#E8B83E" fillStrokeWidth={1.5} />
          <Shape kind="fill" cx={71} cy={27} radius={5} fillColor="#8CC79A" fillStrokeWidth={1.5} />
          <Shape kind="fill" cx={48} cy={23} radius={4.5} fillColor="#C9B6EC" fillStrokeWidth={1.5} />
        </g>
        <Shape kind="stroke" d="M47,16 q5,-7 13,-7" color="#FFFFFF" strokeWidth={3} />
        <Shape kind="ink" x={53} y={3} width={14} height={5} radius={2} />
        <Shape kind="part" x={42} y={49} width={36} height={8} radius={3} />
        <Shape kind="body" d="M26,93 L34,56 H86 L94,93 Z" /><Face x={40} y={63} face={face} />
        <Shape kind="part" d="M93,80 h12 l5,10 h-15 Z" />
      </>;
      case "typewriter": return <>
        <Shadow width={46} /><Feet a={22} b={84} width={14} />
        <g className="machine-motion machine-motion--print"><Shape kind="part" x={40} y={8} width={40} height={38} radius={2} /><Shape kind="line" d="M46,16 h28 M46,22 h20 M46,28 h26 M46,34 h16" strokeWidth={1.5} /></g>
        <Shape kind="part" x={24} y={42} width={72} height={11} radius={5.5} /><Shape kind="knob" cx={20} cy={47.5} radius={6} /><Shape kind="knob" cx={100} cy={47.5} radius={6} />
        <Shape kind="body" d="M12,93 L22,53 H98 L108,93 Z" /><Face x={40} y={58} face={face} />
        {[27, 39, 51, 72, 84].map((x, index) => <Shape key={x} kind="fill" x={x} y={84} width={index === 2 ? 18 : 9} height={5} radius={2} fillColor="var(--card)" fillStrokeWidth={1.5} />)}
      </>;
      case "mailbox": return <>
        <Shadow width={32} /><Feet a={40} b={68} />
        <Shape kind="body" d="M32,93 V46 A28,28 0 0 1 88,46 V93 Z" /><Shape kind="knob" cx={60} cy={16} radius={5} /><Face x={40} y={40} face={face} />
        <Shape kind="ink" x={44} y={74} width={32} height={5} radius={2.5} />
        <g className={!((face === "wait")) ? "machine-mail-flag machine-mail-flag--down" : "machine-mail-flag"}>
          <Shape kind="line" d="M92,74 V42" strokeWidth={3} /><Shape kind="gold" d="M92,42 h17 l-5,7 l5,7 h-17 Z" />
        </g><Shape kind="ink" cx={92} cy={74} radius={3} />
      </>;
      case "robot": return <>
        <Shadow width={30} /><Shape kind="ink" x={44} y={88} width={10} height={12} radius={3} /><Shape kind="ink" x={66} y={88} width={10} height={12} radius={3} />
        <Shape kind="line" d="M34,66 q-12,4 -12,16" strokeWidth={2.2} /><Shape kind="knob" cx={22} cy={84} radius={4.5} />
        <g className="machine-motion machine-motion--wave" style={{ transformOrigin: "86px 66px" }}><Shape kind="line" d="M86,66 q12,-4 14,-18" strokeWidth={2.2} /><Shape kind="line" d="M100,48 l2,-9 M97,41 l8,-1" strokeWidth={2.6} /></g>
        <Shape kind="body" x={34} y={58} width={52} height={32} radius={10} />
        <g className="machine-motion machine-motion--gear" style={{ transformOrigin: "60px 74px" }}><circle cx={60} cy={74} r={8} fill="none" stroke="var(--machine-outline)" strokeWidth={4} strokeDasharray="2.6 2.6" /></g>
        <Shape kind="knob" cx={60} cy={74} radius={4} /><Shape kind="ink" x={54} y={51} width={12} height={8} />
        <Shape kind="knob" x={31} y={26} width={7} height={13} radius={3.5} /><Shape kind="knob" x={82} y={26} width={7} height={13} radius={3.5} />
        <Shape kind="body" x={36} y={14} width={48} height={38} radius={11} /><Face x={40} y={21} face={face} />
        <Shape kind="line" d="M60,14 V6" strokeWidth={2.2} /><g className="machine-motion machine-motion--bulb"><Shape kind="knob" cx={60} cy={5} radius={4} /></g>
      </>;
      case "microscope": return <>
        <Shadow width={42} /><Feet a={24} b={80} />
        <Shape kind="part" x={78} y={22} width={12} height={44} radius={5} /><Shape kind="knob" cx={84} cy={44} radius={6} />
        <g className="machine-motion machine-motion--focus"><Shape kind="body" x={88} y={14} width={18} height={30} radius={6} /><Shape kind="ink" x={92} y={6} width={10} height={9} radius={2} /><Shape kind="ink" x={93} y={44} width={8} height={7} radius={2} /></g>
        <Shape kind="part" x={86} y={58} width={24} height={6} radius={2} /><Shape kind="fill" x={92} y={55} width={12} height={3} radius={1} fillColor="#7FA3D6" />
        <Shape kind="body" x={14} y={62} width={84} height={31} radius={12} /><Face x={24} y={66} face={face} />
      </>;
      case "reader": return <>
        <Shadow width={32} /><Feet a={40} b={68} /><Shape kind="body" cx={60} cy={58} radius={35} /><Face x={40} y={50} face={face} />
        <Shape kind="stroke" cx={50} cy={61} radius={10} color="var(--machine-outline)" strokeWidth={2.4} /><Shape kind="stroke" cx={70} cy={61} radius={10} color="var(--machine-outline)" strokeWidth={2.4} />
        <Shape kind="line" d="M58,60 h4" strokeWidth={2.2} /><Shape kind="line" d="M48,24 l-6,-10 M72,24 l6,-10" strokeWidth={2.2} />
      </>;
      case "van": return <>
        <Shadow width={44} />
        <g className="machine-motion machine-motion--bounce">
          <Shape kind="part" x={26} y={24} width={20} height={14} radius={2} /><Shape kind="line" d="M36,24 v14" strokeWidth={1.5} />
          <Shape kind="body" x={12} y={36} width={72} height={54} radius={14} /><Shape kind="body" d="M82,52 H96 Q103,52 106,62 L109,76 Q109,90 100,90 H82 Z" />
          <Shape kind="glass" d="M86,57 H95 Q99,57 101,64 L102,70 H86 Z" /><Face x={28} y={50} face={face} />
        </g>
        <Shape kind="ink" cx={32} cy={92} radius={9} /><Shape kind="fill" cx={32} cy={92} radius={3.5} fillColor="var(--card)" />
        <Shape kind="ink" cx={92} cy={92} radius={9} /><Shape kind="fill" cx={92} cy={92} radius={3.5} fillColor="var(--card)" />
      </>;
      case "telescope": return <>
        <Shadow width={40} /><Feet a={30} b={78} /><Shape kind="part" x={58} y={38} width={8} height={22} radius={2} />
        <g className="machine-motion machine-motion--wave" style={{ transformOrigin: "62px 38px" }}><Shape kind="body" d="M37.6,45.6 L89.1,11.3 L98.9,28.7 L42.4,54.4 Z" /><Shape kind="line" d="M81.4,16.5 L90.4,32.5" strokeWidth={2.2} /><Shape kind="ink" cx={40} cy={50} radius={5} /></g>
        <Shape kind="knob" cx={62} cy={38} radius={4} /><Shape kind="body" x={22} y={58} width={76} height={35} radius={12} /><Face x={40} y={64} face={face} />
      </>;
      case "stamp": return <>
        <Shadow width={40} /><Feet a={26} b={82} /><Shape kind="part" x={30} y={54} width={60} height={7} radius={1.5} /><Shape kind="line" d="M34,57.5 h6 M80,57.5 h6" strokeWidth={1.5} />
        <g className="machine-motion machine-motion--press"><Shape kind="knob" cx={60} cy={16} radius={7} /><Shape kind="ink" x={56} y={22} width={8} height={14} radius={2} /><Shape kind="body" x={40} y={34} width={40} height={14} radius={4} /><Shape kind="gold" x={43} y={48} width={34} height={6} radius={1.5} /></g>
        <Shape kind="body" x={20} y={61} width={80} height={32} radius={12} /><Face x={40} y={66} face={face} />
      </>;
      case "crane": return <>
        <Shadow width={46} /><Feet a={22} b={54} />
        <g className="machine-motion machine-motion--print"><Shape kind="line" d="M92,18 V60" strokeWidth={1.5} /><Shape kind="gold" x={80} y={60} width={24} height={20} radius={3} /><Shape kind="line" d="M86,60 v20 M98,60 v20" strokeWidth={1.5} /></g>
        <Shape kind="part" x={34} y={14} width={8} height={44} radius={1} /><Shape kind="line" d="M34,20 L42,28 L34,36 L42,44 L34,52" strokeWidth={1.2} />
        <Shape kind="part" x={14} y={10} width={94} height={8} radius={2} /><Shape kind="ink" x={16} y={18} width={12} height={9} radius={2} />
        <Shape kind="body" x={14} y={56} width={56} height={37} radius={12} /><Face x={22} y={62} face={face} />
      </>;
      case "drill": return <>
        <Shadow width={36} /><Feet a={34} b={74} /><Shape kind="line" d="M44,58 L56,8 H64 L76,58" strokeWidth={2.2} /><Shape kind="line" d="M49,40 H71 M53,24 H67" strokeWidth={1.5} />
        <g className="machine-motion machine-motion--focus"><Shape kind="line" d="M60,30 V52" strokeWidth={3} /><Shape kind="line" d="M56,37 l8,4 M56,44 l8,4" strokeWidth={1.5} /><Shape kind="gold" x={51} y={18} width={18} height={12} radius={3} /></g>
        <Shape kind="body" x={26} y={56} width={68} height={37} radius={12} /><Face x={40} y={63} face={face} />
      </>;
      case "radar": return <>
        <Shadow width={34} /><Feet a={38} b={70} /><Shape kind="part" x={56} y={36} width={8} height={18} radius={2} />
        <g className="machine-motion machine-motion--wave" style={{ transformOrigin: "60px 38px" }}><Shape kind="part" d="M30,20 Q60,54 90,20 Z" /><Shape kind="line" d="M60,36 V12" strokeWidth={2} /><Shape kind="knob" cx={60} cy={10} radius={3.5} /></g>
        <Shape kind="body" x={28} y={52} width={64} height={41} radius={12} /><Face x={40} y={60} face={face} />
      </>;
      case "box": return <>
        <Shadow width={32} /><Feet a={40} b={68} /><Shape kind="body" x={28} y={30} width={64} height={62} radius={14} /><Face x={40} y={44} face={face} /><Shape kind="line" d="M44,80 h32" strokeWidth={2.2} />
      </>;
    }
  })();
  const bubble = face === "wait" || face === "held";
  return <svg className={classes} data-machine={machine} data-face={face} data-tint={tint} data-running={running} role="img" aria-label={`${machine} machine, ${face}`} viewBox="0 0 120 108" width={width} height={height} preserveAspectRatio="xMidYMid meet" style={rootStyle}>
    {art}
    {bubble && <g className="machine-motion machine-motion--bubble"><rect x={88} y={0} width={24} height={20} rx={10} fill={face === "wait" ? "var(--slate)" : "var(--red)"} stroke="var(--machine-outline)" strokeWidth={1.5} /><text x={100} y={10} textAnchor="middle" dominantBaseline="central" className="machine-bubble-text">{face === "wait" ? "!" : "?"}</text></g>}
  </svg>;
}

type NamedMachineProps = Omit<MachineProps, "width" | "height"> & { width?: number | string; height?: number | string };
const defaults = { face: "idle" as MachineFace, tint: "blue" as MachineTint, width: 120, height: 108, dimmed: false, frozen: false, paused: false };
function named(machine: MachineId): ComponentType<NamedMachineProps> {
  function NamedMachine(props: NamedMachineProps) { return <MachineDrawing machine={machine} {...defaults} {...props} />; }
  NamedMachine.displayName = `${machine[0]!.toUpperCase()}${machine.slice(1)}Machine`;
  return NamedMachine;
}

export const SorterMachine = named("sorter");
export const TypewriterMachine = named("typewriter");
export const MailboxMachine = named("mailbox");
export const RobotMachine = named("robot");
export const MicroscopeMachine = named("microscope");
export const ReaderMachine = named("reader");
export const VanMachine = named("van");
export const BoxMachine = named("box");
export const TelescopeMachine = named("telescope");
export const StampMachine = named("stamp");
export const CraneMachine = named("crane");
export const DrillMachine = named("drill");
export const RadarMachine = named("radar");

export const machineComponents: Record<MachineId, ComponentType<NamedMachineProps>> = {
  sorter: SorterMachine, typewriter: TypewriterMachine, mailbox: MailboxMachine, robot: RobotMachine,
  microscope: MicroscopeMachine, reader: ReaderMachine, van: VanMachine, box: BoxMachine,
  telescope: TelescopeMachine, stamp: StampMachine, crane: CraneMachine, drill: DrillMachine, radar: RadarMachine
};
