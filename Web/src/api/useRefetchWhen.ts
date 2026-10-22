import { useEffect, useRef } from "react";

/**
 * Refetches a cached read when a value it depends on changes while its page stays mounted, such as the selected
 * item: a page that is already showing its browser does not otherwise ask again when the person opens an item and
 * comes back. The value's first render is only remembered, since the read's own mount already fetches it. `enabled`
 * says whether the read is wanted for the new value; a read that is not wanted is left alone.
 */
export function useRefetchWhen(refetch: () => unknown, value: unknown, enabled = true): void {
  const seen = useRef(value);
  const latestRefetch = useRef(refetch);
  latestRefetch.current = refetch;
  useEffect(() => {
    if (Object.is(seen.current, value)) return;
    seen.current = value;
    if (enabled) void latestRefetch.current();
  }, [value, enabled]);
}
