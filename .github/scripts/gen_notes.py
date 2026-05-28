#!/usr/bin/env python3
"""
Release notes generator for xkeen-control-panel.

Reads environment variables set by the GitHub Actions workflow:
  MODE          - "prerelease" or "stable"
  GITHUB_REPOSITORY, GITHUB_SHA, GITHUB_ACTOR
  VERSION       - e.g. "v0.12.0" or "v0.13.0-dev"
  COMMIT_COUNT  - number of commits since last stable (pre-release only)
  GITHUB_ENV    - path to GitHub Actions env file

Writes /tmp/release-notes.md with structured, professional release notes.
Also appends PREV_TAG and SHORT_SHA to $GITHUB_ENV.
"""

import os
import re
import subprocess
import sys
from datetime import datetime, timezone


def git(*args):
    result = subprocess.run(["git"] + list(args), capture_output=True, text=True)
    return result.stdout.strip()


def get_stable_tags():
    raw = git("tag", "-l", "--sort=version:refname")
    return [t for t in raw.split("\n") if t and re.match(r"^v\d+\.\d+\.\d+$", t)]


def parse_commits(range_spec):
    """Return grouped commits: (feats, fixes, others)."""
    repo_url = f"https://github.com/{os.environ['GITHUB_REPOSITORY']}"
    try:
        raw = git("log", "--pretty=format:%s\x1f%h\x1f%H", range_spec)
    except Exception:
        return [], [], []

    feats, fixes, others = [], [], []
    for line in raw.split("\n"):
        if not line or "\x1f" not in line:
            continue
        parts = line.split("\x1f", 2)
        if len(parts) < 3:
            continue
        subject, short_h, full_h = parts
        if subject.startswith("Merge "):
            continue
        link = f"[`{short_h}`]({repo_url}/commit/{full_h})"
        # Strip conventional commit prefix: feat(scope): text → text
        clean = re.sub(r"^[a-z]+(\([^)]+\))?!?:\s*", "", subject)
        entry = f"- {clean} {link}"
        if re.match(r"^feat", subject):
            feats.append(entry)
        elif re.match(r"^fix|^security", subject):
            fixes.append(entry)
        else:
            others.append(entry)

    return feats, fixes, others


def build_changelog(feats, fixes, others):
    sections = []
    if feats:
        sections.append("### 🚀 Новые возможности\n\n" + "\n".join(feats))
    if fixes:
        sections.append("### 🐛 Исправления\n\n" + "\n".join(fixes))
    if others:
        sections.append("### ⚙️ Прочее\n\n" + "\n".join(others))
    return "\n\n".join(sections) if sections else "_Список изменений недоступен._"


ARCH_TABLE = """\
| Архитектура | Модели роутеров | Файл |
|---|---|---|
| **ARM64** | KN-2710, KN-1811, KN-1012 и др. | `xcp_{version}_{arch}` |
| **MIPSLE** | KN-1010, KN-1810, KN-1910 | `xcp_{version}_{arch}` |
| **MIPS** | KN-2510, KN-2410, KN-2010 | `xcp_{version}_{arch}` |\
"""


def arch_table(version):
    rows = [
        f"| **ARM64** | KN-2710, KN-1811, KN-1012 и др. | `xcp_{version}_arm64` |",
        f"| **MIPSLE** | KN-1010, KN-1810, KN-1910 | `xcp_{version}_mipsle` |",
        f"| **MIPS** | KN-2510, KN-2410, KN-2010 | `xcp_{version}_mips` |",
    ]
    return "| Архитектура | Модели роутеров | Файл |\n|---|---|---|\n" + "\n".join(rows)


def prerelease_notes(version, commit_count, pr_number, prev_tag, short_sha, full_sha):
    repo = os.environ["GITHUB_REPOSITORY"]
    repo_url = f"https://github.com/{repo}"
    date_str = datetime.now(timezone.utc).strftime("%Y-%m-%d")

    range_spec = f"{prev_tag}..HEAD" if prev_tag else "HEAD~30..HEAD"
    feats, fixes, others = parse_commits(range_spec)
    changelog = build_changelog(feats, fixes, others)

    stable_note = ""
    if prev_tag:
        stable_note = (
            f" Последний стабильный релиз: "
            f"[**{prev_tag}**]({repo_url}/releases/tag/{prev_tag})"
        )

    if pr_number:
        build_info = f"**PR:** [#{pr_number}]({repo_url}/pull/{pr_number})"
    else:
        build_info = f"**Сборка:** #{commit_count}"

    return f"""\
> ⚠️ **Нестабильная сборка** — Не рекомендуется для production-использования!
> {stable_note}

**Коммит:** [`{short_sha}`]({repo_url}/commit/{full_sha}) · {build_info} · **Дата:** {date_str}

---

{changelog}

---

## 📥 Скачать

> 📦 Бинарники сжаты [UPX](https://upx.github.io/) `--best --lzma` — размер ~60% от стабильного релиза.

{arch_table(version)}

**Верификация SHA256:**
```sh
sha256sum -c xcp_{version}_arm64.sha256
```
"""


def stable_notes(version, prev_tag):
    repo = os.environ["GITHUB_REPOSITORY"]
    repo_url = f"https://github.com/{repo}"

    if prev_tag and prev_tag != version:
        range_spec = f"{prev_tag}..{version}"
        compare_url = f"{repo_url}/compare/{prev_tag}...{version}"
        compare_line = f"**[Все изменения: {prev_tag} → {version}]({compare_url})**"
    else:
        range_spec = "HEAD~30..HEAD"
        compare_line = f"**[История релизов]({repo_url}/releases)**"

    feats, fixes, others = parse_commits(range_spec)
    changelog = build_changelog(feats, fixes, others)

    return f"""\
> Веб-панель управления XKeen для роутеров Keenetic/Netcraze — единый бинарник без зависимостей.

{changelog}

---

## 📥 Установка и обновление

{arch_table(version)}

**Быстрая установка / обновление:**
```sh
curl -Ls https://raw.githubusercontent.com/{repo}/main/scripts/setup.sh | sh
```

**Ручная установка (пример для ARM64):**
```sh
curl -fL -o /opt/sbin/xcp \\
  "https://github.com/{repo}/releases/download/{version}/xcp_{version}_arm64"
chmod +x /opt/sbin/xcp
```

**Верификация SHA256:**
```sh
sha256sum -c xcp_{version}_arm64.sha256
```

---

{compare_line}
"""


def main():
    mode = os.environ.get("MODE", "stable")
    version = os.environ["VERSION"]
    full_sha = os.environ.get("GITHUB_SHA", "")
    short_sha = full_sha[:7]
    commit_count = os.environ.get("COMMIT_COUNT", "?")
    pr_number = os.environ.get("PR_NUMBER", "")
    github_env = os.environ.get("GITHUB_ENV", "")

    stable_tags = get_stable_tags()

    if mode == "prerelease":
        prev_tag = stable_tags[-1] if stable_tags else ""
        notes = prerelease_notes(version, commit_count, pr_number, prev_tag, short_sha, full_sha)
    else:
        # For stable: prev is the tag before current version
        prev_tag = next((t for t in reversed(stable_tags) if t != version), "")
        notes = stable_notes(version, prev_tag)

    with open("/tmp/release-notes.md", "w") as f:
        f.write(notes)

    if github_env:
        with open(github_env, "a") as f:
            f.write(f"PREV_TAG={prev_tag}\n")
            f.write(f"SHORT_SHA={short_sha}\n")

    print(f"✓ Release notes written to /tmp/release-notes.md ({len(notes)} chars)")
    print(f"  mode={mode}, version={version}, prev_tag={prev_tag}")


if __name__ == "__main__":
    main()
