#!/bin/sh
# Copies the belts and agents of an old factory or marketplace repository into its
# root belts/ and agents/ folders, so the repository can be used as a library
# repository. A factory repository gives its newest version of one factory (named
# by a second argument when there are several); a marketplace repository gives every
# package version. Only the working tree changes: review it, then commit it yourself.
# Usage: scripts/migrate-library-repo.sh <path-to-a-clone> [factory-id]
set -eu

if [ $# -lt 1 ] || [ ! -d "$1" ]; then
	echo "usage: $0 <path-to-a-clone> [factory-id]" >&2
	exit 2
fi
cd "$1"

# copy_version copies the belts/ and agents/ folders of one version folder into the root.
copy_version() {
	for kind in belts agents; do
		if [ -d "$1/$kind" ]; then
			mkdir -p "$kind"
			cp -R "$1/$kind/." "$kind/"
		fi
	done
}

if [ -d factories ]; then
	factory=${2:-}
	if [ -z "$factory" ]; then
		count=0
		for folder in factories/*/; do
			[ -d "$folder" ] || continue
			factory=$(basename "$folder")
			count=$((count + 1))
		done
		if [ "$count" -ne 1 ]; then
			echo "$count factories found: name one with a second argument" >&2
			exit 1
		fi
	fi
	newest=0
	for folder in factories/"$factory"/v*/; do
		[ -d "$folder" ] || continue
		number=${folder%/}
		number=${number##*/v}
		if [ "$number" -gt "$newest" ]; then
			newest=$number
		fi
	done
	if [ "$newest" -eq 0 ]; then
		echo "no versions of factory $factory" >&2
		exit 1
	fi
	copy_version "factories/$factory/v$newest"
elif [ -d packages ]; then
	for folder in packages/*/v*/; do
		[ -d "$folder" ] && copy_version "${folder%/}"
	done
else
	echo "$1 has neither factories/ nor packages/" >&2
	exit 1
fi
echo "Copied the belts and agents into $1: review them, then commit."
