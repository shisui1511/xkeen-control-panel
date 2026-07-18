---
phase: 68-mobile-shell
plan: "03"
subsystem: testing
tags: [playwright, e2e, mobile, svelte, css]

requires:
  - phase: 68-mobile-shell
    provides: "68-01: .dashboard-layout column-flow fix (.mobile-header top strip); 68-02: burger-gated drawer a11y (Escape/focus-trap, drawer-locked body scroll)"
provides:
  - "Playwright-проект mobile (devices['Pixel 5'], 393px) в frontend/playwright.config.ts, строго скоупленный на frontend/tests/mobile-shell.spec.ts (D-06/D-07)"
  - "Смоук-спек mobile-shell.spec.ts: геометрия мобильного каркаса, drawer open/Escape-close (D-10), input font-size >=16px против iOS-zoom (D-09)"
affects: [69, 70, 71, 72, 73, 74, 75, 76]

tech-stack:
  added: []
  patterns:
    - "testMatch/testIgnore пара в playwright.config.ts как единственный механизм изоляции нового Playwright-проекта от существующих desktop-спеков"

key-files:
  created:
    - "frontend/tests/mobile-shell.spec.ts"
  modified:
    - "frontend/playwright.config.ts"

key-decisions:
  - "D-06 (locked): mobile-проект использует стандартный пресет devices['Pixel 5'] без кастомного viewport."
  - "D-07: mobile-проект скоуплен только на mobile-shell.spec.ts через testMatch, а chromium получил симметричный testIgnore на тот же файл — 24 desktop-спека физически не запускаются под мобильным вьюпортом."

patterns-established:
  - "Новый спек, проверяющий мобильную геометрию/поведение, добавляется плоско в frontend/tests/ и матчится отдельным Playwright-проектом через testMatch, а не через per-test viewport override в общем chromium-проекте."

requirements-completed: [REQ-1]

coverage:
  - id: D1
    description: "Playwright-проект mobile (Pixel 5, 393px) добавлен и строго скоуплен на mobile-shell.spec.ts; chromium игнорирует этот спек"
    requirement: "REQ-1"
    verification:
      - kind: e2e
        ref: "frontend/tests/mobile-shell.spec.ts — npx playwright test --list --project=mobile / --project=chromium"
        status: pass
    human_judgment: false
  - id: D2
    description: "Смоук-тест геометрии: .mobile-header — верхняя полоса во всю ширину (top/left ≈0, height<80px), .main-content full-width, нет горизонтального скролла"
    requirement: "REQ-1"
    verification:
      - kind: e2e
        ref: "frontend/tests/mobile-shell.spec.ts#geometry smoke: header is a full-width top strip, content is full-width, no horizontal scroll"
        status: pass
    human_judgment: false
  - id: D3
    description: "Смоук-тест drawer: burger открывает .sidebar-open, Escape закрывает (D-10)"
    verification:
      - kind: e2e
        ref: "frontend/tests/mobile-shell.spec.ts#drawer: burger opens sidebar, Escape closes it"
        status: pass
    human_judgment: false
  - id: D4
    description: "Смоук-тест input-zoom: #password computed font-size >=16px на мобильном вьюпорте (D-09)"
    verification:
      - kind: e2e
        ref: "frontend/tests/mobile-shell.spec.ts#input no iOS-zoom: #password computed font-size is >= 16px"
        status: pass
    human_judgment: false
  - id: D5
    description: "Backstop: полный прогон npx playwright test (оба проекта) остаётся зелёным — 24 desktop-спека не регрессировали от добавления mobile-проекта"
    verification:
      - kind: e2e
        ref: "npx playwright test (frontend) — 131 passed (128 chromium + 3 mobile)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Ручная визуальная проверка мобильного каркаса на роутере (390px, шапка/drawer/qa-grid-mini) после сборки и деплоя ARM64-бинарника"
    verification: []
    human_judgment: true
    rationale: "Деплой на роутер и визуальная проверка выполняются пользователем/оркестратором после мержа волны — worktree-исполнителю запрещено ssh/scp-деплоить и запускать службы локально (CLAUDE.md, parallel_execution override)"

duration: 25min
completed: 2026-07-18
status: complete
---

# Phase 68 Plan 03: Playwright mobile-shell coverage Summary

**Playwright-проект `mobile` на пресете Pixel 5 (393px), строго скоупленный testMatch/testIgnore-парой, и смоук-спек `mobile-shell.spec.ts`, проверяющий геометрию каркаса, drawer Escape-close и защиту от iOS-zoom**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-07-18T16:05:00Z
- **Completed:** 2026-07-18T16:29:00Z
- **Tasks:** 2
- **Files modified:** 2 (1 created, 1 modified)

## Accomplishments

- В `frontend/playwright.config.ts` добавлен второй проект `mobile` (`devices['Pixel 5']`, 393px), ограниченный `testMatch: '**/mobile-shell.spec.ts'` (D-06 locked).
- На проекте `chromium` добавлен симметричный `testIgnore: '**/mobile-shell.spec.ts'` — это единственный механизм, гарантирующий, что 24 существующих desktop-спека не запускаются под мобильным вьюпортом (D-07).
- Создан `frontend/tests/mobile-shell.spec.ts` с тремя смоук-тестами: геометрия каркаса (шапка — верхняя полоса, контент во всю ширину, нет горизонтального скролла), drawer (burger открывает, Escape закрывает — D-10), input-zoom (`#password` font-size ≥16px — D-09).
- Локально подтверждено: `npx playwright test --list --project=mobile` выдаёт ровно 3 теста из mobile-shell.spec.ts; `npx playwright test --list --project=chromium` НЕ включает mobile-shell.spec.ts (скоупинг работает).
- `npx playwright test --project=mobile` — 3/3 зелёных, код выхода 0.
- Backstop-прогон `npx playwright test` (оба проекта разом) — 131 тест пройден (128 chromium + 3 mobile), код выхода 0. Регрессий на существующих desktop-спеках нет.

## Task Commits

Each task was committed atomically:

1. **Task 1: Мобильный Playwright-проект Pixel 5 + скоупинг (D-06 locked, D-07)** - `13cb374` (feat)
2. **Task 2: Смоук-спек mobile-shell.spec.ts — геометрия + drawer (D-10) + input-zoom (D-09)** - `8f7be88` (test)

**Plan metadata:** (this commit)

## Files Created/Modified

- `frontend/playwright.config.ts` - добавлен проект `mobile` (Pixel 5) с `testMatch`, `testIgnore` на `chromium`
- `frontend/tests/mobile-shell.spec.ts` - новый смоук-спек: geometry / drawer+Escape / input-zoom

## Decisions Made

- Локально `frontend/node_modules` в worktree отсутствовал — временно создавался симлинк на `node_modules` основного репозитория только для запуска тестов; симлинк и артефакты (`playwright-report`, `test-results`) удалены перед коммитом/возвратом, worktree чист.
- Порядок верификации Task 1 скорректирован (без изменения содержания задачи): полная команда из плана (`npx playwright test --list --project=mobile | grep mobile-shell`) технически требует существования `mobile-shell.spec.ts` из Task 2, поэтому для Task 1 сначала прогнаны структурные grep-проверки конфигурации, а полная команда `--list` для обоих проектов и `--project=mobile` запущена после появления файла в Task 2 — обе прошли успешно, ничего в поведении Task 1 не изменилось.

## Deviations from Plan

None - plan executed exactly as written. Task 1's full automated `<verify>` command inherently depends on Task 2's artifact (see "Decisions Made" above) — this is a plan-authoring sequencing quirk, not a deviation in implementation; both tasks' acceptance criteria are fully satisfied.

## Issues Encountered

- Во время прогона тестов dev-сервер логировал `Unhandled error: TypeError: Cannot read properties of undefined (reading 'api_reachable')` в `Dashboard.svelte:831` — это pre-existing поведение при моке `/api/**` через `{ success: true, data: {} }` (тот же паттерн, что и в `basic.spec.ts`), не влияет на прохождение тестов (все assert'ы в mobile-shell.spec.ts таргетируют DOM/CSS-геометрию и классы, а не capabilities-панель). Вне скоупа плана 68-03 — не исправлялось (scope boundary).

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- REQ-1 полностью закрыт: layout-фикс (68-01) + a11y drawer (68-02) + автоматизированный регресс-гейт (68-03).
- **Терминальный шаг фазы 68 (обязателен per CLAUDE.md, выполняется оркестратором/пользователем после мержа волны, НЕ worktree-исполнителем):** сборка фронтенда, `make keenetic-arm64`, деплой через SSH-alias `router-shi`, ручная визуальная проверка мобильного каркаса на 390px на роутере (шапка/контент/qa-grid-mini/drawer burger+Escape+overlay-click).
- Блокеров для фазы 69+ нет.

---
*Phase: 68-mobile-shell*
*Completed: 2026-07-18*
