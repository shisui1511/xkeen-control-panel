#!/usr/bin/env python3
"""
Release notes generator for xkeen-control-panel.

Reads environment variables set by the GitHub Actions workflow:
  MODE          - "beta" (pre-release) or "stable"
  SOAK_HOURS    - hold time of an X.Y.0 pre-release before promotion (beta only, shown in the banner);
                  "milestone" - held until the milestone closes
  GITHUB_REPOSITORY, GITHUB_SHA, GITHUB_ACTOR
  VERSION       - e.g. "v0.12.0" or "v0.13.0-beta.1"
  RELEASE_MARKER - xcp-release block line from `auto_release.sh marker` (urgent, hold)
  GITHUB_ENV    - path to GitHub Actions env file

Writes /tmp/release-notes.md with structured, professional release notes. The first line is the
xcp-release block the panels read (contract: .github/scripts/auto_release.sh); a missing or
malformed RELEASE_MARKER is written as {"urgent":false,"hold":true}, so a fresh release never goes
to auto-install on an unread stop. For beta and stable the notes then open with the pre-release banner (beta only: X.Y.0 stable after the hold and a green stand
smoke, X.Y.Z with Z>0 right after the smoke), then the panel description and a "## Что вошло"
list (titles of merged PRs of the prev..tag range that contain at least two user-facing feat/fix/
perf/refactor/revert lines, dependency bumps skipped; a PR with a single such line is not repeated
by its title; omitted when there are none). The architecture table is
followed by a note that MIPS is experimental.

The range: a stable release lists everything since the previous stable (the "Основной" channel
never sees pre-releases); a pre-release lists only what is new since the previous published
release of any kind (the panel shows every release between the installed and the new version in
a row, so cumulative notes would repeat), and the footer links both that diff and the full one
since the last stable.

Only what the user of the panel can notice gets into the lists: test/ci/chore/docs/build commits,
merge and "wave" commits, commits of the developer tooling scopes (DEV_SCOPES) and dependency
bumps are skipped, review codes like "WR-01" or "G-147-3" are cut from the text, repeats are
dropped, a section longer than MAX_SECTION_ITEMS is cut with a pointer to the full diff.

`gen_notes.py VERSION MODE` (arguments instead of env, GITHUB_REPOSITORY still from env) prints
the notes for an existing tag to stdout without the xcp-release block and without touching
$GITHUB_ENV: the release-notes workflow regenerates published releases with it.
Otherwise appends PREV_TAG and SHORT_SHA to $GITHUB_ENV.
"""

import os
import re
import subprocess
import sys


def git(*args):
    result = subprocess.run(["git"] + list(args), capture_output=True, text=True)
    return result.stdout.strip()


RELEASE_TAG = re.compile(r"^v(\d+)\.(\d+)\.(\d+)(?:-(beta|rc)\.(\d+))?$")


def version_key(tag):
    """Ключ сортировки тега релиза: стабильная версия старше своих beta/rc; не релиз — None."""
    m = RELEASE_TAG.match(tag)
    if not m:
        return None
    major, minor, patch, kind, num = m.groups()
    pre = (1, 0) if kind is None else (0, int(num))
    return (int(major), int(minor), int(patch)) + pre


def release_tags():
    """Теги опубликованных видов релизов (stable, -beta.N, старые -rc.N) от старых к новым.
    Сборки -alpha.N релизами не публикуются и в расчёт не идут."""
    raw = git("tag", "-l")
    tags = [t for t in raw.split("\n") if t and version_key(t)]
    return sorted(tags, key=version_key)


def get_stable_tags():
    return [t for t in release_tags() if re.match(r"^v\d+\.\d+\.\d+$", t)]


def previous_release(version):
    """Ближайший более старый тег stable/beta/rc: база списка изменений пре-релиза."""
    key = version_key(version)
    older = [t for t in release_tags() if key is None or version_key(t) < key]
    return older[-1] if older else ""


# Области инструментов разработки: стенд, уборка, e2e, CI, хуки. В панели и установщике их не видно
DEV_SCOPES = {
    "router", "router-test", "rt", "tidy", "e2e", "test", "tests", "ci", "hooks", "build", "make",
    "deploy", "deps", "deps-dev", "dev", "lint", "gsd", "planning",
}
# Типы, которые пользователю показываются, и раздел для каждого
SECTION_OF = {
    "feat": "feat", "fix": "fix", "security": "fix",
    "perf": "other", "refactor": "other", "revert": "other", "style": "other",
}
CONVENTIONAL = re.compile(r"^([a-z]+)(?:\(([^)]*)\))?!?:\s*")
# Служебные коммиты слияний и волн агентов: «chore: merge executor worktree (…)», «слияние main …»
SERVICE_SUBJECT = re.compile(
    r"^(merge\b|слияние\b|волна\s+\d)|worktree-agent-", re.IGNORECASE
)
# Коды замечаний ревью в начале текста: «WR-01», «IN-05», «CR-02», «G-147-3», «T-147-19»,
# «WR-01/WR-02», «D-12»
REVIEW_CODES = re.compile(r"^(?:(?:WR|IN|CR|G|T|D)-\d+(?:[-.]\d+)*[/,]?\s*)+")
# Строк в одном разделе не больше: тело релиза GitHub ограничено 125 000 символами,
# а панель скачивает тела 30 последних релизов
MAX_SECTION_ITEMS = 120


def entry_text(subject):
    """Текст строки списка: без префикса Conventional Commits и кодов ревью, с заглавной буквы.
    Пустая строка — коммит в заметки не идёт."""
    m = CONVENTIONAL.match(subject)
    ctype, scope = (m.group(1), (m.group(2) or "").strip().lower()) if m else ("", "")
    if m and (ctype not in SECTION_OF or scope in DEV_SCOPES):
        return "", ""
    text = subject[m.end():] if m else subject
    if SERVICE_SUBJECT.search(text) or DEPS_TITLE.match(text):
        return "", ""
    text = REVIEW_CODES.sub("", text).strip()
    if not text:
        return "", ""
    return SECTION_OF.get(ctype, "other"), capitalize(text)


def capitalize(text):
    """Первая буква заглавная, если первое слово — обычное слово, а не имя вроде jq или xkeen."""
    word = text.split(" ", 1)[0]
    if word.isalpha() and word.islower() and re.match(r"[а-яё]", word):
        return text[0].upper() + text[1:]
    return text


def parse_commits(range_spec):
    """Return grouped commits: (feats, fixes, others)."""
    repo_url = f"https://github.com/{os.environ['GITHUB_REPOSITORY']}"
    try:
        raw = git("log", "--pretty=format:%s\x1f%h\x1f%H", range_spec)
    except Exception:
        return [], [], []

    groups = {"feat": [], "fix": [], "other": []}
    seen = set()
    for line in raw.split("\n"):
        if not line or "\x1f" not in line:
            continue
        parts = line.split("\x1f", 2)
        if len(parts) < 3:
            continue
        subject, short_h, full_h = parts
        if subject.startswith("Merge "):
            continue
        section, clean = entry_text(subject)
        if not clean or clean.lower() in seen:
            continue
        seen.add(clean.lower())
        link = f"[`{short_h}`]({repo_url}/commit/{full_h})"
        groups[section].append(f"- {clean} {link}")

    return groups["feat"], groups["fix"], groups["other"]


def capped(entries, more_url):
    """Раздел не длиннее MAX_SECTION_ITEMS строк, остаток — ссылкой на полный список."""
    if len(entries) <= MAX_SECTION_ITEMS:
        return entries
    rest = len(entries) - MAX_SECTION_ITEMS
    # Простым текстом: панель (frontend/src/lib/releaseNotes.ts) разметку ссылок в строках не разбирает
    tail = f"- …и ещё {rest} — полный список по ссылке внизу" if more_url else f"- …и ещё {rest}"
    return entries[:MAX_SECTION_ITEMS] + [tail]


def build_changelog(feats, fixes, others, more_url=""):
    sections = []
    if feats:
        sections.append("### 🚀 Новые возможности\n\n" + "\n".join(capped(feats, more_url)))
    if fixes:
        sections.append("### 🐛 Исправления\n\n" + "\n".join(capped(fixes, more_url)))
    if others:
        sections.append("### ⚙️ Прочее\n\n" + "\n".join(capped(others, more_url)))
    return "\n\n".join(sections) if sections else "_Список изменений недоступен._"


# Коммиты, ради которых выходит релиз (то же выражение, что WORTHY в auto_release.sh)
WORTHY = re.compile(
    r"^(feat|fix|perf|refactor|revert)(\([^)]*\))?!?:|^[a-z]+(\([^)]*\))?!:|^BREAKING[ -]CHANGE",
    re.MULTILINE,
)


def clean_title(title):
    """Заголовок PR без префикса фазы, префикса Conventional Commits и служебных хвостов в скобках:
    «(фаза N)», «(слой N)», «(слой 3, отложен из 145)» и т.п., сколько бы их ни было подряд."""
    title = re.sub(r"^(Phase|Фаза)\s+[\d.]+:\s*", "", title.strip())
    title = re.sub(r"^[a-z]+(\([^)]+\))?!?:\s*", "", title)
    tail = re.compile(r"\s*\((?:фаза|phase|слой|layer)\s*[\d.]+[^)]*\)\s*$", re.IGNORECASE)
    prev = None
    while prev != title:
        prev = title
        title = tail.sub("", title)
    return title.strip()


# Обновления зависимостей (заголовки dependabot на английском) пользователю ничего не говорят
DEPS_TITLE = re.compile(r"^(bump|update|upgrade)\b", re.IGNORECASE)


def included_list(range_spec):
    """Заголовки слитых PR диапазона, где есть достойные коммиты (от старых к новым).

    Пустой список, если таких PR нет или история недоступна (неглубокий клон).
    """
    merges = git("log", "--merges", "--first-parent", "--format=%H", range_spec).split("\n")
    titles = []
    for merge in reversed([m for m in merges if m]):
        body = git("log", "-1", "--format=%b", merge)
        title = next((line for line in body.split("\n") if line.strip()), "")
        if not title:
            continue
        inner = git("log", "--no-merges", "--format=%s", f"{merge}^1..{merge}^2").split("\n")
        shown = {entry_text(s)[1].lower() for s in inner if s and WORTHY.search(s) and entry_text(s)[1]}
        # Одна строка в списках ниже уже говорит то же самое: заголовок PR её только повторил бы
        if len(shown) < 2:
            continue
        title = capitalize(REVIEW_CODES.sub("", clean_title(title)).strip())
        if title and not DEPS_TITLE.match(title) and not SERVICE_SUBJECT.search(title) \
                and title not in titles:
            titles.append(title)
    return titles


def arch_table(version):
    rows = [
        f"| **ARM64** | KN-2710, KN-1811, KN-1012 и др. | `xcp_{version}_arm64` |",
        f"| **MIPSLE** | KN-1010, KN-1810, KN-1910 | `xcp_{version}_mipsle` |",
        f"| **MIPS** | KN-2510, KN-2410, KN-2010 | `xcp_{version}_mips` |",
    ]
    return (
        "| Архитектура | Модели роутеров | Файл |\n|---|---|---|\n"
        + "\n".join(rows)
        + "\n\n> **MIPS** — экспериментально, на железе не проверяется."
    )


def stable_notes(version, prev_tag, banner="", stable_tag=""):
    """prev_tag — база списка изменений; stable_tag (только у пре-релиза) — последний stable
    для второй ссылки «все изменения», если база списка — другой пре-релиз."""
    repo = os.environ["GITHUB_REPOSITORY"]
    repo_url = f"https://github.com/{repo}"

    compare_url = ""
    if prev_tag and prev_tag != version:
        range_spec = f"{prev_tag}..{version}"
        compare_url = f"{repo_url}/compare/{prev_tag}...{version}"
        if stable_tag and stable_tag != prev_tag:
            compare_line = (
                f"**[Изменения с прошлого пре-релиза: {prev_tag} → {version}]({compare_url})**"
                f" · [все с последнего стабильного: {stable_tag} → {version}]"
                f"({repo_url}/compare/{stable_tag}...{version})"
            )
        else:
            compare_line = f"**[Все изменения: {prev_tag} → {version}]({compare_url})**"
    else:
        range_spec = "HEAD~30..HEAD"
        compare_line = f"**[История релизов]({repo_url}/releases)**"

    feats, fixes, others = parse_commits(range_spec)
    changelog = build_changelog(feats, fixes, others, compare_url)

    included = included_list(range_spec)
    head = banner.rstrip("\n") + "\n\n" if banner else ""
    head += "Веб-панель управления XKeen для роутеров Keenetic/Netcraze — единый бинарник без зависимостей.\n\n"
    if included:
        head += "## Что вошло\n\n" + "\n".join(f"- {t}" for t in included) + "\n\n"

    return head + f"""\
{changelog}

---

## 📥 Установка и обновление

{arch_table(version)}

**Быстрая установка / обновление:**
```sh
curl -Ls https://raw.githubusercontent.com/{repo}/main/scripts/setup.sh | sh
```

**Ручная установка с проверкой SHA256 (пример для ARM64):**
```sh
cd /tmp
curl -fLO "https://github.com/{repo}/releases/download/{version}/xcp_{version}_arm64"
curl -fLO "https://github.com/{repo}/releases/download/{version}/xcp_{version}_arm64.sha256"
sha256sum -c xcp_{version}_arm64.sha256 && \\
  mv xcp_{version}_arm64 /opt/sbin/xcp && chmod +x /opt/sbin/xcp && \\
  /opt/etc/init.d/S99xcp restart
```

---

{compare_line}
"""


def beta_notes(version, prev_tag, stable_tag=""):
    repo_url = f"https://github.com/{os.environ['GITHUB_REPOSITORY']}"
    base = version.split("-")[0]
    soak = os.environ.get("SOAK_HOURS", "24")
    patch = base.split(".")[-1] != "0"
    if patch:
        banner = (
            f"> 🧪 **Пре-релиз исправлений {base}** — доступен в канале обновлений «Предварительный».\n"
            f"> Станет стабильным релизом **{base}** сразу после зелёного смоука на стенде.\n"
        )
    elif soak == "milestone":
        banner = (
            f"> 🧪 **Пре-релиз {base}** — доступен в канале обновлений «Предварительный».\n"
            f"> Войдёт в стабильный релиз при закрытии текущей вехи разработки, после полного "
            f"прогона на стенде.\n"
        )
    else:
        banner = (
            f"> 🧪 **Пре-релиз {base}** — доступен в канале обновлений «Предварительный».\n"
            f"> Станет стабильным релизом **{base}** после выдержки {soak} ч и зелёного смоука "
            f"на стенде; выдержку сбрасывает только следующий пре-релиз этой же версии.\n"
        )
    banner += (
        f"> Нашли проблему — [сообщите о регрессии]"
        f"({repo_url}/issues/new?template=regression.yml).\n"
    )
    return stable_notes(version, prev_tag, banner, stable_tag)


# Блок xcp-release первой строки тела: формат пишет auto_release.sh (marker и finalize)
MARKER_RE = re.compile(r'^<!-- xcp-release: \{"urgent":(true|false),"hold":(true|false)\} -->$')
MARKER_FALLBACK = '<!-- xcp-release: {"urgent":false,"hold":true} -->'


def release_marker():
    """Строка блока из RELEASE_MARKER; нет или битая — hold:true (стоп не прочитан)."""
    marker = os.environ.get("RELEASE_MARKER", "").strip()
    if MARKER_RE.match(marker):
        return marker
    print("::warning::блок xcp-release не получен, в заметки записан hold:true")
    return MARKER_FALLBACK


def notes_for(version, mode):
    """(заметки без блока xcp-release, база списка изменений)."""
    stable_tags = get_stable_tags()
    base_version = version.split("-")[0]
    last_stable = next((t for t in reversed(stable_tags)
                        if t != base_version and version_key(t) < version_key(version)), "")
    if mode == "beta":
        prev_tag = previous_release(version) or last_stable
        return beta_notes(version, prev_tag, last_stable), prev_tag
    # For stable: prev is the stable tag before current version
    return stable_notes(version, last_stable), last_stable


def main():
    if len(sys.argv) == 3:
        notes, _ = notes_for(sys.argv[1], sys.argv[2])
        sys.stdout.write(notes)
        return

    mode = os.environ.get("MODE", "stable")
    version = os.environ["VERSION"]
    full_sha = os.environ.get("GITHUB_SHA", "")
    short_sha = full_sha[:7]
    github_env = os.environ.get("GITHUB_ENV", "")

    notes, prev_tag = notes_for(version, mode)
    notes = release_marker() + "\n\n" + notes

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
