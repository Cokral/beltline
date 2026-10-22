#!/bin/bash
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
# The generated tree go:embed reads for an embedweb build (daemon/internal/server/server_web_assets_embedded.go).
destination="$repo/daemon/internal/server/webdist"

while (($#)); do
    case "$1" in
        --output-dir)
            if (($# < 2)) || [[ -z "$2" ]]; then
                echo "Usage: scripts/web.sh [--output-dir <directory>]" >&2
                exit 2
            fi
            destination="$2"
            shift 2
            ;;
        *)
            echo "Usage: scripts/web.sh [--output-dir <directory>]" >&2
            exit 2
            ;;
    esac
done
# A relative --output-dir is relative to where the script was started, not to Web/.
if [[ "$destination" != /* ]]; then destination="$PWD/$destination"; fi

cd "$repo/Web"
# npm ci empties node_modules and installs it again, which every make check paid for.
# Skip it while node_modules was installed from this package-lock.json; removing
# Web/node_modules installs again.
stamp=node_modules/.beltline-lock
lock="$(shasum -a 256 package-lock.json | cut -d ' ' -f 1)"
if [[ ! -f "$stamp" || "$(cat "$stamp")" != "$lock" ]]; then
    npm ci
    mkdir -p node_modules
    printf '%s\n' "$lock" > "$stamp"
fi
npm run build

# Checked before anything is replaced: an embedded build must never take a tree without its shell.
if [[ ! -f "$repo/Web/dist/index.html" ]]; then
    echo "Web/dist/index.html is missing after npm run build; no web files were copied." >&2
    exit 1
fi

# Replaces the destination's previous files, so no old hashed asset can enter the next build.
rm -rf "$destination"
mkdir -p "$destination"
cp -R "$repo/Web/dist/." "$destination/"
echo "Copied Web/dist to $destination"
