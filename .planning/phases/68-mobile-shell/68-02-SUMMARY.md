---
phase: 68-mobile-shell
plan: "02"
subsystem: ui
tags: [svelte5, a11y, aria, focus-trap, inert, mobile]

requires:
  - phase: 68-mobile-shell
    plan: "01"
    provides: "body.drawer-locked CSS contract (scrollY-preserving scroll-lock class) и .burger-btn/.sidebar-overlay:focus-visible accent-кольца"
provides:
  - "isMobile $state (matchMedia '(max-width: 768px)') + change-listener, drawerIsModal $derived — единый гейт всей модальной a11y-логики drawer"
  - "getDrawerFocusables() + handleDrawerKeydown() — Escape закрывает drawer, Tab-trap wrap-around внутри .sidebar, зеркалит паттерн Modal.svelte"
  - "$effect focus save/restore: фокус переносится в .sidebar при открытии, восстанавливается на burger-кнопку при закрытии"
  - "role=\"dialog\"/aria-labelledby=\"mobile-header-title\"/tabindex=\"-1\" на .sidebar, aria-modal гейтится drawerIsModal"
  - "inert={drawerIsModal} на .main-content — фон недоступен клавиатуре/AT только пока drawer модально открыт"
  - "$effect scrollY-preserving body scroll-lock: тоггл body.drawer-locked + сохранение/восстановление window.scrollY"

affects: [68-03-mobile-shell]

tech-stack:
  added: []
  patterns:
    - "drawerIsModal = isMobile && $isSidebarOpen — единая точка гейтирования модальных a11y-эффектов (focus-trap, inert, aria-modal, scroll-lock), защищает desktop (>768px) от регрессии"

key-files:
  created: []
  modified:
    - "frontend/src/Dashboard.svelte"

key-decisions:
  - "role=\"dialog\" и aria-labelledby оставлены статичными на .sidebar (как явно требуют <action>/<acceptance_criteria>/<verify> плана), но всё поведенческое (aria-modal, inert, focus-trap keydown, body-lock) строго гейтится drawerIsModal — так десктопный сайдбар остаётся ARIA-регионом с именем, но не модальным диалогом по факту поведения"
  - "handleDrawerKeydown дополнительно гейтирован ранним return при !drawerIsModal (не было явно прописано в <action>, но требуется prohibition #2 и threat T-68-02-E) — без этого Escape/Tab внутри постоянно видимого desktop-сайдбара ошибочно закрывал бы drawer и запирал фокус"
  - "tabindex=\"-1\" добавлен на .sidebar (не было в плане explicitly, но требуется правилом a11y_interactive_supports_focus для role=dialog и нужен для программного sidebarEl.focus() fallback), зеркалит modalElement в Modal.svelte"

requirements-completed: [REQ-1]

coverage:
  - id: D1
    description: "Гейт drawerIsModal (isMobile && $isSidebarOpen) с matchMedia listener, единая точка включения всей модальной a11y-логики drawer"
    requirement: "REQ-1"
    verification:
      - kind: unit
        ref: "grep -q 'drawerIsModal' src/Dashboard.svelte && grep -q \"matchMedia('(max-width: 768px)')\" src/Dashboard.svelte"
        status: pass
    human_judgment: false
  - id: D2
    description: "Escape закрывает мобильный drawer, Tab заперт внутри .sidebar (wrap-around), фокус сохраняется/восстанавливается на burger-кнопку — гейтировано drawerIsModal"
    requirement: "REQ-1"
    verification:
      - kind: unit
        ref: "grep -q 'handleDrawerKeydown' src/Dashboard.svelte && grep -q 'previouslyFocusedElement' src/Dashboard.svelte"
        status: pass
      - kind: manual_procedural
        ref: "Поведенческая проверка (burger open → фокус внутрь, Escape → фокус на burger) выполняется Playwright mobile-смоуком в плане 68-03"
        status: unknown
    human_judgment: true
    rationale: "Реальное клавиатурное поведение (фокус-трансфер, wrap-around) требует рендеринга в браузере/viewport 393px — не проверяется статическим grep этого плана; покрывается смоук-тестом 68-03"
  - id: D3
    description: "role=dialog/aria-labelledby на .sidebar, aria-modal и inert на .main-content гейтированы drawerIsModal, scrollY-preserving body-lock через body.drawer-locked"
    requirement: "REQ-1"
    verification:
      - kind: unit
        ref: "grep -q 'role=\"dialog\"' src/Dashboard.svelte && grep -q 'inert={drawerIsModal}' src/Dashboard.svelte && grep -q \"classList.add('drawer-locked')\" src/Dashboard.svelte"
        status: pass
      - kind: other
        ref: "npm run lint (svelte-check + eslint), npx vite build"
        status: pass
    human_judgment: false
  - id: D4
    description: "Desktop (>768px) не регрессирует: focus-trap/inert/aria-modal/body-lock неактивны, Ctrl/Cmd+B collapse не сломан"
    requirement: "REQ-1"
    verification:
      - kind: manual_procedural
        ref: "Визуальная/клавиатурная проверка desktop-режима после деплоя на роутер (выполняется в терминальном плане 68-03 согласно CLAUDE.md)"
        status: unknown
    human_judgment: true
    rationale: "Регрессия desktop-раскладки/фокуса проверяется реальным взаимодействием в браузере на роутере; локально запуск служб запрещён правилами CLAUDE.md, поэтому финальная сквозная проверка отнесена к терминальному плану фазы"

duration: 20min
completed: 2026-07-18
status: complete
---

# Phase 68 Plan 02: Мобильный drawer как доступный модальный диалог Summary

**Мобильный off-canvas drawer закалён до доступного модального диалога: единый гейт `drawerIsModal`, Escape-to-close, Tab focus-trap с восстановлением фокуса на burger, `role=dialog`/`aria-modal`/`inert` и scrollY-preserving body-lock, зеркалящие паттерн Modal.svelte**

## Performance

- **Duration:** 20 min
- **Started:** 2026-07-18T15:48:00Z
- **Completed:** 2026-07-18T16:08:27Z
- **Tasks:** 2
- **Files modified:** 1

## Accomplishments

- Введён единый гейт `drawerIsModal = isMobile && $isSidebarOpen` (matchMedia `(max-width: 768px)` с change-listener) — вся модальная a11y-логика активируется только когда drawer реально модален.
- Реализован focus-trap/restore, зеркалящий `Modal.svelte`: `getDrawerFocusables()`, `handleDrawerKeydown()` (Escape → `closeSidebar()`, Tab → wrap-around trap), `$effect` сохранения/восстановления фокуса на burger-кнопку.
- Добавлена ARIA-семантика на `.sidebar` (`role="dialog"`, `aria-labelledby="mobile-header-title"`, `aria-modal` гейтирован `drawerIsModal`) и `inert={drawerIsModal}` на `.main-content`.
- Реализован scrollY-preserving body scroll-lock: `$effect`, тогглящий `body.drawer-locked` (CSS-контракт из плана 68-01) с сохранением/восстановлением `window.scrollY`.
- Вся логика реактивна на `drawerIsModal` — авто-снимается при закрытии drawer и при авто-закрытии через nav-item (`isSidebarOpen.set(false)`), без застрявшего состояния.

## Task Commits

Each task was committed atomically:

1. **Task 1: Гейт drawerIsModal + focus-trap/restore + Escape** - `949bccb` (feat)
2. **Task 2: ARIA-семантика, inert фона и scrollY-preserving body-lock** - `7448f0e` (feat)

**Auto-fix:** `fd1c977` (fix) — гейтирование `handleDrawerKeydown` через `drawerIsModal`, обнаружено при пост-имплементационной проверке desktop-регрессии.

_Note: третий коммит — auto-fix (Rule 1), не отдельная задача плана._

## Files Created/Modified

- `frontend/src/Dashboard.svelte` — гейт `drawerIsModal`, focus-trap/restore, drawer-scoped keydown, ARIA-атрибуты, `inert`-биндинг, body-lock `$effect`

## Decisions Made

- `role="dialog"`/`aria-labelledby` оставлены статичными на `.sidebar` (не гейтированы `drawerIsModal`), т.к. это явно и многократно предписано в `<action>`, `<acceptance_criteria>` и `<verify>` (статический grep `'role="dialog"'`) плана — переход на вычисляемый `role={drawerIsModal ? 'dialog' : undefined}` сломал бы собственную автоматическую верификацию плана. Реальное поведенческое модальное ограничение (`aria-modal`, `inert`, focus-trap keydown, body-lock) строго гейтировано `drawerIsModal`, что удовлетворяет сути prohibition #2 (десктоп не должен вести себя как модаль).
- `tabindex="-1"` добавлен на `.sidebar` — устраняет lint-warning `a11y_interactive_supports_focus` (элемент с `role="dialog"` обязан иметь `tabindex`) и обеспечивает программный `sidebarEl.focus()` fallback, зеркаля `modalElement` в `Modal.svelte`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Гейтирование `handleDrawerKeydown` через `drawerIsModal`**
- **Found during:** Task 2 (пост-имплементационная сверка с prohibition #2 / threat T-68-02-E в самом плане)
- **Issue:** `handleDrawerKeydown` был привязан к `.sidebar` безусловно (Task 1). На desktop (>768px), где `.sidebar` — постоянно видимая non-modal панель, нажатие Escape внутри неё вызвало бы `closeSidebar()`, а Tab — попал бы в wrap-around trap среди фокусируемых элементов сайдбара, ломая обычный desktop Tab-порядок. Это прямое нарушение must_haves prohibition #2 ("MUST NOT применять модальные семантики drawer... на desktop-ширинах >768px") и threat T-68-02-E из threat_model плана.
- **Fix:** Добавлен ранний `return` в начале `handleDrawerKeydown`, если `!drawerIsModal` — Escape/Tab внутри сайдбара становятся no-op вне модального режима.
- **Files modified:** `frontend/src/Dashboard.svelte`
- **Verification:** `npm run lint` (0 ошибок), `npx vite build` успешен; логика проверена чтением кода — Tab/Escape внутри `.sidebar` теперь функциональны только при `isMobile && $isSidebarOpen`
- **Committed in:** `fd1c977`

**2. [Rule 1 - Bug] `tabindex="-1"` на `.sidebar`**
- **Found during:** Task 2 (`npm run lint` после добавления `role="dialog"`)
- **Issue:** eslint/svelte-check выдал `a11y_interactive_supports_focus`: "Elements with the 'dialog' interactive role must have a tabindex value" — новый a11y-warning, введённый добавлением `role="dialog"`.
- **Fix:** Добавлен `tabindex="-1"` на `.sidebar` (как у `modalElement` в `Modal.svelte`), устраняющий warning и позволяющий программный fallback-фокус.
- **Files modified:** `frontend/src/Dashboard.svelte`
- **Verification:** `npm run lint` — 0 ошибок, 0 новых warnings на `.sidebar`
- **Committed in:** `7448f0e` (часть Task 2 commit)

---

**Total deviations:** 2 auto-fixed (2 bug — Rule 1)
**Impact on plan:** Оба фикса необходимы для корректности (desktop keyboard regression) и чистоты lint. Скоуп не расширен — оба фикса внутри `frontend/src/Dashboard.svelte`, как и предписано планом.

## Issues Encountered

- `.planning/` каталог отсутствовал в worktree при старте (гитигнорится, `git worktree add` не копирует untracked/ignored файлы) — скопирован вручную из основного репозитория перед чтением плана.
- `frontend/node_modules` отсутствовал в worktree (гитигнорится, symlink на директорию не матчится gitignore-паттерном с trailing slash) — создан симлинк на `node_modules` основного репозитория (идентичный `package-lock.json`) для запуска `npm run lint`/`vite build`. Симлинк не закоммичен (файлы стейджились поимённо, `git add -A` не использовался).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Геометрический фикс (68-01) и a11y-закалка drawer (68-02, этот план) готовы к слиянию волны 1.
- Поведенческая верификация (Playwright mobile-смоук: burger→focus-in, Escape→focus-restore, scroll-lock/unlock, desktop keyboard regression) и финальный деплой на роутер — задача терминального плана 68-03, как прописано в `<verification>` этого плана.
- Блокеров нет.

---
*Phase: 68-mobile-shell*
*Completed: 2026-07-18*

## Self-Check: PASSED

- FOUND: frontend/src/Dashboard.svelte
- FOUND: .planning/phases/68-mobile-shell/68-02-SUMMARY.md
- FOUND commits: 949bccb, 7448f0e, fd1c977, 4266be1
