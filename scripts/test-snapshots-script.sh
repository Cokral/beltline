#!/bin/bash
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
temporary="$(mktemp -d "${TMPDIR%/}/beltline-snapshots-test.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT INT TERM
fixture="$temporary/repository with spaces"
mockbin="$temporary/mock bin"
test_tmp="$temporary/test tmp"
mkdir -p "$fixture/scripts" "$fixture/Web" "$fixture/daemon" "$mockbin" "$test_tmp"
cp "$repo/scripts/snapshots.sh" "$repo/scripts/web.sh" "$fixture/scripts/"
chmod +x "$fixture/scripts/snapshots.sh" "$fixture/scripts/web.sh"
printf '{"lockfileVersion": 3}\n' > "$fixture/Web/package-lock.json"

cat > "$mockbin/go" <<'MOCK'
#!/bin/bash
set -euo pipefail
[[ "$PWD" == "$FIXTURE/daemon" && "$1 $2 $4" == "build -o ./cmd/beltlined" ]] || { echo "unexpected go $* in $PWD" >&2; exit 2; }
daemon="$3"
mkdir -p "$(dirname "$daemon")"
cat > "$daemon" <<'DAEMON'
#!/bin/bash
set -euo pipefail
parent="$TMPDIR/beltline-demo"
mkdir -p "$parent/beltline-demo-$$-fixture"
printf '%s\n' "${BELTLINE_WEB_DIR:-}" > "$RECORD_DIR/daemon.web-dir"
if [[ ! -f "${BELTLINE_WEB_DIR:-}/index.html" ]]; then echo 'the web directory has no index.html' >&2; exit 5; fi
printf '%s\n' "$$" > "$DAEMON_PID_FILE"
mode="${DAEMON_MODE:-normal}"
token="${SENTINEL_TOKEN:-0000000000000000000000000000000000000000000000000000000000000000}"
case "$mode" in
    startup_failure) echo "startup rejected token=$token"; exit 7 ;;
    readiness_wait) exec /bin/sleep 300 ;;
    invalid_discovery) printf '{"pid":%s,"port":7331,"token":"%s"}\n' "$(( $$ + 1 ))" "$token" > "$parent/beltline-demo-$$-fixture/daemon.json" ;;
    *) printf '{"pid":%s,"port":7331,"token":"%s"}\n' "$$" "$token" > "$parent/beltline-demo-$$-fixture/daemon.json" ;;
esac
echo 'Beltline ready at http://127.0.0.1:7331/'
exec /bin/sleep 300
DAEMON
chmod +x "$daemon"
MOCK

cat > "$mockbin/node" <<'MOCK'
#!/bin/bash
set -euo pipefail
if [[ " $* " == *" -e import "* ]]; then
    [[ "${MOCK_BROWSER_MISSING:-0}" == 1 ]] && exit 1
    exit 0
fi
exec "$REAL_NODE" "$@"
MOCK

cat > "$mockbin/npm" <<'MOCK'
#!/bin/bash
set -euo pipefail
case "$*" in
    ci) exit 0 ;;
    "run build")
        printf 'VITE_BELTLINE_SNAPSHOTS=%s\n' "${VITE_BELTLINE_SNAPSHOTS:-}" > "$RECORD_DIR/vite.env"
        mkdir -p "$FIXTURE/Web/dist/assets"
        printf '%s\n' '<!doctype html>' > "$FIXTURE/Web/dist/index.html"
        ;;
    "run snapshots")
        printf '%s\n' "$BELTLINE_SNAPSHOT_ACTION" > "$CAPTURE_STARTED"
        case "$BELTLINE_SNAPSHOT_ACTION" in
            success) exit 0 ;;
            failure) exit 9 ;;
            wait) exec /bin/sleep 300 ;;
            *) exit 2 ;;
        esac
        ;;
    *) echo "unexpected npm $*" >&2; exit 2 ;;
esac
MOCK
chmod +x "$mockbin"/*

real_node="$(command -v node)"
run_wrapper() {
    PATH="$mockbin:$PATH" TMPDIR="$test_tmp" FIXTURE="$fixture" REAL_NODE="$real_node" \
        DAEMON_PID_FILE="$temporary/daemon.pid" CAPTURE_STARTED="$temporary/capture.started" RECORD_DIR="$temporary" \
        MOCK_BROWSER_MISSING="${MOCK_BROWSER_MISSING:-0}" \
        DAEMON_MODE="${DAEMON_MODE:-normal}" SENTINEL_TOKEN="${SENTINEL_TOKEN:-0000000000000000000000000000000000000000000000000000000000000000}" \
        BELTLINE_SNAPSHOTS="$temporary/output" BELTLINE_SNAPSHOT_ACTION="${BELTLINE_SNAPSHOT_ACTION:-success}" \
        "$fixture/scripts/snapshots.sh"
}

run_group_signal() {
    local mode="$1" action="$2" signal_name="$3" marker="$4" log="$5" expected="$6"
    python3 - "$fixture/scripts/snapshots.sh" "$mockbin" "$test_tmp" "$fixture" "$temporary" "$mode" "$action" "$signal_name" "$marker" "$log" "$expected" <<'PY'
import os
import signal
import subprocess
import sys
import time

script, mockbin, test_tmp, fixture, temporary, mode, action, signal_name, marker, log, expected = sys.argv[1:]
env = os.environ.copy()
env.update({
    "PATH": mockbin + os.pathsep + env["PATH"],
    "TMPDIR": test_tmp,
    "FIXTURE": fixture,
    "REAL_NODE": subprocess.check_output(["which", "node"], text=True).strip(),
    "DAEMON_PID_FILE": os.path.join(temporary, "daemon.pid"),
    "RECORD_DIR": temporary,
    "CAPTURE_STARTED": os.path.join(temporary, "capture.started"),
    "DAEMON_MODE": mode,
    "BELTLINE_SNAPSHOT_ACTION": action,
    "BELTLINE_SNAPSHOTS": os.path.join(temporary, "output"),
})
with open(log, "w") as output:
    process = subprocess.Popen([script], env=env, stdout=output, stderr=subprocess.STDOUT, start_new_session=True)
    deadline = time.monotonic() + 15
    while time.monotonic() < deadline and not os.path.exists(marker):
        if process.poll() is not None:
            raise SystemExit(f"wrapper exited before signal target {marker}")
        time.sleep(0.05)
    if not os.path.exists(marker):
        process.kill()
        raise SystemExit(f"wrapper did not reach signal target {marker}")
    daemon_pid = int(open(os.path.join(temporary, "daemon.pid")).read())
    os.killpg(process.pid, getattr(signal, signal_name))
    status = process.wait(timeout=10)
    if status != int(expected):
        raise SystemExit(f"wrapper exited {status} after {signal_name}, expected {expected}")
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline:
        try:
            os.kill(daemon_pid, 0)
        except ProcessLookupError:
            break
        time.sleep(0.05)
    else:
        raise SystemExit(f"daemon {daemon_pid} remained after {signal_name}")
PY
}

run_wrapper > "$temporary/normal.log"
grep -Fq 'Snapshot suite completed from http://127.0.0.1:7331.' "$temporary/normal.log"
[[ -z "$(find "$test_tmp" -mindepth 1 -maxdepth 1 -name 'beltline-snapshot-run.*' -print -quit)" ]]
normal_daemon_pid="$(cat "$temporary/daemon.pid")"
! kill -0 "$normal_daemon_pid" 2>/dev/null
grep -Fxq 'VITE_BELTLINE_SNAPSHOTS=1' "$temporary/vite.env"
recorded_web_dir="$(cat "$temporary/daemon.web-dir")"
[[ "$recorded_web_dir" == "$test_tmp"/beltline-snapshot-run.*/web ]]
[[ ! -e "$fixture/daemon/internal/server/webdist" && ! -e "$fixture/build" ]]
mkdir -p "$temporary/live-demo"
printf '{"live":true}\n' > "$temporary/live-demo/sentinel.json"

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
set +e
BELTLINE_SNAPSHOT_ACTION=failure run_wrapper > "$temporary/capture-failure.log" 2>&1
failure_status=$?
set -e
[[ "$failure_status" -eq 9 ]]
failure_daemon_pid="$(cat "$temporary/daemon.pid")"
! kill -0 "$failure_daemon_pid" 2>/dev/null
[[ -z "$(find "$test_tmp" -mindepth 1 -maxdepth 1 -name 'beltline-snapshot-run.*' -print -quit)" ]]

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
set +e
DAEMON_MODE=startup_failure run_wrapper > "$temporary/startup-failure.log" 2>&1
startup_status=$?
set -e
[[ "$startup_status" -ne 0 ]]
grep -Fq 'Snapshot daemon exited before readiness' "$temporary/startup-failure.log"
grep -Fq '[redacted]' "$temporary/startup-failure.log"
! grep -Fq '0000000000000000000000000000000000000000000000000000000000000000' "$temporary/startup-failure.log"

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
set +e
DAEMON_MODE=invalid_discovery run_wrapper > "$temporary/discovery-failure.log" 2>&1
discovery_status=$?
set -e
[[ "$discovery_status" -ne 0 ]]
grep -Fq 'discovery data did not match' "$temporary/discovery-failure.log"
! grep -Fq '0000000000000000000000000000000000000000000000000000000000000000' "$temporary/discovery-failure.log"

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
set +e
MOCK_BROWSER_MISSING=1 run_wrapper > "$temporary/missing-browser.log" 2>&1
browser_status=$?
set -e
[[ "$browser_status" -ne 0 ]]
grep -Fq 'Playwright Chromium is missing' "$temporary/missing-browser.log"
grep -Fq 'cd Web && npx playwright install chromium' "$temporary/missing-browser.log"
[[ ! -f "$temporary/daemon.pid" ]]
[[ -z "$(find "$test_tmp" -mindepth 1 -maxdepth 1 -name 'beltline-snapshot-run.*' -print -quit)" ]]

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
set +e
DAEMON_MODE=readiness_wait run_wrapper > "$temporary/no-ready-line.log" 2>&1
ready_status=$?
set -e
[[ "$ready_status" -ne 0 ]]
grep -Fq 'did not print its ready line within 30 seconds' "$temporary/no-ready-line.log"
timeout_daemon_pid="$(cat "$temporary/daemon.pid")"
! kill -0 "$timeout_daemon_pid" 2>/dev/null
[[ -z "$(find "$test_tmp" -mindepth 1 -maxdepth 1 -name 'beltline-snapshot-run.*' -print -quit)" ]]

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
PATH="$mockbin:$PATH" TMPDIR="$test_tmp" FIXTURE="$fixture" REAL_NODE="$real_node" \
    DAEMON_PID_FILE="$temporary/daemon.pid" CAPTURE_STARTED="$temporary/capture.started" RECORD_DIR="$temporary" \
    BELTLINE_SNAPSHOTS="$temporary/output" BELTLINE_SNAPSHOT_ACTION=wait \
    "$fixture/scripts/snapshots.sh" > "$temporary/interrupted.log" 2>&1 &
wrapper_pid=$!
for ((attempt=0; attempt<100; attempt++)); do
    [[ -f "$temporary/capture.started" ]] && break
    kill -0 "$wrapper_pid" 2>/dev/null || break
    sleep 0.1
done
[[ -f "$temporary/capture.started" ]] || { echo 'snapshot capture did not start for interruption test' >&2; exit 1; }
interrupted_daemon_pid="$(cat "$temporary/daemon.pid")"
kill -TERM "$wrapper_pid"
set +e
wait "$wrapper_pid"
interrupted_status=$?
set -e
[[ "$interrupted_status" -eq 143 ]]
! kill -0 "$interrupted_daemon_pid" 2>/dev/null
[[ -z "$(find "$test_tmp" -mindepth 1 -maxdepth 1 -name 'beltline-snapshot-run.*' -print -quit)" ]]
grep -Fq 'Starting isolated demo daemon' "$temporary/interrupted.log"
test -f "$temporary/live-demo/sentinel.json"

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
run_group_signal normal wait SIGINT "$temporary/capture.started" "$temporary/capture-interrupted.log" 130
[[ -z "$(find "$test_tmp" -mindepth 1 -maxdepth 1 -name 'beltline-snapshot-run.*' -print -quit)" ]]

rm -f "$temporary/capture.started" "$temporary/daemon.pid"
run_group_signal readiness_wait success SIGTERM "$temporary/daemon.pid" "$temporary/readiness-interrupted.log" 143
[[ -z "$(find "$test_tmp" -mindepth 1 -maxdepth 1 -name 'beltline-snapshot-run.*' -print -quit)" ]]
test -f "$temporary/live-demo/sentinel.json"

echo 'snapshot script lifecycle checks passed'
