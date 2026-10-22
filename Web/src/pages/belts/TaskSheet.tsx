import { useState } from "react";
import { useNavigate } from "react-router-dom";
import type { Belt, BeltContext, RunResponse } from "../../api/wire.gen";
import { useCommand } from "../../api/useCommand";
import { ErrorProblems } from "../../components/ProblemList";
import { Sheet } from "../../components/Sheet";
import { manualEntries } from "./TriggerForm";

/**
 * Start a task on a saved, live manual trigger. It takes the saved belt's triggers and repositories, never the
 * unsent draft's. The daemon's task defaults (the first manual entry and its first repository) come from the
 * editor's context when the belt has no unsent changes; otherwise the saved belt is read here. Cancel sends
 * nothing; a rejected start keeps the sheet and its input; a started task closes the sheet and opens its run.
 */
export function TaskSheet({ belt, context, onClose }: { belt: Belt; context?: BeltContext | null; onClose: () => void }) {
  const navigate = useNavigate();
  const command = useCommand();
  const manual = context?.taskEntries
    ? context.taskEntries.map(item => ({ entry: { id: item.id, repos: item.repos }, position: item.position }))
    : manualEntries(belt);
  const [entryId, setEntryId] = useState(context?.task?.entry?.id ?? manual[0]?.entry.id ?? "");
  const chosen = manual.find(item => item.entry.id === entryId) ?? manual[0];
  const entry = chosen?.entry;
  const repos = entry?.repos ?? [];
  const [repo, setRepo] = useState(context?.task?.repo ?? repos[0] ?? "");
  const repository = repos.includes(repo) ? repo : repos[0] ?? "";
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");

  const chooseEntry = (next: string) => {
    setEntryId(next);
    const nextRepos = manual.find(item => item.entry.id === next)?.entry.repos ?? [];
    setRepo(nextRepos.includes(repository) ? repository : nextRepos[0] ?? "");
  };

  const titleReason = title.trim() === "" ? "Give the task a title." : null;
  const repoReason = repos.length === 0 ? "No repository is eligible for this task. Add one to the trigger first." : null;
  const reason = titleReason ?? repoReason;

  const start = () => {
    if (reason !== null || entry === undefined) return;
    command.submit({
      path: "/api/v1/tasks",
      body: { belt: belt.id, entry: entry.id, repo: repository, title, body },
      affects: ["runs", "state"],
      publish: (_queryClient, result: RunResponse) => {
        onClose();
        navigate(`/runs/${encodeURIComponent(result.run.id)}`);
      }
    });
  };

  const footer = <>
    {reason && <span className="action-reason" id="task-start-reason">{reason}</span>}
    <button type="button" className="action-button" onClick={onClose}>Cancel</button>
    <button type="button" className="action-button primary" disabled={reason !== null || command.pending} aria-describedby={reason ? "task-start-reason" : undefined} onClick={start}>Start</button>
  </>;

  return <Sheet title="Start a task" open onClose={onClose} footer={footer}>
    {manual.length > 1 && <label className="belt-field"><span>Trigger</span>
      <select aria-label="Trigger" value={entry?.id ?? ""} onChange={event => chooseEntry(event.target.value)}>
        {manual.map(item => <option key={item.entry.id} value={item.entry.id}>Manual task trigger {item.position}</option>)}
      </select>
    </label>}
    <label className="belt-field"><span>Repository</span>
      <select aria-label="Repository" value={repository} disabled={repos.length === 0} onChange={event => setRepo(event.target.value)}>
        {repos.map(item => <option key={item} value={item}>{item}</option>)}
      </select>
    </label>
    <label className="belt-field"><span>Title</span>
      <input aria-label="Title" value={title} onChange={event => setTitle(event.target.value)} />
    </label>
    <label className="belt-field"><span>What to do</span>
      <textarea aria-label="What to do" rows={5} value={body} onChange={event => setBody(event.target.value)} />
    </label>
    {command.error !== null && command.error !== undefined && <ErrorProblems error={command.error} />}
  </Sheet>;
}
