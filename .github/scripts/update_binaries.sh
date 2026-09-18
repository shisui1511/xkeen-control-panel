#!/usr/bin/env bash
# Обновляет ветку binaries — зеркало бинарников для setup.sh (jsDelivr, raw.githubusercontent.com).
# Ветка состоит из одного коммита с бинарниками последнего stable и последней dev-сборки,
# чтобы dev-сборка не стирала stable с зеркала, а история не копила мегабайты бинарников.
# Использование: update_binaries.sh VERSION ARTIFACTS_DIR
set -euo pipefail

version="$1"
artifacts="$2"
branch="binaries"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

version_le() { [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | tail -1)" = "$2" ]; }

# Бинарник другого канала остаётся на зеркале; dev-сборка переживает stable, только если она новее
keep_file() {
  local ver="$1"
  if [[ "$version" == *-dev ]]; then
    [[ "$ver" != *-dev ]]
  else
    local base="${ver#v}"
    [[ "$ver" == *-dev ]] && ! version_le "${base%-dev}" "${version#v}"
  fi
}

build_commit() {
  rm -rf "$work/bin"
  mkdir -p "$work/bin"

  remote_sha=""
  if git fetch --quiet --depth 1 origin "refs/heads/${branch}" 2>/dev/null; then
    remote_sha=$(git rev-parse FETCH_HEAD)
    while IFS= read -r path; do
      name="${path#bin/}"
      ver="${name#xcp_}"
      ver="${ver%_*}"
      if keep_file "$ver"; then
        git cat-file blob "${remote_sha}:${path}" >"$work/bin/${name}"
      fi
    done < <(git ls-tree --name-only "${remote_sha}" bin/)
  fi

  cp "$artifacts"/xcp_*/xcp_"${version}"_* "$work/bin/"

  local bin_tree root_tree f
  bin_tree=$(
    for f in "$work/bin"/*; do
      printf '100644 blob %s\t%s\n' "$(git hash-object -w "$f")" "$(basename "$f")"
    done | git mktree
  )
  root_tree=$(printf '040000 tree %s\tbin\n' "$bin_tree" | git mktree)
  commit=$(git commit-tree "$root_tree" -m "$version")

  echo "Содержимое ${branch}:"
  ls -1 "$work/bin"
}

for attempt in 1 2 3; do
  build_commit
  if git push --force-with-lease="refs/heads/${branch}:${remote_sha}" origin "${commit}:refs/heads/${branch}"; then
    exit 0
  fi
  echo "Ветку ${branch} обновили параллельно, повтор ${attempt}..."
  sleep 5
done

echo "Не удалось обновить ветку ${branch}" >&2
exit 1
