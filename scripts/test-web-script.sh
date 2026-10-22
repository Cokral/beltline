#!/bin/bash
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
temporary="$(mktemp -d "${TMPDIR%/}/beltline-web-script-test.XXXXXX")"
trap 'rm -rf "$temporary"' EXIT INT TERM

fixture="$temporary/repository with spaces"
mockbin="$temporary/mock bin"
failbin="$temporary/failing bin"
mkdir -p "$fixture/scripts" "$fixture/Web" "$fixture/daemon/internal/server" "$mockbin" "$failbin"
printf '{"lockfileVersion": 3}\n' > "$fixture/Web/package-lock.json"
cp "$repo/scripts/web.sh" "$fixture/scripts/web.sh"
chmod +x "$fixture/scripts/web.sh"
cp "$repo/Makefile" "$fixture/Makefile"

# The mock npm builds the same tree as Vite: a fresh dist/, an index.html that names a hashed asset
# and that MISSING_INDEX=1 leaves out.
cat > "$mockbin/npm" <<'MOCK'
#!/bin/bash
set -euo pipefail
printf 'npm %s\n' "$*" >> "$TRACE"
if [[ "${FAIL_NPM:-}" == "ci" && "$*" == "ci" ]]; then exit 41; fi
if [[ "${FAIL_NPM:-}" == "build" && "$*" == "run build" ]]; then exit 42; fi
if [[ "$*" == "run build" ]]; then
    rm -rf "$FIXTURE/Web/dist"
    mkdir -p "$FIXTURE/Web/dist/assets"
    if [[ "${MISSING_INDEX:-}" != 1 ]]; then
        printf '%s\n' '<!doctype html><script src="/assets/index-1234abcd.js"></script>' > "$FIXTURE/Web/dist/index.html"
    fi
    printf '%s\n' 'export {}' > "$FIXTURE/Web/dist/assets/index-1234abcd.js"
fi
MOCK
chmod +x "$mockbin/npm"

# The mock go records each build and writes a stand-in beltlined at its -o path, which records
# its own arguments, so the Makefile's build and install targets run without compiling anything.
cat > "$mockbin/go" <<'MOCK'
#!/bin/bash
set -euo pipefail
printf 'go %s\n' "$*" >> "$TRACE"
output=""
previous=""
for argument in "$@"; do
    if [[ "$previous" == -o ]]; then output="$argument"; fi
    previous="$argument"
done
mkdir -p "$(dirname "$output")"
cat > "$output" <<'BELTLINED'
#!/bin/bash
printf 'beltlined %s\n' "$*" >> "$TRACE"
BELTLINED
chmod +x "$output"
MOCK
chmod +x "$mockbin/go"

cat > "$failbin/cp" <<'MOCK'
#!/bin/bash
exit 44
MOCK
chmod +x "$failbin/cp"

default_destination="$fixture/daemon/internal/server/webdist"
mkdir -p "$default_destination"
printf old > "$default_destination/old-hash.js"
trace="$temporary/command trace"

PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" "$fixture/scripts/web.sh" > "$temporary/default.log"
[[ "$(cat "$trace")" == $'npm ci\nnpm run build' ]]
[[ -f "$default_destination/index.html" && -f "$default_destination/assets/index-1234abcd.js" ]]
[[ ! -e "$default_destination/old-hash.js" ]]

# node_modules is already installed from this lock file, so npm ci is skipped; a relative
# --output-dir is taken from where the script was started.
other_destination="$temporary/other output"
: > "$trace"
(cd "$temporary" && PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" "$fixture/scripts/web.sh" --output-dir "other output" > /dev/null)
[[ "$(cat "$trace")" == 'npm run build' ]]
[[ -f "$other_destination/index.html" && -f "$other_destination/assets/index-1234abcd.js" ]]

# An explicit output directory leaves the production destination alone.
printf 'production-sentinel' > "$default_destination/sentinel.txt"
snapshot_destination="$temporary/snapshot output"
PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" "$fixture/scripts/web.sh" --output-dir "$snapshot_destination" > /dev/null
[[ "$(cat "$default_destination/sentinel.txt")" == production-sentinel ]]
[[ -f "$snapshot_destination/index.html" && ! -e "$snapshot_destination/sentinel.txt" ]]

for rejected in --release --bin-dir; do
    if PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" "$fixture/scripts/web.sh" "$rejected" "$temporary/old bin" > /dev/null 2>&1; then
        echo "the option $rejected was accepted" >&2
        exit 1
    fi
done
if PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" "$fixture/scripts/web.sh" --output-dir > /dev/null 2>&1; then
    echo "--output-dir without a directory was accepted" >&2
    exit 1
fi

# A changed lock file installs again.
printf '{"lockfileVersion": 3, "changed": true}\n' > "$fixture/Web/package-lock.json"
: > "$trace"
PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" "$fixture/scripts/web.sh" > /dev/null
[[ "$(cat "$trace")" == $'npm ci\nnpm run build' ]]

for failure in ci build; do
    rm -rf "$fixture/Web/node_modules"
    : > "$trace"
    if PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" \
        FAIL_NPM="$failure" "$fixture/scripts/web.sh" > /dev/null 2>&1; then
        echo "npm $failure failure was ignored" >&2
        exit 1
    fi
    if [[ "$failure" == ci ]]; then
        # The failed npm ci left no lock stamp, so the next build installs again.
        [[ ! -e "$fixture/Web/node_modules/.beltline-lock" ]]
    fi
done

# A build without index.html is refused before the destination is touched.
rm -rf "$fixture/Web/node_modules"
printf 'kept' > "$snapshot_destination/kept.txt"
: > "$trace"
if PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" MISSING_INDEX=1 \
    "$fixture/scripts/web.sh" --output-dir "$snapshot_destination" > "$temporary/missing-index.log" 2>&1; then
    echo "a build without index.html was copied" >&2
    exit 1
fi
grep -Fq 'Web/dist/index.html is missing' "$temporary/missing-index.log"
[[ "$(cat "$snapshot_destination/kept.txt")" == kept ]]

if PATH="$failbin:$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" "$fixture/scripts/web.sh" > /dev/null 2>&1; then
    echo "copy failure was ignored" >&2
    exit 1
fi

# make build prepares the web app before it compiles, and make install compiles before it
# registers. Each stands-in beltlined records its arguments; a failed web build compiles nothing.
rm -rf "$fixture/build" "$fixture/Web/node_modules" "$fixture/Web/dist"
: > "$trace"
PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" make -C "$fixture" build > "$temporary/make-build.log" 2>&1
[[ "$(cat "$trace")" == $'npm ci\nnpm run build\ngo build -tags embedweb'* ]]
[[ -x "$fixture/build/bin/beltlined" && -f "$default_destination/index.html" ]]

: > "$trace"
PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" make -C "$fixture" install > "$temporary/make-install.log" 2>&1
[[ "$(cat "$trace")" == $'npm run build\ngo build -trimpath -tags embedweb'*$'\nbeltlined install' ]]
[[ -x "$fixture/build/release/beltlined" ]]

rm -rf "$fixture/build"
: > "$trace"
if PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" FAIL_NPM=build make -C "$fixture" build > /dev/null 2>&1; then
    echo "make build compiled after a failed web build" >&2
    exit 1
fi
if grep -Fq 'go ' "$trace"; then echo 'make build compiled after a failed web build' >&2; exit 1; fi
[[ ! -e "$fixture/build/bin/beltlined" ]]

rm -rf "$fixture/build"
: > "$trace"
if PATH="$mockbin:$PATH" TRACE="$trace" FIXTURE="$fixture" MISSING_INDEX=1 make -C "$fixture" install > /dev/null 2>&1; then
    echo "make install registered a build without index.html" >&2
    exit 1
fi
if grep -Fq 'go ' "$trace"; then echo 'make install compiled a build without index.html' >&2; exit 1; fi
[[ ! -e "$fixture/build/release/beltlined" ]]

echo "web script checks passed"
"$repo/scripts/test-snapshots-script.sh"
