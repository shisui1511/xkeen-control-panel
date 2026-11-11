#!/usr/bin/env node
// Выбор проверок на роутерах по diff (make router-test SUITE=changed, D-25; области — 148.1-09).
//
// Что выбирается:
//   Области     файл продукта (Go, setup.sh, контракты, testdata) относят к областям по карте
//               scripts/router/areas.json (глобы paths). Правка файла области выбирает по
//               ИМЕНАМ все Go-тесты области (строка `// router-area:` в файле теста) и все спеки
//               области (D-12). Область с флагом shared (kernel, configlayer, server) добавляет
//               канарейки всех областей, Go и спеки (D-10). Подобласти (поле subareas, RTR-14,
//               D-12 фазы 148.1.1): файл, найденный глобом подобласти, выбирает только её tests
//               по именам — прямое затрагивание, ярус full разрешён — и канарейки shared-родителя;
//               тесты родителя целиком не выбираются, откат на пакет не нужен. Файл родителя вне
//               всех подобластей выбирает область целиком, как раньше (страж требует покрытия).
//               Области contract-* выбирают проверки
//               из поля tests. Изменённый роутерный тест (со строкой области или без неё)
//               выбирает тесты своего файла по именам и, по объявлениям (D-32), тесты TestRouter
//               пакета, использующие изменённые объявления файла (goDeclDiff, goDeclClosure);
//               файл без тестов TestRouter (помощники, обычный Test, Fuzz) в роутерном пакете —
//               только по объявлениям. Неразобранное (init, TestMain, переменная `_` или с вызовом
//               вне PURE_INIT_CALLS, пустой или точечный импорт, строка build или package,
//               раскладка не gofmt, нет базы) — пакет целиком с причиной. Продукт без области —
//               пакет целиком (откат, как раньше: пакет и пакеты, от которых он зависит,
//               `go list -deps`).
//   Go          записи RT_GO_SEQ `<каталог пакета>:<TestRouterИмя>`; пакеты — каталоги записей.
//               Роутерный пакет — каталог с `_test.go`, у которого первая строка `//go:build router`.
//   Playwright  роутерные спеки frontend/tests/router/*.spec.ts. Правка frontend/src — по графу
//               импортов (scripts/router/import-graph.mjs) обеих ревизий (база и head: перенос
//               или удаление импорта не теряет потребителей старой стороны): спек выбирается,
//               если открывает затронутый маршрут (`#/<маршрут>`, вкладка `#/config/<вкладка>`
//               или `// e2e-pages: …`; вход через корень `/` — маршрут dashboard только у спека
//               без других маршрутов, D-32). Обычный общий компонент добавляет pages с маршрутами
//               потребителей и читающую канарейку оболочки (RTR-13, D-09). Область карты у
//               файла frontend/src — только отметка RT_AREAS и объяснения: тесты области
//               целиком не выбираются. Раньше графа — отдельные основания со своими
//               объяснениями: локали (D-07) — по изменённым ключам обеих ревизий (литералы
//               t('…') и шаблоны `…${…}…`, комментарии не считаются) → файлы-потребители →
//               маршруты, ключ без потребителей выбор не расширяет (статика i18n, смоук);
//               стили, токены, тема и frontend/public/ (D-08) — pages без сужения и читающая
//               канарейка оболочки; каркас (D-09: index.html, App, main, Dashboard, tabFromHash,
//               PageHeader) — pages без сужения
//               и одна канарейка «правка → Применить»; хаб (D-10: модуль оболочки или
//               достижимый строго больше чем с половины страниц) — как каркас плюс спеки,
//               ссылающиеся на модуль напрямую.
//               `// e2e-pages: *` — спек обходит все страницы: при выборе только по маршрутам он
//               сужается до них через --grep; выбранный по иной причине (область, зависимости,
//               правка самого спека) идёт без сужения. core-switch.spec.ts не выбирается: его
//               запускает матрица ядер.
//
// Триггеры (RTR-02 в редакции D-21…D-25 фазы 148.1.1). Обязательный полный выбор — только три
// основания, каждое со строкой причин «обязательный полный выбор: <основание>» (её читает
// select-replay.mjs, D-29): механизм выбора (SELECTOR_SELF: этот файл, core-marks.cjs, run-plan.mjs — состав гейта,
// import-graph.mjs) — «механизм выбора (<файл>)»; формат карты (version, набор ключей верхнего
// уровня, ключей областей, подобластей или канареек; карта появилась) — «формат карты (<что>)»;
// признак shared области — «признак shared (<область>: <было> → <стало>)».
// Карта областей (areas.json) выбирается по смыслу: обе версии (база диффа, в режиме --files —
// HEAD; head — рабочее дерево или --head) разбираются areasFromText и сравниваются diffAreaMaps
// (core-marks.cjs). Изменённый шаблон paths — тесты затронутых областей по обеим картам
// (метки базы и head, поле tests; ярус full, если область не shared; канарейки, если shared),
// перенос — обе области, даже если набор файлов совпал (D-21); записи tests и подобластей —
// свои тесты, если они существуют; добавленная, удалённая или переименованная область — тесты по
// обеим картам (D-23); канарейка (D-22) — добавленная новая, замена — прежняя и новая, удалённая
// — прежняя, если тест есть; блок harness — RT_HARNESS=1; только title, пояснение канарейки или
// порядок — выбор не расширяется. Каждое изменение — строка «карта: <что> — Go n, спеков m»
// (D-25). Выбор по карте объединяется с выбором по продукту без повторов.
// Остановка (D-24, код EXIT_STOP = 3): карту базы или head нельзя разобрать или она не проходит
// validateAreas (в том числе канарейка null без reason), карта удалена, в режиме --files нет
// git, метки `// router-area:` существующих тестов ссылаются на область, которой нет в head
// (при --head — только удалённые диффом области). stdout пуст, причины — строки
// «стоп: <файл>: <причина>» в stderr (всегда, не только с --explain); полный выбор вместо
// остановки не подставляется. Обязательный полный выбор того же диффа перекрывает остановку —
// строка «карта: не сравнить (…) — перекрыто обязательным полным выбором».
// Конфигурации TypeScript фронтенда (TS_CONFIG: frontend/**/tsconfig*.json, jsconfig*.json и
// любые вершины их extends; D-26…D-28 фазы 148.1.1) — по конечным потребителям базы и head:
// корни тестов — поле tsconfig конфигов frontend/playwright*.config.ts, корни сборки —
// frontend/tsconfig.json и --tsconfig скриптов build, check, lint из package.json той же
// ревизии; потребители — корни, в чьей цепочке extends есть файл, по всем уровням и обеим
// ревизиям (удалённый — по базе, новый — по head, смена extends — обе цепочки). Только тесты —
// RT_HARNESS=1; есть сборка панели — RT_HARNESS=1 и все браузерные спеки; строка «конфигурация:
// <файл> — потребители: … — …». Широкая проверка сборочных конфигураций (FRONTEND_ALL:
// vite.config.ts, svelte.config.js; D-26) — все роутерные спеки. index.html (шаблон точки входа)
// и public/ (статика, копируется в сборку без обработки) — не конфигурации: проверяются обходом
// всех маршрутов pages и канарейкой — index.html как каркас (D-09), public/ как тема (D-08), D-32.
// Неразрешимая связь (extends на
// несуществующий файл, цикл наследования, неразбираемый JSON, ссылка на удалённую
// конфигурацию) или потребитель не определён ни на одной ревизии — остановка с кодом 3 по тому
// же контракту, что у карты: ни сужения, ни полного выбора.
// Обвязка стенда (HARNESS: прочие
// scripts/router/**, frontend/tests/router/lib/**, playwright.router.config.ts, global-setup/
// teardown, internal/routertest, цели стенда и VERSION в Makefile) — RT_HARNESS=1 без полного
// выбора. Правка .go internal/routertest ещё и выбирает по объявлениям (D-32) тесты TestRouter
// зависимых пакетов, которые используют изменённое: экспортируемое — через имя импорта
// (`routertest.<Имя>`), методы — через селектор `.<Имя>`; изменённый тип — через функции
// routertest, которые его возвращают. Неразобранное — прежний откат через go list -deps
// (зависимые пакеты целиком, у каждого строка «использует internal/routertest — правка не
// разобрана по объявлениям»). Зависимости (DEPS: package.json, package-lock.json, go.mod, go.sum) — патч/минор:
// pages.spec.ts без сужения и канарейки всех областей, в том числе патч и минор svelte, vite и
// @playwright/test (D-11 фазы 148.1.1); мажор (D-13) — смена мажора спецификации в package.json
// или версии svelte/vite/@playwright/test в lock — канарейки плюс все спеки (npm) или все
// Go-тесты (go.mod). Неизвестный файл — только смоук и предупреждение в причинах.
//
// Ярус full (D-14). Тест яруса full (метка Tier full у Go, @full у Playwright; core-marks.cjs,
// testFacts → tier) идёт в гейте, только если его область затронута напрямую: изменён файл
// продукта области по areas.json (область без флага shared), файл подобласти (тест в её tests)
// или сам файл теста. Через канарейку,
// зависимости, граф импортов фронтенда и общий код (shared) тесты full не выбираются. Playwright:
// RT_PW_INVERT='@full', пока ни один выбранный спек не выбран напрямую по области и не изменён
// сам (иначе тесты full выбранных спеков идут все, расширение). Откат на
// пакет целиком и полный выбор идут со всеми ярусами. Отсев Go — исключение из RT_GO_SEQ и из
// RT_GO_CORE_RUN; число отсеянных тестов — в причинах (--explain).
//
// Порядок (RTR-03): сначала тесты затронутых областей, затем остальные выбранные; внутри группы —
// по убыванию частоты падений из истории (history.mjs failFreq, окно 30 дней). Спеки затронутых
// областей — RT_SPECS_FIRST. Ошибка истории — без сортировки (причина в stderr).
//
// При сомнении выбор только расширяется (D-25, T-148.1-24): небезопасное имя, ошибка карты
// рабочей копии, фактов тестов или разбора, нет базы сравнения у зависимостей и Makefile —
// полный выбор или расширение (пакет целиком, мажор, обвязка). Исключения — правка карты, которую
// нельзя надёжно сравнить, и конфигурация TypeScript без определимого потребителя: остановка
// (код 3), не полный выбор.
//
// Вывод (stdout) — 13 строк для eval в sh в фиксированном порядке, значения в одинарных
// кавычках и только из безопасных символов (T-144.1-23, T-148.1-23); run.sh сверяет число и
// форму каждой строки:
//   RT_SELECT_ALL=0|1
//   RT_GO_PKGS='./internal/services ./internal/utils/xtables'
//   RT_SPECS='tests/router/pages.spec.ts'
//   RT_PW_GREP='^(?!.*page:(?!(?:logs|dashboard) ))'
//   RT_GO_CORE_PKGS='./internal/handlers ./internal/services'
//   RT_GO_CORE_RUN='^(TestRouterA|TestRouterB)$'
//   RT_SPECS_CORE='tests/router/pages.spec.ts'
//   RT_KERNEL_TOUCHED=0|1
//   RT_GO_SEQ='internal/handlers:TestRouterA internal/services:TestRouterB'
//   RT_SPECS_FIRST='tests/router/access-log.spec.ts'
//   RT_PW_INVERT=''
//   RT_HARNESS=0|1
//   RT_AREAS='access-log configlayer'
// Строки 5–7 — тесты выбора, зависящие от ядра (D-01, D-03, D-04); по ним run.sh решает,
// нужна ли вторая ветвь (второе ядро); при выборе по именам они берутся только из выбранных:
//   RT_GO_CORE_PKGS  выбранные пакеты, где core-marks.cjs нашёл размеченные тесты (core,
//                    known-core); при полном выборе — все такие пакеты
//   RT_GO_CORE_RUN   выражение -test.run по именам этих тестов; пусто, если размеченных нет
//   RT_SPECS_CORE    выбранные спеки, где `playwright test --list` с отбором @core (вместе
//                    с сужением RT_PW_GREP) находит тест; список не получен — все выбранные
//   RT_KERNEL_TOUCHED  1, если изменён код запуска или перезапуска ядра либо watchdog
//                    (KERNEL_PATHS, D-07) или выбор полный по любой причине; иначе 0
// Строки 9–13: RT_GO_SEQ — порядок и состав Go первой ветви по именам (пусто — пакеты целиком,
// откат); RT_SPECS_FIRST — спеки затронутых областей; RT_PW_INVERT — значение --grep-invert:
// '@full' (отсев яруса full), если ни один выбранный спек не выбран напрямую по области и не
// изменён сам, иначе пусто (расширение: тесты full выбранных спеков идут все), при полном выборе
// пусто; RT_HARNESS — изменена обвязка стенда; RT_AREAS — затронутые области.
// Сбой расчёта ядра не роняет выбор: списки расширяются (все пакеты, все спеки), не сужаются.
// Причины выбора — в stderr при --explain; последняя строка «вид выбора: …» (полный | обвязка |
// зависимости | области | смоук) читают run.sh и select-replay.mjs. Код 3 — выбор остановлен:
// stdout пуст, в stderr «выбор: остановлен …» и причины «стоп: …».
//
// Использование (из корня репозитория):
//   node scripts/router/select-changed.mjs [--base <ref>] [--head <ref>] [--explain]
//   node scripts/router/select-changed.mjs --files a,b,c [--explain]   # без git
// --hub-percent <0…100> — диагностика: другой порог хаба (по умолчанию HUB_PERCENT_DEFAULT) для
// сравнения вариантов в select-replay.mjs --hub-percent; штатный гейт его не задаёт.
// --head <ref> — сравнивать <база>..<head> без рабочего дерева (прогон по слитым PR,
// select-replay.mjs); карта областей, факты тестов и граф импортов при этом берутся из
// рабочей копии. Без --base база — merge-base origin/main и head (или HEAD).
//
// RT_PW_GREP: для названий вида `page:<маршрут> theme:… vp:…` оставляет только затронутые
// маршруты. Форма с отрицательным просмотром вперёд, а не `page:(a|b) `, чтобы тесты без
// префикса `page:` (другие сценарии в выбранных спеках) не отфильтровывались.

import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import path from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import coreMarks from './core-marks.cjs';

const {
  goMarks,
  analyzeGo,
  loadAreas,
  areasFromText,
  diffAreaMaps,
  subareasOf,
  matchGlob,
  testFacts,
  goTestFacts,
  specTestFacts,
  areaOfText,
  declarations,
  lex,
  AREAS_FILE
} = coreMarks;

// Изменённые файлы текущего выбора: goMarks читает только индекс, новые тесты он не видит
let changedFiles = [];
// Границы сравнения: from — база (null без git), to — head (null — рабочее дерево)
const ctx = { from: null, to: null };
// С --head: Go-тесты рабочей копии из изменённых в диффе файлов (имя → файл), см. buildFacts
const replacedTests = new Map();

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..');
const SPEC_DIR = 'frontend/tests/router';

// Только безопасные имена путей: значения уходят в eval и в командную строку.
const SAFE_PATH = /^[A-Za-z0-9_.][A-Za-z0-9_./-]*$/;
const SAFE_ROUTE = /^[a-z][a-z0-9_-]*$/;
const SEQ_ENTRY = /^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$/;

// Выбор остановлен (D-24, D-28): его нельзя надёжно получить. stdout пуст, причины — строки
// stderr «  - стоп: <файл>: <причина>»; run.sh краснеет до деплоя, select-replay.mjs пишет
// строку PR как ошибку. Полный выбор вместо остановки не подставляется.
const EXIT_STOP = 3;

// Правка механизма выбора: полный выбор (RTR-02). Карта областей сюда не входит: её правка
// разбирается по смыслу сравнением двух версий (compareMaps, D-21…D-23 фазы 148.1.1).
const SELECTOR_SELF = [/^scripts\/router\/(select-changed\.mjs|core-marks\.cjs|import-graph\.mjs|run-plan\.mjs)$/];

// Обвязка стенда: RT_HARNESS=1 без полного выбора (набор обвязки — 148.1-11). Цели стенда в
// Makefile разбирает makefileStand(), internal/routertest — ветка Go.
const HARNESS = [
  /^scripts\/router\//,
  /^scripts\/version\.sh$/,
  /^frontend\/playwright\.router\.config\.ts$/,
  /^frontend\/tests\/router\/lib\//,
  /^frontend\/tests\/router\/global-(setup|teardown)\.ts$/
];

// Помощники роутерных тестов: правка — обвязка (RT_HARNESS=1) и тесты зависимых пакетов,
// использующие изменённые объявления (D-32)
const RT_DIR = 'internal/routertest';

// Зависимости: узкий набор (смоук, pages, канарейки), при мажоре — шире (D-13).
const DEPS = [/^frontend\/package\.json$/, /^frontend\/package-lock\.json$/, /^go\.mod$/, /^go\.sum$/];
// У этих пакетов мажор проверяется и по версии в package-lock.json, не только в package.json.
// Патч и минор — узкий выбор, как у прочих пакетов (D-11 фазы 148.1.1, RTR-13).
const WATCHED_NPM = new Set(['svelte', 'vite', '@playwright/test']);

// Цели Makefile, влияющие на стенд: сборка для роутеров и версия.
const STAND_TARGET = /^(router-[A-Za-z0-9_-]+|update-version|version)$/;

// Код запуска или перезапуска ядра и watchdog (D-07): изменение даёт RT_KERNEL_TOUCHED=1 —
// run.sh берёт полное окно смоука. `_test.go` не считаются. Файл при этом разбирается как
// обычно, признак лишь добавляется к выбору.
const KERNEL_PATHS = [
  /^internal\/services\/kernel[\w]*\.go$/,
  /^internal\/services\/watchdog\.go$/,
  /^internal\/services\/xkeen[\w]*\.go$/,
  /^internal\/handlers\/kernel[\w]*\.go$/,
  /^internal\/handlers\/watchdog\.go$/,
  /^internal\/handlers\/service\.go$/,
  // перезапуск запущенного ядра после «Применить», его подтверждение и флаг перезапуска
  /^internal\/services\/configlayer\/(pipeline|restart|flag)\.go$/,
  // перезапуск Mihomo из обработчика (kernel_restarting)
  /^internal\/handlers\/mihomo\.go$/,
  // запуск служб и watchdog
  /^cmd\/xcp\/[\w]+\.go$/
];

// Ничего не добавляют (смоук идёт всё равно): документация, CI, хуки, служебное.
const IGNORED = [
  /\.md$/,
  /^\.github\//,
  /^\.githooks\//,
  /^\.claude\//,
  /^\.planning\//,
  /^docs\//,
  /^LICENSE/,
  /^\.gitignore$/,
  /^\.golangci\.ya?ml$/,
  // уборка ПК и статические проверки CI на роутер не влияют (D-25)
  /^scripts\/tidy\.sh$/,
  /^scripts\/check-[\w-]+\.js$/,
  /^scripts\/release-guard\.js$/,
  /^scripts\/lib\//,
  // Playwright-спеки с подменой сети и юнит-тесты фронтенда на роутерах не запускаются
  /^frontend\/tests\/(?!router\/)/,
  /^frontend\/src\/.*\.test\.ts$/,
  /^frontend\/scripts\//,
  /^frontend\/\.prettier/,
  /^frontend\/eslint\.config\./,
  /^frontend\/\.gitignore$/
];

// Спеки фронтенда с особой ролью (RTR-13). pages — read-обход всех маршрутов (`e2e-pages: *`).
// Читающая канарейка оболочки (D-08) — существующий read-спек: шапка `// e2e-kind: read`
// проверяется при каждом выборе. Не найден или не read — все спеки (расширение).
const PAGES_SPEC = 'tests/router/pages.spec.ts';
const SHELL_READ_CANARY = 'tests/router/config-visual.spec.ts';
// Канарейка каркаса (D-09) — один существующий спек с путём «правка → Применить»: в тексте есть
// вызов confirmApply(. Не найден — все спеки.
const SHELL_APPLY_CANARY = 'tests/router/config-presets.spec.ts';

// Стили, токены и тема (D-08): pages со всеми маршрутами и читающая канарейка оболочки.
// public/ — статика первого кадра и PWA (theme-init.js, manifest.json, иконки), Vite копирует её в
// сборку без обработки: отрисовывается на каждой странице одинаково, как тема (D-32).
const FRONT_STYLE = [/^src\/styles\//, /^src\/assets\//, /\.css$/, /^src\/lib\/theme\.ts$/, /^public\//];
// Каркас (D-09): точка входа, корневой компонент, маршрутизация и заголовок страниц — pages со
// всеми маршрутами и канарейка «правка → Применить». Dashboard.svelte ещё и страница dashboard.
// index.html — HTML-шаблон точки входа (подключает /theme-init.js и /src/main.ts), как main.ts (D-32).
const FRONT_FRAME = [
  /^index\.html$/,
  /^src\/App\.svelte$/,
  /^src\/main\.ts$/,
  /^src\/Dashboard\.svelte$/,
  /^src\/lib\/tabFromHash\.ts$/,
  /^src\/PageHeader\.svelte$/
];
// Локали (D-07): выбор по изменённым ключам.
const FRONT_LOCALE = /^src\/locales\/[\w-]+\.json$/;
// Хаб фронтенда (D-10): модуль оболочки или достижимый строго больше чем с HUB_PERCENT процентов
// страниц (маршрутов Dashboard; вкладки config считаются одной страницей) в любой из ревизий.
// Хаб — pages без сужения, канарейка «правка → Применить» и спеки, ссылающиеся на модуль
// напрямую. Порог выбран повтором выбора (148.1.1-03: >45% и >50% дали одинаковый выбор на
// одном снимке, >55% расширил один PR до всех спеков; при равенстве — >50%); подтверждается
// повтором после интеграции фазы. --hub-percent — для сравнения вариантов.
const HUB_PERCENT_DEFAULT = 50;
let hubPercent = HUB_PERCENT_DEFAULT;

// Сборочные конфигурации фронтенда (D-26): все роутерные спеки (не Go) — широкая проверка
// сохраняется. Конфигурации TypeScript сюда не входят: их выбор — по конечным потребителям
// (TS_CONFIG, D-26). index.html (шаблон точки входа) и public/ (статика, копируется в сборку без
// обработки) — не конфигурации: они идут в touchedSrc и проверяются обходом всех маршрутов pages
// и канарейкой (FRONT_FRAME и FRONT_STYLE, D-32).
const FRONTEND_ALL = [/^frontend\/vite\.config\.ts$/, /^frontend\/svelte\.config\.js$/];
// Файлы frontend/ вне src/, которые разбираются как файлы фронтенда (каркас и тема, D-32)
const FRONTEND_SHELL_STATIC = [/^frontend\/index\.html$/, /^frontend\/public\//];

// Конфигурации TypeScript фронтенда (D-26…D-28): frontend/**/tsconfig*.json и jsconfig*.json
// вне node_modules. Выбор — по потребителям обеих ревизий (tsConfigConsumers).
const TS_CONFIG = /^frontend\/(?:(?!node_modules\/)[\w.-]+\/)*(?:tsconfig|jsconfig)[\w.-]*\.json$/;
// Конфиги Playwright: их поле tsconfig — корни цепочки тестов
const PW_CONFIG = /^frontend\/playwright[\w.-]*\.config\.ts$/;
// Корень цепочки сборки панели: ближайшая к frontend/src конфигурация
const BUILD_TS_ROOT = 'frontend/tsconfig.json';
// Скрипты package.json, чей --tsconfig — тоже корень цепочки сборки
const BUILD_SCRIPTS = ['build', 'check', 'lint'];
const TS_CONSUMER_LABEL = { build: 'сборка панели', tests: 'тесты' };

function git(args) {
  return execFileSync('git', args, { cwd: ROOT, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 });
}

/** Текст файла в ref (null — нет файла или ref). */
function gitShow(ref, file) {
  try {
    return execFileSync('git', ['show', `${ref}:${file}`], {
      cwd: ROOT,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
      maxBuffer: 64 * 1024 * 1024
    });
  } catch {
    return null;
  }
}

/** Текст файла на стороне head: в ref, если задан --head, иначе в рабочем дереве. */
function headText(file) {
  if (ctx.to) return gitShow(ctx.to, file);
  const abs = path.join(ROOT, file);
  return existsSync(abs) ? readFileSync(abs, 'utf8') : null;
}

const firstLine = (e) => String(e?.message ?? e).split('\n', 1)[0];

// --- конфигурации TypeScript фронтенда: потребители двух ревизий (D-26…D-28) ---------------

/**
 * JSON с комментариями `//`, `/* *\/` и висячими запятыми — как tsconfig читает tsc. Разбор как
 * данных, без исполнения (T-148.1.1-25).
 */
function parseJsonc(text) {
  const src = text.replace(/^﻿/, '');
  // строка JSON с позиции i: индекс после закрывающей кавычки
  const strEnd = (s, i) => {
    let j = i + 1;
    while (j < s.length && s[j] !== '"') j += s[j] === '\\' ? 2 : 1;
    if (j >= s.length) throw new Error('незакрытая строка');
    return j + 1;
  };
  let bare = '';
  for (let i = 0; i < src.length; ) {
    if (src[i] === '"') {
      const j = strEnd(src, i);
      bare += src.slice(i, j);
      i = j;
    } else if (src.startsWith('//', i)) {
      while (i < src.length && src[i] !== '\n') i++;
    } else if (src.startsWith('/*', i)) {
      const end = src.indexOf('*/', i + 2);
      if (end < 0) throw new Error('незакрытый комментарий /*');
      bare += ' ';
      i = end + 2;
    } else {
      bare += src[i++];
    }
  }
  let out = '';
  for (let i = 0; i < bare.length; ) {
    if (bare[i] === '"') {
      const j = strEnd(bare, i);
      out += bare.slice(i, j);
      i = j;
      continue;
    }
    if (bare[i] === ',') {
      let j = i + 1;
      while (j < bare.length && /\s/.test(bare[j])) j++;
      if (bare[j] === '}' || bare[j] === ']') {
        i++;
        continue;
      }
    }
    out += bare[i++];
  }
  return JSON.parse(out);
}

/** Файлы frontend/ ревизии ref (null — рабочее дерево, вне node_modules). */
function frontendFilesAt(ref) {
  if (ref) return git(['ls-tree', '-r', '--name-only', ref, '--', 'frontend']).split('\n').filter(Boolean);
  const out = [];
  const walk = (dir) => {
    for (const d of readdirSync(path.join(ROOT, dir), { withFileTypes: true })) {
      if (d.name === 'node_modules' || d.name.startsWith('.')) continue;
      const rel = `${dir}/${d.name}`;
      if (d.isDirectory()) walk(rel);
      else out.push(rel);
    }
  };
  if (existsSync(path.join(ROOT, 'frontend'))) walk('frontend');
  return out;
}

/** Путь p относительно каталога dir; null — вне frontend/ («..» за пределы, абсолютный путь). */
function frontendPath(dir, p) {
  if (p.startsWith('/') || /^[A-Za-z]:/.test(p)) return null;
  const out = path.posix.normalize(path.posix.join(dir, p));
  return out.startsWith('frontend/') ? out : null;
}

/**
 * Цепочки extends конфигураций TypeScript фронтенда на ревизии ref (null — рабочее дерево):
 * { roots, nodes, issues, chainIssues(file) }. roots — { kind: 'tests' | 'build', file, source,
 * reach } (reach — все конфигурации цепочки корня, включая его самого). Корни тестов — поле
 * tsconfig конфигов frontend/playwright*.config.ts (без поля — ближайший к testDir tsconfig.json,
 * как ищет Playwright); корни сборки — frontend/tsconfig.json и пути --tsconfig в скриптах
 * build, check, lint frontend/package.json той же ревизии. extends с «.» в начале — локальный
 * файл относительно конфигурации (без расширения — с .json); иное — пакет, внешняя вершина: его
 * изменения приходят через правила зависимостей. issues — неразрешимые связи: { file, why }.
 */
function tsConfigGraph(ref) {
  const read = (f) => {
    if (ref) return gitShow(ref, f);
    const abs = path.join(ROOT, f);
    return existsSync(abs) ? readFileSync(abs, 'utf8') : null;
  };
  const files = frontendFilesAt(ref);
  const has = new Set(files);
  const nodes = new Map(); // файл → { exists, ext: [локальные пути], error }
  const issues = [];
  const issue = (file, why) => {
    if (!issues.some((x) => x.file === file && x.why === why)) issues.push({ file, why });
  };
  const node = (f) => {
    if (nodes.has(f)) return nodes.get(f);
    const text = read(f);
    const n = { exists: text !== null, ext: [], error: null };
    nodes.set(f, n);
    if (text === null) return n;
    let cfg;
    try {
      cfg = parseJsonc(text);
    } catch (e) {
      n.error = `не разбирается: ${firstLine(e)}`;
      return n;
    }
    const raw = cfg && typeof cfg === 'object' && !Array.isArray(cfg) ? cfg.extends : undefined;
    const list = raw === undefined ? [] : Array.isArray(raw) ? raw : [raw];
    for (const v of list) {
      if (typeof v !== 'string') {
        n.error = 'не разбирается: extends — не строка и не массив строк';
        break;
      }
      if (!v.startsWith('.') && !v.startsWith('/') && !/^[A-Za-z]:/.test(v)) continue; // пакет
      let p = frontendPath(path.posix.dirname(f), v);
      if (p === null) {
        n.error = `extends вне frontend/: ${v}`;
        break;
      }
      if (!p.endsWith('.json')) p += '.json';
      n.ext.push(p);
    }
    return n;
  };
  // Обход цепочки от start: все вершины; неразрешимое — в issues
  const chain = (start) => {
    const reach = new Set();
    const stack = [];
    const visit = (f, from) => {
      const at = stack.indexOf(f);
      if (at >= 0) {
        issue(f, `цикл наследования ${[...stack.slice(at), f].join(' → ')}`);
        return;
      }
      if (reach.has(f)) return;
      const n = node(f);
      if (!n.exists) {
        issue(from ?? f, `extends на несуществующий файл ${f}`);
        return;
      }
      reach.add(f);
      if (n.error) {
        issue(f, n.error);
        return;
      }
      stack.push(f);
      for (const p of n.ext) visit(p, f);
      stack.pop();
    };
    visit(start, null);
    return reach;
  };
  const roots = [];
  const addRoot = (kind, file, source) => {
    if (file === null || !has.has(file)) {
      issue(source, `tsconfig на несуществующий файл ${file ?? 'вне frontend/'}`);
      return;
    }
    roots.push({ kind, file, source, reach: chain(file) });
  };
  for (const f of files.filter((x) => PW_CONFIG.test(x)).sort()) {
    const text = stripComments(read(f) ?? '');
    const m = /\btsconfig\s*:\s*(['"`])([^'"`]+)\1/.exec(text);
    if (m) {
      const p = frontendPath('frontend', m[2]);
      addRoot('tests', p, f);
      continue;
    }
    // Без поля tsconfig Playwright берёт ближайший к файлам тестов tsconfig.json
    const td = /\btestDir\s*:\s*(['"`])([^'"`]+)\1/.exec(text);
    let dir = frontendPath('frontend', td ? td[2] : '.') ?? 'frontend';
    while (dir.startsWith('frontend/') && !has.has(`${dir}/tsconfig.json`)) dir = path.posix.dirname(dir);
    if (has.has(`${dir}/tsconfig.json`)) addRoot('tests', `${dir}/tsconfig.json`, `${f} (без поля tsconfig)`);
  }
  if (has.has(BUILD_TS_ROOT)) addRoot('build', BUILD_TS_ROOT, 'frontend/src');
  const pkgText = has.has('frontend/package.json') ? read('frontend/package.json') : null;
  if (pkgText !== null) {
    let scripts = {};
    try {
      scripts = JSON.parse(pkgText).scripts || {};
    } catch (e) {
      issue('frontend/package.json', `не разбирается: ${firstLine(e)}`);
    }
    for (const name of BUILD_SCRIPTS) {
      const cmd = scripts[name];
      if (typeof cmd !== 'string') continue;
      for (const m of cmd.matchAll(/--tsconfig(?:=|\s+)(['"]?)([^\s'"]+)\1/g)) {
        addRoot('build', frontendPath('frontend', m[2]), `frontend/package.json (скрипт ${name})`);
      }
    }
  }
  // Все конфигурации ревизии: неразрешимое в цепочке любой из них тоже видно (D-28)
  for (const f of files.filter((x) => TS_CONFIG.test(x))) chain(f);
  return { roots, nodes, issues };
}

const tsGraphs = { done: false, base: null, head: null, baseRef: null, error: null };

/** Графы конфигураций базы (база диффа, в режиме --files — HEAD) и head; строятся один раз. */
function tsConfigGraphs() {
  if (tsGraphs.done) return tsGraphs;
  tsGraphs.done = true;
  tsGraphs.baseRef = mapBaseRef();
  try {
    if (tsGraphs.baseRef) tsGraphs.base = tsConfigGraph(tsGraphs.baseRef);
    tsGraphs.head = tsConfigGraph(ctx.to);
  } catch (e) {
    tsGraphs.error = `конфигурации ревизий не прочитаны: ${firstLine(e)}`;
  }
  return tsGraphs;
}

/** Файл — конфигурация TypeScript фронтенда: по шаблону или вершина extends на любой ревизии. */
function isTsConfig(file) {
  if (TS_CONFIG.test(file)) return true;
  if (!/^frontend\/(?!src\/|node_modules\/)[\w./-]*\.json$/.test(file) || /\/package(-lock)?\.json$/.test(file)) return false;
  const g = tsConfigGraphs();
  return [g.base, g.head].some((x) => x?.nodes.get(file)?.exists);
}

/**
 * Потребители изменённой конфигурации: { kinds: Set('tests' | 'build'), issues }. Объединение
 * корней, в чьей цепочке extends есть файл, по базе и head: удалённый — по базе, новый — по head,
 * смена extends — обе цепочки, даже если итоговые настройки совпали (D-27). issues — неразрешимые
 * связи обеих ревизий: { file, why } с пометкой ревизии.
 */
function tsConfigConsumers(file) {
  const g = tsConfigGraphs();
  const out = { kinds: new Set(), issues: [] };
  if (g.error) {
    out.issues.push({ file, why: g.error });
    return out;
  }
  for (const [side, graph] of [
    [`база ${g.baseRef}`, g.base],
    ['head', g.head]
  ]) {
    if (!graph) continue;
    for (const r of graph.roots) if (r.reach.has(file)) out.kinds.add(r.kind);
    for (const x of graph.issues) out.issues.push({ file: x.file, why: `${x.why} (${side})` });
  }
  return out;
}

// --- карта областей: сравнение двух версий (RTR-14, D-21…D-25 фазы 148.1.1) ----------------

/** Ревизия базы для сравнения карты: база диффа, в режиме --files — HEAD; null — git нет. */
function mapBaseRef() {
  if (ctx.from) return ctx.from;
  try {
    execFileSync('git', ['rev-parse', '--verify', '--quiet', 'HEAD^{commit}'], { cwd: ROOT, stdio: 'ignore' });
    return 'HEAD';
  } catch {
    return null;
  }
}

/**
 * Две версии карты: { baseRef, base, head, diff, appeared, problems }. Обе разбираются
 * areasFromText(текст, null) — тот же validateAreas, что у стража; problems — причины, по
 * которым карты нельзя надёжно сравнить; appeared — в базе карты нет.
 */
function compareMaps() {
  const out = { baseRef: mapBaseRef(), base: null, head: null, diff: null, appeared: false, problems: [] };
  if (out.baseRef === null) {
    out.problems.push('нет базы сравнения: режим --files без git (ревизии HEAD нет)');
  } else {
    const text = gitShow(out.baseRef, AREAS_FILE);
    if (text === null) out.appeared = true;
    else {
      try {
        out.base = areasFromText(text, null);
      } catch (e) {
        out.problems.push(`карта базы (${out.baseRef}) не разбирается или не проходит проверку: ${firstLine(e)}`);
      }
    }
  }
  const text = headText(AREAS_FILE);
  if (text === null) out.problems.push('карта удалена — сравнивать не с чем');
  else {
    try {
      out.head = areasFromText(text, null);
    } catch (e) {
      out.problems.push(`карта head не разбирается или не проходит проверку: ${firstLine(e)}`);
    }
  }
  if (out.base && out.head) out.diff = diffAreaMaps(out.base, out.head);
  return out;
}

/** Метки `// router-area:` роутерных тестов в ревизии ref: Map файл → область. */
function areaMarksAt(ref) {
  const marks = new Map();
  let text;
  try {
    text = execFileSync('git', ['grep', '-n', '-I', '-e', '^// router-area: ', ref, '--', 'internal', 'cmd', 'frontend/tests/router'], {
      cwd: ROOT,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'ignore'],
      maxBuffer: 64 * 1024 * 1024
    });
  } catch (e) {
    if (e.status === 1) return marks; // совпадений нет
    throw new Error(`метки router-area ревизии ${ref} не прочитаны (${firstLine(e)})`);
  }
  const seen = new Map();
  for (const line of text.split('\n')) {
    if (!line.startsWith(`${ref}:`)) continue;
    const rest = line.slice(ref.length + 1);
    const m = /^([^:]+):(\d+):\/\/ router-area: ([a-z][a-z0-9-]*)$/.exec(rest);
    if (!m || Number(m[2]) > 5) continue;
    const file = m[1];
    if (!/_test\.go$/.test(file) && !/^frontend\/tests\/router\/[\w.-]+\.spec\.ts$/.test(file)) continue;
    seen.set(file, (seen.get(file) || 0) + 1);
    marks.set(file, m[3]);
  }
  // строка области повторяется — метка неоднозначна, файл не считается (так же, как areaOfText)
  for (const [file, n] of seen) if (n > 1) marks.delete(file);
  return marks;
}

function modulePath() {
  const m = readFileSync(path.join(ROOT, 'go.mod'), 'utf8').match(/^module\s+(\S+)/m);
  if (!m) throw new Error('go.mod: не найдена строка module');
  return m[1];
}

/** Каталоги с роутерными тестами: `_test.go`, первая строка которого `//go:build router`. */
function routerPackages() {
  const found = new Set();
  const walk = (dir) => {
    for (const dirent of readdirSync(path.join(ROOT, dir), {
      withFileTypes: true
    })) {
      const entry = dirent.name;
      const rel = `${dir}/${entry}`;
      if (dirent.isDirectory()) {
        if (entry !== 'node_modules' && entry !== 'testdata') walk(rel);
      } else if (entry.endsWith('_test.go')) {
        const first = readFileSync(path.join(ROOT, rel), 'utf8').split('\n', 1)[0].trim();
        if (first === '//go:build router') found.add(dir);
      }
    }
  };
  for (const top of ['internal', 'cmd']) if (existsSync(path.join(ROOT, top))) walk(top);
  return [...found].sort();
}

/** Ближайший каталог с .go-файлами для изменённого файла (в том числе удалённого). */
function goDirOf(file) {
  let dir = path.posix.dirname(file);
  if (dir === '.') return '.';
  while (dir !== '.' && dir !== '') {
    const abs = path.join(ROOT, dir);
    if (existsSync(abs) && statSync(abs).isDirectory()) {
      if (readdirSync(abs).some((f) => f.endsWith('.go'))) return dir;
    }
    dir = path.posix.dirname(dir);
  }
  return null;
}

/*
 * Выбор по объявлениям Go (RTR-14, D-32 фазы 148.1.1). Изменённый `_test.go` роутерного пакета и
 * файл internal/routertest сравниваются с базой по объявлениям верхнего уровня (func, var, const,
 * type; core-marks.cjs declarations): изменённые имена → объявления пакета, которые их упоминают
 * (по границе слова, до неподвижной точки, и переменные пакета, упомянутые функциями множества, —
 * общее состояние) → тесты TestRouter замыкания. Одноимённые объявления файла (методы разных
 * получателей, несколько func init) сравниваются мультимножеством кодов: имя изменено при любом
 * расхождении. Неразобранное — пакет целиком с причиной (неизвестное — широко).
 */

// Вызовы, допустимые в инициализаторе переменной пакета без отката на пакет целиком. Пополнять
// только функциями стандартной библиотеки без побочных эффектов, кроме возврата значения или
// паники: инициализатор с иным вызовом может менять общее состояние до запуска тестов.
const PURE_INIT_CALLS = [
  /^regexp\.MustCompile(?:POSIX)?$/,
  /^errors\.New$/,
  /^fmt\.(?:Errorf|Sprintf)$/,
  /^strings\.[A-Za-z_]\w*$/,
  /^time\.Duration$/,
  /^(?:len|make|new|append)$/
];
// Литералы Go в коде без комментариев (lex): строка, сырая строка, руна
const GO_LITERAL = /"(?:[^"\\\n]|\\.)*"|`[^`]*`|'(?:[^'\\\n]|\\.)*'/g;
const GO_IDENT_LIST = /^[ \t]*([A-Za-z_]\w*(?:[ \t]*,[ \t]*[A-Za-z_]\w*)*)/;

/**
 * Объявления верхнего уровня Go-файла: { ok, why, build, pkg, blankDot, imports, decls }.
 * decls — [{ kind: func|var|const|type, names, receiver, code (нормализованный, без
 * комментариев), masked (строки замаскированы), idents (Set слов кода), calls (вызовы в
 * инициализаторе var) }]. ok=false — разбор ненадёжен (раскладка не gofmt, нет package, имена не
 * разобраны): вызывающий откатывается на пакет целиком.
 */
const goDeclCache = new Map();
function goDeclIndex(text) {
  if (!goDeclCache.has(text)) goDeclCache.set(text, goDeclIndexOf(text));
  return goDeclCache.get(text);
}

function goDeclIndexOf(text) {
  const out ={ ok: true, why: null, build: null, pkg: null, blankDot: [], imports: [], decls: [] };
  const fail = (why) => {
    if (out.ok) {
      out.ok = false;
      out.why = why;
    }
  };
  const { code, masked } = lex(text);
  // Раскладка gofmt: на нулевой глубине скобок строка начинается с колонки 0 — иначе деление на
  // объявления по колонке 0 (declarations) ненадёжно. Сырые строки в бэктиках не считаются;
  // продолжение выражения (предыдущая строка кончается бинарным оператором, запятой или точкой:
  // по правилу точки с запятой Go оператор там не кончается) gofmt сдвигает — это допустимо.
  let depth = 0;
  let raw = false;
  let cont = false;
  const lines = masked.split('\n');
  for (let li = 0; li < lines.length && out.ok; li++) {
    const line = lines[li];
    if (!raw && depth === 0 && !cont && /^[ \t]+\S/.test(line)) fail(`раскладка не gofmt (строка ${li + 1})`);
    const tail = line.trimEnd();
    if (tail) cont = !/(?:\+\+|--)$/.test(tail) && /[-+*/%&|^<>=,.!:]$/.test(tail);
    for (const c of line) {
      if (c === '`') raw = !raw;
      else if (raw) continue;
      else if (c === '{' || c === '(' || c === '[') depth++;
      else if (c === '}' || c === ')' || c === ']') depth--;
    }
    if (depth < 0) fail(`скобки не сходятся (строка ${li + 1})`);
  }
  const decls = declarations(text);
  const firstStart = code.length - decls.reduce((s, d) => s + d.code.length, 0);
  const preText = text.slice(0, firstStart);
  const preCode = code.slice(0, firstStart);
  out.build = (preText.match(/^\/\/(?:go:build| \+build)[^\n]*/gm) || []).join('\n') || null;
  const pkg = /^package[ \t]+([A-Za-z_]\w*)/m.exec(masked.slice(0, firstStart));
  if (pkg) out.pkg = pkg[1];
  else fail('нет строки package');
  for (const m of preCode.matchAll(/(?:^import[ \t]+(?:\([ \t]*)?|^[ \t]+)(?:([A-Za-z_]\w*|[_.])[ \t]+)?"([^"\n]+)"/gm)) {
    out.imports.push({ name: m[1] || null, path: m[2] });
    if (m[1] === '_' || m[1] === '.') out.blankDot.push(`${m[1]} "${m[2]}"`);
  }
  out.blankDot.sort();
  for (const d of decls) {
    const head = d.masked.split('\n', 1)[0];
    const kw = /^(func|var|const|type)\b[ \t]*(.*)$/.exec(head);
    const kind = kw ? kw[1] : 'other';
    let names = [];
    if (kind === 'func') {
      if (d.kind !== 'func' || !d.name) fail(`не разобрано объявление func: ${head.slice(0, 60)}`);
      else names = [d.name];
    } else if (kw && kw[2].startsWith('(')) {
      // блок var ( … ), const ( … ), type ( … ): имена — в начале строк на глубине 1
      let dd = 0;
      for (const line of d.masked.split('\n')) {
        if (dd === 1) {
          const m = GO_IDENT_LIST.exec(line);
          if (m) names.push(...m[1].split(',').map((s) => s.trim()));
        }
        for (const c of line) {
          if (c === '{' || c === '(' || c === '[') dd++;
          else if (c === '}' || c === ')' || c === ']') dd--;
        }
      }
    } else if (kw) {
      const m = GO_IDENT_LIST.exec(kw[2]);
      if (m) names = m[1].split(',').map((s) => s.trim());
      else fail(`не разобраны имена объявления ${kind}: ${head.slice(0, 60)}`);
    }
    const calls = [];
    if (kind === 'var') {
      for (const m of d.masked.matchAll(/(?<![\w.])([A-Za-z_]\w*(?:\.[A-Za-z_]\w*)*)[ \t]*\(/g)) {
        if (m[1] !== 'func' && m[1] !== 'var') calls.push(m[1]);
      }
      // немедленный вызов литерала функции или выражения: }() , )( , ](
      if (/[)\]}][ \t]*\(/.test(d.masked)) calls.push('(…)(…)');
    }
    // тип получателя метода: правка метода — правка его типа (метод вызывает не код тестов, а
    // стандартная библиотека или другой пакет через интерфейс: Write, RoundTrip, Read, ServeHTTP)
    const rm = kind === 'func' && d.receiver ? /^func[ \t]*\([ \t]*(?:[A-Za-z_]\w*[ \t]+)?\*?[ \t]*([A-Za-z_]\w*)/.exec(head) : null;
    out.decls.push({
      kind,
      names,
      receiver: d.receiver,
      receiverType: rm ? rm[1] : null,
      // пробелы схлопываются только вне литералов (в masked их содержимое замещено пробелами);
      // литералы сравниваются дословно: отступ в сырой строке (YAML, вывод команды) значим
      code: `${d.masked.replace(/\s+/g, ' ').trim()}\u0001${[...d.code.matchAll(GO_LITERAL)].map((m) => m[0]).join('\u0000')}`,
      masked: d.masked,
      idents: new Set(d.masked.match(/[A-Za-z_]\w*/g) || []),
      calls
    });
  }
  return out;
}

/**
 * Сравнение базы и head одного Go-файла по объявлениям: { changed: Set имён, wide: причина | null }.
 * null на стороне — файла нет (добавлен: пустая база; удалён: пустой head): все объявления
 * существующей стороны изменены, строки build и package не сравниваются. По имени сравнивается
 * мультимножество нормализованных кодов всех одноимённых объявлений файла, а не «имя → один код»:
 * declarations() отдаёт получатель только как признак, и правка второго одноимённого метода иначе
 * потерялась бы. Порядок changed — объявления head, затем удалённые.
 */
function goDeclDiff(baseText, headText) {
  const b = baseText == null ? null : goDeclIndex(baseText);
  const h = headText == null ? null : goDeclIndex(headText);
  const changed = new Set();
  let wide = null;
  const setWide = (w) => {
    if (!wide) wide = w;
  };
  if (b && !b.ok) setWide(`база: ${b.why}`);
  if (h && !h.ok) setWide(`head: ${h.why}`);
  if (wide) return { changed, wide };
  if (b && h) {
    if (b.build !== h.build) setWide('изменена строка //go:build');
    if (b.pkg !== h.pkg) setWide('изменена строка package');
    if (b.blankDot.join('\n') !== h.blankDot.join('\n')) setWide('изменён пустой или точечный импорт');
    // тот же идентификатор пакета указывает на другой путь (x/v1 → x/v2, подмена пакета): код
    // объявлений не меняется, а поведение — да
    const impBy = (ix) => {
      const m = new Map();
      for (const i of ix.imports) m.set(i.name || i.path.replace(/\/v\d+$/, '').split('/').pop(), i.path);
      return m;
    };
    const bi = impBy(b);
    for (const [id, p] of impBy(h)) if (bi.has(id) && bi.get(id) !== p) setWide(`изменён путь импорта ${id}`);
  } else {
    const side = b || h;
    if (side && side.blankDot.length > 0) setWide(`пустой или точечный импорт (${side.blankDot.join(', ')})`);
  }
  const multi = (ix) => {
    const m = new Map();
    for (const d of ix ? ix.decls : []) {
      for (const n of d.names) {
        if (!m.has(n)) m.set(n, []);
        m.get(n).push(d.code);
      }
    }
    for (const v of m.values()) v.sort();
    return m;
  };
  const bm = multi(b);
  const hm = multi(h);
  for (const [n, codes] of hm) if (JSON.stringify(codes) !== JSON.stringify(bm.get(n) || [])) changed.add(n);
  for (const n of bm.keys()) if (!hm.has(n)) changed.add(n);
  const all = [...(h ? h.decls : []), ...(b ? b.decls : [])];
  // изменённый метод изменяет тип получателя: его неявные вызовы (через интерфейс) по имени метода
  // не находятся, а тип встречается в потребителях (литерал, поле, конструктор)
  for (const d of all) {
    if (d.receiverType && d.names.some((n) => changed.has(n))) changed.add(d.receiverType);
  }
  for (const n of changed) {
    for (const d of all) {
      if (!d.names.includes(n)) continue;
      if (d.kind === 'func' && !d.receiver && (n === 'init' || n === 'TestMain')) setWide(`изменена func ${n}`);
      if (d.kind !== 'var') continue;
      if (n === '_') setWide('изменена переменная _');
      const impure = d.calls.find((c) => !PURE_INIT_CALLS.some((re) => re.test(c)));
      if (impure) setWide(`переменная ${n}: вызов ${impure} вне PURE_INIT_CALLS`);
    }
  }
  return { changed, wide };
}

const goDirCache = new Map();
/**
 * Go-файлы каталога на стороне head (рабочее дерево или --head): [{ file, text }], с кешем.
 * Подкаталоги не входят.
 */
function goDirFiles(dir) {
  if (goDirCache.has(dir)) return goDirCache.get(dir);
  let names;
  if (ctx.to) {
    names = git(['ls-tree', '--name-only', ctx.to, '--', `${dir}/`])
      .split('\n')
      .filter((f) => f.endsWith('.go') && path.posix.dirname(f) === dir);
  } else {
    const abs = path.join(ROOT, dir);
    names = existsSync(abs)
      ? readdirSync(abs, { withFileTypes: true })
          .filter((e) => e.isFile() && e.name.endsWith('.go'))
          .map((e) => `${dir}/${e.name}`)
      : [];
  }
  const out = [];
  for (const file of names.sort()) {
    const text = headText(file);
    if (text !== null) out.push({ file, text });
  }
  goDirCache.set(dir, out);
  return out;
}

/** `_test.go` каталога на стороне head: [{ file, text }]. */
function goPkgTestFiles(dir) {
  return goDirFiles(dir).filter((f) => f.file.endsWith('_test.go'));
}

/**
 * Объявления набора файлов: { ok, why, decls } — decls с полем file; ok=false — какой-то файл не
 * разобран (why — «<файл>: <причина>»).
 */
function goFilesIndex(files) {
  const decls = [];
  for (const f of files) {
    const ix = goDeclIndex(f.text);
    if (!ix.ok) return { ok: false, why: `${f.file}: ${ix.why}`, decls: [] };
    for (const d of ix.decls) decls.push({ ...d, file: f.file });
  }
  return { ok: true, why: null, decls };
}

/**
 * Замыкание имён по объявлениям (точка неподвижности): объявление, в коде которого есть слово из
 * множества, добавляет свои имена; переменная пакета, упомянутая функцией из множества, тоже
 * добавляется (общее состояние: функция её читает или пишет). Возвращает Set имён.
 */
function goDeclClosure(decls, seed) {
  const set = new Set(seed);
  const vars = decls.filter((d) => d.kind === 'var');
  const expanded = new Set();
  const addAll = (d) => {
    let grew = false;
    for (const n of d.names) {
      if (!set.has(n)) {
        set.add(n);
        grew = true;
      }
    }
    return grew;
  };
  for (let grew = true; grew; ) {
    grew = false;
    for (const d of decls) {
      if (d.names.every((n) => set.has(n))) continue;
      for (const w of d.idents) {
        if (set.has(w) && !d.names.includes(w)) {
          grew = addAll(d) || grew;
          break;
        }
      }
    }
    for (const f of decls) {
      if (f.kind !== 'func' || expanded.has(f) || !f.names.some((n) => set.has(n))) continue;
      expanded.add(f);
      for (const v of vars) if (v.names.some((n) => f.idents.has(n))) grew = addAll(v) || grew;
    }
  }
  return set;
}

/** Тесты TestRouter (без получателя) среди объявлений, чьё имя в множестве: [имя] без повторов. */
function goRouterTestsIn(decls, set) {
  const out = new Set();
  for (const d of decls) {
    if (d.kind === 'func' && !d.receiver && /^TestRouter[A-Za-z0-9_]+$/.test(d.names[0]) && set.has(d.names[0])) {
      out.add(d.names[0]);
    }
  }
  return [...out];
}

/** Все пакеты, от которых зависит роутерный пакет (импорт-пути), по двум архитектурам стендов. */
function depsOf(pkgDir) {
  const out = new Set();
  const variants = [
    { GOARCH: 'arm64', GOMIPS: '' },
    { GOARCH: 'mipsle', GOMIPS: 'softfloat' }
  ];
  for (const v of variants) {
    const text = execFileSync(
      'go',
      ['list', '-tags', 'router', '-test', '-deps', '-f', '{{.ImportPath}}', `./${pkgDir}`],
      {
        cwd: ROOT,
        encoding: 'utf8',
        env: { ...process.env, CGO_ENABLED: '0', GOOS: 'linux', ...v }
      }
    );
    for (const line of text.split('\n')) {
      const p = line.trim().replace(/ \[.*\]$/, '');
      if (p) out.add(p);
    }
  }
  return out;
}

function listRouterSpecs() {
  const dir = path.join(ROOT, SPEC_DIR);
  if (!existsSync(dir)) return [];
  return readdirSync(dir)
    .filter((f) => /^[\w.-]+\.spec\.ts$/.test(f) && f !== 'core-switch.spec.ts')
    .sort()
    .map((f) => `tests/router/${f}`);
}

/** Изменённые файлы: { files, from, to }. С head — только diff <база>..<head>, без рабочего дерева. */
function changedFromGit(base, head) {
  let from = base;
  if (!from) {
    from = git(['merge-base', 'origin/main', head || 'HEAD']).trim();
  }
  const files = git(['diff', '--name-only', '--no-renames', from, ...(head ? [head] : [])])
    .split('\n')
    .filter(Boolean);
  if (!head) {
    const status = git(['status', '--porcelain', '--untracked-files=all'])
      .split('\n')
      .filter(Boolean)
      .map((l) => l.slice(3));
    for (const s of status) {
      // переименование «старое -> новое»: значимы оба имени
      for (const part of s.split(' -> ')) files.push(part);
    }
  }
  return { files: [...new Set(files.map((f) => path.posix.normalize(f)))], from, to: head || null };
}

// --- зависимости: мажорное обновление (D-13) ----------------------------------------------

/** Мажорная часть спецификации версии npm: `^5.1.0` → 5, `~0.4.2` → 0.4; нестандартная — вся строка. */
function majorOf(spec) {
  const m = /(\d+)(?:\.(\d+))?/.exec(String(spec));
  if (!m) return String(spec);
  return m[1] === '0' && m[2] !== undefined ? `0.${m[2]}` : m[1];
}

/**
 * Зависимости npm (D-11, RTR-13): { major, changed }. major — причина широкого выбора (строка)
 * или null: смена мажора спецификации в package.json у любого пакета, у svelte, vite и
 * @playwright/test — ещё и смена мажора версии в lock. Патч и минор (в том числе этих трёх
 * пакетов) мажором не считаются: changed — их список для объяснения. Нет базы или текста —
 * мажор (расширение).
 */
function npmMajorReason(files) {
  const changed = [];
  const major = (why) => ({ major: why, changed });
  if (!ctx.from) return major('нет базы сравнения — считается мажором');
  const baseTxt = gitShow(ctx.from, 'frontend/package.json');
  const headTxt = headText('frontend/package.json');
  if (baseTxt === null || headTxt === null) return major('package.json не прочитан — считается мажором');
  let a;
  let b;
  try {
    a = JSON.parse(baseTxt);
    b = JSON.parse(headTxt);
  } catch {
    return major('package.json не разобран — считается мажором');
  }
  const flat = (o) => ({
    ...(o.optionalDependencies || {}),
    ...(o.peerDependencies || {}),
    ...(o.devDependencies || {}),
    ...(o.dependencies || {})
  });
  const da = flat(a);
  const db = flat(b);
  for (const name of [...new Set([...Object.keys(da), ...Object.keys(db)])].sort()) {
    if (da[name] === db[name]) continue;
    if (da[name] === undefined || db[name] === undefined) {
      changed.push(`${name} ${da[name] === undefined ? 'добавлен' : 'удалён'}`);
      continue;
    }
    if (majorOf(da[name]) !== majorOf(db[name])) return major(`${name}: ${da[name]} → ${db[name]}`);
    changed.push(`${name} ${da[name]} → ${db[name]}`);
  }
  if (files.has('frontend/package-lock.json')) {
    const baseLock = gitShow(ctx.from, 'frontend/package-lock.json');
    const headLock = headText('frontend/package-lock.json');
    if (baseLock === null || headLock === null) return major('package-lock.json не прочитан — считается мажором');
    try {
      const la = JSON.parse(baseLock).packages || {};
      const lb = JSON.parse(headLock).packages || {};
      for (const name of WATCHED_NPM) {
        const va = (la[`node_modules/${name}`] || {}).version;
        const vb = (lb[`node_modules/${name}`] || {}).version;
        if (va === vb) continue;
        // пакет появился, пропал или сменил мажор в lock — широкий выбор
        if (va === undefined || vb === undefined || majorOf(va) !== majorOf(vb)) {
          return major(`${name} (lock): ${va ?? '-'} → ${vb ?? '-'}`);
        }
        changed.push(`${name} (lock) ${va} → ${vb}`);
      }
    } catch {
      return major('package-lock.json не разобран — считается мажором');
    }
  }
  return { major: null, changed };
}

/** Мажор Go: смена `go`, `toolchain` или модуля с суффиксом /vN. Нет базы — мажор. */
function goMajorReason() {
  if (!ctx.from) return 'нет базы сравнения — считается мажором';
  const baseTxt = gitShow(ctx.from, 'go.mod');
  const headTxt = headText('go.mod');
  if (baseTxt === null || headTxt === null) return 'go.mod не прочитан — считается мажором';
  const keys = (text) => {
    const set = new Set();
    for (const line of text.split('\n')) {
      const t = line.replace(/\/\/.*$/, '').trim();
      let m = /^(go|toolchain)\s+(\S+)$/.exec(t);
      if (m) set.add(`${m[1]} ${m[2]}`);
      m = /^(?:require\s+)?(\S+\/v\d+)\s+v\S+/.exec(t);
      if (m) set.add(`module ${m[1]}`);
      m = /^module\s+(\S+)$/.exec(t);
      if (m) set.add(`module ${m[1]}`);
    }
    return set;
  };
  const ka = keys(baseTxt);
  const kb = keys(headTxt);
  const diff = [...ka].filter((x) => !kb.has(x)).concat([...kb].filter((x) => !ka.has(x)));
  return diff.length > 0 ? diff.join(', ') : null;
}

// --- Makefile ---------------------------------------------------------------------------

/** Владельцы строк Makefile: 'comment' | 'blank' | имя цели | 'top' (переменные и условия). */
function makefileOwners(text) {
  const lines = text.split('\n');
  const owners = [];
  let cur = 'top';
  let cont = false;
  for (const line of lines) {
    if (cont) {
      owners.push(cur);
    } else if (/^\s*$/.test(line)) {
      owners.push('blank');
    } else if (/^\s*#/.test(line)) {
      owners.push('comment');
    } else if (/^\t/.test(line)) {
      owners.push(cur);
    } else if (/^\s*[A-Za-z_][A-Za-z0-9_]*\s*[:?+]?=/.test(line) || /^\s*(ifeq|ifneq|ifdef|ifndef|else|endif|include)\b/.test(line)) {
      cur = 'top';
      owners.push('top');
    } else {
      const m = /^([^\s:=][^:=]*?)\s*:(?!=)/.exec(line);
      if (m) cur = m[1].trim().split(/\s+/)[0];
      owners.push(m ? cur : 'top');
    }
    cont = /\\\s*$/.test(line);
  }
  return owners;
}

/** Правка Makefile затрагивает цели стенда или VERSION: { stand, why }. Нет базы — стенд (расширение). */
function makefileStand() {
  if (!ctx.from) return { stand: true, why: 'нет базы сравнения — считается правкой обвязки' };
  let diff;
  try {
    diff = git(['diff', '-U0', '--no-color', '--no-renames', ctx.from, ...(ctx.to ? [ctx.to] : []), '--', 'Makefile']);
  } catch {
    return { stand: true, why: 'diff Makefile не получен — считается правкой обвязки' };
  }
  const baseTxt = gitShow(ctx.from, 'Makefile');
  const headTxt = headText('Makefile');
  const baseLines = (baseTxt ?? '').split('\n');
  const headLines = (headTxt ?? '').split('\n');
  const baseOwn = makefileOwners(baseTxt ?? '');
  const headOwn = makefileOwners(headTxt ?? '');
  const standLine = (owner, text) => {
    if (owner === 'comment' || owner === 'blank') return false;
    if (STAND_TARGET.test(owner)) return true;
    return owner === 'top' && /\b(VERSION|BINARY_NAME)\b/.test(text);
  };
  for (const m of diff.matchAll(/^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@/gm)) {
    const oldStart = Number(m[1]);
    const oldLen = m[2] === undefined ? 1 : Number(m[2]);
    const newStart = Number(m[3]);
    const newLen = m[4] === undefined ? 1 : Number(m[4]);
    for (let i = 0; i < oldLen; i++) {
      const idx = oldStart - 1 + i;
      if (standLine(baseOwn[idx] ?? 'top', baseLines[idx] ?? '')) return { stand: true, why: `строка ${oldStart + i} (цель ${baseOwn[idx]})` };
    }
    for (let i = 0; i < newLen; i++) {
      const idx = newStart - 1 + i;
      if (standLine(headOwn[idx] ?? 'top', headLines[idx] ?? '')) return { stand: true, why: `строка ${newStart + i} (цель ${headOwn[idx]})` };
    }
  }
  return { stand: false, why: '' };
}

// --- граф импортов фронтенда (RTR-13) ----------------------------------------------------

/**
 * Модели графа обеих ревизий: база (снимок ctx.from, если он есть) и head (снимок ctx.to или
 * рабочее дерево). Ошибка чтения или разбора — исключение: вызывающий выбирает все спеки.
 * specSources(абсолютный путь спека) — прямые ссылки спека на файлы src (кэш).
 */
async function frontGraph() {
  const mod = await import(pathToFileURL(path.join(ROOT, 'scripts/router/import-graph.mjs')));
  const fe = path.join(ROOT, 'frontend');
  const models = [];
  if (ctx.from) models.push({ side: 'база', model: mod.buildModel(fe, { reader: mod.gitReader(ROOT, ctx.from) }) });
  models.push({
    side: 'head',
    model: mod.buildModel(fe, { reader: ctx.to ? mod.gitReader(ROOT, ctx.to) : mod.fsReader(fe) })
  });
  const routes = new Set(models.flatMap((m) => [...m.model.routes]));
  const srcFiles = [...new Set(models.flatMap((m) => m.model.reader.files(path.join(fe, 'src'))))];
  const cache = new Map();
  const specSources = (specAbs) => {
    if (!cache.has(specAbs)) cache.set(specAbs, mod.specSources(specAbs, srcFiles, fe));
    return cache.get(specAbs);
  };
  /**
   * Хаб (D-10) по ревизиям present ([{ side, model }]): null — не хаб, иначе строка основания.
   * Оболочка (рендерится на каждой странице) — хаб всегда.
   */
  const hubOf = (abs, present) => {
    const shell = present.filter((m) => m.model.shell.has(abs)).map((m) => m.side);
    if (shell.length > 0) return `оболочка приложения (${shell.join(', ')})`;
    for (const m of present) {
      const pages = new Set(m.model.routesOf(abs).map((r) => mod.topRoute(r)));
      const total = m.model.topRoutes.size;
      if (total > 0 && (pages.size * 100) / total > hubPercent) {
        return `достижим с ${pages.size} из ${total} страниц (${m.side}) — больше ${hubPercent}%`;
      }
    }
    return null;
  };
  return { mod, models, routes, srcFiles, specSources, hubOf };
}

// --- локали: ключи и потребители (D-07) ------------------------------------------------------

/** Ключи вложенного JSON локали: Map 'раздел.ключ' → JSON значения (лист или массив). */
function flattenLocale(obj, prefix = '', out = new Map()) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k;
    if (v !== null && typeof v === 'object' && !Array.isArray(v)) flattenLocale(v, key, out);
    else out.set(key, JSON.stringify(v));
  }
  return out;
}

/**
 * Изменённые ключи файла локали между базой и head: [{ key, status }], status — изменён,
 * добавлен, удалён, сменил тип, переименован (старое имя → новое: оба ключа в списке).
 * Нет базы сравнения или файл не разобран — исключение (вызывающий расширяет выбор).
 */
function localeKeyChanges(file) {
  if (!ctx.from) throw new Error('нет базы сравнения ключей');
  const parse = (text, side) => {
    if (text === null) return new Map();
    try {
      const v = JSON.parse(text);
      if (v === null || typeof v !== 'object' || Array.isArray(v)) throw new Error('не объект');
      return flattenLocale(v);
    } catch (e) {
      throw new Error(`${side}: JSON не разобран (${String(e.message ?? e).split('\n', 1)[0]})`);
    }
  };
  const a = parse(gitShow(ctx.from, file), 'база');
  const b = parse(headText(file), 'head');
  const kindOf = (s) => (s.startsWith('{') ? 'object' : s.startsWith('[') ? 'array' : typeof JSON.parse(s));
  const out = [];
  const removed = [];
  const added = [];
  for (const k of new Set([...a.keys(), ...b.keys()])) {
    const va = a.get(k);
    const vb = b.get(k);
    if (va === vb) continue;
    if (va === undefined) added.push(k);
    else if (vb === undefined) removed.push(k);
    else out.push({ key: k, status: kindOf(va) === kindOf(vb) ? 'изменён' : 'сменил тип' });
  }
  for (const k of removed) {
    const to = added.find((n) => b.get(n) === a.get(k));
    out.push({ key: k, status: to ? `переименован в ${to}` : 'удалён' });
  }
  for (const k of added) {
    const from = removed.find((o) => a.get(o) === b.get(k));
    out.push({ key: k, status: from ? `переименован из ${from}` : 'добавлен' });
  }
  return out.sort((x, y) => (x.key < y.key ? -1 : x.key > y.key ? 1 : 0));
}

/** Текст без комментариев JS/CSS/разметки: использование ключа в комментарии не считается. */
function stripComments(text) {
  return text
    .replace(/<!--[\s\S]*?-->/g, ' ')
    .replace(/\/\*[\s\S]*?\*\//g, ' ')
    .replace(/(^|[^:\\'"`\w])\/\/[^\n]*/g, '$1');
}

/**
 * Использования ключей в файле: точные строковые литералы вида 'раздел.ключ' (в t('…'), $t("…")
 * и переданные как значение) и шаблоны `…${…}…`: внутри t(…) — любой шаблон, вне — только со
 * статическим началом, содержащим точку. Шаблон проверяется как регулярное выражение по ключу
 * (`${…}` — любая непустая часть): совпадающий статический префикс или суффикс выбирает файл,
 * шаблон без статической части (`${key}`) — любой ключ.
 */
function keyUsages(text) {
  const code = stripComments(text);
  const literals = new Set();
  for (const m of code.matchAll(/(['"`])([A-Za-z_][\w-]*(?:\.[\w-]+)+)\1/g)) literals.add(m[2]);
  const patterns = [];
  for (const m of code.matchAll(/`([^`]*\$\{[^`]*)`/g)) {
    const inT = /(?:^|[^\w$.])\$?t\(\s*$/.test(code.slice(Math.max(0, m.index - 12), m.index));
    const parts = m[1].split(/\$\{[^}]*\}/);
    if (!inT && !parts[0].includes('.')) continue;
    const esc = (s) => s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    patterns.push({ src: m[1], re: new RegExp(`^${parts.map(esc).join('.+')}$`) });
  }
  return { literals, patterns };
}

/**
 * Выбор по изменённым ключам локалей (D-07): ключ → файлы, где он используется (в базе для
 * прежнего имени, в head для нового) → маршруты по графу той же ревизии. Потребитель в оболочке —
 * pages без сужения и читающая канарейка. Спек, читающий файл локали и называющий ключ, —
 * напрямую. Ключ без потребителей выбор не расширяет: его ловит статика i18n, идёт базовый смоук.
 * Ошибка сравнения ключей — все спеки. Возвращает { reasons, all, routes, specs, shell }.
 */
function localeSelect(graph, localeFiles, specsAll) {
  const out = { reasons: [], all: false, routes: new Set(), specs: new Set(), shell: false };
  const keys = new Map(); // ключ → { statuses: Set, files: Set }
  for (const rel of localeFiles) {
    const file = `frontend/${rel}`;
    let changes;
    try {
      changes = localeKeyChanges(file);
    } catch (e) {
      out.all = true;
      out.reasons.push(`${file}: ключи не сравнить (${e.message}) — все роутерные спеки`);
      continue;
    }
    if (changes.length === 0) out.reasons.push(`${file}: ключи и значения не изменились — проверок не добавляет`);
    for (const c of changes) {
      if (!keys.has(c.key)) keys.set(c.key, { statuses: new Set(), files: new Set() });
      keys.get(c.key).statuses.add(c.status);
      keys.get(c.key).files.add(path.posix.basename(rel));
    }
  }
  if (keys.size === 0) return out;
  // Индекс использований по ревизиям: файлы графа (без стилей), литералы и шаблоны
  const index = graph.models.map((m) => {
    const rows = [];
    for (const f of m.model.graph.keys()) {
      if (/\.css$/.test(f)) continue;
      const text = m.model.reader.read(f);
      if (text !== null) rows.push({ file: f, ...keyUsages(text) });
    }
    return { side: m.side, model: m.model, rows };
  });
  const localeAbs = new Set(localeFiles.map((rel) => path.join(ROOT, 'frontend', rel)));
  const readers = specsAll.filter((s) => {
    const src = graph.specSources(path.join(ROOT, 'frontend', s));
    return [...localeAbs].some((a) => src.has(a));
  });
  const readerUsages = new Map(readers.map((s) => [s, keyUsages(readFileSync(path.join(ROOT, 'frontend', s), 'utf8'))]));
  for (const [key, info] of [...keys].sort((x, y) => (x[0] < y[0] ? -1 : 1))) {
    const where = new Map(); // файл → { sides: Set, routes: Set, shell: bool }
    for (const ix of index) {
      for (const row of ix.rows) {
        if (!row.literals.has(key) && !row.patterns.some((p) => p.re.test(key))) continue;
        const rel = path.relative(path.join(ROOT, 'frontend'), row.file);
        if (!where.has(rel)) where.set(rel, { sides: new Set(), routes: new Set(), shell: false });
        const w = where.get(rel);
        w.sides.add(ix.side);
        if (graph.hubOf(row.file, [ix])) w.shell = true;
        for (const r of ix.model.routesOf(row.file)) w.routes.add(r);
      }
    }
    const specHits = readers.filter((s) => {
      const u = readerUsages.get(s);
      return u.literals.has(key) || u.patterns.some((p) => p.re.test(key));
    });
    const head = `${[...info.files].sort().join(', ')}: ключ ${key} (${[...info.statuses].join(', ')})`;
    if (where.size === 0 && specHits.length === 0) {
      out.reasons.push(`${head}: потребителей нет ни в одной ревизии — выбор не расширяет (статика i18n, базовый смоук)`);
      continue;
    }
    const parts = [];
    for (const [rel, w] of [...where].sort((x, y) => (x[0] < y[0] ? -1 : 1))) {
      const sides = [...w.sides].join(', ');
      if (w.shell) {
        out.shell = true;
        parts.push(`${rel} (${sides}) — оболочка или хаб`);
      } else if (w.routes.size > 0) {
        for (const r of w.routes) out.routes.add(r);
        parts.push(`${rel} (${sides}) → страницы ${[...w.routes].join(', ')}`);
      } else {
        parts.push(`${rel} (${sides}) — не импортируется приложением`);
      }
    }
    for (const s of specHits) {
      out.specs.add(s);
      parts.push(`${s} читает ключ из файла локали`);
    }
    out.reasons.push(`${head}: ${parts.join('; ')}`);
  }
  return out;
}

// --- факты тестов и порядок -------------------------------------------------------------

/** Факты роутерных тестов: индекс git плюс новые (ещё не в индексе) изменённые тесты с диска. */
function buildFacts(changed) {
  let rows = testFacts(ROOT);
  const isGoTest = (f) => /^(internal|cmd)\/.*_test\.go$/.test(f) && !f.startsWith('internal/routertest/');
  const isSpec = (f) => /^frontend\/tests\/router\/[\w.-]+\.spec\.ts$/.test(f);
  const own = new Set(changed.filter((f) => SAFE_PATH.test(f) && (isGoTest(f) || isSpec(f))));
  // С --head изменённые тесты берутся из коммита слияния, а не из рабочей копии. Имена, которые
  // были только в рабочей копии, запоминаются: подобласть, назвавшая такой тест, при повторе
  // слитого PR не расширяет выбор до полного — в ревизии head этого теста ещё (или уже) нет
  if (ctx.to) {
    for (const r of rows) if (own.has(r.file) && r.kind === 'go') replacedTests.set(r.test, r.file);
    rows = rows.filter((r) => !own.has(r.file));
  }
  const have = new Set(rows.map((r) => r.file));
  for (const file of own) {
    if (have.has(file)) continue;
    const text = headText(file);
    if (text === null) continue;
    const { area } = areaOfText(text);
    if (isGoTest(file)) {
      if (text.split('\n', 1)[0].trim() !== '//go:build router') continue;
      for (const t of goTestFacts(text)) rows.push({ kind: 'go', file, pkg: path.posix.dirname(file), spec: null, area, ...t });
    } else {
      for (const t of specTestFacts(text)) rows.push({ kind: 'pw', file, pkg: null, spec: file.replace(/^frontend\//, ''), area, ...t });
    }
  }
  return rows;
}

/** Падения Go-тестов по истории за 30 дней: Map 'go:<Имя>' → число падений (с подтестами) или null. */
async function goFailScores(reasons) {
  try {
    const hist = await import(pathToFileURL(path.join(ROOT, 'scripts/router/history.mjs')));
    const rows = hist.readHistory();
    if (rows.length === 0) {
      reasons.push('порядок: история пуста — без сортировки по частоте падений');
      return null;
    }
    const scores = new Map();
    for (const [check, v] of hist.failFreq(rows, { days: 30 })) {
      if (!check.startsWith('go:')) continue;
      const base = check.split('/', 1)[0];
      scores.set(base, (scores.get(base) || 0) + v.fails);
    }
    reasons.push(`порядок: по частоте падений за 30 дней (строк истории ${rows.length})`);
    return scores;
  } catch (e) {
    reasons.push(`порядок: история не прочитана (${String(e.message ?? e).split('\n', 1)[0]}) — без сортировки по частоте падений`);
    return null;
  }
}

/** Решение по списку изменённых файлов. */
async function select(changed) {
  const reasons = [];
  const routerPkgs = routerPackages();
  const specsAll = listRouterSpecs();
  const res = {
    all: false,
    goSel: new Map(), // 'пакет:Тест' → true (затронутая область) | false (канарейка, зависимости)
    goFullOk: new Set(), // 'пакет:Тест', которым ярус full не мешает: область напрямую, свой файл, откат на пакет
    fullDropGo: [], // тесты яруса full, отсеянные из выбора (D-14)
    goAllPkgs: false, // все роутерные пакеты целиком (откат, мажор Go)
    specsDirect: new Set(), // спеки затронутых областей, правок и маршрутов — идут первыми
    specsOther: new Set(), // канарейки и зависимости
    specsForced: new Set(), // выбраны не по маршрутам: без сужения обхода
    specsFullOk: new Set(), // спеки, чья область затронута напрямую или изменённые сами: тесты full идут (D-14)
    pwFull: [], // факты Playwright-тестов яруса full: { spec } — для числа отсеянных
    grepRoutes: null, // null — без сужения
    areas: new Set(), // затронутые области
    harness: false,
    deps: false,
    kernelTouched: false, // затронут код запуска ядра или watchdog (KERNEL_PATHS)
    kind: 'смоук',
    stop: [], // причины остановки выбора (D-24): непусто — код 3, stdout пуст
    reasons
  };
  const escalate = (why) => {
    res.all = true;
    reasons.push(`всё: ${why}`);
  };
  const warnUnknown = (file) =>
    reasons.push(`предупреждение: неизвестный файл ${file} — только смоук; добавьте путь в areas.json или в список не влияющих`);

  // Обязательный полный выбор (D-23) — только три основания: механизм выбора, формат карты,
  // признак shared. Строки «обязательный полный выбор: <основание>» читает повтор выбора (D-29).
  const required = [];
  for (const f of changed) {
    if (SAFE_PATH.test(f) && SELECTOR_SELF.some((re) => re.test(f))) required.push(`механизм выбора (${f})`);
  }
  // Правка карты областей: две версии сравниваются по смыслу (D-21), выбор — ниже, в разделе
  // областей. Карты нельзя надёжно сравнить — остановка (D-24), если её не перекрывает
  // обязательный полный выбор того же диффа.
  const cmp = changed.includes(AREAS_FILE) ? compareMaps() : null;
  const mapRequired = [];
  if (cmp) {
    if (cmp.appeared) mapRequired.push('формат карты (карта появилась: в базе её нет)');
    if (cmp.diff) {
      if (cmp.diff.format.length > 0) mapRequired.push(`формат карты (${cmp.diff.format.join('; ')})`);
      for (const s of cmp.diff.shared) {
        reasons.push(`карта: признак shared области ${s.area}: ${s.from} → ${s.to}`);
        mapRequired.push(`признак shared (${s.area}: ${s.from} → ${s.to})`);
      }
    }
    required.push(...mapRequired);
  }
  for (const r of required) reasons.push(`обязательный полный выбор: ${r}`);
  for (const r of mapRequired) escalate(`${AREAS_FILE}: ${r}`);
  /** Остановка выбора (D-24): без обязательного полного выбора — код 3, иначе — только пометка. */
  const stopOr = (why) => {
    if (required.length > 0) {
      reasons.push(`карта: не сравнить (${why}) — перекрыто обязательным полным выбором`);
      return false;
    }
    res.stop.push(`${AREAS_FILE}: ${why}`);
    reasons.push(`стоп: ${AREAS_FILE}: ${why}`);
    return true;
  };
  if (cmp) {
    let stopped = false;
    for (const p of cmp.problems) stopped = stopOr(p) || stopped;
    if (stopped) return { res, routerPkgs, specsAll };
  }

  // Карта областей и факты тестов: без них имена не выбрать — расширение до полного выбора
  let areaMap;
  let facts;
  try {
    areaMap = loadAreas(ROOT).areas;
    facts = buildFacts(changed);
  } catch (e) {
    escalate(`карта областей или факты тестов не получены (${String(e.message ?? e).split('\n', 1)[0]})`);
    return { res, routerPkgs, specsAll };
  }
  // Метки существующих тестов ссылаются на область, которой нет в head (D-24): карту и разметку
  // исправляют вместе. В гейте факты и карта head — одно дерево, проверяются все метки; при
  // повторе слитого PR (--head) факты из рабочей копии — только метки областей, удалённых диффом.
  if (cmp?.diff) {
    const gone = (area) => area && !(area in cmp.head.areas) && (!ctx.to || cmp.diff.removed.includes(area));
    const stale = [...new Set(facts.filter((r) => gone(r.area)).map((r) => `${r.file}: ${r.area}`))].sort();
    if (stale.length > 0 && stopOr(`метки router-area ссылаются на области, которых нет в карте: ${stale.join(', ')}`)) {
      return { res, routerPkgs, specsAll };
    }
  }
  const areasOf = (file) => Object.keys(areaMap).filter((n) => areaMap[n].paths.some((g) => matchGlob(g, file)));
  const goRows = facts.filter((r) => r.kind === 'go');
  const specOfRow = (r) => r.spec;
  // fullOk — основание выбрать и тест яруса full (D-14): область затронута напрямую, изменён сам
  // тест или откат на пакет целиком; канарейки, зависимости и общий код его не дают
  const addGo = (pkg, test, direct, fullOk = false) => {
    const key = `${pkg}:${test}`;
    res.goSel.set(key, direct || res.goSel.get(key) === true);
    if (fullOk) res.goFullOk.add(key);
  };
  const addWholePkg = (pkg, direct) => {
    for (const r of goRows) if (r.pkg === pkg) addGo(r.pkg, r.test, direct, true);
  };
  /** База файла для сравнения по объявлениям: текст, null (файла в базе нет) или undefined (нет базы). */
  const declBase = (file) => (ctx.from ? gitShow(ctx.from, file) : undefined);
  /**
   * Тесты TestRouter пакета pkg из замыкания по объявлениям (seed — имена): добавляет в выбор
   * (прямое затрагивание, ярус full разрешён). own — тесты своего файла (уже выбраны, в N не
   * идут). Возвращает { n, missing } или { wide } — пакет не разобран.
   */
  const addDeclConsumers = (pkg, seed, own) => {
    const ix = goFilesIndex(goPkgTestFiles(pkg));
    if (!ix.ok) return { wide: ix.why };
    const set = goDeclClosure(ix.decls, seed);
    const facts = new Set(goRows.filter((r) => r.pkg === pkg).map((r) => r.test));
    let n = 0;
    const missing = [];
    for (const t of goRouterTestsIn(ix.decls, set)) {
      if (own.has(t)) continue;
      if (facts.has(t)) {
        addGo(pkg, t, true, true);
        n++;
      } else {
        missing.push(t);
      }
    }
    return { n, missing };
  };
  /**
   * Изменённый `_test.go` роутерного пакета по объявлениям (D-32): изменённые объявления файла →
   * тесты TestRouter пакета, которые их используют. Неразобранное — пакет целиком с причиной.
   */
  const selectByDecls = (file, dir, own) => {
    const base = declBase(file);
    const d = base === undefined ? { changed: new Set(), wide: 'нет базы сравнения' } : goDeclDiff(base, headText(file));
    let r = d.wide ? { wide: d.wide } : null;
    if (!r && d.changed.size > 0) r = addDeclConsumers(dir, d.changed, own);
    if (r && r.wide) {
      addWholePkg(dir, true);
      reasons.push(`${file}: по объявлениям — пакет ${dir} целиком (${r.wide})`);
      return;
    }
    if (d.changed.size === 0) {
      reasons.push(`${file}: по объявлениям — объявления не изменены (комментарии, импорты или пробелы)`);
      return;
    }
    if (r.missing.length > 0) reasons.push(`${file}: по объявлениям — ${r.missing.join(', ')} нет в фактах — пропущен`);
    reasons.push(`${file}: по объявлениям — изменены ${[...d.changed].join(', ')}; тестов TestRouter, использующих их: ${r.n}`);
  };
  const specSelectable = (s) => specsAll.includes(s);

  /** Тесты области: Go и спеки (факты + поле tests). Нет теста из tests — расширение. */
  const addAreaTests = (name, direct) => {
    const a = areaMap[name];
    // общий код (shared) задевает все области: тесты full его области напрямую не выбираются
    const fullOk = direct && !a.shared;
    let go = 0;
    const specs = new Set();
    for (const r of goRows) if (r.area === name) { addGo(r.pkg, r.test, direct, fullOk); go++; }
    for (const r of facts) if (r.kind === 'pw' && r.area === name && specOfRow(r)) specs.add(specOfRow(r));
    for (const t of a.tests || []) {
      if (t.startsWith('go:')) {
        const hit = goRows.filter((r) => r.test === t.slice(3));
        if (hit.length === 0) escalate(`область ${name}: тест ${t} из поля tests не найден`);
        for (const r of hit) { addGo(r.pkg, r.test, direct, fullOk); go++; }
      } else {
        specs.add(t);
      }
    }
    const pick = [...specs].filter(specSelectable);
    for (const s of pick) {
      (direct ? res.specsDirect : res.specsOther).add(s);
      res.specsForced.add(s);
      if (fullOk) res.specsFullOk.add(s);
    }
    return { go, specs: pick.length };
  };
  /**
   * Тесты подобласти по именам из её tests (RTR-14): прямое затрагивание — тесты идут первыми,
   * ярус full разрешён (fullOk); канарейки и признак shared родителя его не дают. Нет теста —
   * расширение до полного выбора, как у поля tests области.
   */
  const addSubareaTests = (name, sub) => {
    let go = 0;
    const specs = new Set();
    for (const t of areaMap[name].subareas[sub].tests) {
      if (t.startsWith('go:')) {
        const hit = goRows.filter((r) => r.test === t.slice(3));
        if (hit.length === 0 && replacedTests.has(t.slice(3))) {
          // Повтор слитого PR (--head): файл теста изменён в диффе, его тесты выбраны по правке
          // файла из ревизии head, а этого имени в head нет — запускать нечего
          reasons.push(
            `подобласть ${name}/${sub}: ${t} нет в ревизии head (${replacedTests.get(t.slice(3))} изменён в диффе) — пропущен`
          );
          continue;
        }
        if (hit.length === 0) escalate(`подобласть ${name}/${sub}: тест ${t} из поля tests не найден`);
        for (const r of hit) { addGo(r.pkg, r.test, true, true); go++; }
      } else {
        specs.add(t);
      }
    }
    const pick = [...specs].filter(specSelectable);
    for (const s of pick) {
      res.specsDirect.add(s);
      res.specsForced.add(s);
      res.specsFullOk.add(s);
    }
    return { go, specs: pick.length };
  };
  /** Канарейки: по одному Go-тесту и спеку каждой области (D-11). */
  const addCanaries = (why) => {
    for (const [name, a] of Object.entries(areaMap)) {
      if (a.canary.go) {
        const hit = goRows.filter((r) => r.test === a.canary.go);
        if (hit.length === 0) escalate(`канарейка ${a.canary.go} области ${name} не найдена`);
        for (const r of hit) if (!res.goSel.has(`${r.pkg}:${r.test}`)) addGo(r.pkg, r.test, false);
      }
      if (a.canary.spec && specSelectable(a.canary.spec)) {
        res.specsForced.add(a.canary.spec);
        if (!res.specsDirect.has(a.canary.spec)) res.specsOther.add(a.canary.spec);
      }
    }
    reasons.push(`канарейки всех областей (${why})`);
  };

  const touchedSrc = [];
  // Области, отмеченные только файлами frontend/src: в RT_AREAS и объяснении, но тесты области
  // целиком не выбираются — фронтенд классифицирует граф импортов (D-09)
  const frontMarked = new Set();
  const touchedSpecs = new Set();
  const goChanged = new Set(); // каталоги изменённых Go-пакетов без области (откат на пакет)
  // Каталоги, попавшие в goChanged откатом по объявлениям (только internal/routertest): у
  // зависимых пакетов своя строка причины, отличимая от прежнего правила (D-32)
  const goChangedByDecls = new Set();
  const rtFiles = []; // изменённые .go internal/routertest — выбор по объявлениям
  const goTestFiles = new Set(); // изменённые _test.go
  const depFiles = new Set();
  let makefile = false;
  let pwAll = false;
  const cfgStops = []; // конфигурации TypeScript без определимого потребителя: { file, why }
  let sharedHit = false;
  // Области, чьи тесты выбираются целиком (RT_AREAS шире: подобласти и отметки фронтенда)
  const wholeAreas = new Set();
  // Затронутые подобласти: '<родитель>/<подобласть>'
  const subHits = new Set();
  /**
   * Файл продукта в областях hit: подобласть, если её глоб находит файл, иначе область целиком
   * (файл родителя вне подобластей — до заполнения раздела, страж требует покрытия).
   */
  const markProduct = (file, hit, shared) => {
    for (const n of hit) {
      res.areas.add(n);
      if (areaMap[n].shared) sharedHit = true;
      const tag = shared && areaMap[n].shared ? ' (общий код)' : '';
      const subs = subareasOf(areaMap, n, file);
      if (subs.length > 0) {
        for (const s of subs) {
          subHits.add(`${n}/${s}`);
          reasons.push(`${file}: подобласть ${n}/${s}${tag}`);
        }
        continue;
      }
      wholeAreas.add(n);
      reasons.push(
        areaMap[n].subareas
          ? `${file}: область ${n}${tag} — ни одна подобласть не находит файл, тесты области целиком`
          : `${file}: область ${n}${tag}`
      );
    }
  };

  /**
   * Выбор по смысловому сравнению карт (D-21, D-23, D-25): изменённая привязка путей выбирает
   * тесты области по правилам прямой правки (метки базы и head, поле tests, ярус full — если
   * область не shared, канарейки — если shared); записи tests и подобластей — свои тесты, если
   * они существуют; добавленная или удалённая область — тесты по обеим картам. Каждое изменение —
   * строка «карта: …» с числом выбранных им проверок; повторы с выбором по продукту не считаются
   * дважды (общие множества res).
   */
  const applyMapDiff = (c) => {
    const d = c.diff;
    const base = c.base.areas;
    const head = c.head.areas;
    let baseMarks = null;
    const marksAtBase = () => (baseMarks ??= areaMarksAt(c.baseRef));
    const acc = () => ({ go: new Set(), specs: new Set(), missing: [] });
    const merged = (list) => {
      const a = acc();
      for (const x of list) {
        x.go.forEach((k) => a.go.add(k));
        x.specs.forEach((s) => a.specs.add(s));
        a.missing.push(...x.missing.filter((m) => !a.missing.includes(m)));
      }
      return a;
    };
    const tally = (a) =>
      `Go ${a.go.size}, спеков ${a.specs.size}` + (a.missing.length > 0 ? ` (не выбраны: ${a.missing.join(', ')})` : '');
    const pickGo = (r, fullOk, a) => {
      addGo(r.pkg, r.test, true, fullOk);
      a.go.add(`${r.pkg}:${r.test}`);
    };
    const pickSpec = (s, fullOk, a) => {
      if (!specSelectable(s)) return false;
      res.specsDirect.add(s);
      res.specsForced.add(s);
      if (fullOk) res.specsFullOk.add(s);
      a.specs.add(s);
      return true;
    };
    // strict — запись head: нет теста — расширение, как у поля tests области; иначе (запись
    // базы, удалённая) — выбирается, только если тест ещё существует
    const pickEntries = (list, fullOk, strict, where, a) => {
      for (const t of list) {
        if (!t.startsWith('go:')) {
          if (!pickSpec(t, fullOk, a)) a.missing.push(t);
          continue;
        }
        const hit = goRows.filter((r) => r.test === t.slice(3));
        if (hit.length === 0) {
          if (strict && !replacedTests.has(t.slice(3))) escalate(`${where}: тест ${t} из карты не найден`);
          a.missing.push(t);
          continue;
        }
        for (const r of hit) pickGo(r, fullOk, a);
      }
    };
    const sharedOf = (name) => Boolean(head[name]?.shared || base[name]?.shared);
    const touch = (name) => {
      res.areas.add(name);
      if (sharedOf(name)) sharedHit = true;
    };
    const whole = new Map();
    // Тесты области целиком по обеим картам: метки head (факты) и базы, поле tests head и базы
    const wholeArea = (name) => {
      if (whole.has(name)) return whole.get(name);
      const a = acc();
      const fullOk = !sharedOf(name);
      touch(name);
      const marked = new Set();
      for (const [file, area] of marksAtBase()) if (area === name) marked.add(file);
      for (const r of facts) {
        if (r.area !== name && !marked.has(r.file)) continue;
        if (r.kind === 'go') pickGo(r, fullOk, a);
        else if (r.spec) pickSpec(r.spec, fullOk, a);
      }
      const headTests = head[name]?.tests || [];
      pickEntries(headTests, fullOk, true, `область ${name}`, a);
      pickEntries(
        (base[name]?.tests || []).filter((t) => !headTests.includes(t)),
        fullOk,
        false,
        `область ${name}`,
        a
      );
      whole.set(name, a);
      return a;
    };
    const signed = (added, removed) => [...added.map((x) => `+${x}`), ...removed.map((x) => `−${x}`)].join(' ');

    for (const m of d.paths) {
      const names = [...new Set([...m.from, ...m.to])].sort();
      const a = merged(names.map(wholeArea));
      const what =
        m.from.length > 0 && m.to.length > 0
          ? `перенесён: ${m.from.join(', ')} → ${m.to.join(', ')}`
          : m.to.length > 0
            ? `добавлен: ${m.to.join(', ')}`
            : `удалён: ${m.from.join(', ')}`;
      reasons.push(`карта: шаблон ${m.pattern} ${what} — ${tally(a)}`);
    }
    for (const t of d.tests) {
      const a = acc();
      const fullOk = !sharedOf(t.area);
      touch(t.area);
      pickEntries(t.added, fullOk, true, `область ${t.area}`, a);
      pickEntries(t.removed, fullOk, false, `область ${t.area}`, a);
      reasons.push(`карта: область ${t.area}, tests ${signed(t.added, t.removed)} — ${tally(a)}`);
    }
    for (const s of d.subareas) {
      const a = acc();
      const key = `${s.area}/${s.sub}`;
      touch(s.area);
      let what;
      if (s.kind === 'added') {
        pickEntries(s.headTests, true, true, `подобласть ${key}`, a);
        what = 'добавлена';
      } else if (s.kind === 'removed') {
        pickEntries(s.baseTests, true, false, `подобласть ${key}`, a);
        what = 'удалена';
      } else {
        const parts = [];
        if (s.paths.added.length + s.paths.removed.length > 0) {
          // изменена привязка путей: тесты подобласти обеих карт
          pickEntries(s.headTests, true, true, `подобласть ${key}`, a);
          pickEntries(s.baseTests.filter((t) => !s.headTests.includes(t)), true, false, `подобласть ${key}`, a);
          parts.push(`paths ${signed(s.paths.added, s.paths.removed)}`);
        } else {
          pickEntries(s.tests.added, true, true, `подобласть ${key}`, a);
          pickEntries(s.tests.removed, true, false, `подобласть ${key}`, a);
        }
        if (s.tests.added.length + s.tests.removed.length > 0) parts.push(`tests ${signed(s.tests.added, s.tests.removed)}`);
        what = parts.join(', ');
      }
      reasons.push(`карта: подобласть ${key} ${what} — ${tally(a)}`);
    }
    // Канарейки (D-22): добавленная — новая, замена — прежняя (если тест есть) и новая,
    // удалённая (null с reason) — прежняя, если тест есть. Удаление без reason карта head не
    // проходит validateAreas — до сюда не доходит (остановка). Канарейка — не прямое
    // затрагивание: ярус full ей не положен, область в RT_AREAS не отмечается.
    for (const ch of d.canary) {
      const a = acc();
      if (ch.kind === 'go') {
        for (const name of [ch.to, ch.from]) {
          if (!name) continue;
          const hit = goRows.filter((r) => r.test === name);
          if (hit.length === 0) {
            // новая канарейка не найдена — расширение, как в addCanaries
            if (name === ch.to) escalate(`канарейка ${name} области ${ch.area} не найдена`);
            a.missing.push(`go:${name}`);
            continue;
          }
          for (const r of hit) {
            addGo(r.pkg, r.test, false);
            a.go.add(`${r.pkg}:${r.test}`);
          }
        }
      } else {
        for (const s of [ch.to, ch.from]) {
          if (!s) continue;
          if (!specSelectable(s)) {
            a.missing.push(s.endsWith('/core-switch.spec.ts') ? `${s} — спек матрицы ядер, идёт в каждой ветви` : s);
            continue;
          }
          res.specsForced.add(s);
          if (!res.specsDirect.has(s)) res.specsOther.add(s);
          a.specs.add(s);
        }
      }
      reasons.push(
        `карта: канарейка ${ch.kind === 'go' ? 'Go' : 'спек'} области ${ch.area}: ${ch.from ?? 'нет'} → ${ch.to ?? 'нет'} — ${tally(a)}`
      );
    }
    for (const name of d.added) reasons.push(`карта: область ${name} добавлена — ${tally(wholeArea(name))}`);
    for (const name of d.removed) reasons.push(`карта: область ${name} удалена — ${tally(wholeArea(name))}`);
    if (d.harness) {
      res.harness = true;
      reasons.push('карта: изменён блок harness — обвязка стенда (RT_HARNESS=1)');
    }
    const substantive =
      d.format.length + d.shared.length + d.paths.length + d.tests.length + d.subareas.length + d.canary.length +
        d.added.length + d.removed.length >
        0 || d.harness;
    if (!substantive) {
      reasons.push(
        `карта: изменены только названия или порядок — выбор не расширяется${d.cosmetic.length > 0 ? ` (${d.cosmetic.slice(0, 8).join('; ')}${d.cosmetic.length > 8 ? '; …' : ''})` : ''}`
      );
    } else if (d.cosmetic.length > 0) {
      reasons.push(`карта: без влияния на выбор — ${d.cosmetic.slice(0, 8).join('; ')}${d.cosmetic.length > 8 ? '; …' : ''}`);
    }
  };

  for (const file of changed) {
    if (!SAFE_PATH.test(file)) {
      escalate(`небезопасное имя файла ${JSON.stringify(file)}`);
      continue;
    }
    // Признак не отменяет разбор файла: он идёт дальше как обычно (выбор тестов)
    if (!file.endsWith('_test.go') && KERNEL_PATHS.some((re) => re.test(file))) {
      res.kernelTouched = true;
      reasons.push(`${file}: код запуска или перезапуска ядра либо watchdog — полное окно смоука`);
    }
    if (SELECTOR_SELF.some((re) => re.test(file))) {
      escalate(`${file}: правка самого выбора`);
      continue;
    }
    // Карта областей: выбор по сравнению двух версий (compareMaps), не обвязка и не полный
    if (file === AREAS_FILE) continue;
    // Конфигурация TypeScript фронтенда (D-26): по конечным потребителям базы и head. Только
    // тесты — обвязка; есть сборка панели — обвязка и все браузерные спеки (прежняя широкая
    // проверка сборочных файлов FRONTEND_ALL остаётся отдельно). Правки продукта того же диффа
    // разбираются своими ветками — выборы объединяются.
    if (isTsConfig(file)) {
      const c = tsConfigConsumers(file);
      const kinds = ['build', 'tests'].filter((k) => c.kinds.has(k));
      const who = kinds.map((k) => TS_CONSUMER_LABEL[k]).join(', ');
      // Неразрешимая связь или потребителя нет ни на одной ревизии (D-28) — остановка после
      // разбора всех файлов: ни сужения, ни полного выбора
      if (c.issues.length > 0 || kinds.length === 0) {
        const list = c.issues.length > 0 ? c.issues : [{ file, why: 'потребитель не определён' }];
        for (const x of list) {
          if (!cfgStops.some((y) => y.file === x.file && y.why === x.why)) cfgStops.push(x);
        }
        continue;
      }
      res.harness = true;
      if (c.kinds.has('build')) {
        pwAll = true;
        reasons.push(`конфигурация: ${file} — потребители: ${who} — обвязка и все браузерные спеки`);
      } else {
        reasons.push(`конфигурация: ${file} — потребители: ${who} — обвязка`);
      }
      continue;
    }
    if (HARNESS.some((re) => re.test(file))) {
      res.harness = true;
      reasons.push(`${file}: обвязка стенда — набор обвязки, полный выбор не нужен`);
      continue;
    }
    if (DEPS.some((re) => re.test(file))) {
      depFiles.add(file);
      continue;
    }
    if (file === 'Makefile') {
      makefile = true;
      continue;
    }
    // Контракты и данные тестов — области contract-* (поле tests). Подобласти общего кода
    // тоже несут tests, но вложенные: их разбирает ветка файлов продукта ниже.
    const contract = areasOf(file).filter((n) => n.startsWith('contract-') && areaMap[n].tests);
    if (contract.length > 0) {
      for (const n of contract) {
        res.areas.add(n);
        wholeAreas.add(n);
        reasons.push(`${file}: область ${n} — проверки из поля tests`);
      }
      continue;
    }
    if (/(^|\/)testdata\//.test(file)) {
      warnUnknown(file);
      continue;
    }
    if (IGNORED.some((re) => re.test(file))) {
      reasons.push(`${file}: на роутерные проверки не влияет`);
      continue;
    }
    if (/^frontend\/tests\/router\/[\w.-]+\.spec\.ts$/.test(file)) {
      if (file.endsWith('/core-switch.spec.ts')) {
        reasons.push(`${file}: спек матрицы ядер, запускается в каждой ветви`);
      } else if (headText(file) !== null) {
        touchedSpecs.add(`tests/router/${path.posix.basename(file)}`);
        reasons.push(`${file}: изменён сам спек`);
      } else {
        reasons.push(`${file}: спек удалён`);
      }
      continue;
    }
    if (FRONTEND_ALL.some((re) => re.test(file))) {
      pwAll = true;
      reasons.push(`${file}: влияет на сборку панели — все роутерные спеки`);
      continue;
    }
    if (file.startsWith('frontend/src/') || FRONTEND_SHELL_STATIC.some((re) => re.test(file))) {
      touchedSrc.push(file.slice('frontend/'.length));
      for (const n of areasOf(file)) {
        frontMarked.add(n);
        reasons.push(`${file}: область ${n} (выбор — по графу импортов, не всеми тестами области)`);
      }
      continue;
    }
    if (file.startsWith('frontend/')) {
      warnUnknown(file);
      continue;
    }
    if (/^(internal|cmd)\//.test(file) || /^[\w.-]+\.go$/.test(file)) {
      if (file.endsWith('_test.go')) {
        goTestFiles.add(file);
        continue;
      }
      const rtGo = file.startsWith(`${RT_DIR}/`) && file.endsWith('.go');
      if (file.startsWith(`${RT_DIR}/`)) {
        res.harness = true;
        reasons.push(
          rtGo
            ? `${file}: помощники роутерных тестов — обвязка; тесты зависимых пакетов — по объявлениям`
            : `${file}: помощники роутерных тестов — обвязка; тесты пакетов, зависящих от них, выбираются как раньше`
        );
      }
      const hit = areasOf(file);
      if (hit.length > 0) {
        // Найденная привязка (область или подобласть) — без отката на пакет и go list
        markProduct(file, hit, true);
        continue;
      }
      if (rtGo) {
        // Разбор по объявлениям — после разделов областей и изменённых тестов (D-32)
        rtFiles.push(file);
        continue;
      }
      const dir = goDirOf(file);
      if (dir === null) {
        // Данные без пакета рядом: владельца не определить — все роутерные пакеты
        res.goAllPkgs = true;
        reasons.push(`${file}: нет пакета Go рядом — все роутерные пакеты`);
      } else {
        goChanged.add(dir);
        reasons.push(`${file}: нет области — пакет ${dir} целиком`);
      }
      continue;
    }
    // прочие файлы продукта, покрытые картой (scripts/setup.sh)
    const hit = areasOf(file);
    if (hit.length > 0) {
      markProduct(file, hit, false);
      continue;
    }
    warnUnknown(file);
  }

  // Конфигурации TypeScript, чьих потребителей нельзя определить (D-28): остановка — res.stop
  // даёт код EXIT_STOP в main; обязательный полный выбор того же диффа её перекрывает.
  if (cfgStops.length > 0) {
    if (required.length > 0) {
      for (const x of cfgStops) {
        reasons.push(`конфигурация: не определить (${x.file}: ${x.why}) — перекрыто обязательным полным выбором`);
      }
    } else {
      for (const x of cfgStops) {
        res.stop.push(`${x.file}: ${x.why}`);
        reasons.push(`стоп: ${x.file}: ${x.why}`);
      }
      return { res, routerPkgs, specsAll };
    }
  }

  // --- Makefile ---------------------------------------------------------------
  if (makefile && !res.all) {
    const mk = makefileStand();
    if (mk.stand) {
      res.harness = true;
      reasons.push(`Makefile: ${mk.why || 'изменены цели стенда или VERSION'} — обвязка стенда`);
    } else {
      reasons.push('Makefile: изменены цели и переменные вне стенда — на роутер не влияет');
    }
  }

  // --- зависимости (D-13) -----------------------------------------------------
  if (depFiles.size > 0 && !res.all) {
    res.deps = true;
    const npmTouched = depFiles.has('frontend/package.json') || depFiles.has('frontend/package-lock.json');
    const goTouched = depFiles.has('go.mod') || depFiles.has('go.sum');
    const files = [...depFiles].join(', ');
    // патч и минор: pages.spec.ts без сужения и канарейки всех областей
    const pages = 'tests/router/pages.spec.ts';
    if (specSelectable(pages)) {
      res.specsOther.add(pages);
      res.specsForced.add(pages);
    }
    addCanaries(`зависимости: ${files}`);
    reasons.push(`${files}: зависимости — смоук, pages без сужения и канарейки`);
    if (npmTouched) {
      const npm = npmMajorReason(depFiles);
      if (npm.major) {
        const why = npm.major;
        specsAll.forEach((s) => {
          res.specsOther.add(s);
          res.specsForced.add(s);
        });
        reasons.push(`npm: мажорное обновление или смена Svelte/Vite/Playwright (${why}) — канарейки и все спеки`);
      } else {
        // D-11: патч и минор любого пакета, в том числе svelte, vite и @playwright/test
        reasons.push(
          `npm: патч или минор без смены мажора (${npm.changed.join(', ') || 'версии пакетов не изменились, только lock'}) — смоук, pages и канарейки областей`
        );
      }
    }
    if (depFiles.has('go.mod')) {
      const why = goMajorReason();
      if (why) {
        res.goAllPkgs = true;
        reasons.push(`go.mod: мажорное обновление или смена Go (${why}) — канарейки и все Go-тесты`);
      }
    } else if (goTouched) {
      reasons.push('go.sum: без go.mod — патч/минор');
    }
  }

  // --- области: тесты по именам -----------------------------------------------
  if (!res.all) {
    for (const name of [...wholeAreas].sort()) {
      const n = addAreaTests(name, true);
      reasons.push(`область ${name}: тестов Go ${n.go}, спеков ${n.specs}`);
    }
    for (const key of [...subHits].sort()) {
      const [name, sub] = key.split('/');
      const n = addSubareaTests(name, sub);
      reasons.push(`подобласть ${key}: тестов Go ${n.go}, спеков ${n.specs}`);
    }
    if (cmp?.diff) {
      try {
        applyMapDiff(cmp);
      } catch (e) {
        escalate(`${AREAS_FILE}: выбор по сравнению карт не получен (${firstLine(e)})`);
      }
    }
    if (sharedHit && !res.all) addCanaries('правка общего кода');
    // Отметка фронтенда: только RT_AREAS и объяснение, тесты выбирает граф импортов
    for (const name of [...frontMarked].sort()) {
      if (res.areas.has(name)) continue;
      res.areas.add(name);
      reasons.push(`область ${name}: отмечена правкой frontend/src — тесты области целиком не выбираются`);
    }
  }

  // --- изменённые роутерные тесты ---------------------------------------------
  if (!res.all) {
    for (const file of [...goTestFiles].sort()) {
      const dir = path.posix.dirname(file);
      const fileRows = goRows.filter((r) => r.file === file);
      if (headText(file) === null) {
        // Удалён: роутерные тесты файла исчезли — запускать нечего; иначе (помощники, обычный
        // тест) — по объявлениям с пустым head, база неизвестна — пакет целиком
        const baseText = ctx.from ? gitShow(ctx.from, file) : null;
        const tagged = baseText !== null && baseText.split('\n', 1)[0].trim() === '//go:build router';
        if (tagged && goTestFacts(baseText).length > 0) {
          reasons.push(`${file}: файл роутерных тестов удалён — запускать нечего`);
          continue;
        }
      }
      if (fileRows.length > 0) {
        // Тесты своего файла — по именам из фактов, строка области для этого не нужна (страж
        // check-router-areas.js в CI её всё равно требует); затем потребители изменённых
        // объявлений файла из других файлов пакета (D-32)
        for (const r of fileRows) addGo(r.pkg, r.test, true, true);
        const untagged = fileRows[0].area ? '' : ', без строки router-area';
        reasons.push(`${file}: изменён роутерный тест — тесты файла (${fileRows.length}${untagged})`);
        selectByDecls(file, dir, new Set(fileRows.map((r) => r.test)));
      } else if (routerPkgs.includes(dir)) {
        // Помощники, обычный тест или удалённый файл без тестов TestRouter (D-32): тесты пакета,
        // использующие изменённые объявления файла
        selectByDecls(file, dir, new Set());
      } else {
        reasons.push(`${file}: изменён обычный тест, роутерных тестов в пакете ${dir} нет`);
      }
    }
  }

  // --- internal/routertest: по объявлениям (D-32) -------------------------------
  // Изменённые объявления routertest → замыкание внутри routertest (изменённый тип доходит до
  // функций, которые его возвращают или содержат) → в каждом роутерном пакете объявления
  // `_test.go`, где есть `<имя импорта>.<Экспортируемое>` или селектор `.<Метод>` (метод вызывают на
  // значении без имени пакета; одноимённые методы других типов только расширяют выбор) → замыкание
  // пакета → его тесты TestRouter. Неразобранное — прежний откат через go list: зависимые пакеты
  // целиком, у каждого своя строка причины.
  if (!res.all) {
    for (const file of rtFiles) {
      const fallback = (why) => {
        goChanged.add(RT_DIR);
        goChangedByDecls.add(RT_DIR);
        reasons.push(`${file}: нет области — пакет ${RT_DIR} целиком`);
        reasons.push(`${file}: по объявлениям — зависимые пакеты целиком (${why})`);
      };
      const base = declBase(file);
      if (base === undefined) {
        fallback('нет базы сравнения');
        continue;
      }
      const d = goDeclDiff(base, headText(file));
      if (d.wide) {
        fallback(d.wide);
        continue;
      }
      if (d.changed.size === 0) {
        reasons.push(`${file}: по объявлениям — объявления не изменены; тестов TestRouter в зависимых пакетах не выбрано`);
        continue;
      }
      const rix = goFilesIndex(goDirFiles(RT_DIR).filter((f) => !f.file.endsWith('_test.go')));
      if (!rix.ok) {
        fallback(rix.why);
        continue;
      }
      const inRt = goDeclClosure(rix.decls, d.changed);
      const exported = new Set();
      const methods = new Set();
      for (const x of rix.decls) {
        for (const n of x.names) {
          if (!inRt.has(n)) continue;
          if (x.kind === 'func' && x.receiver) methods.add(n);
          else if (/^[A-Z]/.test(n)) exported.add(n);
        }
      }
      // удалённое из routertest имя: вид неизвестен — ищется и через пакет, и через селектор
      for (const n of d.changed) {
        if (/^[A-Z]/.test(n) && !rix.decls.some((x) => x.names.includes(n))) {
          exported.add(n);
          methods.add(n);
        }
      }
      const alt = (s) => [...s].sort().join('|');
      const methodRe = methods.size > 0 ? new RegExp(`\\.[ \\t]*(?:${alt(methods)})\\b`) : null;
      const rtImport = `${modulePath()}/${RT_DIR}`;
      let total = 0;
      const hitPkgs = [];
      for (const pkg of routerPkgs) {
        let whole = null;
        const decls = [];
        const seeds = new Set();
        for (const f of goPkgTestFiles(pkg)) {
          const ix = goDeclIndex(f.text);
          if (!ix.ok) {
            whole = `${f.file}: ${ix.why}`;
            break;
          }
          const imp = ix.imports.find((i) => i.path === rtImport);
          const alias = imp ? imp.name || 'routertest' : null;
          if (alias === '.' || alias === '_') {
            whole = `${f.file}: ${alias === '.' ? 'точечный' : 'пустой'} импорт ${RT_DIR}`;
            break;
          }
          const exportRe = alias && exported.size > 0 ? new RegExp(`\\b${alias}[ \\t]*\\.[ \\t]*(?:${alt(exported)})\\b`) : null;
          for (const x of ix.decls) {
            decls.push(x);
            if ((exportRe && exportRe.test(x.masked)) || (methodRe && methodRe.test(x.masked))) {
              for (const n of x.names) seeds.add(n);
            }
          }
        }
        if (whole) {
          addWholePkg(pkg, true);
          hitPkgs.push(pkg);
          reasons.push(`${pkg}: по объявлениям ${RT_DIR} — пакет целиком (${whole})`);
          continue;
        }
        if (seeds.size === 0) continue;
        const set = goDeclClosure(decls, seeds);
        const facts = new Set(goRows.filter((r) => r.pkg === pkg).map((r) => r.test));
        let n = 0;
        const missing = [];
        for (const t of goRouterTestsIn(decls, set)) {
          if (facts.has(t)) {
            addGo(pkg, t, true, true);
            n++;
          } else {
            missing.push(t);
          }
        }
        if (missing.length > 0) reasons.push(`${pkg}: по объявлениям ${RT_DIR} — ${missing.join(', ')} нет в фактах — пропущен`);
        if (n > 0) {
          total += n;
          hitPkgs.push(pkg);
        }
      }
      reasons.push(
        `${file}: по объявлениям — изменены ${[...d.changed].join(', ')}; тестов TestRouter в зависимых пакетах, использующих их: ${total} (${hitPkgs.join(', ') || 'нет'})`
      );
    }
  }

  // --- пакеты без области: зависимости go list --------------------------------
  if (!res.all) {
    if (res.goAllPkgs) {
      reasons.push('Go: все роутерные пакеты целиком');
    } else if (goChanged.size > 0) {
      const mod = modulePath();
      const imp = (d) => (d === '.' ? mod : `${mod}/${d}`);
      const changedImports = new Set([...goChanged].map(imp));
      const byDecls = new Set([...goChangedByDecls].map(imp));
      let any = false;
      for (const pkg of routerPkgs) {
        const own = `${mod}/${pkg}`;
        const deps = depsOf(pkg);
        const hits = [...changedImports].filter((p) => p === own || deps.has(p));
        if (hits.length === 0) continue;
        addWholePkg(pkg, true);
        any = true;
        // Откат по объявлениям (неизвестное — широко) отличим от прежнего правила: своя строка
        const plain = hits.find((p) => !byDecls.has(p));
        reasons.push(
          plain
            ? `${pkg}: зависит от изменённого пакета ${plain.slice(mod.length + 1) || '.'} — пакет целиком`
            : `${pkg}: использует ${RT_DIR} — правка не разобрана по объявлениям, пакет целиком`
        );
      }
      if (!any) reasons.push('Go: ни один роутерный пакет не затронут');
    }
  }

  // --- ярус full: отсев Go-тестов без прямого затрагивания (D-14) ---------------
  // Откат на пакет целиком (goAllPkgs) отсева не знает: при сомнении выбор только расширяется
  if (!res.all && !res.goAllPkgs) {
    const tierOf = new Map(goRows.map((r) => [`${r.pkg}:${r.test}`, r.tier]));
    for (const key of [...res.goSel.keys()]) {
      if (tierOf.get(key) === 'full' && !res.goFullOk.has(key)) {
        res.goSel.delete(key);
        res.fullDropGo.push(key);
      }
    }
    if (res.fullDropGo.length > 0) {
      reasons.push(
        `ярус full: ${res.fullDropGo.length} тестов не выбраны — область затронута не напрямую (${res.fullDropGo.map((k) => k.slice(k.indexOf(':') + 1)).join(', ')})`
      );
    }
  }

  // --- Playwright -------------------------------------------------------------
  if (!res.all) {
    const affected = new Set();
    const touchedAbs = touchedSrc.map((rel) => path.join(ROOT, 'frontend', rel));
    let graph = null;
    const said = new Set();
    const sayOnce = (line) => {
      if (said.has(line)) return;
      said.add(line);
      reasons.push(line);
    };
    // Спек с особой ролью (pages без сужения, канарейки оболочки): выбран не по маршрутам
    const addForced = (spec, why) => {
      if (!specSelectable(spec)) return false;
      res.specsForced.add(spec);
      if (!res.specsDirect.has(spec)) res.specsOther.add(spec);
      sayOnce(`${spec}: ${why}`);
      return true;
    };
    // Канарейка оболочки: существующий спек, шапка или содержимое сверяются при каждом выборе
    const addShellCanary = (spec, fits, what, why) => {
      const abs = path.join(ROOT, 'frontend', spec);
      const text = existsSync(abs) ? readFileSync(abs, 'utf8') : null;
      if (text === null || !specSelectable(spec) || !fits(text)) {
        pwAll = true;
        sayOnce(`${spec}: ${what} не найдена или не подходит — все роутерные спеки`);
        return;
      }
      addForced(spec, `${what} (${why})`);
    };
    const isReadSpec = (text) => text.split('\n', 5).some((l) => /^\/\/\s*e2e-kind:\s*read\s*$/.test(l.trim()));

    if (touchedSrc.length > 0) {
      try {
        graph = await frontGraph();
        reasons.push(`граф импортов: ${graph.models.map((m) => `${m.side} — маршрутов ${m.model.routes.size}, оболочка ${m.model.shell.size} файлов`).join('; ')}`);
      } catch (e) {
        pwAll = true;
        reasons.push(`граф импортов не построен (${String(e.message ?? e).split('\n', 1)[0]}) — все роутерные спеки`);
      }
    }
    let componentHit = false;
    const hubHit = [];
    const styleHit = [];
    const frameHit = [];
    const localeFiles = [];
    // Стили, каркас и локали — раньше общего графа: у каждого основания своё правило и объяснение
    for (const rel of touchedSrc) {
      if (FRONT_LOCALE.test(rel)) localeFiles.push(rel);
      else if (FRONT_STYLE.some((re) => re.test(rel))) styleHit.push(rel);
      else if (FRONT_FRAME.some((re) => re.test(rel))) frameHit.push(rel);
    }
    for (const rel of styleHit) {
      reasons.push(
        rel.startsWith('public/')
          ? `frontend/${rel}: статика первого кадра и PWA (D-08, как тема) — pages со всеми маршрутами и читающая канарейка оболочки`
          : `frontend/${rel}: стили, токены или тема (D-08) — pages со всеми маршрутами и читающая канарейка оболочки`
      );
    }
    for (const rel of frameHit) {
      if (rel === 'index.html') {
        reasons.push(`frontend/${rel}: точка входа страницы (D-09, как main.ts) — pages со всеми маршрутами и канарейка «правка → Применить»`);
      } else if (rel === 'src/Dashboard.svelte') {
        // переключатель маршрутов и одновременно страница dashboard: её спеки — по маршруту
        affected.add('dashboard');
        reasons.push(`frontend/${rel}: каркас приложения (D-09) и страница dashboard — pages со всеми маршрутами, канарейка «правка → Применить» и спеки dashboard`);
      } else {
        reasons.push(`frontend/${rel}: каркас приложения (D-09) — pages со всеми маршрутами и канарейка «правка → Применить»`);
      }
    }
    if (styleHit.length > 0) {
      addForced(PAGES_SPEC, 'обход всех маршрутов без сужения (стили)');
      addShellCanary(SHELL_READ_CANARY, isReadSpec, 'читающая канарейка оболочки', 'правка стилей');
    }
    if (frameHit.length > 0) {
      addForced(PAGES_SPEC, 'обход всех маршрутов без сужения (каркас)');
      addShellCanary(SHELL_APPLY_CANARY, (t) => /\bconfirmApply\(/.test(t), 'канарейка каркаса «правка → Применить»', 'правка каркаса');
    }
    const special = new Set([...styleHit, ...frameHit, ...localeFiles]);
    if (graph && localeFiles.length > 0) {
      const lr = localeSelect(graph, localeFiles, specsAll);
      for (const line of lr.reasons) reasons.push(line);
      if (lr.all) pwAll = true;
      for (const r of lr.routes) affected.add(r);
      for (const s of lr.specs) {
        res.specsDirect.add(s);
        sayOnce(`${s}: читает изменённые ключи локали`);
      }
      if (lr.shell) {
        addForced(PAGES_SPEC, 'обход всех маршрутов без сужения (ключ локали в оболочке или хабе)');
        addShellCanary(SHELL_READ_CANARY, isReadSpec, 'читающая канарейка оболочки', 'ключ локали в оболочке или хабе');
      }
    }
    if (graph) {
      for (const rel of touchedSrc) {
        if (special.has(rel)) continue;
        const abs = path.join(ROOT, 'frontend', rel);
        const present = graph.models.filter((m) => m.model.reader.isFile(abs));
        if (present.length === 0) {
          reasons.push(`frontend/${rel}: удалён (нет в ${graph.models.map((m) => m.side).join(' и ')})`);
          continue;
        }
        const hub = graph.hubOf(abs, present);
        if (hub) {
          hubHit.push({ rel, abs, why: hub });
          continue;
        }
        // Маршруты обеих ревизий: перенос или удаление импорта не теряет потребителей старой стороны
        const per = present.map((m) => ({ side: m.side, routes: m.model.routesOf(abs) }));
        const routes = [...new Set(per.flatMap((p) => p.routes))];
        if (routes.length === 0) {
          reasons.push(`frontend/${rel}: не импортируется приложением (${per.map((p) => p.side).join(', ')})`);
          continue;
        }
        routes.forEach((r) => affected.add(r));
        componentHit = true;
        const sides =
          per.length > 1 && per.some((p) => p.routes.join() !== per[0].routes.join())
            ? ` (${per.map((p) => `${p.side}: ${p.routes.join(', ') || '-'}`).join('; ')})`
            : '';
        reasons.push(`frontend/${rel}: страницы ${routes.join(', ')}${sides}`);
      }
    }
    // Хаб (D-10): как каркас — pages без сужения и канарейка «правка → Применить», плюс спеки,
    // ссылающиеся на модуль напрямую; прочие основания выбора сохраняются
    if (hubHit.length > 0) {
      for (const h of hubHit) {
        const direct = specsAll.filter((s) => graph.specSources(path.join(ROOT, 'frontend', s)).has(h.abs));
        reasons.push(
          `frontend/${h.rel}: хаб — ${h.why}; pages со всеми маршрутами, канарейка «правка → Применить»` +
            (direct.length > 0 ? `, спеки с прямой ссылкой: ${direct.join(', ')}` : ', спеков с прямой ссылкой нет')
        );
        for (const s of direct) {
          res.specsDirect.add(s);
          res.specsForced.add(s);
        }
      }
      addForced(PAGES_SPEC, 'обход всех маршрутов без сужения (хаб)');
      addShellCanary(SHELL_APPLY_CANARY, (t) => /\bconfirmApply\(/.test(t), 'канарейка каркаса «правка → Применить»', 'правка хаба');
    }
    // Обычный общий компонент: спеки маршрутов-потребителей, pages с их маршрутами и читающая
    // канарейка оболочки (D-09)
    if (componentHit && !pwAll) {
      addShellCanary(SHELL_READ_CANARY, isReadSpec, 'читающая канарейка оболочки', 'правка общего компонента');
    }

    for (const s of touchedSpecs) {
      res.specsDirect.add(s);
      res.specsForced.add(s);
      res.specsFullOk.add(s);
    }

    // Обход всех страниц (`// e2e-pages: *`) сужается до маршрутов, только если ни одна
    // причина, кроме маршрутов, его не выбрала
    const star = new Set();
    for (const spec of specsAll) {
      const text = readFileSync(path.join(ROOT, 'frontend', spec), 'utf8');
      const ann = [...text.matchAll(/\/\/\s*e2e-pages:\s*([^\n]+)/g)].flatMap((m) =>
        m[1].split(',').map((r) => r.trim())
      );
      if (ann.includes('*')) star.add(spec);
    }

    if (pwAll) {
      specsAll.forEach((s) => res.specsDirect.add(s));
    } else if (affected.size > 0) {
      for (const spec of specsAll) {
        if (res.specsForced.has(spec)) continue;
        if (star.has(spec)) {
          res.specsDirect.add(spec);
          reasons.push(`${spec}: обходит все страницы — сужается до затронутых`);
          continue;
        }
        const specAbs = path.join(ROOT, 'frontend', spec);
        const text = readFileSync(specAbs, 'utf8');
        const routes = graph.mod.specRoutes(text, graph.routes);
        if (routes === null) {
          // Маршрут не известен: прямые ссылки спека на изменённые файлы называются, иначе
          // включение консервативное
          res.specsDirect.add(spec);
          const hit = touchedAbs.filter((a) => graph.specSources(specAbs).has(a));
          reasons.push(
            hit.length > 0
              ? `${spec}: маршруты не определены, ссылается напрямую на ${hit.map((a) => path.relative(ROOT, a)).join(', ')}`
              : `${spec}: маршруты не определены (нет #/…, литералов маршрутов и e2e-pages) — включается консервативно`
          );
        } else if ([...routes].some((r) => affected.has(r))) {
          res.specsDirect.add(spec);
          reasons.push(`${spec}: открывает затронутые страницы (${[...routes].filter((r) => affected.has(r)).sort().join(', ')})`);
        } else if (affected.has('dashboard') && graph.mod.rootEntryIgnored?.(text, routes)) {
          // сужение принято (D-32): вход через корень не считается маршрутом dashboard, если у спека
          // есть другие маршруты (в том числе случайный литерал); страховка — pages и канарейка
          reasons.push(`${spec}: вход через корень не считается маршрутом dashboard (у спека есть другие маршруты) — не выбран`);
        }
      }
      const all = new Set([...res.specsDirect, ...res.specsOther]);
      const walkSelected = [...all].some((s) => star.has(s));
      const walkForced = [...res.specsForced].some((s) => star.has(s));
      if (walkSelected && !walkForced) {
        // Обход pages называет страницы (`page:config`), не вкладки: вкладка сужает до страницы
        const tops = new Set([...affected].map((r) => graph.mod.topRoute(r)));
        const routes = [...tops].filter((r) => SAFE_ROUTE.test(r)).sort();
        if (routes.length !== tops.size) {
          reasons.push('маршрут с небезопасным именем — обход идёт целиком');
        } else {
          res.grepRoutes = routes;
        }
      }
    }
  }

  res.pwFull = facts.filter((r) => r.kind === 'pw' && r.tier === 'full').map((r) => ({ spec: r.spec }));
  return { res, routerPkgs, specsAll, routerGoRows: goRows };
}

/** Размеченные тесты Go в пакетах `./internal/…`: { pkgs, run, rows } (rows — для причин). */
function goCoreOf(goPkgs, only) {
  const want = goPkgs.map((p) => p.replace(/^\.\//, ''));
  // Пустой список goMarks понимает как «все пакеты»: пустой выбор отдаём сами
  let rows = want.length > 0 ? goMarks(ROOT, want) : [];
  // Новые (ещё не в индексе) и изменённые роутерные тесты читаем с диска сами
  const seen = new Set(rows.map((r) => `${r.pkg}\t${r.test}`));
  for (const file of changedFiles) {
    const pkg = path.posix.dirname(file);
    if (!want.includes(pkg) || !/_test\.go$/.test(file) || !SAFE_PATH.test(file)) continue;
    const abs = path.join(ROOT, file);
    if (!existsSync(abs)) continue;
    const text = readFileSync(abs, 'utf8');
    if (text.split('\n', 1)[0].trim() !== '//go:build router') continue;
    for (const m of analyzeGo(text).marks) {
      if (seen.has(`${pkg}\t${m.test}`)) continue;
      seen.add(`${pkg}\t${m.test}`);
      rows.push({ pkg, test: m.test, reason: m.reason, file });
    }
  }
  // Выбор по именам: вторая ветвь берёт только выбранные размеченные тесты
  if (only) rows = rows.filter((r) => only.has(`${r.pkg}:${r.test}`));
  const names = [
    ...new Set(rows.map((r) => r.test).filter((t) => /^TestRouter[A-Za-z0-9_]+$/.test(t)))
  ].sort();
  const withMarks = new Set(rows.map((r) => r.pkg));
  return {
    pkgs: want.filter((p) => withMarks.has(p)).map((p) => `./${p}`),
    run: names.length > 0 ? `^(${names.join('|')})$` : '',
    rows
  };
}

/** Выражение отбора тегов ядра для Playwright; то же строит pw-suite.sh при RT_PW_CORE_ONLY=1. */
function coreGrep(grep) {
  return grep === '' ? '@core' : `(?=.*@core)${grep}`;
}

/** Выбранные спеки, в которых есть хотя бы один тест с тегом ядра: { specs, reasons }. */
function pwCoreOf(specs, grep) {
  const reasons = [];
  if (specs.length === 0) return { specs: [], reasons };
  const fallback = (why) => {
    reasons.push(
      `ядро: список Playwright не получен (${why}) — вторая ветвь берёт все выбранные спеки`
    );
    return { specs, reasons };
  };
  const frontend = path.join(ROOT, 'frontend');
  const bin = path.join(frontend, 'node_modules', '.bin', 'playwright');
  if (!existsSync(bin)) return fallback('нет frontend/node_modules/.bin/playwright');
  // Конфигу переменные стенда не нужны (--list), адрес и пароль панели в окружение не передаём
  const env = Object.fromEntries(
    Object.entries(process.env).filter(([k]) => !k.startsWith('XCP_'))
  );
  let json;
  try {
    let text;
    try {
      text = execFileSync(
        bin,
        [
          'test',
          '-c',
          'playwright.router.config.ts',
          '--list',
          '--reporter=json',
          '--grep',
          coreGrep(grep),
          ...specs
        ],
        {
          cwd: frontend,
          env,
          encoding: 'utf8',
          timeout: 60000,
          maxBuffer: 64 * 1024 * 1024
        }
      );
    } catch (e) {
      // «No tests found» — код 1, но это законный ответ «тестов с тегом нет»: JSON есть в stdout
      if (typeof e.stdout !== 'string' || e.stdout === '') throw e;
      text = e.stdout;
    }
    json = JSON.parse(text);
  } catch (e) {
    return fallback(
      String(e.message ?? e)
        .split('\n', 1)[0]
        .slice(0, 120)
    );
  }
  const errors = json && Array.isArray(json.errors) ? json.errors : [];
  const noTests = errors.every((er) => /No tests found/.test(String(er.message ?? '')));
  if (!json || !Array.isArray(json.suites) || !noTests) {
    return fallback('ответ с ошибками или неожиданной формы');
  }
  if (errors.length > 0 && json.suites.length > 0) {
    return fallback('ответ с ошибками или неожиданной формы');
  }
  const counts = new Map();
  const walk = (suite) => {
    for (const sp of suite.specs ?? []) {
      const key = `tests/router/${path.posix.basename(String(sp.file ?? ''))}`;
      counts.set(key, (counts.get(key) ?? 0) + (sp.tests ?? []).length);
    }
    for (const child of suite.suites ?? []) walk(child);
  };
  json.suites.forEach(walk);
  const found = specs.filter((s) => (counts.get(s) ?? 0) > 0);
  for (const s of found) reasons.push(`ядро: pw ${s} — тестов с тегом ядра ${counts.get(s)}`);
  return { specs: found, reasons };
}

/**
 * Тесты выбора, зависящие от ядра (D-01, D-03, D-04): Go — размеченные core/known-core,
 * Playwright — спеки с тегом @core. Ошибка расчёта не роняет выбор, а расширяет списки
 * до безопасных значений: все выбранные пакеты (без отбора по именам) и все выбранные спеки.
 * only — множество 'пакет:Тест' выбора по именам (null — выбор пакетами).
 */
function coreSelection(goPkgs, specs, grep, only) {
  const reasons = [];
  let go;
  try {
    go = goCoreOf(goPkgs, only);
    for (const r of go.rows) reasons.push(`ядро: go ./${r.pkg} ${r.test} (${r.reason})`);
  } catch (e) {
    reasons.push(`ядро: разметка Go не получена (${e.message}) — берутся все выбранные пакеты`);
    go = { pkgs: goPkgs, run: '', rows: [] };
  }
  let pw;
  try {
    pw = pwCoreOf(specs, grep);
  } catch (e) {
    reasons.push(`ядро: отбор Playwright не удался (${e.message}) — берутся все выбранные спеки`);
    pw = { specs, reasons: [] };
  }
  reasons.push(...pw.reasons);
  return { goPkgs: go.pkgs, goRun: go.run, specs: pw.specs, reasons };
}

/** Безопасные значения ядра для отката на полный выбор: все пакеты с размеченными тестами и все спеки. */
function coreSafeAll(routerPkgs, specsAll, reasons) {
  const all = routerPkgs.map((p) => `./${p}`);
  try {
    const go = goCoreOf(all, null);
    return { goPkgs: go.pkgs, goRun: go.run, specs: specsAll, reasons };
  } catch {
    return { goPkgs: all, goRun: '', specs: specsAll, reasons };
  }
}

async function emit(res, routerPkgs, specsAll) {
  const quote = (s) => `'${s}'`;
  let all = res.all;
  let go;
  let seq = [];
  let specs;
  let first = [];
  let grep = '';
  let names = null; // выбор Go по именам: множество 'пакет:Тест'
  if (all) {
    go = routerPkgs.map((p) => `./${p}`);
    specs = specsAll;
  } else {
    if (res.goAllPkgs) {
      go = routerPkgs.map((p) => `./${p}`);
    } else {
      names = new Set(res.goSel.keys());
      const scores = names.size > 0 ? await goFailScores(res.reasons) : null;
      const score = (key) => (scores ? scores.get(`go:${key.slice(key.indexOf(':') + 1)}`) || 0 : 0);
      const order = [...res.goSel.entries()].sort((x, y) => {
        if (x[1] !== y[1]) return x[1] ? -1 : 1; // затронутые области — первыми
        return score(y[0]) - score(x[0]) || (x[0] < y[0] ? -1 : x[0] > y[0] ? 1 : 0);
      });
      seq = order.map(([k]) => k);
      const dirs = new Set(seq.map((k) => k.slice(0, k.indexOf(':'))));
      go = routerPkgs.filter((p) => dirs.has(p)).map((p) => `./${p}`);
    }
    const sel = new Set([...res.specsDirect, ...res.specsOther]);
    specs = specsAll.filter((s) => sel.has(s));
    first = specs.filter((s) => res.specsDirect.has(s));
    if (res.grepRoutes) {
      grep = `^(?!.*page:(?!(?:${res.grepRoutes.join('|')}) ))`;
    }
  }
  const okList = (l) => l.every((x) => SAFE_PATH.test(x.replace(/^\.\//, '')));
  const okGrep = (g) => /^[A-Za-z0-9_|()?!^*.: -]*$/.test(g);
  const okRun = (r) => /^[A-Za-z0-9_|^$()]*$/.test(r);
  const okSeq = (l) => l.every((x) => SEQ_ENTRY.test(x));
  const okAreas = (l) => l.every((x) => /^[a-z][a-z0-9-]*$/.test(x));
  const areas = [...res.areas].sort();
  let core = coreSelection(go, specs, grep, names);
  // Последняя проверка безопасности вывода: иначе — всё
  if (
    !okList(go) ||
    !okList(specs) ||
    !okList(first) ||
    !okSeq(seq) ||
    !okAreas(areas) ||
    !okGrep(grep) ||
    !okList(core.goPkgs) ||
    !okList(core.specs) ||
    !okRun(core.goRun)
  ) {
    all = true;
    go = routerPkgs.map((p) => `./${p}`);
    seq = [];
    specs = specsAll;
    first = [];
    grep = '';
    core = coreSafeAll(routerPkgs, specsAll, core.reasons);
    res.reasons.push('всё: значение выбора не прошло проверку безопасных символов');
  }
  // Ярус full (D-14): спеки выбраны не напрямую по области и не правкой самого спека — тесты
  // @full отсеиваются; иначе (расширение) идут все тесты выбранных спеков. Полный выбор — пусто.
  const specsFullOk = res.specsFullOk ?? new Set();
  const invert = all || specs.some((s) => specsFullOk.has(s)) ? '' : '@full';
  if (invert) {
    const dropped = (res.pwFull ?? []).filter((r) => specs.includes(r.spec)).length;
    res.reasons.push(
      `ярус full: Playwright — тестов @full в выбранных спеках ${dropped} не выбраны — область затронута не напрямую`
    );
  }
  res.reasons.push(...core.reasons);
  const nothing = !all && go.length === 0 && specs.length === 0;
  const kind = all
    ? 'полный'
    : res.harness
      ? 'обвязка'
      : res.deps
        ? 'зависимости'
        : nothing
          ? 'смоук'
          : 'области';
  res.reasons.push(`вид выбора: ${kind}`);
  return [
    `RT_SELECT_ALL=${all ? 1 : 0}`,
    `RT_GO_PKGS=${quote(go.join(' '))}`,
    `RT_SPECS=${quote(specs.join(' '))}`,
    `RT_PW_GREP=${quote(grep)}`,
    `RT_GO_CORE_PKGS=${quote(core.goPkgs.join(' '))}`,
    `RT_GO_CORE_RUN=${quote(core.goRun)}`,
    `RT_SPECS_CORE=${quote(core.specs.join(' '))}`,
    `RT_KERNEL_TOUCHED=${all || res.kernelTouched ? 1 : 0}`,
    `RT_GO_SEQ=${quote(all ? '' : seq.join(' '))}`,
    `RT_SPECS_FIRST=${quote(first.join(' '))}`,
    `RT_PW_INVERT=${quote(invert)}`,
    `RT_HARNESS=${res.harness ? 1 : 0}`,
    `RT_AREAS=${quote(areas.join(' '))}`
  ].join('\n');
}

async function main(argv) {
  const args = { base: null, head: null, files: null, explain: false };
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i];
    if (a === '--base') args.base = argv[++i];
    else if (a === '--head') args.head = argv[++i];
    else if (a === '--files') args.files = (argv[++i] ?? '').split(',').filter(Boolean);
    else if (a === '--explain') args.explain = true;
    else if (a === '--hub-percent') {
      // диагностика: сравнение порогов хаба (D-10) в select-replay.mjs
      const v = argv[++i] ?? '';
      if (!/^\d{1,3}(?:\.\d+)?$/.test(v) || Number(v) > 100) {
        console.error(`select-changed: --hub-percent: нужно число 0…100, получено ${JSON.stringify(v)}`);
        process.exit(2);
      }
      hubPercent = Number(v);
    } else {
      console.error(`select-changed: неизвестный аргумент ${a}`);
      process.exit(2);
    }
  }

  let out;
  let reasons;
  let stopped = false;
  try {
    let changed;
    if (args.files) {
      changed = args.files.map((f) => path.posix.normalize(f));
    } else {
      const g = changedFromGit(args.base, args.head);
      changed = g.files;
      ctx.from = g.from;
      ctx.to = g.to;
    }
    changedFiles = changed;
    const { res, routerPkgs, specsAll } = await select(changed);
    reasons = res.reasons;
    if (res.stop.length > 0) {
      stopped = true;
      out = '';
    } else {
      out = await emit(res, routerPkgs, specsAll);
    }
  } catch (e) {
    // Не смогли посчитать — безопасный вариант: всё
    const orEmpty = (fn) => {
      try {
        return fn();
      } catch {
        return [];
      }
    };
    const res = {
      all: true,
      goSel: new Map(),
      specsDirect: new Set(),
      specsOther: new Set(),
      areas: new Set(),
      harness: false,
      deps: false,
      reasons: [`всё: выбор не удался (${e.message})`]
    };
    out = await emit(res, orEmpty(routerPackages), orEmpty(listRouterSpecs));
    reasons = res.reasons;
  }

  if (stopped) {
    // причины остановки печатаются всегда, не только с --explain: stdout пуст
    console.error('выбор: остановлен (код 3) — полный выбор не подставляется');
    console.error('причины:');
    for (const r of reasons) console.error(`  - ${r}`);
    process.exitCode = EXIT_STOP;
    return;
  }
  if (args.explain) {
    const first = out.split('\n')[0];
    console.error(`выбор: ${first}`);
    for (const line of out.split('\n').slice(1)) console.error(`  ${line.replace(/^RT_/, '')}`);
    console.error('причины:');
    for (const r of reasons) console.error(`  - ${r}`);
  }
  console.log(out);
}

await main(process.argv.slice(2));
