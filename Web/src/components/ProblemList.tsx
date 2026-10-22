import { describeError } from "../api/errors";

/**
 * A failure message with the daemon's problems in the order it sent them. Text is rendered as text,
 * never as HTML, and duplicate or empty problems are kept as they arrived.
 */
export function ProblemList({ message, problems, className = "control-error" }: { message: string; problems: readonly string[]; className?: string }) {
  return <div role="alert" className={className}>
    <span>{message}</span>
    <ProblemItems problems={problems} />
  </div>;
}

/** The daemon's problems as an ordered list; renders nothing when there are none. */
export function ProblemItems({ problems }: { problems: readonly string[] }) {
  if (problems.length === 0) return null;
  return <ol>{problems.map((problem, index) => <li key={index}>{problem}</li>)}</ol>;
}

/** Shows any thrown value through ProblemList, so read and command errors look the same. */
export function ErrorProblems({ error, className }: { error: unknown; className?: string }) {
  const { message, problems } = describeError(error);
  return <ProblemList message={message} problems={problems} className={className} />;
}
