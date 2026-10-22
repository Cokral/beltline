#!/bin/zsh
# Stands in for an agent CLI in the tmux integration tests: finds the output
# file the brief names, writes an outcome for the stage, and exits. An
# interactive stage's brief ends with its handoff: the stand-in runs it once the
# output is written and stays open, echoing what is typed, until it is stopped.
# Interactive Continue has resume arguments but no prompt. Keep taking turns
# until the coordinator ends its tmux session.
if [[ "$1" == --version ]]; then echo 'stand-in agent 1.0'; exit 0; fi
if [[ "$1" == create-chat ]]; then
  # uuidgen is macOS's; Linux without uuid-runtime has the kernel's.
  { uuidgen 2>/dev/null || cat /proc/sys/kernel/random/uuid; } | tr '[:upper:]' '[:lower:]'
  exit 0
fi
# Continue now always carries its historical explicit model at the end.
if [[ $# -ge 2 && ( "${@[-2]}" == --model || "${@[-2]}" == -m ) ]]; then
  set -- "${@[1,-3]}"
fi
# Codex's interactive forms trust their checkout for the launch: drop that override.
for (( i=1; i<$#; i++ )); do
  if [[ "${@[i]}" == -c && "${@[i+1]}" == projects=* ]]; then set -- "${@[1,i-1]}" "${@[i+2,-1]}"; break; fi
done
# Pi's Continue carries --session since Pi runs its stages headless; --session-id is the shape it had before.
if [[ $# == 4 && "$1" == --force && "$2" == --trust && "$3" == --resume ]] || [[ $# == 1 && "$1" == --dangerously-bypass-approvals-and-sandbox ]] || [[ $# == 3 && "$1" == resume ]] || [[ $# == 4 && "$1" == --tui-mode && ( "$3" == --session || "$3" == --session-id ) ]] || [[ $# == 2 && "$1" == --resume=* ]] || [[ $# == 5 && "$1" == --resume && "$3" == --allow-dangerously-skip-permissions ]]; then
  echo "stand-in Continue in $(pwd)"
  while read -r message; do echo "Continue reply: $message"; done
  exit 0
fi
session_id=""
copilot_managed=0
for (( i=1; i<=$#; i++ )); do
  case "${@[i]}" in
    --session-id|--session|--resume) session_id="${@[i+1]}" ;;
    --resume=*) session_id="${@[i]#--resume=}" ;;
    --name=*) session_id="${session_id:-${@[i]#--name=}}" ;;
    --name) session_id="${session_id:-${@[i+1]}}" ;;
    --autopilot) copilot_managed=1 ;;
  esac
done
session_id="${session_id:0:100}"
aborted=0
write_copilot_event() {
  [[ "$copilot_managed" == 1 ]] || return 0
  local root="${BELTLINE_COPILOT_STATE:-$HOME/.copilot/session-state}"
  local key="${session_id:-anon}"
  mkdir -p "$root/$key"
  printf '{"type":"%s"}\n' "$1" >> "$root/$key/events.jsonl"
}
hand_back() {
  local command
  command=$(print -r -- "$prompt" | grep "^printf '%s" | tail -1)
  [[ -n "$command" ]] && eval "$command"
}
stay_open() {
  [[ "$copilot_managed" == 1 || "$prompt" == *"## Handing the work back"* ]] || return 0
  while read -r message; do echo "Continue reply: $message"; done
  exit 0
}
state_root="${TMPDIR:-/tmp}/beltline-stand-in/${session_id:-anon}"
prompt="${@[-1]}"
for (( i=1; i <= $#; i++ )); do
  if [[ "${@[i]}" == --prompt || "${@[i]}" == --interactive ]]; then
    prompt="${@[i+1]}"
    break
  fi
done
if [[ "$prompt" == *"Beltline configuration assistant"* ]]; then
  exec /bin/zsh "${0:A:h}/stand-in-conversation.sh"
fi
# The AFK assistant approves whatever waits for it.
if [[ "$prompt" == *"deciding for someone who is away"* ]]; then
  decision=$(print -r -- "$prompt" | sed -n 's#^Write it to \(/.*\), in this shape:$#\1#p' | tail -1)
  printf '## Message\n\nApproved by the stand-in.\n\n## Why\n\nThe stand-in approves.\n\nDecision: decide approved\n' > "$decision"
  exit 0
fi
out=$(print -r -- "$prompt" | sed -n 's#.*End \(/.*\) with one line.*#\1#p' | tail -1)
save_interrupt() {
  mkdir -p "$state_root"
  print -r -- "$out" > "$state_root/output"
  print -r -- "$prompt" > "$state_root/prompt"
  print -r -- "$PWD" > "$state_root/cwd"
  exit 130
}
if [[ "$copilot_managed" == 1 ]]; then
  trap 'aborted=1' INT
else
  trap save_interrupt INT
fi
# Resume sends exactly Continue. The original output path and prompt stay in the state file.
if [[ "${prompt%%$'\n'*}" == "Continue" ]]; then
  mkdir -p "$state_root"
  saved_out="$(cat "$state_root/output" 2>/dev/null || true)"
  saved_prompt="$(cat "$state_root/prompt" 2>/dev/null || true)"
  echo "continued ${saved_prompt}" >> "$state_root/transcript"
  if [[ -n "$saved_out" ]]; then
    if [[ "$saved_out" == *triage.md ]]; then
      printf '# Output\n\nContinued.\n\nOutcome: ready to spec\n' > "$saved_out"
    else
      printf '# Output\n\nContinued.\n\nOutcome: done\n' > "$saved_out"
    fi
  fi
  echo "stand-in continued ${session_id}"
  write_copilot_event session.task_complete
  hand_back
  stay_open
  exit 0
fi
echo "stand-in agent in $(pwd)"
print -r -- "$prompt"
if [[ "$prompt" == *CURSOR-EXIT-FAILURE* ]]; then exit 1; fi
if [[ "$prompt" == *CURSOR-NO-OUTPUT* ]]; then exit 0; fi
if [[ "$prompt" == *CURSOR-REPLY-ONLY* ]]; then echo 'Outcome: ready to spec'; exit 0; fi
case "$out" in
  *triage.md) outcome="ready to spec" ;;
  *spec.md) outcome="complete" ;;
  *evidence.md) outcome="done"; echo change >> README.md; git -c user.name=t -c user.email=t@t commit -qam "Build" ;;
  *verdict.md) outcome="pass" ;;
  *) outcome="done" ;;
esac
# The abort test waits for this marker before stopping the live session.
# A follow-up has no issue text; a rerun includes the earlier-work section.
if [[ "$prompt" == *WAIT-FOR-ABORT* && "$prompt" != *"What this stage already had"* ]]; then
  touch "${out:h}/ready-to-abort"
  sleep 60
fi
if [[ "$prompt" == *WAIT-FOR-PAUSE* ]]; then
  if [[ "$prompt" == *PARTIAL-OUTCOME* && -n "$out" ]]; then
    printf 'partial\n\nOutcome: done\n' > "$out"
  fi
  touch "${out:h}/ready-to-pause"
  sleep 120
fi
if [[ "$prompt" == *EDIT-THE-CHECKOUT* ]]; then echo vandal >> README.md; fi
# Waits for a person to type a line into the open interface before finishing.
steered=""
if [[ "$prompt" == *WAIT-FOR-STEER* ]]; then
  echo "stand-in ready for steering"
  touch "${out:h}/ready-to-steer"
  read -r steered
  echo "stand-in steered: $steered"
fi
# Finishes its output and stays open without handing back.
if [[ "$prompt" == *NEVER-HAND-BACK* ]]; then
  printf '# Output\n\nWritten without handing back.\n\nOutcome: %s\n' "$outcome" > "$out"
  while read -r message; do echo "reply: $message"; done
  exit 0
fi
# Exits after writing its output, without handing back.
if [[ "$prompt" == *EXIT-BEFORE-HANDOFF* ]]; then
  printf '# Output\n\nWritten, then exited.\n\nOutcome: %s\n' "$outcome" > "$out"
  exit 0
fi
if [[ "$prompt" == *CURSOR-INVALID-OUTCOME* ]]; then outcome="not a configured outcome"; fi
# Holds for you until it is answered: a continued session's prompt has no issue text.
if [[ "$prompt" == *HOLD-FOR-ME* ]]; then outcome="needs info"; fi
if [[ "$prompt" == *"Their answer"* ]]; then echo "resumed with: $*"; fi
if [[ "$aborted" == 1 ]]; then
  mkdir -p "$state_root"
  print -r -- "$out" > "$state_root/output"
  print -r -- "$prompt" > "$state_root/prompt"
  print -r -- "$PWD" > "$state_root/cwd"
  write_copilot_event abort
  stay_open
  exit 130
fi
# Fails the way a headless runner does: its reason printed, no output written.
if [[ "$prompt" == *EXIT-WITHOUT-OUTPUT* ]]; then
  echo "stand-in: test provider refused the request"
  exit 42
fi
if [[ -n "$steered" ]]; then
  printf '# Output\n\nWritten by the stand-in agent, steered: %s\n\nOutcome: %s\n' "$steered" "$outcome" > "$out"
else
  printf '# Output\n\nWritten by the stand-in agent.\n\nOutcome: %s\n' "$outcome" > "$out"
fi
write_copilot_event session.task_complete
hand_back
stay_open
