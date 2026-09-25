#!/usr/bin/env bash
# Удаляет pre-release вместе с тегами.
# Использование: prune_dev_releases.sh [VERSION]
#   без аргумента — удалить все rolling dev-релизы (vX.Y.Z-dev), RC не трогаются;
#   с VERSION     — dev-релизы и release candidate (vX.Y.Z-rc.N), чья базовая версия
#                   не выше VERSION (после выхода stable).
set -euo pipefail

limit="${1:-}"
limit="${limit#v}"

# Тег может пережить свой релиз и наоборот — собираем оба списка
tags=$(
  {
    git ls-remote --tags --refs origin | sed 's#.*refs/tags/##'
    gh release list --limit 100 --json tagName --jq '.[].tagName'
  } | grep -E -- "^v[0-9]+\\.[0-9]+\\.[0-9]+-(dev${limit:+|rc\\.[0-9]+})\$" | sort -u || true
)

for tag in $tags; do
  base="${tag#v}"
  base="${base%%-*}"
  if [ -n "$limit" ] && [ "$(printf '%s\n%s\n' "$base" "$limit" | sort -V | tail -1)" != "$limit" ]; then
    echo "Оставляем ${tag}: новее v${limit}"
    continue
  fi
  echo "Удаляем ${tag}"
  gh release delete "$tag" --yes 2>/dev/null || true
  git push origin --delete "refs/tags/${tag}" 2>/dev/null || true
done
