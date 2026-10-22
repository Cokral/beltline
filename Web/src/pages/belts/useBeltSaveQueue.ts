import { useEffect, useRef, useState } from "react";

export interface BeltSaveQueue {
  /** True from the click until the save's answer, including a click that waits for another belt command. */
  busy: boolean;
  /** Starts a save now, or queues one behind the belt command that is running. A click while saving is the same save. */
  request: (blocked: boolean) => void;
  /** Runs the newest save, for the assistant, which keeps the callback from an earlier render. */
  saveLatest: (then?: (savedKey: string) => void) => void;
}

/**
 * One save at a time for the belt editor. A Save clicked while another belt command runs waits for it and goes as
 * soon as that command has its answer. `blocked` is the editor's own refusal (for example a missing saved belt), which
 * turns the click into nothing.
 */
export function useBeltSaveQueue(pending: boolean, save: (then?: (savedKey: string) => void) => void): BeltSaveQueue {
  const [saveQueued, setSaveQueued] = useState(false);
  const [saving, setSaving] = useState(false);
  // The page keeps this callback while the editor stays dirty; point it at the latest draft on every render.
  const latestSave = useRef(save);
  latestSave.current = save;
  // A Save clicked while another belt command ran goes as soon as that command has its answer.
  useEffect(() => {
    if (pending) return;
    if (saving) setSaving(false);
    if (!saveQueued) return;
    setSaveQueued(false);
    setSaving(true);
    latestSave.current();
  }, [saveQueued, saving, pending]);

  const request = (blocked: boolean) => {
    if (blocked || saving || saveQueued) return;
    if (pending) setSaveQueued(true);
    else { setSaving(true); save(); }
  };

  return {
    busy: saving || saveQueued,
    request,
    saveLatest: then => latestSave.current(then)
  };
}
