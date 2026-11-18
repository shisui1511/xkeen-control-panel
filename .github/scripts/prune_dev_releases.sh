#!/usr/bin/env bash
# Удаляет pre-release новой эпохи вместе с тегами.
# Использование: prune_dev_releases.sh [VERSION]
#   без аргумента — удалить все rolling dev-релизы (vX.Y.Z-dev), пре-релизы не трогаются;
#   с VERSION     — при stable vX.Y.Z удаляются dev-релизы и пре-релизы (vX.Y.Z-<суффикс>.N,
#                   любой суффикс из строчных букв: rc, beta…) с базой не выше X.Y.Z;
#                   пре-релизы более новых баз остаются — они могут стать stable позже
#                   (выдержанный пре-релиз).
source "$(dirname "${BASH_SOURCE[0]}")/auto_release.sh"

init_release_source
require_release_access
collect_releases

limit="${1:-}"
if [ -n "$limit" ]; then
  if ! [[ "$limit" =~ $STABLE_RE ]] || ! grep -qxF -- "$limit" <<<"$PUBLISHED_TAGS"; then
    logerr "для ротации указан неопубликованный или неподтверждённый stable"
    exit 1
  fi
  limit_sha=$(git rev-parse --verify "refs/tags/$limit^{commit}")
  require_source_tag "$limit" "$limit_sha"
  [ "$(release_tag_sha "$limit")" = "$limit_sha" ] || exit 1
fi

# Учитываем неопубликованные теги, например оставшиеся после прерванной сборки.
source_tags=$(git ls-remote --tags --refs origin)
release_tags=$(release_git ls-remote --tags --refs "$RELEASE_URL")
pattern="^v[0-9]+\\.[0-9]+\\.[0-9]+-(dev${limit:+|[a-z]+\\.[0-9]+})\$"
tags=$(
  { printf '%s\n' "$source_tags" "$release_tags" | awk -F 'refs/tags/' 'NF == 2 { print $2 }'
    printf '%s\n' "$RELEASED"; } |
    { grep -E -- "$pattern" || [ "$?" -eq 1 ]; } | sort -u
)

for tag in $tags; do
  base="${tag%%-*}"
  if [ -n "$limit" ] && version_gt "$base" "$limit"; then
    continue
  fi
  ref="refs/tags/$tag"
  source_oid=$(awk -v ref="$ref" '$2 == ref { print $1 }' <<<"$source_tags")
  if [ -z "$source_oid" ] || [ "$(git rev-parse -q --verify "$ref")" != "$source_oid" ] || ! release_commit "$source_oid"; then
    continue
  fi
  release_oid=$(awk -v ref="$ref" '$2 == ref { print $1 }' <<<"$release_tags")
  if [ -n "$release_oid" ] && [ "$release_oid" != "$source_oid" ]; then
    logerr "$tag: тег в источнике выпусков отличается от исходного, ротация остановлена"
    exit 1
  fi
  if grep -qxF -- "$tag" <<<"$RELEASED"; then
    if [ -z "$release_oid" ]; then
      logerr "$tag: у релиза нет подтверждённого тега, ротация остановлена"
      exit 1
    fi
    GH_TOKEN="$RELEASE_TOKEN" gh release delete "$tag" --repo "$RELEASE_REPO" --yes
  fi
  if [ -n "$release_oid" ]; then
    release_git push --no-follow-tags --force-with-lease="$ref:$release_oid" "$RELEASE_URL" ":$ref"
  fi
  git push --no-follow-tags --force-with-lease="$ref:$source_oid" origin ":$ref"
done
