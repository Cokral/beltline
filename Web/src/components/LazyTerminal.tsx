// The terminal, loaded when one first opens: xterm.js is most of the app's JavaScript, and most pages never show a terminal.
// The fallback keeps the terminal's frame, so the panel does not move when the code arrives.
import { lazy, Suspense } from "react";
import type { TerminalProps } from "./Terminal";

const Terminal = lazy(() => import("./Terminal").then(module => ({ default: module.Terminal })));

export function LazyTerminal(props: TerminalProps) {
  return <Suspense fallback={<div className="terminal" data-status="loading" aria-busy="true" />}>
    <Terminal {...props} />
  </Suspense>;
}
