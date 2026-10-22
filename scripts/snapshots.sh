#!/bin/bash
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
web="$repo/Web"
temporary=""
daemon_pid=""
capture_pid=""
demo_root=""
interrupted=0

cleanup() {
    local status=$?
    trap - EXIT INT TERM
    if [[ -n "$capture_pid" ]] && kill -0 "$capture_pid" 2>/dev/null; then
        kill -TERM "$capture_pid" 2>/dev/null || true
        wait "$capture_pid" 2>/dev/null || true
    fi
    if [[ -n "$daemon_pid" ]] && kill -0 "$daemon_pid" 2>/dev/null; then
        kill -TERM "$daemon_pid" 2>/dev/null || true
        wait "$daemon_pid" 2>/dev/null || true
    fi
    if [[ -n "$temporary" && -d "$temporary" ]]; then rm -rf "$temporary"; fi
    if (( interrupted )); then exit "$interrupted"; fi
    exit "$status"
}
on_signal() {
    local signal="$1"
    if [[ "$signal" == INT ]]; then interrupted=130; else interrupted=143; fi
    if [[ -n "$capture_pid" ]]; then kill -TERM "$capture_pid" 2>/dev/null || true; fi
    if [[ -n "$daemon_pid" ]]; then kill -TERM "$daemon_pid" 2>/dev/null || true; fi
    exit "$interrupted"
}
trap cleanup EXIT
trap 'on_signal INT' INT
trap 'on_signal TERM' TERM

output="${BELTLINE_SNAPSHOTS:-$repo/build/web-snapshots}"
if [[ "$output" != /* ]]; then output="$repo/$output"; fi
mkdir -p "$output"
temporary="$(mktemp -d "${TMPDIR%/}/beltline-snapshot-run.XXXXXX")"
mkdir -p "$temporary/tmp"

printf 'Building daemon and snapshot web app…\n'
# The daemon is built without the embedweb tag and the snapshot web app is prepared into the
# run's temporary folder, which the daemon reads through BELTLINE_WEB_DIR. The embedding inputs
# and make build's executable keep the normal web app.
bin_dir="$temporary/bin"
web_dir="$temporary/web"
(cd "$repo/daemon" && go build -o "$bin_dir/beltlined" ./cmd/beltlined)
VITE_BELTLINE_SNAPSHOTS=1 "$repo/scripts/web.sh" --output-dir "$web_dir"

browser_path="${PLAYWRIGHT_BROWSERS_PATH:-$HOME/Library/Caches/ms-playwright}"
if ! (cd "$web" && PLAYWRIGHT_BROWSERS_PATH="$browser_path" node --input-type=module -e 'import { chromium } from "@playwright/test"; import { existsSync } from "node:fs"; if (!existsSync(chromium.executablePath())) process.exit(1)') >/dev/null 2>&1; then
    printf 'Playwright Chromium is missing. Install it once after Web dependencies are installed with: cd Web && npx playwright install chromium\n' >&2
    exit 1
fi

mkdir -p "$temporary/tmp/beltline-demo"
daemon="$bin_dir/beltlined"
TMPDIR="$temporary/tmp" BELTLINE_WEB_DIR="$web_dir" BELTLINE_DEMO_LONG_MARKDOWN=1 "$daemon" --demo >"$temporary/daemon.log" 2>&1 &
daemon_pid=$!
printf 'Starting isolated demo daemon…\n'
ready_line=""
for ((second=0; second<30; second++)); do
    if ! kill -0 "$daemon_pid" 2>/dev/null; then
        printf 'Snapshot daemon exited before readiness. See daemon startup output (token values are suppressed).\n' >&2
        sed -E 's/[0-9a-f]{64}/[redacted]/g' "$temporary/daemon.log" >&2
        exit 1
    fi
    ready_line="$(grep -E 'Beltline ready at http://127\.0\.0\.1:[0-9]+/' "$temporary/daemon.log" | tail -n 1 || true)"
    if [[ -n "$ready_line" ]]; then break; fi
    sleep 1
done
if [[ -z "$ready_line" ]]; then
    printf 'Snapshot daemon did not print its ready line within 30 seconds.\n' >&2
    sed -E 's/[0-9a-f]{64}/[redacted]/g' "$temporary/daemon.log" >&2
    exit 1
fi
origin="${ready_line#* at }"
origin="${origin%/}"
port="${origin##*:}"
demo_parent="$temporary/tmp/beltline-demo"
shopt -s nullglob
discoveries=("$demo_parent"/beltline-demo-"$daemon_pid"-*/daemon.json)
if ((${#discoveries[@]} != 1)); then
    printf 'Snapshot daemon discovery file was not found or was ambiguous for pid %s.\n' "$daemon_pid" >&2
    exit 1
fi
discovery="${discoveries[0]}"
discovery_values="$(node --input-type=module - "$discovery" "$daemon_pid" "$port" <<'NODE'
import { readFileSync } from "node:fs";
const value = JSON.parse(readFileSync(process.argv[2], "utf8"));
if (Number(value.pid) !== Number(process.argv[3]) || Number(value.port) !== Number(process.argv[4]) || typeof value.token !== "string" || !/^[0-9a-f]{64}$/.test(value.token)) process.exit(2);
process.stdout.write(`${value.pid}\t${value.port}\t${value.token}`);
NODE
 )" || {
    printf 'Snapshot daemon discovery data did not match its process and ready port.\n' >&2
    exit 1
}
IFS=$'\t' read -r pid port token <<< "$discovery_values"
origin="http://127.0.0.1:$port"

mkdir -p "$output"
set +e
(
    cd "$web"
    BELTLINE_SNAPSHOT_ORIGIN="$origin" BELTLINE_SNAPSHOT_TOKEN="$token" BELTLINE_SNAPSHOTS="$output" \
        BELTLINE_SNAPSHOT_ONLY="${BELTLINE_SNAPSHOT_ONLY:-}" BELTLINE_SNAPSHOT_SIZE="${BELTLINE_SNAPSHOT_SIZE:-}" \
        BELTLINE_SNAPSHOT_TEST_OVERFLOW="${BELTLINE_SNAPSHOT_TEST_OVERFLOW:-}" \
        exec npm run snapshots
) &
capture_pid=$!
wait "$capture_pid"
status=$?
capture_pid=""
set -e
if (( status != 0 )); then exit "$status"; fi
printf 'Snapshot suite completed from %s.\n' "$origin"
