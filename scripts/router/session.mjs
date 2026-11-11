#!/usr/bin/env node
// scripts/router/session.mjs — сессия на стенде по спеке (make router-test SUITE=session).
//
// Запуск — только через run.sh: `make router-test SUITE=session SPEC=<спека.json>`. run.sh
// проверяет спеку и чистоту дерева, проверяет метки известных падений, берёт общую
// блокировку, создаёт каталог отчёта (RT_REPORT) и после сессии держит блокировку
// RT_HOLD_MINUTES. Этот файл проводит проходы спеки и сверяет результаты с ожиданиями.
// Этапы — существующие скрипты каталога: deploy.sh и smoke.sh ставят сборку,
// pw-suite.sh --switch переключает ядро через интерфейс панели, go-suite.sh (затем
// settle.sh) и pw-suite.sh гоняют тесты. При любом исходе стенд возвращается: рабочее
// дерево — к HEAD, на цели — сборка HEAD, активное ядро — исходное.
//
// Спека (JSON):
//   {
//     "target": "arm64",                    id цели из локального конфига
//     "cores": ["xray", "mihomo"],          ядра проходов (по умолчанию оба)
//     "keep_going": false,                  необязательно (true/false, по умолчанию false):
//                                           true — все проходы до конца (полный список
//                                           падений), false — остановка на первом
//                                           несовпадении (см. ниже)
//     "budget_reason": "…",                 необязательно: почему спека сверх бюджета 20 мин
//                                           (непустая строка до 300 символов без перевода
//                                           строки), см. «Оценка и бюджет»
//     "passes": [
//       {
//         "name": "before-fix",             [a-z0-9-]
//         "product": {                      необязательно: файлы продукта из другого коммита
//           "ref": "0aae0a7c",              (воспроизведение до исправления); тесты — из HEAD
//           "files": ["frontend/src/Editor.svelte"]
//         },
//         "version": "v0.33.0",             необязательно: сборка HEAD
//                                           под номером вышедшего релиза (тег vX.Y.Z[-<суффикс>.N]
//                                           из локального git или latest — новейший
//                                           опубликованный); сеть и данные GitHub
//                                           настоящие, подменён только номер установленной версии;
//                                           не сочетается с product и с проходом-прогоном; номер
//                                           живёт только в деплое прохода: XCP_VERSION снимается
//                                           со всех этапов, возврат стенда собирает HEAD без номера
//         "tree": "71c55fea",               необязательно: продукт и тесты прохода — из дерева
//                                           коммита (ссылка git, коммит есть): отдельный worktree
//                                           tree-step.sh prepare, деплой и смоук, Go и Playwright
//                                           идут из его каталога (скрипты этапов — из HEAD, имена
//                                           и метки ядра тестов — из дерева; спеки прохода
//                                           проверяются в нём). Сборка ставится один раз на проход,
//                                           следующий проход с другим ключом ставит свою; не сочетается
//                                           с product, version и проходом-прогоном. Нужен для повтора
//                                           случая до исправления целиком (воспроизведение 148-25).
//                                           Worktree снимает tree-step.sh cleanup в конце сессии
//         "cores": ["mihomo"],              необязательно: ядра прохода, на каждом все тесты
//                                           (перекрывает правило «ядра как в гейте», см. ниже)
//         "go": { "pkgs": "./internal/x", "run": "^TestRouterX$" },    необязательно
//         "specs": ["tests/router/editor-mobile.spec.ts"],              необязательно, от frontend/
//         "grep": "value-missing|backups-switch",                       необязательно, --grep
//         "expect": {
//           "count": 3,                     необязательно: число тестов go:/pw: на ядро
//           "default": "PASS",              статус тестов вне tests; без него они справочные
//           "tests": {
//             "pw:editor-json-tail value-missing vp:1280": { "status": "FAIL", "detail": "no parser" }
//           }
//         }
//       }
//     ]
//   }
// Проход-прогон набора (вместо go/specs): прогон набора router-test целиком как этап сессии.
//   {
//     "name": "narrow-go",                  [a-z0-9-]
//     "suite": "changed",                   full, changed, smoke (с release), session, harness, rerun,
//                                           delta или bisect (поля — ниже)
//     "files": ["internal/nodes/router_nodes_test.go"],    только у changed: выбор по списку
//                                           файлов (RT_FILES) вместо git; пути без «..»
//     "expect": {
//       "exit": 0,                          код выхода run.sh, по умолчанию 0
//       "summary": ["- Окно смоука: 30 с"], подстроки summary.md вложенного отчёта
//       "same_failures_as": "full-1",       множество строк FAIL, KNOWN и XPASS (статус, ядро,
//                                           проверка) совпадает с более ранним проходом-прогоном
//       "max_minutes": 30                   у любого прохода-прогона, целое 1…600: вердикт
//     }                                     «время прохода: N мин из max_minutes» по замеру
//   }                                       вложенного прогона
// Вложенная сессия — проход-прогон набора session (проверка самой сессии: ожидаемая остановка,
// предел времени):
//   {
//     "name": "nested",
//     "suite": "session",
//     "spec": "build/spec-check/inner.json", обязателен: путь от корня рабочей копии без «..»,
//                                           файл есть и не совпадает с внешней спекой; цель
//                                           та же; своих проходов session у вложенной спеки
//                                           нет (глубина 1)
//     "expect": { "exit": 1, "max_minutes": 30, "summary": ["Остановлено на первом несовпадении"] }
//   }
// Вложенный `sh run.sh session` идёт с SPEC=<spec>, под блокировкой сессии, отчёт — как у
// прогонов наборов; exit и summary сверяются по её коду и summary.md. files, release,
// interrupt и same_failures_as у session не допускаются. Стенд после неё — по её session.json
// (сборка HEAD, её исходное ядро); прерывание внешней сессии уходит только run.sh вложенной,
// он передаёт сигнал её session.mjs один раз — она возвращает стенд сама.
// Смоук установленного релиза — проход-прогон набора smoke с полем release:
//   {
//     "name": "release-smoke",
//     "suite": "smoke",                     набор smoke: деплой артефакта релиза и смоук
//     "release": "latest",                  тег vX.Y.Z[-<суффикс>.N] или latest — новейший
//                                           опубликованный (не draft) релиз; обязателен у smoke,
//                                           у full и changed запрещён
//     "expect": { "exit": 0, "summary": ["- Статус на GitHub: stand/arm64-smoke success"] }
//   }
// Вложенный `run.sh smoke` идёт с RELEASE=<тег>; статус stand/<arch>-smoke на коммите тега
// публикует сам run.sh. files и interrupt у smoke не допускаются. После прогона на стенде
// остаётся сборка релиза, поэтому следующий проход и конец сессии снова ставят HEAD.
// Остальные наборы механики (148.1-16) — проходы-прогоны с собственными полями; interrupt и files
// у них не допускаются, ожидания exit, summary и max_minutes — как у прочих:
//   {"name": "mech", "suite": "harness", "expect": {"exit": 0}}
//        набор harness: проверка механики стенда (деплой, смоук, снимок, тесты блока harness на
//        обоих ядрах, самопроверка сессии); полей набора нет.
//   {"name": "again", "suite": "rerun", "from": "mech", "expect": {"exit": 0}}
//        набор rerun: перепрогон упавших тестов отчёта более раннего прохода-прогона from (имя
//        прохода: его отчёт — прогон набора или вложенная сессия). Перепрогон дописывает раздел
//        «Перепрогон N» в summary.md отчёта from, поэтому строки summary ищутся там, а не в
//        собственном каталоге прохода. Упавших тестов нет — код 0, стенд не тронут.
//   {"name": "top-up", "suite": "delta", "release": "latest", "dry_run": true, "expect": {"exit": 0}}
//        набор delta: добор stand/<arch>-full на тег (release — latest или тег vX.Y.Z[-<суффикс>.N]);
//        dry_run (булево, по умолчанию false) — статус на GitHub не отправляется, решение добора
//        остаётся в summary.md. Без dry_run проход публикует статус: ставить только осознанно.
//   {"name": "culprit", "suite": "bisect", "test": "go:TestRouterX", "core": "mihomo",
//    "from": "71c55fea", "to": "12094e1f", "mode": "fix", "expect": {"exit": 0}}
//        набор bisect: поиск виновного коммита. test — go:TestRouter<Имя>, go:<пакет>:TestRouter<Имя>
//        или pw:<название>; core — xray или mihomo (ядро, на котором тест падает); from и to — ссылки
//        git (коммиты есть; to по умолчанию HEAD); mode — regress (по умолчанию) или fix; для pw: —
//        spec (tests/router/<имя>.spec.ts или имя файла; файл есть) и необязательно grep (одна
//        строка без одинарной кавычки). Итог — раздел «Поиск виновного коммита» summary.md прохода.
// Вложенный run.sh, как у прочих проходов-прогонов, идёт под блокировкой сессии без RT_CORES,
// RT_BASE и настроек окружения оператора; FROM, TEST, CORE, TO, MODE, GREP, DRY_RUN, BASE_REPORT
// выставляются только проходом, которому они нужны.
// Прерывание прогона посреди этапа (необязательно, проверка исправления обрыва, G-145.1-5):
//     "interrupt": {
//       "stage": "go",                      обязателен; поддерживается только go
//       "signal": "TERM",                   TERM (kill группе make) или INT (Ctrl+C), по умолчанию TERM
//       "after": 20,                        1..600 с после появления go-<пакет>.raw
//       "deadline": 300                     30..3600 с на выход run.sh, по умолчанию 600
//     }
// Триггер — файл go-<пакет>.raw в <отчёт прохода>/<цель>/<ядро>/: go-suite.sh держит его, пока
// бинарник тестов работает на цели. Вложенный run.sh с interrupt запускается отдельной группой
// процессов (detached), как make в терминале; через after секунд после появления файла сигнал
// уходит всей группе (только при целом pid > 1), не вышел за deadline — SIGKILL группе. Этапы
// обвязки идут под setsid и сигнал не получают: их гасит сам run.sh (план 145.1-16). Вердикты
// прохода с interrupt, кроме exit и summary: «interrupt: сигнал» (отправлен), «interrupt: выход
// run.sh» (до deadline без SIGKILL), «interrupt: процессы на ПК» (нет: группа вложенного
// прогона пуста и нет go-suite/pw-suite/smoke — они держали бы блокировку), «interrupt:
// тестовые процессы на цели» (нет; в отчёте только число), restore-verify, xcp-after-restore и
// core-restored (PASS) и по вердикту на каждую лишнюю строку FAIL и XPASS результатов
// вложенного прогона. Прерывание по спеке не прерывает сессию: следующие проходы идут дальше.
// После SIGKILL run.sh не успевает восстановить стенд и оставляет снимок на цели: стенд по нему
// (rt_restore со сверкой) возвращает сессия, вердикт «interrupt: возврат стенда сессией»; не
// вернулся — сессия останавливается. Снимок, оставшийся к концу сессии, возвращает cleanup.
// С go, specs, grep и product проход-прогон не сочетается. Вложенный `sh run.sh [changed]`
// идёт под блокировкой сессии (RT_LOCK_HELD=1) без RT_HOLD_MINUTES, RT_CORES, RT_BASE и
// SPEC (SPEC — только у session), сам ставит сборку HEAD, снимает снимок и восстанавливает стенд. Его отчёт — в
// build/router/<каталог сессии>-<NN>-<проход> (RT_REPORT_DIR; ссылка last остаётся на
// сессии). В summary.md сессии — раздел «Прогоны наборов»: проход, набор, код, итог,
// оболочка, путь отчёта; те же данные — в session.json (suites).
//
// Статус ожидания — PASS, FAIL, KNOWN, XPASS, SKIP, QPASS или QFAIL; несколько — через «|»
// (PASS|KNOWN). detail — подстрока детали в results.tsv. Без default тест вне tests справочный,
// но FAIL и XPASS у него — расхождение: неожиданное падение не теряется.
// Карантин (D-17): QPASS и QFAIL — исход теста с меткой карантина. Тест в карантине вне tests
// (под default или справочный) несовпадением не считается и сессию не останавливает: строка
// вердикта «карантин» со статусом. Тест из tests сверяется строго по указанным статусам —
// QFAIL можно ждать явно ("QPASS|QFAIL" — любой исход карантина).
//
// Остановка на первом несовпадении (по умолчанию; "keep_going": true её выключает): красная
// сессия не дожидается конца, когда итог уже ясен. Первое несовпадение с ожиданиями:
//   - Go-этап — по мере вывода: сессия раз в секунду дочитывает файлы go-*.raw этапа (с
//     RT_GO_RUN_SEQ — go-<пакет>-<k>.raw) в
//     <этап>/<цель>/<ядро>/ (вывод -test.v=test2json, пока бинарник работает на цели) и готовые
//     go-*.parsed пакетов; строка `--- FAIL: <тест>`, чей статус (XPASS при маркере
//     `XPASS slug=`, QFAIL при маркере `QUARANTINE slug=` у теста или предка, иначе FAIL)
//     ожидание теста не допускает, останавливает этап: TERM группе процессов этапа (как при
//     прерывании), ожидание выхода и опустения группы, затем добивание тестовых бинарников на
//     цели. Родитель, упавший из-за подтеста, отдельно не сверяется (как в go-parse.awk);
//     деталь (detail) по ходу вывода не сверяется — её сверяет разбор после этапа;
//   - Playwright — после каждого спека (сверка results.tsv), деплой со смоуком и прогоны
//     наборов — после этапа; несовпадение в Go-части прохода со спеками не пускает спеки.
// Остановленный проход сверяется по уже записанным результатам: тест, на котором остановились, —
// несовпадение, невыполненные тесты не сверяются. Следующие ядра и проходы не запускаются,
// стенд возвращается как при любом исходе (сборка HEAD, исходное ядро). В summary.md — раздел
// «## Остановлено на первом несовпадении»: проход, ядро, тест, ожидаемый и полученный статус и
// «Не выполнено:» (ядра прохода и проходы); код выхода 1. Несовпадение в последнем этапе
// остановкой не считается: невыполненного нет.
//
// Смена версии стенда (D-13, контракт сверки версии): перед каждым этапом go-suite.sh и
// pw-suite.sh (и переключением ядра pw-suite.sh --switch) сессия пишет <этап>/<цель>/expect-version
// — версию последнего деплоя сессии (файл version каталога деплоя, у прохода с version — его номер)
// или вложенного прогона (version его отчёта, у вложенной сессии — её последнего деплоя); версия
// неизвестна — файла нет, сверка этапа выключена. Этапы сверяют версию на каждой границе пакета
// и спека. Код этапа 4 или строка stop в <этап>/<цель>/version-stop.tsv — остановка сессии
// независимо от keep_going: тесты остановленного этапа с ожиданиями не сверяются (их падения —
// класс stand), сторож Go, оборвавший этап раньше границы, сверяет версию стенда сам (xcp -v).
// В summary.md — раздел «## Остановлено: смена версии стенда» (проход, ядро, граница, ожидалась,
// на стенде, «Не выполнено:» с ядрами, спеками, проходами и строками notrun этапа), в session.json
// — stopped.reason: version (остановка на несовпадении — mismatch); код выхода 1. На стенде могла
// остаться чужая сборка: конец сессии ставит сборку HEAD заново. Отчёт со строкой stop (этап
// сессии или вложенный прогон) в историю не пишется — прогон неполный.
//
// Ядра как в гейте (D-23): на первом ядре прохода (активное ядро стенда, если оно среди ядер
// прохода) идут все тесты прохода, на остальных ядрах — только тесты с меткой ядра (core-marks):
// Go — routertest.CoreSpecific или метка Known с ядром в селекторе (goMarks), спеки — тег ядра
// CORE_TAG, @core-label через coreTagsOf (specFacts); такие спеки идут с --grep @core, как во
// второй ветви changed (RT_PW_CORE_ONLY=1). Размеченных тестов в проходе нет — на остальные ядра
// проход не переключается. Ожидание из tests для теста, не шедшего на ядре, — не несовпадение
// (вердикт «не шёл на ядре — ядро не важно»); count сверяется только на ядре со всеми тестами.
// Имена Go-тестов — из исходников (TestRouter* в файлах `//go:build router` пакетов go.pkgs,
// отобранные регэкспом go.run); регэксп с подтестами («/»), пакеты с «...» или пустой отбор —
// Go-часть идёт прежним способом (RT_GO_PKGS и RT_GO_RUN) на всех ядрах.
// Поле прохода "cores": ["mihomo"] (непустой список из xray, mihomo без повторов) перекрывает
// правило: проход идёт на этих ядрах, на каждом — все тесты.
//
// Дешёвые тесты первыми (todo 12 п. 3): медианы длительности — из истории прогонов
// (history.mjs: окно 30 дней, без строк в окне — вся история; ключ — проверка и ядро). Go-тесты
// прохода на ядре идут одним этапом go-suite.sh через RT_GO_RUN_SEQ (`<каталог пакета>:<Имя>`):
// неизвестные истории — в начале в исходном порядке, затем по возрастанию медианы. Спеки — по
// одному запуску pw-suite.sh на спек, каждый в свой каталог этапа <NN>-<проход>-<ядро>-pw<k>, в
// том же порядке; медиана спека — сумма медиан его тестов (список — `playwright test --list` без
// стенда), спек с неизвестным тестом или без списка — неизвестный. Без списка размеченный спек
// на ядре без полного состава определяется по specFacts. Settle после Go — один раз перед первым
// спеком. Несовпадение после спека останавливает проход до следующего спека.
//
// Оценка и бюджет (D-23): оценка длительности сессии — сумма по проходам и ядрам медиан
// выбранных тестов из истории (тест без медианы на ядре — медиана другого ядра, иначе 30 с) и
// накладных расходов: деплой и смоук на смену продукта 180 с, переключение ядра 45 с, settle
// после Go 30 с, запуск pw-suite.sh 15 с на спек; проход-прогон и вложенная сессия — max_minutes
// или 15 мин. dry-run печатает `Оценка: N мин (бюджет 20 мин)` и при превышении — `сверх
// бюджета: причина — <budget_reason>` или `сверх бюджета: нет budget_reason`. С --budget (только
// вместе с --dry-run) спека сверх бюджета без budget_reason — код 4: проверка плана не пускает
// спеку на часы без причины. Настоящий запуск сверх бюджета не запрещён: строка предупреждения в
// session.log и stderr, сессия идёт. После возврата стенда отчёты этапов сессии и прогонов
// наборов записываются в историю (history.mjs append): оценки следующих сессий опираются на
// них; ошибка записи код сессии не меняет (строка в «Стенд в конце»).
//
// Порядок: проходы по очереди; внутри прохода ядра змейкой, первым — активное. На N проходов
// приходится не больше N переключений, и ещё одно в конце, если стенд не на исходном ядре.
// Сборка ставится в начале и при смене продукта.
//
// Отчёт — в RT_REPORT: <NN>-<этап>/ — отчёты этапов в формате router-test (meta,
// <цель>/results.tsv), spec.json, session.log (вывод всех этапов), summary.md и session.json
// (итог по ожиданиям).
//
// Режимы без стенда (из корня репозитория):
//   node scripts/router/session.mjs --dry-run <спека>   план этапов, порядок и оценка
//   node scripts/router/session.mjs --dry-run --budget <спека>
//                                                       то же; сверх бюджета без
//                                                       budget_reason — код 4
//   node scripts/router/session.mjs --target <спека>    проверить спеку и напечатать цель
//
// Код выхода сессии: 0 — все ожидания совпали и стенд вернулся в исходное состояние;
// 1 — расхождение, или этап сломался, или стенд не вернулся; 2 — неверная спека или запуск
// не через run.sh; 3 — сбой связи ПК↔стенд (не красный прогон, разбор по AGENTS.md; в том
// числе FAIL reachable во вложенном прогоне набора); 4 — только --dry-run --budget: оценка
// сверх бюджета, budget_reason нет; 130 — прерывание.
// Прерывание во время вложенного прогона: сигнал уходит всей группе процессов текущего этапа
// (каждый этап и вложенный run.sh идут отдельной группой), сессия ждёт выхода run.sh (стенд по
// снимку он восстанавливает сам) и опустения группы; повторный сигнал — выход без возврата.
// Прерывание по спеке (interrupt) сессию не прерывает: после выхода вложенного прогона
// сессия сверяет вердикты и продолжает следующие проходы.

import { execFileSync, spawn, spawnSync } from 'node:child_process';
import {
  appendFileSync,
  closeSync,
  existsSync,
  fstatSync,
  mkdirSync,
  openSync,
  readdirSync,
  readFileSync,
  readSync,
  realpathSync,
  rmSync,
  symlinkSync,
  unlinkSync,
  writeFileSync
} from 'node:fs';
import { constants as osConstants } from 'node:os';
import path from 'node:path';
import { StringDecoder } from 'node:string_decoder';
import { fileURLToPath } from 'node:url';

import coreMarks from './core-marks.cjs';
import { appendReport, durations, readHistory } from './history.mjs';

const { goMarks, specFacts } = coreMarks;
const SCRIPT = fileURLToPath(import.meta.url);
const SCRIPT_DIR = path.dirname(SCRIPT);
const CORES = ['xray', 'mihomo'];
const STATUSES = ['PASS', 'FAIL', 'KNOWN', 'XPASS', 'SKIP', 'QPASS', 'QFAIL'];
// Статусы теста в карантине (метка quarantine, D-17): на итог не влияют, если их не ждут явно.
const QUARANTINE = new Set(['QPASS', 'QFAIL']);
const SAFE_PATH = /^[A-Za-z0-9_.][A-Za-z0-9_./-]*$/;
const SAFE_NAME = /^[a-z0-9][a-z0-9-]*$/;
const SAFE_REF = /^[A-Za-z0-9_.][A-Za-z0-9_./~^-]*$/;
// Тег релиза: стабильный или пре-релиз с любым буквенным суффиксом (rc, beta).
const RELEASE_TAG = /^v\d+\.\d+\.\d+(-[a-z]+\.\d+)?$/;
// Снимок незавершённого прогона на цели (RT_SNAP_DIR из lib.sh): с ним preflight отказывает.
const SNAP_BASE = '/opt/tmp/xcp-rt-snap/baseline';
// Служебные строки этапов: всегда расхождение, если их FAIL не ожидался явно.
const SERVICE = new Set(['pw:run', 'pw:setup']);
// Окно истории для медиан длительности (дни); проверки без строк в окне берут всю историю.
const HISTORY_DAYS = 30;

// Префикс ошибок разбора вложенной спеки (проход-прогон session).
let dieCtx = '';

function die(msg, code = 2) {
  process.stderr.write(`session: ${dieCtx}${msg}\n`);
  process.exit(code);
}

function git(...args) {
  return execFileSync('git', args, { encoding: 'utf8' }).trim();
}

// --- аргументы и спека --------------------------------------------------------

function parseArgs(argv) {
  const out = { spec: '', mode: 'run', budget: false };
  for (const a of argv) {
    if (a === '--dry-run') out.mode = 'dry-run';
    else if (a === '--target') out.mode = 'target';
    else if (a === '--budget') out.budget = true;
    else if (a === '-h' || a === '--help') {
      const lines = readFileSync(SCRIPT, 'utf8').split('\n').slice(1);
      const head = lines.slice(
        0,
        lines.findIndex((l) => !l.startsWith('//'))
      );
      process.stdout.write(head.map((l) => l.replace(/^\/\/ ?/, '')).join('\n') + '\n');
      process.exit(0);
    } else if (!out.spec && !a.startsWith('--')) out.spec = a;
    else die(`неизвестный аргумент: ${a}`);
  }
  if (!out.spec) die('usage: session.mjs [--dry-run [--budget] | --target] <спека.json>');
  if (out.budget && out.mode !== 'dry-run') die('--budget — только вместе с --dry-run');
  return out;
}

function parseStatus(s, where) {
  if (typeof s !== 'string' || !s) die(`${where}: статус должен быть строкой`);
  const parts = s.split('|');
  for (const p of parts) {
    if (!STATUSES.includes(p)) die(`${where}: неизвестный статус «${p}» (${STATUSES.join(', ')})`);
  }
  return parts;
}

// Статусы, которые ожидание прохода допускает для теста: явное ожидание из tests (строго, в том
// числе QPASS и QFAIL), иначе default; без них тест справочный — допустимо всё, кроме FAIL и
// XPASS. Тест в карантине (QPASS, QFAIL) вне tests — не несовпадение ни под default, ни справочно.
function expectedOf(expect, name) {
  const want = expect.tests.get(name);
  if (want) return want.status;
  if (SERVICE.has(name)) return ['PASS'];
  if (expect.def) return expect.def;
  return ['справочно'];
}

function allowedStatus(expect, name, status) {
  if (QUARANTINE.has(status) && !expect.tests.has(name) && !SERVICE.has(name)) return true;
  const want = expectedOf(expect, name);
  if (want[0] === 'справочно') return status !== 'FAIL' && status !== 'XPASS';
  return want.includes(status);
}

// Поле interrupt прохода-прогона: прерывание вложенного run.sh посреди этапа go.
function loadInterrupt(i, where) {
  if (!i || typeof i !== 'object' || Array.isArray(i)) die(`${where}.interrupt: объект`);
  for (const k of Object.keys(i)) {
    if (!['stage', 'signal', 'after', 'deadline'].includes(k)) {
      die(`${where}.interrupt.${k}: допустимы stage, signal, after, deadline`);
    }
  }
  if (i.stage !== 'go') die(`${where}.interrupt.stage: поддерживается только go`);
  const signal = i.signal ?? 'TERM';
  if (signal !== 'TERM' && signal !== 'INT') die(`${where}.interrupt.signal: TERM или INT`);
  if (!Number.isInteger(i.after) || i.after < 1 || i.after > 600) {
    die(`${where}.interrupt.after: целое от 1 до 600 (с)`);
  }
  const deadline = i.deadline ?? 600;
  if (!Number.isInteger(deadline) || deadline < 30 || deadline > 3600) {
    die(`${where}.interrupt.deadline: целое от 30 до 3600 (с)`);
  }
  return { stage: 'go', signal, after: i.after, deadline };
}

// Название прохода-прогона для плана и журнала: «smoke (релиз <тег>)», «changed по файлам …».
function suiteTitle(p, release) {
  if (p.suite === 'smoke') return `smoke (релиз ${release})`;
  if (p.suite === 'session') return `session по ${p.nested.path}`;
  if (p.suite === 'delta') {
    return `delta (релиз ${release}${p.dryRun ? ', dry_run' : ''})`;
  }
  if (p.suite === 'rerun') return `rerun по отчёту прохода ${p.from}`;
  if (p.suite === 'bisect') {
    const b = p.bisect;
    return `bisect ${b.test} на ${b.core}: ${b.from}..${b.to} (${b.mode})`;
  }
  return `${p.suite}${p.files.length ? ` по файлам ${p.files.join(' ')}` : ''}`;
}

// Проход-прогон набора: разбор и проверка полей. earlier — имена прогонов набора выше по спеке,
// ctx — { root, specFile, nested } внешней спеки (nested: спека сама вложенная).
// Поля прохода bisect (набор поиска виновного коммита): те же правила, что у run.sh до блокировки.
function loadBisect(p, where, root) {
  const t = p.test;
  if (typeof t !== 'string' || /[\n\r]/.test(t)) {
    die(`${where}.test: go:TestRouter<Имя>, go:<пакет>:TestRouter<Имя> или pw:<название>`);
  }
  const isPw = t.startsWith('pw:');
  if (isPw ? t.length <= 3 : !/^go:(?:[A-Za-z0-9_./-]+:)?TestRouter[A-Za-z0-9_]+$/.test(t)) {
    die(`${where}.test: go:TestRouter<Имя>, go:<пакет>:TestRouter<Имя> или pw:<название>`);
  }
  if (!CORES.includes(p.core)) die(`${where}.core: ${CORES.join(' или ')}`);
  const commitOk = (ref, key) => {
    if (typeof ref !== 'string' || !SAFE_REF.test(ref)) die(`${where}.${key}: недопустимая ссылка`);
    try {
      git('rev-parse', '--verify', '--quiet', `${ref}^{commit}`);
    } catch {
      die(`${where}.${key}: коммит ${ref} не найден`);
    }
  };
  commitOk(p.from, 'from');
  const to = p.to ?? 'HEAD';
  commitOk(to, 'to');
  const mode = p.mode ?? 'regress';
  if (mode !== 'regress' && mode !== 'fix') die(`${where}.mode: regress или fix`);
  let spec = '';
  if (isPw) {
    if (typeof p.spec !== 'string') die(`${where}.spec: у pw: обязателен — tests/router/<имя>.spec.ts`);
    spec = p.spec.includes('/') ? p.spec : `tests/router/${p.spec}`;
    if (!/^tests\/router\/[A-Za-z0-9_.-]+\.spec\.ts$/.test(spec)) {
      die(`${where}.spec: ожидается tests/router/<имя>.spec.ts`);
    }
    if (!existsSync(path.join(root, 'frontend', spec))) die(`${where}.spec: нет файла frontend/${spec}`);
  } else if (p.spec !== undefined) {
    die(`${where}.spec: только у bisect с pw:`);
  }
  let grep = '';
  if (p.grep !== undefined) {
    if (!isPw) die(`${where}.grep: только у bisect с pw:`);
    if (typeof p.grep !== 'string' || !p.grep || /[\n\r']/.test(p.grep)) {
      die(`${where}.grep: одна строка без одинарной кавычки`);
    }
    grep = p.grep;
  }
  return { test: t, core: p.core, from: p.from, to, mode, spec, grep };
}

const SUITE_KINDS = ['full', 'changed', 'smoke', 'session', 'harness', 'rerun', 'delta', 'bisect'];

function loadSuitePass(p, where, earlier, ctx) {
  if (!SUITE_KINDS.includes(p.suite)) {
    die(`${where}.suite: full, changed, smoke, session, harness, rerun, delta или bisect`);
  }
  if (p.go || p.specs !== undefined || (p.grep !== undefined && p.suite !== 'bisect') || p.product) {
    die(`${where}: проход-прогон не сочетается с go/specs/product`);
  }
  if (p.version !== undefined) die(`${where}.version: только у прохода go/specs`);
  if (p.tree !== undefined) die(`${where}.tree: только у прохода go/specs`);
  // поля наборов механики (148.1-16)
  for (const k of ['test', 'core', 'to', 'mode']) {
    if (p[k] !== undefined && p.suite !== 'bisect') die(`${where}.${k}: только у bisect`);
  }
  if (p.from !== undefined && p.suite !== 'rerun' && p.suite !== 'bisect') {
    die(`${where}.from: только у rerun и bisect`);
  }
  if (p.dry_run !== undefined && p.suite !== 'delta') die(`${where}.dry_run: только у delta`);
  if (['rerun', 'bisect'].includes(p.suite) && p.interrupt !== undefined) {
    die(`${where}.interrupt: у ${p.suite} не поддерживается`);
  }
  let from = null;
  let dryRun = false;
  let bisect = null;
  if (p.suite === 'rerun') {
    if (typeof p.from !== 'string' || !ctx.runs.includes(p.from)) {
      die(`${where}.from: нужно имя более раннего прохода-прогона (его отчёт перегоняется)`);
    }
    from = p.from;
  }
  if (p.suite === 'delta' && p.dry_run !== undefined) {
    if (typeof p.dry_run !== 'boolean') die(`${where}.dry_run: true или false`);
    dryRun = p.dry_run;
  }
  if (p.suite === 'bisect') bisect = loadBisect(p, where, ctx.root);
  let nested = null;
  if (p.suite === 'session') {
    if (ctx.nested) die(`${where}: вложенная сессия не может содержать сессию`);
    if (p.interrupt !== undefined) die(`${where}.interrupt: у session не поддерживается`);
    if (typeof p.spec !== 'string' || !SAFE_PATH.test(p.spec) || p.spec.includes('..')) {
      die(`${where}.spec: путь спеки от корня рабочей копии без «..»`);
    }
    const file = path.join(ctx.root, p.spec);
    if (!existsSync(file)) die(`${where}.spec: нет файла ${p.spec}`);
    if (realpathSync(file) === realpathSync(ctx.specFile)) {
      die(`${where}.spec: вложенная спека совпадает с внешней`);
    }
    dieCtx = `${where}.spec ${p.spec}: `;
    const inner = loadSpec(file, ctx.root, true);
    dieCtx = '';
    nested = { path: p.spec, passes: inner.passes.length, target: inner.target };
    if (inner.target !== ctx.target) {
      die(`${where}.spec: цель вложенной спеки ${inner.target}, у внешней ${ctx.target}`);
    }
  } else if (p.spec !== undefined && p.suite !== 'bisect') {
    die(`${where}.spec: только у session и bisect`);
  }
  let release = null;
  if (p.suite === 'smoke' || p.suite === 'delta') {
    if (typeof p.release !== 'string' || (p.release !== 'latest' && !RELEASE_TAG.test(p.release))) {
      die(`${where}.release: у ${p.suite} обязателен — latest или тег vX.Y.Z[-<суффикс>.N]`);
    }
    release = p.release;
    if (p.interrupt !== undefined) die(`${where}.interrupt: у ${p.suite} не поддерживается`);
  } else if (p.release !== undefined) {
    die(`${where}.release: только у smoke и delta`);
  }
  let files = [];
  if (p.files !== undefined) {
    if (p.suite !== 'changed') die(`${where}.files: только у changed`);
    if (!Array.isArray(p.files) || !p.files.length) die(`${where}.files: непустой список путей`);
    for (const f of p.files) {
      if (typeof f !== 'string' || !SAFE_PATH.test(f) || f.includes('..')) {
        die(`${where}.files: недопустимый путь ${f}`);
      }
    }
    files = [...new Set(p.files)];
  }
  const e = p.expect ?? {};
  for (const k of Object.keys(e)) {
    if (!['exit', 'summary', 'same_failures_as', 'max_minutes'].includes(k)) {
      die(
        `${where}.expect.${k}: у прохода-прогона только exit, summary, same_failures_as, max_minutes`
      );
    }
  }
  if (e.exit !== undefined && (!Number.isInteger(e.exit) || e.exit < 0)) {
    die(`${where}.expect.exit: целое от 0`);
  }
  if (
    e.max_minutes !== undefined &&
    (!Number.isInteger(e.max_minutes) || e.max_minutes < 1 || e.max_minutes > 600)
  ) {
    die(`${where}.expect.max_minutes: целое от 1 до 600`);
  }
  if (p.suite === 'session' && e.same_failures_as !== undefined) {
    die(`${where}.expect.same_failures_as: у session не поддерживается`);
  }
  const summary = e.summary ?? [];
  if (!Array.isArray(summary) || summary.some((x) => typeof x !== 'string' || !x)) {
    die(`${where}.expect.summary: список непустых строк`);
  }
  let same = null;
  if (e.same_failures_as !== undefined) {
    if (typeof e.same_failures_as !== 'string' || !earlier.includes(e.same_failures_as)) {
      die(`${where}.expect.same_failures_as: нужно имя более раннего прохода-прогона`);
    }
    same = e.same_failures_as;
  }
  return {
    name: p.name,
    suite: p.suite,
    release,
    files,
    nested,
    from,
    dryRun,
    bisect,
    interrupt: p.interrupt === undefined ? null : loadInterrupt(p.interrupt, where),
    version: null,
    product: null,
    go: null,
    specs: [],
    grep: '',
    expect: { exit: e.exit ?? 0, summary, same, maxMinutes: e.max_minutes ?? null }
  };
}

// nested — спека прохода-прогона session (глубина вложения 1: своих session у неё нет).
function loadSpec(file, root, nested = false) {
  let spec;
  try {
    spec = JSON.parse(readFileSync(file, 'utf8'));
  } catch (e) {
    die(`спека ${file} не читается: ${e.message}`);
  }
  if (typeof spec.target !== 'string' || !/^[a-z0-9]+$/.test(spec.target)) {
    die('target: id цели из a-z и 0-9');
  }
  const cores = spec.cores ?? CORES;
  if (!Array.isArray(cores) || !cores.length || cores.some((c) => !CORES.includes(c))) {
    die(`cores: непустой список из ${CORES.join(', ')}`);
  }
  if (new Set(cores).size !== cores.length) die('cores: ядро указано дважды');
  const keepGoing = spec.keep_going ?? false;
  if (typeof keepGoing !== 'boolean') die('keep_going: true или false');
  // причина спеки сверх бюджета (D-23): одна непустая строка до 300 символов
  let budgetReason = null;
  if (spec.budget_reason !== undefined) {
    const r = spec.budget_reason;
    if (typeof r !== 'string' || !r.trim() || r.length > 300 || /[\n\r]/.test(r)) {
      die('budget_reason: непустая строка до 300 символов без перевода строки');
    }
    budgetReason = r.trim();
  }
  if (!Array.isArray(spec.passes) || !spec.passes.length) die('passes: непустой список');
  const names = new Set();
  const suites = [];
  const runs = [];
  const passes = spec.passes.map((p, i) => {
    const where = `passes[${i}]`;
    if (typeof p.name !== 'string' || !SAFE_NAME.test(p.name)) die(`${where}.name: [a-z0-9-]`);
    if (names.has(p.name)) die(`${where}.name: «${p.name}» повторяется`);
    names.add(p.name);
    if (p.suite === undefined && p.interrupt !== undefined) {
      die(`${where}.interrupt: только у прохода-прогона`);
    }
    if (p.suite !== undefined && p.cores !== undefined) {
      die(`${where}.cores: только у прохода go/specs`);
    }
    if (p.suite !== undefined) {
      const sp = loadSuitePass(p, where, suites, {
        root,
        specFile: file,
        nested,
        target: spec.target,
        runs
      });
      // same_failures_as сравнивает строки results.tsv прогонов наборов; у вложенной сессии их нет
      if (sp.suite !== 'session') suites.push(sp.name);
      // rerun.from ссылается на любой более ранний проход-прогон, в том числе на вложенную сессию
      runs.push(sp.name);
      return sp;
    }
    let version = null;
    if (p.version !== undefined) {
      if (p.product) die(`${where}: version и product не сочетаются`);
      if (typeof p.version !== 'string' || (p.version !== 'latest' && !RELEASE_TAG.test(p.version))) {
        die(`${where}.version: latest или тег vX.Y.Z[-<суффикс>.N] вышедшего релиза`);
      }
      if (p.version !== 'latest' && !localTagExists(p.version)) {
        die(
          `${where}.version: тега ${p.version} нет — нужен номер вышедшего релиза (git fetch --tags)`
        );
      }
      version = p.version;
    }
    // дерево коммита: продукт и тесты из коммита (worktree), обвязка (скрипты этапов) — из HEAD
    let tree = null;
    let treeSha = null;
    if (p.tree !== undefined) {
      if (p.product || version !== null) die(`${where}: tree не сочетается с product и version`);
      if (typeof p.tree !== 'string' || !SAFE_REF.test(p.tree)) {
        die(`${where}.tree: недопустимая ссылка`);
      }
      try {
        treeSha = git('rev-parse', '--verify', '--quiet', `${p.tree}^{commit}`);
      } catch {
        die(`${where}.tree: коммит ${p.tree} не найден`);
      }
      tree = p.tree;
    }
    let product = null;
    if (p.product) {
      const { ref, files } = p.product;
      if (typeof ref !== 'string' || !SAFE_REF.test(ref))
        die(`${where}.product.ref: недопустимая ссылка`);
      let sha;
      try {
        sha = git('rev-parse', '--verify', '--quiet', `${ref}^{commit}`);
      } catch {
        die(`${where}.product.ref: коммит ${ref} не найден`);
      }
      if (!Array.isArray(files) || !files.length) die(`${where}.product.files: непустой список`);
      for (const f of files) {
        if (typeof f !== 'string' || !SAFE_PATH.test(f) || f.includes('..')) {
          die(`${where}.product.files: недопустимый путь ${f}`);
        }
        if (spawnSync('git', ['cat-file', '-e', `${sha}:${f}`]).status !== 0) {
          die(`${where}.product.files: ${f} нет в ${ref}`);
        }
      }
      product = { ref, sha, files: [...new Set(files)] };
    }
    let go = null;
    if (p.go) {
      const pkgs = String(p.go.pkgs || '');
      if (!pkgs.split(' ').every((s) => SAFE_PATH.test(s)))
        die(`${where}.go.pkgs: пакеты через пробел`);
      const run = p.go.run === undefined ? '' : String(p.go.run);
      if (/[\n\r]/.test(run)) die(`${where}.go.run: одна строка`);
      go = { pkgs, run };
    }
    const specs = p.specs ?? [];
    if (!Array.isArray(specs)) die(`${where}.specs: список путей от frontend/`);
    for (const s of specs) {
      // спек прохода tree берётся из дерева коммита: в HEAD его может уже не быть
      const present =
        typeof s === 'string' && SAFE_PATH.test(s) && !s.includes('..')
          ? treeSha
            ? spawnSync('git', ['cat-file', '-e', `${treeSha}:frontend/${s}`]).status === 0
            : existsSync(path.join(root, 'frontend', s))
          : false;
      if (!present) {
        die(`${where}.specs: нет спека frontend/${s}${treeSha ? ` в ${tree}` : ''}`);
      }
    }
    const grep = p.grep === undefined ? '' : String(p.grep);
    if (/[\n\r]/.test(grep)) die(`${where}.grep: одна строка`);
    if (grep && !specs.length) die(`${where}.grep без specs: укажите спеки явно`);
    if (!go && !specs.length) die(`${where}: нечего запускать (нет go и specs)`);
    const e = p.expect ?? {};
    const expect = { count: null, def: null, tests: new Map() };
    if (e.count !== undefined) {
      if (!Number.isInteger(e.count) || e.count < 1) die(`${where}.expect.count: целое больше 0`);
      expect.count = e.count;
    }
    if (e.default !== undefined) expect.def = parseStatus(e.default, `${where}.expect.default`);
    for (const [name, t] of Object.entries(e.tests ?? {})) {
      if (!/^(pw|go):./.test(name))
        die(`${where}.expect.tests: имя «${name}» должно начинаться с pw: или go:`);
      const tw = `${where}.expect.tests["${name}"]`;
      const status = parseStatus(typeof t === 'string' ? t : t.status, tw);
      const detail = typeof t === 'object' && t.detail !== undefined ? String(t.detail) : null;
      expect.tests.set(name, { status, detail });
    }
    if (!expect.def && !expect.tests.size && expect.count === null) {
      die(`${where}.expect: нужен хотя бы один из count, default, tests`);
    }
    // ядра прохода: перекрывают правило «ядра как в гейте» — все тесты на каждом из них
    let pcores = null;
    if (p.cores !== undefined) {
      if (!Array.isArray(p.cores) || !p.cores.length || p.cores.some((c) => !CORES.includes(c))) {
        die(`${where}.cores: непустой список из ${CORES.join(', ')}`);
      }
      if (new Set(p.cores).size !== p.cores.length) die(`${where}.cores: ядро указано дважды`);
      pcores = [...p.cores];
    }
    return { name: p.name, version, tree, treeSha, product, go, specs, grep, expect, cores: pcores };
  });
  return { target: spec.target, cores, keepGoing, budgetReason, passes };
}

// Ядра прохода змейкой: сначала активное, затем остальные в порядке спеки.
function coreOrder(cores, cur) {
  return cores.includes(cur) ? [cur, ...cores.filter((c) => c !== cur)] : [...cores];
}

// --- ядра как в гейте: состав прохода на ядре -----------------------------------

// Роутерные Go-тесты прохода из исходников: файлы *_test.go из индекса git с первой строкой
// `//go:build router` (как routerGoFiles в core-marks), объявления `func TestRouter<Имя>(` в
// колонке 0 пакетов go.pkgs (без pkgs — все роутерные пакеты), отобранные регэкспом go.run.
// Имя — только по шаблону TestRouter[A-Za-z0-9_]+ (оно идёт в -test.run). Итог — { legacy: null,
// tests: [{ pkg, test, marked }] } (marked — метка ядра по goMarks) или { legacy: причина }:
// состав не определить, Go-часть идёт прежним способом на всех ядрах.
function goFacts(root, go) {
  const pkgs = go.pkgs
    .split(' ')
    .filter(Boolean)
    .map((s) => s.replace(/^\.\//, '').replace(/\/$/, ''));
  if (pkgs.some((s) => s.includes('...'))) return { legacy: 'пакеты с «...»' };
  if (go.run.includes('/')) return { legacy: 'регэксп go.run с подтестами' };
  let re;
  try {
    re = new RegExp(go.run || '^TestRouter');
  } catch {
    return { legacy: 'регэксп go.run не разбирается' };
  }
  const want = pkgs.length ? new Set(pkgs) : null;
  const files = execFileSync('git', ['ls-files', '--', 'internal', 'cmd'], {
    cwd: root,
    encoding: 'utf8',
    maxBuffer: 64 * 1024 * 1024
  })
    .split('\n')
    .filter((f) => /^(internal|cmd)\/.*_test\.go$/.test(f) && !f.startsWith('internal/routertest/'));
  const tests = [];
  const seen = new Set();
  for (const f of files) {
    const pkg = path.posix.dirname(f);
    if (want && !want.has(pkg)) continue;
    let text;
    try {
      text = readFileSync(path.join(root, f), 'utf8');
    } catch {
      continue;
    }
    if (text.split('\n', 1)[0].trim() !== '//go:build router') continue;
    for (const m of text.matchAll(/^func (TestRouter[A-Za-z0-9_]+)\(/gm)) {
      const key = `${pkg}\t${m[1]}`;
      if (seen.has(key) || !re.test(m[1])) continue;
      seen.add(key);
      tests.push({ pkg, test: m[1], marked: false });
    }
  }
  if (!tests.length) return { legacy: 'регэксп go.run не нашёл тестов в пакетах' };
  const marks = new Set(
    goMarks(root, [...new Set(tests.map((t) => t.pkg))]).map((m) => `${m.pkg}\t${m.test}`)
  );
  for (const t of tests) t.marked = marks.has(`${t.pkg}\t${t.test}`);
  return { legacy: null, tests };
}

// Спек с меткой ядра (specFacts): тег ядра (CORE_TAG, '@core', coreTagsOf) или метка
// knownFailure с ядром в селекторе — тег @core-label.
function specMarked(root, spec) {
  const f = specFacts(readFileSync(path.join(root, 'frontend', spec), 'utf8'));
  return f.coreTagged || f.coreLabelSelectors.length > 0;
}

// Факты прохода (считаются один раз): Go-тесты и метки спеков.
function passFacts(p, root) {
  if (!p.facts) {
    p.facts = {
      go: p.go ? goFacts(root, p.go) : null,
      specs: new Map(p.specs.map((s) => [s, specMarked(root, s)]))
    };
  }
  return p.facts;
}

// --- порядок по истории: дешёвые тесты первыми ------------------------------------

// Медианы длительности из истории прогонов (history.mjs): окно HISTORY_DAYS и вся история.
// Истории нет или она не читается — медиан нет, все тесты неизвестные.
function loadMedians() {
  let rows = [];
  try {
    rows = readHistory();
  } catch {
    rows = [];
  }
  return { recent: durations(rows, { days: HISTORY_DAYS }), all: durations(rows) };
}

// Медиана проверки на ядре (секунды) или null — проверка истории неизвестна.
function medianOf(h, check, core) {
  const k = `${check}\t${core}`;
  return h.recent.get(k)?.median ?? h.all.get(k)?.median ?? null;
}

// Тесты спеков по `playwright test --list` (без стенда; XCP_CORE — ядро: названия некоторых
// тестов содержат ядро): Map путь спека → [{ name: 'pw:…' как в results.tsv, core: тег ядра }]
// или null — список не получен. Адрес и пароль панели в окружение не передаются.
const pwListCache = new Map();
function pwList(root, specs, grep, core) {
  const key = JSON.stringify([specs, grep, core]);
  if (pwListCache.has(key)) return pwListCache.get(key);
  let out = null;
  const frontend = path.join(root, 'frontend');
  const bin = path.join(frontend, 'node_modules', '.bin', 'playwright');
  if (specs.length && existsSync(bin)) {
    const env = Object.fromEntries(
      Object.entries(process.env).filter(([k]) => !k.startsWith('XCP_'))
    );
    env.XCP_CORE = core;
    const args = ['test', '-c', 'playwright.router.config.ts', '--list', '--reporter=json'];
    if (grep) args.push('--grep', grep);
    let text = '';
    try {
      text = execFileSync(bin, [...args, ...specs], {
        cwd: frontend,
        env,
        encoding: 'utf8',
        timeout: 60000,
        maxBuffer: 64 * 1024 * 1024,
        stdio: ['ignore', 'pipe', 'ignore']
      });
    } catch (e) {
      // «No tests found» — код 1 с JSON в stdout
      text = typeof e.stdout === 'string' ? e.stdout : '';
    }
    try {
      const json = JSON.parse(text);
      const errors = Array.isArray(json.errors) ? json.errors : [];
      if (
        Array.isArray(json.suites) &&
        errors.every((er) => /No tests found/.test(String(er.message ?? '')))
      ) {
        out = new Map(specs.map((s) => [s, []]));
        const bySuite = new Map(specs.map((s) => [s.replace(/^tests\/router\//, ''), s]));
        // имя — как в pw-parse.js и history.mjs: заголовки через « > », верхний набор с title,
        // равным file, пропускается
        const clean = (t) => {
          const flat = String(t || '')
            .replace(/\s+/g, ' ')
            .trim();
          return flat.length > 300 ? flat.slice(0, 300) + '…' : flat;
        };
        const visit = (node, titles, list) => {
          for (const sp of node.specs || []) {
            const name = `pw:${clean([...titles, sp.title].join(' > '))}`;
            const tagged =
              (sp.tags || []).some((t) => /^@?core/.test(t)) || /@core/.test(sp.title);
            list.push({ name, core: tagged });
          }
          for (const child of node.suites || []) visit(child, [...titles, child.title], list);
        };
        let matched = 0;
        for (const top of json.suites) {
          const s = bySuite.get(top.file);
          if (!s) continue;
          matched++;
          visit(top, top.file === top.title ? [] : [top.title], out.get(s));
        }
        // наборы есть, но ни один не сопоставился со спеком — форма ответа не та
        if (json.suites.length && !matched) out = null;
      }
    } catch {
      out = null;
    }
  }
  pwListCache.set(key, out);
  return out;
}

// Порядок: неизвестные истории (нет медианы для проверки и ядра) первыми в исходном порядке,
// затем по возрастанию медианы.
function cheapFirst(items) {
  const unknown = items.filter((x) => x.median === null);
  const known = items.filter((x) => x.median !== null).sort((a, b) => a.median - b.median);
  return [...unknown, ...known];
}

// Состав прохода на ядре. full — все тесты (первое ядро прохода или поле cores прохода), иначе
// только тесты с меткой ядра. Итог: { full, go: null | { legacy, tests }, specs: [{ spec, names,
// median }], coreOnly: спеки идут с --grep @core, empty: на ядре нечего запускать }. Go-тесты и
// спеки упорядочены: дешёвые первыми (cheapFirst; медиана спека — сумма медиан его тестов, спек с
// неизвестным тестом или без списка — неизвестный). На ядре без полного состава размеченный спек —
// по списку Playwright (тест с тегом ядра есть после --grep), без списка — по specFacts.
function composition(p, full, root, core, h) {
  const f = passFacts(p, root);
  let go = null;
  if (f.go?.legacy) {
    // состав не определён: Go-часть на каждом ядре целиком, как до правила
    go = { legacy: f.go.legacy, tests: [] };
  } else if (f.go) {
    const tests = f.go.tests
      .filter((t) => full || t.marked)
      .map((t) => ({ ...t, median: medianOf(h, `go:${t.test}`, core) }));
    if (tests.length) go = { legacy: null, tests: cheapFirst(tests) };
  }
  const list = p.specs.length ? pwList(root, p.specs, p.grep, core) : null;
  let specs = full
    ? p.specs
    : list
      ? p.specs.filter((s) => list.get(s).some((t) => t.core))
      : p.specs.filter((s) => f.specs.get(s));
  specs = specs.map((s) => {
    const names = list ? list.get(s).filter((t) => full || t.core) : null;
    const ms = names ? names.map((t) => medianOf(h, t.name, core)) : [];
    const median =
      names && names.length && ms.every((m) => m !== null) ? ms.reduce((a, b) => a + b, 0) : null;
    return { spec: s, names, median };
  });
  return {
    full,
    go,
    specs: cheapFirst(specs),
    coreOnly: !full,
    empty: !go && !specs.length
  };
}

// --- оценка длительности и бюджет спеки (D-23) -------------------------------------

// Бюджет размера спеки, минут: оценка больше — нужен budget_reason (проверка плана --budget).
const BUDGET_MIN = 20;
// Накладные расходы оценки, секунды: деплой и смоук на смену продукта, переключение ядра,
// settle после Go, запуск pw-suite.sh на спек, тест без медианы; проход-прогон — max_minutes
// или suiteMinutes минут.
const EST = { deploy: 180, switch: 45, settle: 30, pwStart: 15, unknownTest: 30, suiteMinutes: 15 };

// Оценка теста на ядре: медиана на этом ядре, иначе на другом, иначе unknownTest.
function testSeconds(h, check, core) {
  const other = CORES.find((c) => c !== core);
  return medianOf(h, check, core) ?? medianOf(h, check, other) ?? EST.unknownTest;
}

// Оценка прохода на ядре по составу: Go-тесты, settle, запуски pw-suite и тесты спеков. Спек
// без списка Playwright — unknownTest на каждый вызов test( в тексте спека (не меньше одного);
// Go без состава (legacy) — один неизвестный тест.
function passSeconds(comp, core, h, root) {
  let sec = 0;
  if (comp.go) {
    sec += comp.go.legacy
      ? EST.unknownTest
      : comp.go.tests.reduce((a, t) => a + testSeconds(h, `go:${t.test}`, core), 0);
    if (comp.specs.length) sec += EST.settle;
  }
  for (const s of comp.specs) {
    sec += EST.pwStart;
    if (s.names) {
      sec += s.names.reduce((a, t) => a + testSeconds(h, t.name, core), 0);
    } else {
      let n = 1;
      try {
        const text = readFileSync(path.join(root, 'frontend', s.spec), 'utf8');
        n = Math.max(1, (text.match(/(?<![\w.$])test\(/g) || []).length);
      } catch {
        // спек не читается — один тест
      }
      sec += n * EST.unknownTest;
    }
  }
  return sec;
}

// Имена до 10 первых и «… ещё N».
function headList(names) {
  const head = names.slice(0, 10).join(', ');
  return names.length > 10 ? `${head}, … ещё ${names.length - 10}` : head;
}

// Ядра прохода go/specs: поле cores прохода или ядра спеки.
function passCores(spec, p) {
  return p.cores ?? spec.cores;
}

// Строка состава для плана и журнала.
function compositionTitle(comp) {
  if (comp.full) return 'все';
  const goN = comp.go ? (comp.go.legacy ? `все (${comp.go.legacy})` : comp.go.tests.length) : 0;
  return `только с меткой ядра (Go ${goN}, спеков ${comp.specs.length})`;
}

// Файл version каталога деплоя или отчёта (ожидаемая версия, как её печатает xcp -v); нет — null.
function readVersionFile(dir) {
  try {
    const v = readFileSync(path.join(dir, 'version'), 'utf8').split('\n')[0].trim();
    return v || null;
  } catch {
    return null;
  }
}

// <отчёт этапа>/<цель>/version-stop.tsv (контракт сверки версии стенда): первая строка stop
// (ядро, граница, ожидалась, на стенде), строки notrun (ядро, что не выполнено) и число строк
// suspect. Нет файла — { stop: null, notrun: [], suspect: 0 }.
function readVersionStop(file) {
  const out = { stop: null, notrun: [], suspect: 0 };
  let text = '';
  try {
    text = readFileSync(file, 'utf8');
  } catch {
    return out;
  }
  for (const line of text.split('\n')) {
    const f = line.split('\t');
    if (f[0] === 'stop' && !out.stop) {
      out.stop = {
        core: f[1] || '-',
        boundary: f[2] || '-',
        expected: f[3] || '-',
        observed: f[4] || '-'
      };
    } else if (f[0] === 'notrun' && f[2]) {
      out.notrun.push({ core: f[1] || '-', what: f[2] });
    } else if (f[0] === 'suspect') {
      out.suspect++;
    }
  }
  return out;
}

// В отчёте есть строка stop: <отчёт>/<цель>/version-stop.tsv или то же в его каталогах этапов
// <NN>-<этап> (отчёт сессии).
function hasVersionStop(dir, target) {
  if (readVersionStop(path.join(dir, target, 'version-stop.tsv')).stop) return true;
  let ents = [];
  try {
    ents = readdirSync(dir, { withFileTypes: true });
  } catch {
    return false;
  }
  return ents.some(
    (e) =>
      e.isDirectory() &&
      /^\d{2}-/.test(e.name) &&
      readVersionStop(path.join(dir, e.name, target, 'version-stop.tsv')).stop !== null
  );
}

// Тег есть в локальном git (коммит, на который он указывает, достижим).
function localTagExists(tag) {
  return (
    spawnSync('git', ['rev-parse', '--verify', '--quiet', `refs/tags/${tag}^{commit}`]).status === 0
  );
}

// Номер последнего перепрогона в отчёте: наибольшее N среди rerun-N.meta (run.sh пишет его при старте
// перепрогона); 0 — перепрогонов не было.
function rerunNumber(dir) {
  let n = 0;
  try {
    for (const f of readdirSync(dir)) {
      const m = f.match(/^rerun-(\d+)\.meta$/);
      if (m) n = Math.max(n, Number(m[1]));
    }
  } catch {
    // каталога нет — перепрогонов тоже
  }
  return n;
}

// Новейший опубликованный (не draft) релиз GitHub; при сбое gh бросает.
function latestReleaseTag() {
  return execFileSync(
    'gh',
    [
      'release',
      'list',
      '--exclude-drafts',
      '--limit',
      '1',
      '--json',
      'tagName',
      '--jq',
      '.[0].tagName'
    ],
    { encoding: 'utf8', timeout: 30000 }
  ).trim();
}

// Ключ сборки: HEAD@<номер> всегда отличается от HEAD, поэтому смена номера вызывает деплой,
// а завершающий возврат стенда (deployed !== 'HEAD') ставит HEAD со своей версией.
function productKey(p) {
  if (p.tree) return `tree:${p.treeSha}`;
  if (p.version) return `HEAD@${p.resolved ?? p.version}`;
  return p.product ? `${p.product.sha}:${p.product.files.join(',')}` : 'HEAD';
}

// План сессии без стенда и оценка её длительности. Исходное ядро неизвестно до смоука: план
// считает его первым в cores, на стенде с другим активным ядром порядок ядер зеркальный.
// Итог — { lines, minutes, over }.
function planSession(spec, specFile, root) {
  const c0 = spec.cores[0];
  const h = loadMedians();
  let seconds = 0;
  const lines = [
    `Сессия по ${specFile}: цель ${spec.target}, ядра ${spec.cores.join(', ')} (исходное считается ${c0})`,
    spec.keepGoing
      ? 'keep_going: все проходы до конца, полный список падений'
      : 'остановка на первом несовпадении (keep_going: false): Go — по ходу вывода, остальное — после этапа',
    'ядра как в гейте: на первом ядре прохода — все тесты, на остальных — только с меткой ядра; поле cores прохода — все тесты на своих ядрах',
    'дешёвые тесты первыми: неизвестные истории («?») в начале, затем по возрастанию медианы'
  ];
  let n = 0;
  const step = (s, sec = 0) => {
    seconds += sec;
    lines.push(`${++n}. ${s}`);
  };
  let deployed = null;
  let cur = c0;
  for (const p of spec.passes) {
    if (p.suite) {
      // прогон набора и вложенная сессия — max_minutes прохода или 15 мин
      seconds += (p.expect.maxMinutes ?? EST.suiteMinutes) * 60;
      // вложенный прогон ставит сборку HEAD сам и оставляет активным ядро, найденное в начале
      const irq = p.interrupt
        ? `; прерывание SIG${p.interrupt.signal} через ${p.interrupt.after} с после старта go, выход до ${p.interrupt.deadline} с`
        : '';
      if (p.suite === 'session') {
        step(
          `вложенная сессия по ${p.nested.path}, проходов: ${p.nested.passes}; отчёт build/router/<сессия>-NN-${p.name}`
        );
      } else if (p.suite === 'rerun') {
        step(
          `прогон набора ${suiteTitle(p, p.release)}: вложенный run.sh, перепрогон дописывается в отчёт прохода ${p.from}`
        );
      } else {
        step(
          `прогон набора ${suiteTitle(p, p.release)}: вложенный run.sh, отчёт build/router/<сессия>-NN-${p.name}${irq}`
        );
      }
      if (p.expect.maxMinutes !== null) {
        lines.push(
          `   max_minutes: прогон прохода ${p.name} — не дольше ${p.expect.maxMinutes} мин`
        );
      }
      // смоук релиза оставляет на стенде сборку релиза, а не HEAD; вложенная сессия
      // возвращает стенд сама (сборка HEAD и её исходное ядро)
      deployed = p.suite === 'smoke' || p.suite === 'delta' ? null : 'HEAD';
      continue;
    }
    const key = productKey(p);
    if (key !== deployed) {
      const target = p.tree
        ? `дерево ${p.tree}`
        : p.version
          ? `HEAD под номером ${p.version}`
          : p.product
            ? `${p.product.files.join(' ')} из ${p.product.ref}`
            : 'HEAD';
      step(`деплой и смоук: ${target}`, EST.deploy);
      deployed = key;
    }
    const what = (comp) => {
      const goPart = !comp.go
        ? ''
        : comp.full || comp.go.legacy
          ? `go ${p.go.pkgs}${p.go.run ? ` -run ${p.go.run}` : ''}`
          : `go ${[...new Set(comp.go.tests.map((t) => `./${t.pkg}`))].join(' ')}`;
      const grep = comp.coreOnly ? (p.grep ? `(?=.*@core)${p.grep}` : '@core') : p.grep;
      const pwPart = comp.specs.length
        ? `pw ${comp.specs.map((s) => s.spec).join(' ')}${grep ? ` --grep '${grep}'` : ''}`
        : '';
      return [goPart, pwPart].filter(Boolean).join(' + ');
    };
    const order = coreOrder(passCores(spec, p), cur);
    for (let j = 0; j < order.length; j++) {
      const c = order[j];
      const comp = composition(p, Boolean(p.cores) || j === 0, root, c, h);
      if (comp.empty) {
        lines.push(`   проход ${p.name} на ${c}: пропуск — тестов с меткой ядра нет (ядро не важно)`);
        continue;
      }
      if (c !== cur) step(`переключение на ${c}`, EST.switch);
      cur = c;
      step(
        `проход ${p.name} на ${c}: ${compositionTitle(comp)} — ${what(comp)}`,
        passSeconds(comp, c, h, root)
      );
      const mark = (x) => (x.median === null ? '?' : `${Math.round(x.median)} с`);
      if (comp.go && !comp.go.legacy) {
        lines.push(
          `   порядок Go (RT_GO_RUN_SEQ): ${headList(comp.go.tests.map((t) => `${t.test} ${mark(t)}`))}`
        );
      }
      if (comp.specs.length) {
        lines.push(
          `   порядок спеков (по запуску pw-suite на спек): ${headList(comp.specs.map((s) => `${path.basename(s.spec)} ${mark(s)}`))}`
        );
      }
    }
  }
  if (deployed !== 'HEAD') step('деплой и смоук: HEAD (возврат продукта)', EST.deploy);
  if (cur !== c0) step(`переключение на ${c0} (исходное)`, EST.switch);
  step(
    'проверка стенда: дерево чистое, сборка HEAD, исходное ядро, xcp запущен, снимка незавершённого прогона нет'
  );
  const minutes = Math.ceil(seconds / 60);
  const over = minutes > BUDGET_MIN;
  lines.push(`Оценка: ${minutes} мин (бюджет ${BUDGET_MIN} мин)`);
  if (over) {
    lines.push(
      spec.budgetReason
        ? `сверх бюджета: причина — ${spec.budgetReason}`
        : 'сверх бюджета: нет budget_reason'
    );
  }
  return { lines, minutes, over };
}

// План сессии без стенда: печать плана и оценки. Итог — { minutes, over }.
function dryRun(spec, specFile, root) {
  const plan = planSession(spec, specFile, root);
  process.stdout.write(plan.lines.join('\n') + '\n');
  return plan;
}

// --- сессия -------------------------------------------------------------------

class Session {
  constructor(spec, specFile, root) {
    this.spec = spec;
    this.root = root;
    this.t = spec.target;
    this.head = git('rev-parse', 'HEAD');
    this.dir = process.env.RT_REPORT;
    mkdirSync(this.dir, { recursive: true });
    writeFileSync(path.join(this.dir, 'spec.json'), readFileSync(specFile));
    this.log = path.join(this.dir, 'session.log');
    // медианы длительности из истории: порядок «дешёвые первыми»
    this.h = loadMedians();
    this.steps = [];
    this.verdicts = [];
    this.suites = [];
    this.suiteFails = new Map();
    // имя прохода-прогона -> каталог его отчёта (для rerun.from)
    this.suiteDirs = new Map();
    // проход tree: worktree дерева коммита, пока он идёт (этапы запускаются с его каталогом); treeUsed —
    // worktree создавались, нужен tree-step.sh cleanup
    this.treeWt = null;
    this.treeSha = null;
    this.treeUsed = false;
    this.notes = [];
    this.n = 0;
    this.c0 = null;
    this.cur = null;
    this.deployed = null;
    this.overridden = null;
    this.child = null;
    this.aborted = false;
    this.cleaning = false;
    this.network = false;
    this.broken = false;
    // остановка сессии: reason 'mismatch' — первое несовпадение { pass, core, test, expected,
    // actual, how, live, todo }; reason 'version' — смена версии стенда { pass, core, boundary,
    // expected, observed, stage, suspect, how, todo } (expected и observed — строки версий)
    this.stopped = null;
    // ожидаемая версия стенда для сверки на границах этапов: версия последнего деплоя сессии
    // (файл version каталога деплоя) или вложенного прогона; null — неизвестна, сверка выключена
    this.expectVersion = null;
    // вердикты с этим индексом и дальше ещё не проверены на остановку (checkStop)
    this.checked = 0;
  }

  say(msg) {
    const line = `session: ${msg}\n`;
    process.stdout.write(line);
    appendFileSync(this.log, line);
  }

  // Предупреждение: stderr и session.log.
  warn(msg) {
    const line = `session: ${msg}\n`;
    process.stderr.write(line);
    appendFileSync(this.log, line);
  }

  // Отчёты этапов сессии — в историю прогонов (D-20): каталог сессии (его этапы <NN>-<этап>) и
  // отчёты прогонов наборов. Повторная запись ничего не добавляет (history.idx); ошибка истории
  // код сессии не меняет — строка в notes.
  appendHistory() {
    const dirs = [this.dir, ...this.steps.filter((x) => x.kind === 'suite').map((x) => x.dir)];
    for (const d of [...new Set(dirs)]) {
      if (!existsSync(d)) continue;
      // отчёт со строкой stop (смена версии стенда) неполный и снят с чужой версии: в историю
      // не идёт, как и у run.sh; у отчёта сессии — если stop есть хоть в одном её этапе
      if (hasVersionStop(d, this.t)) {
        this.notes.push(
          `история: отчёт ${path.relative(this.root, d)} не записан — смена версии стенда, прогон неполный`
        );
        continue;
      }
      try {
        appendReport(d);
      } catch (e) {
        this.notes.push(`история: отчёт ${path.relative(this.root, d)} не записан: ${e.message}`);
      }
    }
  }

  // Каталог этапа в формате отчёта router-test (meta как у rt_report_init).
  stepDir(label) {
    const name = `${String(++this.n).padStart(2, '0')}-${label}`;
    const dir = path.join(this.dir, name);
    mkdirSync(path.join(dir, this.t), { recursive: true });
    const dirty = git('status', '--porcelain', '--untracked-files=no') !== '';
    writeFileSync(
      path.join(dir, 'meta'),
      `sha=${this.head}\ndirty=${dirty}\nstarted=${new Date().toISOString().slice(0, 19)}Z\n`
    );
    return dir;
  }

  // Запуск этапа: вывод — в консоль и session.log. Код выхода (сигнал — 128+n).
  // opts.detached — отдельная группа процессов; opts.onSpawn(child) вызывается сразу после
  // spawn и может вернуть функцию, которая будет вызвана при закрытии процесса; opts.drainIf()
  // — true, если после выхода лидера нужно дождаться опустения группы.
  run(cmd, args, env = {}, opts = {}) {
    if (this.aborted && !this.cleaning) return Promise.resolve(130);
    const base = { ...process.env };
    for (const k of [
      'RT_SPECS',
      'RT_PW_GREP',
      'RT_GO_PKGS',
      'RT_GO_RUN',
      // состав и порядок этапа задаёт сессия, не окружение оператора
      'RT_GO_RUN_SEQ',
      'RT_PW_CORE_ONLY',
      'RT_ART_SUB',
      'RT_SPECS_FIRST',
      'RT_REPORT',
      'RT_REPORT_DIR',
      'RT_CORE',
      'XCP_WANT_CORE',
      // номер сборки задаёт только проход с version (env его деплоя), не оператор и не прошлый этап
      'XCP_VERSION'
    ]) {
      delete base[k];
    }
    const full = { ...base, RT_LOCK_HELD: '1', ROUTERS: this.t, ...env };
    // undefined в env — снять переменную (вложенный прогон набора не наследует настройки сессии)
    for (const k of Object.keys(full)) if (full[k] === undefined) delete full[k];
    // проход tree: скрипты этапов — из HEAD (SCRIPT_DIR), каталог запуска — worktree дерева коммита,
    // поэтому deploy.sh, go-suite.sh и pw-suite.sh собирают и гоняют продукт и тесты коммита; отчёт этапа
    // rt_report_use принимает только под build/router каталога запуска — путь переписывается на ссылку
    // build/router worktree (enterTree направляет её на build/router этой копии)
    let cwd = this.root;
    if (this.treeWt) {
      cwd = this.treeWt;
      const prefix = path.join(this.root, 'build', 'router') + path.sep;
      if (typeof full.RT_REPORT === 'string' && full.RT_REPORT.startsWith(prefix)) {
        full.RT_REPORT = path.join(this.treeWt, 'build', 'router', full.RT_REPORT.slice(prefix.length));
      }
    }
    appendFileSync(this.log, `\n$ ${[cmd, ...args].join(' ')}\n`);
    return new Promise((resolve) => {
      const child = spawn(cmd, args, {
        cwd,
        env: full,
        stdio: ['ignore', 'pipe', 'pipe'],
        // каждый этап — своя группа процессов: сигнал прерывания уходит всей группе (этап и
        // его ssh, вложенный run.sh и его deploy.sh), а не одному pid
        detached: opts.detached !== false
      });
      this.child = child;
      this.childLeaderOnly = opts.leaderOnly === true;
      const onClose = opts.onSpawn ? opts.onSpawn(child) : null;
      const out = (d) => {
        process.stdout.write(d);
        appendFileSync(this.log, d);
      };
      child.stdout.on('data', out);
      child.stderr.on('data', out);
      child.on('close', (code, sig) => {
        this.child = null;
        if (onClose) onClose();
        const rc = code ?? 128 + (osConstants.signals[sig] ?? 15);
        // После прерывания потомки группы (deploy.sh вложенного прогона, ssh этапа) могли
        // пережить лидера: возврат стенда не стартует, пока группа не опустеет, иначе он шёл бы
        // одновременно с ними
        const pid = child.pid;
        // opts.drainIf — этап остановлен сессией (первое несовпадение): ждать группу и при коде < 128
        const interrupted = this.aborted || rc >= 128 || (opts.drainIf ? opts.drainIf() : false);
        if (opts.drain !== false && interrupted && Number.isInteger(pid) && pid > 1) {
          this.drainGroup(pid).then(() => resolve(rc));
        } else {
          resolve(rc);
        }
      });
    });
  }

  // Сигнал текущему этапу: всей его группе (лидер группы — сам этап, см. run); не вышло —
  // самому процессу.
  signalChild(sig) {
    const c = this.child;
    if (!c) return;
    const pid = c.pid;
    // вложенная сессия: сигнал только run.sh — он передаёт его session.mjs один раз; сигнал всей
    // группе дошёл бы до session.mjs дважды, а второй сигнал — выход без возврата стенда
    if (!this.childLeaderOnly && Number.isInteger(pid) && pid > 1) {
      try {
        process.kill(-pid, sig);
        return;
      } catch {
        // группы нет — сигнал самому процессу
      }
    }
    try {
      c.kill(sig);
    } catch {
      // процесс уже вышел
    }
  }

  // Ждёт, пока группа процессов опустеет: до 30 с, затем SIGKILL группе и ещё до 5 с.
  async drainGroup(pgid) {
    const alive = () => {
      try {
        process.kill(-pgid, 0);
        return true;
      } catch (e) {
        return e.code !== 'ESRCH';
      }
    };
    const pause = (ms) => new Promise((r) => setTimeout(r, ms));
    for (let i = 0; i < 60 && alive(); i++) await pause(500);
    if (!alive()) return;
    this.say(`потомки этапа живы после прерывания: SIGKILL группе ${pgid}`);
    try {
      process.kill(-pgid, 'SIGKILL');
    } catch {
      // группа уже вышла
    }
    for (let i = 0; i < 10 && alive(); i++) await pause(500);
  }

  // Команда на цели через rt_ssh из lib.sh; печатает stdout команды.
  remote(cmd) {
    const r = spawnSync(
      'sh',
      [
        '-c',
        '. "$1/lib.sh"; rt_load_targets >/dev/null && rt_ssh "$2" "$3"',
        'sh',
        SCRIPT_DIR,
        this.t,
        cmd
      ],
      {
        cwd: this.root,
        env: { ...process.env, ROUTERS: this.t },
        encoding: 'utf8'
      }
    );
    return { rc: r.status ?? 1, out: (r.stdout || '').trim() };
  }

  record(step) {
    this.steps.push(step);
    if (step.rc === 3) this.network = true;
  }

  // --- этапы ---

  applyProduct(p) {
    this.restoreProduct();
    if (!p.product) return;
    this.say(`файлы продукта из ${p.product.ref}: ${p.product.files.join(' ')}`);
    git('checkout', p.product.sha, '--', ...p.product.files);
    this.overridden = p.product.files;
  }

  restoreProduct() {
    if (!this.overridden) return;
    for (const f of this.overridden) {
      if (spawnSync('git', ['cat-file', '-e', `HEAD:${f}`]).status === 0)
        git('checkout', 'HEAD', '--', f);
      else rmSync(path.join(this.root, f), { force: true });
    }
    this.overridden = null;
  }

  // Проход tree: worktree дерева коммита (tree-step.sh prepare печатает его путь) и ссылка build/router
  // в нём на отчёты этой копии, чтобы отчёт этапа, записанный через worktree, лёг в каталог сессии.
  // Тот же коммит подряд — worktree и сборка переиспользуются. Код: true — worktree готов.
  enterTree(p) {
    if (this.treeWt && this.treeSha === p.treeSha) return true;
    this.leaveTree();
    this.say(`проход ${p.name}: дерево коммита ${p.tree} (worktree)`);
    const r = spawnSync('sh', [path.join(SCRIPT_DIR, 'tree-step.sh'), 'prepare', p.treeSha], {
      cwd: this.root,
      env: { ...process.env, ROUTERS: this.t },
      encoding: 'utf8'
    });
    this.treeUsed = true;
    appendFileSync(
      this.log,
      `\n$ tree-step.sh prepare ${p.treeSha}\n${r.stdout || ''}${r.stderr || ''}`
    );
    const wt = (r.stdout || '').trim().split('\n').pop() ?? '';
    if (r.status !== 0 || !wt || !existsSync(wt)) {
      const why = `tree-step.sh prepare вернул код ${r.status ?? 'сигнал'}`;
      this.say(`проход ${p.name} не начат: worktree дерева ${p.tree} не подготовлен (${why})`);
      this.verdicts.push({
        pass: p.name,
        core: '-',
        test: 'tree: worktree',
        expected: ['готов'],
        actual: 'не создан',
        detail: why,
        ok: false,
        report: '-'
      });
      this.broken = true;
      return false;
    }
    try {
      const link = path.join(wt, 'build', 'router');
      try {
        unlinkSync(link);
      } catch {
        // ссылки ещё нет
      }
      symlinkSync(path.join(this.root, 'build', 'router'), link);
    } catch (e) {
      this.notes.push(`ссылка build/router в worktree дерева ${p.tree} не создана: ${e.message}`);
      this.broken = true;
      return false;
    }
    this.treeWt = wt;
    this.treeSha = p.treeSha;
    return true;
  }

  // Этапы снова запускаются из корня рабочей копии; worktree остаётся до конца сессии (cleanupTrees).
  leaveTree() {
    this.treeWt = null;
    this.treeSha = null;
  }

  // Снятие worktree деревьев коммитов (tree-step.sh cleanup) — только если сессия их создавала.
  cleanupTrees() {
    if (!this.treeUsed) return;
    this.treeUsed = false;
    const r = spawnSync('sh', [path.join(SCRIPT_DIR, 'tree-step.sh'), 'cleanup'], {
      cwd: this.root,
      env: { ...process.env, ROUTERS: this.t },
      encoding: 'utf8'
    });
    if (r.status !== 0) {
      this.notes.push(
        `tree-step.sh cleanup вернул код ${r.status ?? 'сигнал'}: worktree деревьев коммитов проверить вручную (build/bisect)`
      );
    }
  }

  // Деплой сборки рабочего дерева и смоук в один каталог этапа. Ядро, найденное смоуком,
  // становится текущим. Код: 0 — сборка стоит (смоук мог быть красным: это расхождение),
  // 3 — нет связи, иначе деплой не удался.
  // version — номер вышедшего релиза: только деплой этого прохода идёт с XCP_VERSION, смоук
  // сверяет xcp -v с этим номером (файл version этапа).
  async deployAndSmoke(key, label, version = null) {
    const dir = this.stepDir(`deploy-${label}`);
    this.say(`деплой и смоук: ${label} → ${path.relative(this.root, dir)}`);
    const drc = await this.run('sh', [path.join(SCRIPT_DIR, 'deploy.sh'), '--target', this.t], {
      RT_REPORT: dir,
      XCP_VERSION: version ?? undefined
    });
    this.record({ kind: 'deploy', label, dir, rc: drc, version });
    if (drc !== 0) {
      this.deployed = null;
      this.expectVersion = null;
      if (drc !== 3) this.broken = true;
      return drc;
    }
    this.deployed = key;
    const ver = readFileSync(path.join(dir, 'version'), 'utf8').trim();
    // ожидаемая версия этапов сессии до следующего деплоя (expect-version этапов go и pw)
    this.expectVersion = ver || null;
    const src = await this.run('sh', [path.join(SCRIPT_DIR, 'smoke.sh'), this.t, ver], {
      RT_REPORT: dir
    });
    this.record({ kind: 'smoke', label, dir, rc: src, version });
    const coreFile = path.join(dir, this.t, 'core');
    const core = existsSync(coreFile) ? readFileSync(coreFile, 'utf8').trim() : '-';
    if (CORES.includes(core)) this.cur = core;
    if (src !== 0 && src !== 3) {
      this.verdicts.push({
        pass: '-',
        core: core,
        test: `smoke (${label})`,
        expected: ['PASS'],
        actual: 'FAIL',
        detail: 'смоук красный: см. results.tsv этапа',
        ok: false,
        report: path.relative(this.root, dir)
      });
    }
    return src === 3 ? 3 : 0;
  }

  // Номер прохода с version: latest разрешается в новейший опубликованный релиз, результат
  // должен быть тегом релиза и существовать в локальном git. Не вышло — проход не запускается:
  // вердикт с причиной, сессия сломана (стенд вернёт cleanup). Итог — в p.resolved.
  resolveVersion(p) {
    if (p.resolved) return true;
    let tag = p.version;
    let err = '';
    if (tag === 'latest') {
      try {
        tag = latestReleaseTag();
      } catch (e) {
        err = `gh release list не отработал: ${e.message.split('\n')[0]}`;
      }
    }
    if (!err && !RELEASE_TAG.test(tag)) {
      err = `новейший релиз не определён или тег «${tag}» не похож на vX.Y.Z`;
    }
    if (!err && !localTagExists(tag)) err = `тега ${tag} нет в локальном git (git fetch --tags)`;
    if (err) {
      this.say(`проход ${p.name} не начат: ${err}`);
      this.verdicts.push({
        pass: p.name,
        core: '-',
        test: 'version: тег',
        expected: [p.version],
        actual: 'не определён',
        detail: err,
        ok: false,
        report: '-'
      });
      this.broken = true;
      return false;
    }
    p.resolved = tag;
    this.say(`проход ${p.name}: сборка HEAD под номером ${tag}`);
    return true;
  }

  // pass — имя прохода, ради которого переключение (для причины остановки по смене версии).
  async switchTo(core, pass = '-') {
    const dir = this.stepDir(`switch-${core}`);
    this.say(`переключение ядра ${this.cur ?? '-'} → ${core}`);
    this.writeExpect(dir);
    const rc = await this.run(
      'sh',
      [path.join(SCRIPT_DIR, 'pw-suite.sh'), this.t, core, '--switch', core],
      {
        RT_REPORT: dir
      }
    );
    this.record({ kind: 'switch', core, dir, rc });
    // смена версии на границе pw:core-switch.spec.ts: ядро стенда не известно, сессия остановлена
    if (this.versionStop('switch', pass, core, dir, rc)) {
      this.cur = null;
      return 4;
    }
    if (rc === 0) this.cur = core;
    else if (rc !== 3) this.broken = true;
    return rc;
  }

  // --- смена версии стенда (D-13) ---

  // <этап>/<цель>/expect-version перед этапом go, pw или переключения ядра: версия последнего
  // деплоя сессии (контракт сверки версии стенда); версия неизвестна — файла нет, сверка этапа
  // выключена (rt_version_guard пишет skip).
  writeExpect(dir) {
    if (!this.expectVersion) return;
    const file = path.join(dir, this.t, 'expect-version');
    mkdirSync(path.dirname(file), { recursive: true });
    writeFileSync(file, `${this.expectVersion}\n`);
  }

  // После этапа: код 4 или строка stop в <этап>/<цель>/version-stop.tsv — смена версии стенда,
  // сессия останавливается независимо от keep_going (класс stand: тесты этапа не сверяются с
  // ожиданиями). seen — смена, найденная сессией сама ({ boundary, observed }: этап остановлен
  // сторожем Go раньше, чем дошёл до границы). Итог: true — сессия остановлена.
  versionStop(kind, pass, core, dir, rc, seen = null) {
    if (this.cleaning) return false;
    const vs = readVersionStop(path.join(dir, this.t, 'version-stop.tsv'));
    if (!vs.stop && rc !== 4 && !seen) return false;
    const s = vs.stop ?? {
      core,
      boundary: seen ? seen.boundary : `${kind}: этап вернул код 4 без строки stop`,
      expected: this.expectVersion ?? '-',
      observed: seen ? seen.observed : 'не прочитана'
    };
    const rel = path.relative(this.root, dir);
    const prev = this.stopped;
    const todo = [
      ...(prev?.todo ?? []),
      ...vs.notrun.map((n) => `${path.basename(dir)}: ${n.what} (${n.core})`)
    ];
    this.stopped = {
      reason: 'version',
      pass,
      core: s.core !== '-' ? s.core : core,
      boundary: s.boundary,
      expected: s.expected,
      observed: s.observed,
      stage: rel,
      suspect: vs.suspect,
      how: `после этапа ${kind}`,
      live: false,
      todo
    };
    this.say(
      `остановка: смена версии стенда (граница ${s.boundary}) — ожидалась ${s.expected}, на стенде ${s.observed}; проход ${pass}, ядро ${this.stopped.core}`
    );
    this.verdicts.push({
      pass,
      core: this.stopped.core,
      test: `смена версии стенда: ${s.boundary}`,
      expected: [s.expected],
      actual: s.observed,
      detail: 'класс stand: сессия остановлена, тесты этапа с ожиданиями не сверялись',
      ok: false,
      report: rel
    });
    // checkStop эту строку не пересматривает: остановка уже задана
    this.checked = this.verdicts.length;
    // на стенде может остаться чужая сборка: конец сессии ставит HEAD заново
    this.deployed = null;
    this.expectVersion = null;
    // сторож Go успел оборвать этап посреди пакета: тестовый бинарник мог остаться на цели
    if (prev?.live) this.killRemoteTests();
    return true;
  }

  // Версия на стенде (первая строка xcp -v) отличается от ожидаемой: { observed } или null (совпала,
  // ожидание неизвестно или стенд не ответил — тогда решает обычная остановка).
  standVersionChanged() {
    if (!this.expectVersion) return null;
    const r = this.remote('/opt/sbin/xcp -v 2>/dev/null | head -n 1');
    if (r.rc !== 0 || !r.out) return null;
    const observed = r.out.split('\n')[0].trim();
    return observed && observed !== this.expectVersion ? { observed } : null;
  }

  // Возврат стенда по снимку, который оставил незавершённый вложенный прогон (SIGKILL по
  // deadline или повторный сигнал посреди восстановления): тем же rt_restore со сверкой и
  // удалением снимка. Без этого следующий прогон откажет на preflight, а стенд останется в
  // состоянии теста. Итог: { had: снимок был, ok: снимка нет или стенд сверен со снимком,
  // detail }.
  async restoreLeftover(label) {
    const r = this.remote(`[ -d ${SNAP_BASE} ] && echo snap=yes || echo snap=no`);
    if (r.rc === 3) {
      this.network = true;
      return { had: false, ok: false, detail: 'нет связи' };
    }
    if (r.rc !== 0 || !/^snap=(yes|no)$/m.test(r.out)) {
      return {
        had: false,
        ok: false,
        detail: `наличие снимка не проверено (код ${r.rc})`
      };
    }
    if (/^snap=no$/m.test(r.out)) return { had: false, ok: true, detail: 'снимка нет' };
    const dir = this.stepDir(`restore-${label}`);
    this.say(
      `остался снимок незавершённого прогона: возврат стенда → ${path.relative(this.root, dir)}`
    );
    const rc = await this.run(
      'sh',
      [
        '-c',
        '. "$1/lib.sh"; rt_load_targets >/dev/null || exit 2; rt_restore "$2"',
        'sh',
        SCRIPT_DIR,
        this.t
      ],
      { RT_REPORT: dir }
    );
    this.record({ kind: 'restore', label, dir, rc });
    const res = path.join(dir, this.t, 'results.tsv');
    let detail = `код ${rc}`;
    if (existsSync(res)) {
      const row = readFileSync(res, 'utf8')
        .split('\n')
        .map((l) => l.split('\t'))
        .find((f) => f[2] === 'restore-verify');
      if (row) {
        detail = row[3] ?? '';
        if (row[0] !== 'PASS' && detail.startsWith('нет связи,')) this.network = true;
      }
    }
    return { had: true, ok: rc === 0, detail };
  }

  // Проход на ядре по составу comp (composition): на ядре без полного состава Go — только
  // тесты с меткой ядра, спеки — только размеченные и с --grep @core. Порядок — дешёвые
  // первыми: Go-тесты одним этапом через RT_GO_RUN_SEQ, спеки — по запуску pw-suite.sh на спек,
  // каждый в свой каталог этапа (<проход>-<ядро>-pw<k>); несовпадение после спека останавливает
  // проход до следующего спека.
  async runPass(p, core, comp) {
    const tag = `${p.name}-${core}`;
    const dirs = [];
    this.say(`проход ${p.name} на ${core}: ${compositionTitle(comp)}`);
    if (comp.go) {
      const dir = this.stepDir(tag);
      dirs.push(dir);
      this.say(`проход ${p.name} на ${core}, Go → ${path.relative(this.root, dir)}`);
      const env = { RT_REPORT: dir };
      if (comp.go.legacy) {
        env.RT_GO_PKGS = p.go.pkgs;
        if (p.go.run) env.RT_GO_RUN = p.go.run;
      } else {
        // имена из исходников по шаблону TestRouter[A-Za-z0-9_]+ (T-148.1-21), история задаёт
        // только порядок; go-suite.sh проверяет каждую запись по шаблону
        env.RT_GO_RUN_SEQ = comp.go.tests.map((t) => `${t.pkg}:${t.test}`).join(' ');
      }
      const watch = this.spec.keepGoing ? {} : this.goWatch(p, core, dir);
      this.writeExpect(dir);
      const g = await this.run(
        'sh',
        [path.join(SCRIPT_DIR, 'go-suite.sh'), this.t, core],
        env,
        watch
      );
      this.record({ kind: 'go', pass: p.name, core, dir, rc: g, version: p.resolved ?? null });
      // сторож Go оборвал этап на несовпадении раньше границы пакета: падение могло быть
      // следствием смены версии — сверить версию стенда самой сессией
      let seen = null;
      if (this.stopped?.live && this.stopped.reason === 'mismatch') {
        const ch = this.standVersionChanged();
        if (ch) {
          seen = { boundary: `go: этап остановлен сессией на ${this.stopped.test}`, ...ch };
        }
      }
      if (this.versionStop('go', p.name, core, dir, g, seen)) {
        if (comp.specs.length) {
          this.stopped.todo.push(
            `проход ${p.name} на ${core}: спеки ${comp.specs.map((s) => path.basename(s.spec)).join(', ')}`
          );
        }
        // тесты этапа шли на чужой версии: с ожиданиями не сверяются
        this.judge(p, core, [], comp);
        return 0;
      }
      if (g === 3 && !this.stopped?.live) return 3;
      if (!this.stopped && comp.specs.length && !this.spec.keepGoing && !this.aborted) {
        // несовпадение в Go-части, которое видно только после этапа (например, PASS вместо
        // ожидаемого FAIL), не пускает этап Playwright
        const m = this.firstMismatch(p, dir);
        if (m) this.setStopped({ pass: p.name, core, ...m, how: 'после этапа go', live: false });
      }
      if (this.stopped) {
        // этап остановлен посреди пакета: тестовый бинарник на цели мог пережить обрыв ssh
        if (this.stopped.live) this.killRemoteTests();
        if (comp.specs.length) {
          this.stopped.todo.push(
            `проход ${p.name} на ${core}: спеки ${comp.specs.map((s) => path.basename(s.spec)).join(', ')}`
          );
        }
        this.judge(p, core, dirs, comp);
        return 0;
      }
      if (comp.specs.length) {
        // после разрушающих Go-тестов спеки начинают с рабочего стенда (как в run.sh)
        const s = spawnSync(
          'sh',
          [
            '-c',
            '. "$1/lib.sh"; rt_load_targets >/dev/null && RT_SSH_STDIN=1 rt_ssh "$2" "sh -s" <"$1/settle.sh"',
            'sh',
            SCRIPT_DIR,
            this.t
          ],
          {
            cwd: this.root,
            env: { ...process.env, ROUTERS: this.t },
            encoding: 'utf8'
          }
        );
        const st = s.status === 0 ? 'PASS' : 'FAIL';
        appendFileSync(
          path.join(dir, this.t, 'results.tsv'),
          `${st}\t${core}\tstand-ready\tпосле Go-тестов стенд ${st === 'PASS' ? 'в рабочем состоянии' : 'не пришёл в рабочее состояние'}\n`
        );
      }
    }
    for (let k = 0; k < comp.specs.length; k++) {
      if (this.aborted) break;
      const sp = comp.specs[k];
      const dir = this.stepDir(`${tag}-pw${k + 1}`);
      dirs.push(dir);
      this.say(
        `проход ${p.name} на ${core}, спек ${sp.spec} → ${path.relative(this.root, dir)}`
      );
      const env = { RT_REPORT: dir, RT_SPECS: sp.spec };
      if (p.grep) env.RT_PW_GREP = p.grep;
      // только тесты с тегом @core, как во второй ветви changed
      if (comp.coreOnly) env.RT_PW_CORE_ONLY = '1';
      this.writeExpect(dir);
      const rc = await this.run('sh', [path.join(SCRIPT_DIR, 'pw-suite.sh'), this.t, core], env);
      this.record({ kind: 'pw', pass: p.name, core, dir, rc, version: p.resolved ?? null });
      const rest = comp.specs.slice(k + 1);
      if (this.versionStop('pw', p.name, core, dir, rc)) {
        if (rest.length) {
          this.stopped.todo.push(
            `проход ${p.name} на ${core}: спеки ${rest.map((s) => path.basename(s.spec)).join(', ')}`
          );
        }
        // спек остановленного этапа шёл на чужой версии: сверяются только этапы до него
        this.judge(p, core, dirs.slice(0, -1), comp);
        return 0;
      }
      if (rc === 3) return 3;
      if (rest.length && !this.spec.keepGoing && !this.aborted) {
        // остановка между спеками: следующие спеки прохода не запускаются
        const m = this.firstMismatch(p, dir);
        if (m) {
          this.setStopped({ pass: p.name, core, ...m, how: 'после спека', live: false });
          this.stopped.todo.push(
            `проход ${p.name} на ${core}: спеки ${rest.map((s) => path.basename(s.spec)).join(', ')}`
          );
          break;
        }
      }
    }
    this.judge(p, core, dirs, comp);
    return 0;
  }

  // --- остановка на первом несовпадении ---

  setStopped(s) {
    this.stopped = { reason: 'mismatch', todo: [], ...s };
    this.say(
      `остановка на первом несовпадении (${s.how}): проход ${s.pass}, ядро ${s.core}: ${s.test} — ожидалось ${s.expected.join('|')}, получено ${s.actual}`
    );
  }

  // Первое несовпадение среди вердиктов, добавленных после прошлой проверки (сверка после
  // этапа). Возвращает true, если сессия остановлена.
  checkStop() {
    if (this.stopped) return true;
    const fresh = this.verdicts.slice(this.checked);
    this.checked = this.verdicts.length;
    if (this.spec.keepGoing || this.aborted) return false;
    const v = fresh.find((x) => !x.ok);
    if (!v) return false;
    this.setStopped({
      pass: v.pass,
      core: v.core,
      test: v.test,
      expected: v.expected,
      actual: v.actual,
      how: 'после этапа',
      live: false
    });
    return true;
  }

  // Первая строка results.tsv этапа, которую ожидание прохода не допускает.
  firstMismatch(p, dir) {
    const file = path.join(dir, this.t, 'results.tsv');
    if (!existsSync(file)) return null;
    for (const line of readFileSync(file, 'utf8').split('\n')) {
      if (!line) continue;
      const [status, , check = ''] = line.split('\t');
      if (/^(pw|go):/.test(check)) {
        if (!allowedStatus(p.expect, check, status)) {
          return { test: check, expected: expectedOf(p.expect, check), actual: status };
        }
      } else if (status === 'FAIL' || status === 'XPASS') {
        return { test: check, expected: ['PASS'], actual: status };
      }
    }
    return null;
  }

  // Сторож Go-этапа: раз в секунду дочитывает go-*.raw в <этап>/<цель>/<ядро>/ (и на уровень
  // ниже) с запомненной позиции и разбирает маркеры теста; готовые go-*.parsed (их .raw уже
  // удалён) сверяет целиком. Несовпадение — this.stopped и TERM группе этапа; если бинарник
  // в этот момент не работает (между пакетами), сигнал уходит, как только появится следующий
  // .raw. Возвращает opts для run.
  goWatch(p, core, dir) {
    const art = path.join(dir, this.t, core);
    const raws = new Map();
    const parsedDone = new Set();
    // статусы тестов, увиденные в go-*.raw: при остановке этапа go-suite.sh гибнет по TERM до
    // разбора, results.tsv этапа не появляется — сессия дописывает их сама (перепрогон rerun
    // и отчёт берут упавшие оттуда)
    const seen = new Map();
    // тест верхнего уровня, чьё несовпадение найдено, пока он ещё идёт (упал подтест): TERM
    // уходит только после строки «--- » самого теста — его t.Cleanup успевает вернуть стенд
    let pendingRoot = '';
    let found = false;
    let sent = false;
    let child = null;
    const MARKS = [
      ['xpass', /XPASS slug=[a-z0-9-]+/],
      ['quarantine', /QUARANTINE slug=[a-z0-9-]+/]
    ];
    const signal = () => {
      if (sent || !child) return;
      sent = true;
      this.stopped.live = true;
      this.say('этап go останавливается: TERM группе этапа');
      this.signalChild('SIGTERM');
    };
    const check = (name, status, hasRaw) => {
      if (found || this.aborted) return;
      const test = `go:${name}`;
      if (allowedStatus(p.expect, test, status)) return;
      found = true;
      this.setStopped({
        pass: p.name,
        core,
        test,
        expected: expectedOf(p.expect, test),
        actual: status,
        how: 'по ходу этапа go',
        live: false
      });
      if (!hasRaw) return;
      const root = name.split('/')[0];
      if (root !== name) pendingRoot = root;
      else signal();
    };
    const list = () => {
      const out = [];
      const walk = (d, depth) => {
        let ents;
        try {
          ents = readdirSync(d, { withFileTypes: true });
        } catch {
          return;
        }
        for (const e of ents) {
          const f = path.join(d, e.name);
          if (e.isDirectory()) {
            if (depth === 0) walk(f, 1);
          } else if (/^go-.+\.(raw|parsed)$/.test(e.name)) {
            out.push(f);
          }
        }
      };
      walk(art, 0);
      return out;
    };
    const onFail = (st, name) => {
      // родитель, упавший из-за подтеста, отдельно не сверяется (badchild в go-parse.awk)
      const byChild = [...st.failed].some((f) => f.startsWith(`${name}/`));
      st.failed.add(name);
      if (byChild) return;
      const has = (t, k) => st.marks.get(t)?.has(k) ?? false;
      const parts = name.split('/');
      const ancestors = parts.map((_, i) => parts.slice(0, i + 1).join('/'));
      let status = 'FAIL';
      if (has(name, 'xpass')) status = 'XPASS';
      else if (ancestors.some((a) => has(a, 'quarantine'))) status = 'QFAIL';
      seen.set(name, status);
      check(name, status, true);
    };
    const rawLine = (st, line) => {
      // вывод -test.v=test2json: служебные строки начинаются с \x16, у подтестов — с отступом
      const l = line.replace(/^[\s\x16]+/, '');
      let m = l.match(/^=== (RUN|CONT|NAME)\s+(\S+)/);
      if (m) {
        st.cur = m[2];
        // запасной путь: строки конца теста не было, а пошёл другой тест верхнего уровня
        if (pendingRoot && m[1] === 'RUN' && m[2].split('/')[0] !== pendingRoot) signal();
        return;
      }
      m = l.match(/^--- (FAIL|PASS|SKIP): (\S+)/);
      if (m) {
        if (m[1] === 'FAIL') onFail(st, m[2]);
        else seen.set(m[2], m[1]);
        if (pendingRoot && m[2] === pendingRoot) signal();
        return;
      }
      if (!st.cur) return;
      for (const [kind, re] of MARKS) {
        if (!re.test(l)) continue;
        if (!st.marks.has(st.cur)) st.marks.set(st.cur, new Set());
        st.marks.get(st.cur).add(kind);
      }
    };
    const readRaw = (f) => {
      let st = raws.get(f);
      if (!st) {
        st = {
          pos: 0,
          dec: new StringDecoder('utf8'),
          rest: '',
          cur: '',
          marks: new Map(),
          failed: new Set()
        };
        raws.set(f, st);
      }
      let fd;
      try {
        fd = openSync(f, 'r');
      } catch {
        return;
      }
      try {
        const size = fstatSync(fd).size;
        while (st.pos < size) {
          const buf = Buffer.alloc(Math.min(size - st.pos, 1 << 20));
          const n = readSync(fd, buf, 0, buf.length, st.pos);
          if (n <= 0) break;
          st.pos += n;
          st.rest += st.dec.write(buf.subarray(0, n));
        }
      } finally {
        closeSync(fd);
      }
      const lines = st.rest.split('\n');
      st.rest = lines.pop();
      for (const line of lines) rawLine(st, line);
    };
    const readParsed = (f) => {
      parsedDone.add(f);
      let text = '';
      try {
        text = readFileSync(f, 'utf8');
      } catch {
        return;
      }
      for (const line of text.split('\n')) {
        const [status, name = ''] = line.split('\t');
        if (name.startsWith('go:')) check(name.slice(3), status, false);
      }
    };
    const poll = () => {
      if (sent || this.aborted) return;
      const files = list();
      const live = files.filter((f) => f.endsWith('.raw'));
      if (found && pendingRoot) {
        // упал подтест: дочитывать вывод до конца теста верхнего уровня (сигнал — в rawLine)
        for (const f of live) readRaw(f);
        return;
      }
      if (found) {
        // несовпадение найдено между пакетами: остановить, как только пойдёт следующий бинарник
        if (live.length) signal();
        return;
      }
      for (const f of live) readRaw(f);
      for (const f of files) {
        if (found) break;
        if (!f.endsWith('.parsed') || parsedDone.has(f)) continue;
        if (existsSync(f.replace(/\.parsed$/, '.raw'))) continue;
        readParsed(f);
      }
      if (found && !sent && live.some((f) => existsSync(f))) signal();
    };
    return {
      drainIf: () => sent,
      onSpawn: (c) => {
        child = c;
        const timer = setInterval(poll, 1000);
        return () => {
          clearInterval(timer);
          child = null;
          if (sent) this.writeSeen(dir, core, seen);
        };
      }
    };
  }

  // Строки results.tsv этапа по статусам, увиденным сторожем Go до остановки: пишутся, только
  // если go-suite.sh не оставил ни одной строки go: этого ядра (его разбор не успел пройти).
  writeSeen(dir, core, seen) {
    if (seen.size === 0) return;
    const file = path.join(dir, this.t, 'results.tsv');
    let text = '';
    try {
      text = readFileSync(file, 'utf8');
    } catch {
      // файла нет — пишется заново
    }
    const has = text.split('\n').some((l) => {
      const c = l.split('\t');
      return c[1] === core && (c[2] ?? '').startsWith('go:');
    });
    if (has) return;
    const why = 'этап остановлен сессией на первом несовпадении';
    const rows = [...seen].map(([name, status]) => `${status}\t${core}\tgo:${name}\t${why}`);
    mkdirSync(path.dirname(file), { recursive: true });
    appendFileSync(file, rows.join('\n') + '\n');
    this.say(`результаты остановленного этапа go: строк ${rows.length} в ${path.relative(this.root, file)}`);
  }

  // Тестовые бинарники на цели после остановки Go-этапа: ssh этапа оборван, бинарник мог
  // остаться работать — добить, чтобы возврат стенда не шёл одновременно с тестом.
  killRemoteTests() {
    const r = this.remote(
      'n=0; for f in /proc/[0-9]*/cmdline; do p=${f#/proc/}; p=${p%/cmdline}; tr "\\000" " " <"$f" 2>/dev/null | grep -q "[.]test -test[.]" && kill "$p" 2>/dev/null && n=$((n+1)); done; echo "n=$n"'
    );
    const m = r.out.match(/^n=(\d+)$/m);
    if (r.rc === 3) this.network = true;
    if (!m) {
      this.notes.push(
        `после остановки этапа go тестовые процессы на цели не проверены (код ${r.rc})`
      );
    } else if (Number(m[1]) > 0) {
      this.say(`после остановки этапа go добито тестовых процессов на цели: ${m[1]}`);
    }
  }

  // Прерывание вложенного run.sh по спеке (interrupt). Вложенный прогон идёт отдельной группой
  // процессов (detached), как make в терминале. Опрос раз в секунду: появился go-<пакет>.raw в
  // <отчёт>/<цель>/<ядро>/ — go-этап начался; через after секунд сигнал уходит группе; не вышла
  // за deadline секунд — SIGKILL группе. Возвращает opts для run и состояние для вердиктов.
  interruptWatch(i, dir) {
    const state = {
      pkg: '',
      goAt: 0,
      sentAt: 0,
      closedAt: 0,
      killed: false,
      pgid: 0
    };
    const findRaw = () => {
      const tdir = path.join(dir, this.t);
      let cores;
      try {
        cores = readdirSync(tdir, { withFileTypes: true }).filter((d) => d.isDirectory());
      } catch {
        return '';
      }
      for (const c of cores) {
        let names;
        try {
          names = readdirSync(path.join(tdir, c.name));
        } catch {
          continue;
        }
        const raw = names.find((n) => /^go-.+\.raw$/.test(n));
        if (raw) return raw.slice(3, -4);
      }
      return '';
    };
    const opts = {
      detached: true,
      // потомков после выхода run.sh проверяет вердикт «процессы на ПК», группу не добивать
      drain: false,
      onSpawn: (child) => {
        // группа вложенного прогона — только по целому pid > 1; иначе сигнал не шлём вовсе
        const pid = child.pid;
        const okPid = Number.isInteger(pid) && pid > 1;
        if (okPid) state.pgid = pid;
        let kill = null;
        const poll = setInterval(() => {
          if (state.sentAt || !okPid) return;
          if (!state.goAt) {
            const pkg = findRaw();
            if (!pkg) return;
            state.goAt = Date.now();
            state.pkg = pkg;
            return;
          }
          if (this.aborted || Date.now() - state.goAt < i.after * 1000) return;
          state.sentAt = Date.now();
          this.say(`прерывание: SIG${i.signal} группе вложенного прогона`);
          try {
            process.kill(-pid, 'SIG' + i.signal);
          } catch {
            // группа уже вышла
          }
          kill = setTimeout(() => {
            state.killed = true;
            this.say(`вложенный прогон не вышел за ${i.deadline} с: SIGKILL группе`);
            try {
              process.kill(-pid, 'SIGKILL');
            } catch {
              // группа уже вышла
            }
          }, i.deadline * 1000);
        }, 1000);
        return () => {
          clearInterval(poll);
          if (kill) clearTimeout(kill);
        };
      }
    };
    return { opts, state };
  }

  // Проход-прогон набора: вложенный run.sh под блокировкой сессии, отчёт в build/router.
  // Вложенный прогон сам ставит сборку HEAD, снимает снимок и восстанавливает стенд.
  async runSuite(p) {
    const base = `${path.basename(this.dir)}-${String(++this.n).padStart(2, '0')}-${p.name}`;
    let dir = path.join(this.root, 'build', 'router', base);
    // rerun дописывает перепрогон в отчёт более раннего прохода-прогона (from), своего каталога нет
    if (p.suite === 'rerun') {
      dir = this.suiteDirs.get(p.from) ?? '';
      if (!dir || !existsSync(dir)) {
        const err = `отчёт прохода ${p.from} не найден: перегонять нечего`;
        this.say(`прогон набора rerun не начат: ${err}`);
        this.verdicts.push({
          pass: p.name,
          core: '-',
          test: 'rerun: отчёт from',
          expected: [p.from],
          actual: 'не найден',
          detail: err,
          ok: false,
          report: '-'
        });
        return 0;
      }
    }
    const rel = path.relative(this.root, dir);
    this.suiteDirs.set(p.name, dir);
    const rerunBefore = p.suite === 'rerun' ? rerunNumber(dir) : 0;
    // смоук релиза и добор: тег — из спеки или новейший опубликованный (не draft) релиз
    let tag = null;
    if (p.suite === 'smoke' || p.suite === 'delta') {
      tag = p.release;
      let err = '';
      if (tag === 'latest') {
        try {
          tag = latestReleaseTag();
        } catch (e) {
          err = `gh release list не отработал: ${e.message.split('\n')[0]}`;
        }
      }
      if (!err && !RELEASE_TAG.test(tag)) {
        err = `новейший релиз не определён или тег «${tag}» не похож на vX.Y.Z`;
      }
      if (err) {
        this.say(`прогон набора ${p.suite} не начат: ${err}`);
        this.verdicts.push({
          pass: p.name,
          core: '-',
          test: 'release: тег',
          expected: [p.release],
          actual: 'не определён',
          detail: err,
          ok: false,
          report: '-'
        });
        return 0;
      }
    }
    this.say(`прогон набора ${suiteTitle(p, tag)} → ${rel}`);
    const bs = p.bisect;
    const env = {
      // rerun пишет в отчёт from (FROM), свой каталог у него не создаётся
      RT_REPORT_DIR: p.suite === 'rerun' ? undefined : dir,
      RT_HOLD_MINUTES: undefined,
      RT_CORES: undefined,
      RT_BASE: undefined,
      // настройки окна смоука и выбора набора вложенный прогон выставляет сам
      RT_SMOKE_WINDOW: undefined,
      RT_SMOKE_IPT_WAIT: undefined,
      RT_PW_CORE_ONLY: undefined,
      RT_GO_CORE_PKGS: undefined,
      RT_GO_CORE_RUN: undefined,
      RT_SPECS_CORE: undefined,
      RT_KERNEL_TOUCHED: undefined,
      RT_SELECT_ALL: undefined,
      // сужение состава оператора (отладка точечного прогона) во вложенный прогон не переходит:
      // full и delta с ним публиковали бы stand/<arch>-full по части набора
      RT_SEL_DIR: undefined,
      RT_GO_PKGS: undefined,
      RT_GO_RUN: undefined,
      RT_GO_RUN_SEQ: undefined,
      RT_GO_SEQ: undefined,
      RT_SPECS: undefined,
      RT_SPECS_FIRST: undefined,
      RT_PW_GREP: undefined,
      RT_PW_GREP_INVERT: undefined,
      RT_PW_INVERT: undefined,
      RT_RESULTS_NAME: undefined,
      RT_ART_SUB: undefined,
      RT_FILES: p.files.length ? p.files.join(',') : undefined,
      // вложенная сессия: её спека; у bisect — спек теста pw:; у остальных наборов SPEC не наследуется
      SPEC: p.suite === 'session' ? p.nested.path : bs?.spec || undefined,
      SUITE: undefined,
      // установка артефакта релиза и публикация статуса — только у smoke и delta с release
      RELEASE: tag ?? undefined,
      // поля наборов rerun, delta и bisect (148.1-16): у остальных проходов снимаются, чтобы
      // вложенный прогон не унаследовал их от оператора
      FROM: p.suite === 'rerun' ? dir : bs?.from,
      TO: bs?.to,
      TEST: bs?.test,
      CORE: bs?.core,
      MODE: bs?.mode,
      GREP: bs?.grep || undefined,
      DRY_RUN: p.suite === 'delta' ? (p.dryRun ? '1' : '0') : undefined,
      BASE_REPORT: undefined
    };
    const args = [path.join(SCRIPT_DIR, 'run.sh')];
    if (p.suite !== 'full') args.push(p.suite);
    const irq = p.interrupt ? this.interruptWatch(p.interrupt, dir) : null;
    const t0 = Date.now();
    const rc = await this.run(
      'sh',
      args,
      env,
      irq ? irq.opts : { leaderOnly: p.suite === 'session' }
    );
    const took = Math.round((Date.now() - t0) / 1000);
    if (irq) irq.state.closedAt = Date.now();
    this.record({
      kind: 'suite',
      pass: p.name,
      label: tag ? `${p.suite} ${tag}` : p.suite,
      dir,
      rc
    });
    if (this.aborted && !existsSync(dir)) return 0;

    // перепрогон пишет строки в rerun-N.tsv отчёта from (N — следующий свободный); упавших тестов
    // нет — стенд не тронут и новой записи нет
    const rerunAfter = p.suite === 'rerun' ? rerunNumber(dir) : 0;
    const rerunTouched = rerunAfter > rerunBefore;
    let res = path.join(dir, this.t, 'results.tsv');
    if (p.suite === 'rerun') {
      res = rerunTouched ? path.join(dir, this.t, `rerun-${rerunAfter}.tsv`) : '';
    }
    const rows = [];
    if (res && existsSync(res)) {
      for (const line of readFileSync(res, 'utf8').split('\n')) {
        if (!line) continue;
        const [status, core = '-', check = '', detail = ''] = line.split('\t');
        rows.push({ status, core, check, detail });
      }
    }
    // вложенный прогон поставил сборку HEAD и вернул ядро снимка. Признак — только код деплоя 0
    // из deploy.rc: прогон мог не дойти до деплоя (check-known, нет целей, неверный отчёт), и тогда
    // results.tsv нет, а на стенде остаётся сборка прохода product.ref. Нет признака — сборку HEAD
    // вернёт cleanup
    const deployRc = path.join(dir, this.t, 'deploy.rc');
    const deployOk = existsSync(deployRc) && readFileSync(deployRc, 'utf8').trim() === '0';
    if (p.suite === 'rerun') {
      // перепрогон без упавших тестов стенд не трогает: признак сборки остаётся прежним; иначе
      // deploy.rc отчёта from перезаписан деплоем перепрогона (сборка HEAD)
      if (rerunTouched) this.deployed = deployOk ? 'HEAD' : null;
    } else {
      // (после смоука релиза и добора на стенде артефакт релиза — признак тоже сбрасывается)
      this.deployed = p.suite !== 'smoke' && p.suite !== 'delta' && deployOk ? 'HEAD' : null;
    }
    // ожидаемая версия следующих этапов — то, что поставил вложенный прогон (файл version его
    // отчёта); прогон без деплоя оставляет прежнюю, неудачный деплой — неизвестную
    if (p.suite !== 'rerun' || rerunTouched) {
      this.expectVersion = deployOk ? readVersionFile(dir) : null;
    }
    // вложенный прогон остановлен сменой версии: на стенде может остаться чужая сборка
    if (hasVersionStop(dir, this.t)) {
      this.deployed = null;
      this.expectVersion = null;
    }
    if (p.suite === 'session') this.nestedEnd(p, dir, rc);
    // сбой связи — только проверка reachable и потеря связи при восстановлении; по подстроке
    // «нет связи» в любой строке нельзя: так пишет и смоук, когда не отвечает сама панель
    if (
      rows.some(
        (r) =>
          r.status === 'FAIL' &&
          (r.check === 'reachable' ||
            (r.check === 'restore-verify' && r.detail.startsWith('нет связи,')))
      )
    ) {
      this.network = true;
    }
    if (!this.c0) {
      const snap = path.join(dir, this.t, 'snapshot.log');
      const coreFile = path.join(dir, this.t, 'core');
      let core = '';
      if (existsSync(snap)) {
        const m = readFileSync(snap, 'utf8').match(/^core=(.*)$/m);
        if (m) core = m[1].trim();
      }
      if (!CORES.includes(core) && existsSync(coreFile))
        core = readFileSync(coreFile, 'utf8').trim();
      if (!CORES.includes(core) && !existsSync(snap) && !this.network && !this.aborted) {
        // вложенный прогон вышел до снимка (отказ проверок до блокировки, код 2) и стенд не
        // трогал: исходное ядро — то, что активно на цели сейчас
        const r = this.remote(
          'x=$(pidof xray); m=$(pidof mihomo); [ -n "$x" ] && [ -z "$m" ] && echo xray; [ -n "$m" ] && [ -z "$x" ] && echo mihomo; true'
        );
        if (r.rc === 3) this.network = true;
        else if (r.rc === 0) core = r.out.trim();
      }
      if (CORES.includes(core)) {
        // восстановление вложенного прогона вернуло ядро снимка; позже снимок берёт активное
        // ядро как есть, поэтому this.cur после первого прогона не трогаем
        this.c0 = core;
        this.cur = core;
        this.say(`исходное ядро стенда: ${core}`);
      } else if (!this.network && !this.aborted) {
        this.notes.push(
          `после прогона набора ${p.name} не найдено исходное ядро: сессия остановлена`
        );
        this.broken = true;
      }
    }

    let md = '';
    const mdFile = path.join(dir, 'summary.md');
    if (existsSync(mdFile)) md = readFileSync(mdFile, 'utf8');
    const num = (re) => {
      const m = md.match(re);
      return m ? Number(m[1]) : null;
    };
    this.suites.push({
      pass: p.name,
      suite: tag ? `${p.suite} ${tag}` : p.suite,
      files: p.files,
      exit: rc,
      // у вложенной сессии строки «Итого» нет — время по замеру прохода; у перепрогона summary.md
      // принадлежит отчёту from, его «Итого» и «Оболочка» — не этого прохода
      total:
        p.suite === 'rerun'
          ? took
          : (num(/^- Итого: (\d+) с/m) ?? (p.suite === 'session' ? took : null)),
      shell: p.suite === 'rerun' ? null : num(/^- Оболочка: (\d+) с/m),
      report: rel
    });

    // вердикты прохода
    const push = (test, expected, actual, detail, ok) =>
      this.verdicts.push({
        pass: p.name,
        core: '-',
        test,
        expected,
        actual,
        detail,
        ok,
        report: rel
      });
    push('exit', [String(p.expect.exit)], String(rc), '', rc === p.expect.exit);
    if (p.expect.maxMinutes !== null) {
      const min = Math.round(took / 6) / 10;
      push(
        `время прохода: ${min} мин из ${p.expect.maxMinutes}`,
        [`до ${p.expect.maxMinutes} мин`],
        `${min} мин`,
        `${took} с`,
        took <= p.expect.maxMinutes * 60
      );
    }
    if (irq) {
      const st = irq.state;
      const sent = st.sentAt > 0;
      push(
        'interrupt: сигнал',
        ['отправлен'],
        sent ? 'отправлен' : 'не отправлен',
        sent
          ? `SIG${p.interrupt.signal} через ${Math.round((st.sentAt - st.goAt) / 1000)} с после старта go-${st.pkg}`
          : 'этап go не начался до выхода run.sh',
        sent
      );
      const secs = sent ? Math.round((st.closedAt - st.sentAt) / 1000) : 0;
      push(
        'interrupt: выход run.sh',
        [`до ${p.interrupt.deadline} с`],
        !sent ? 'не вышел' : st.killed ? 'не вышел' : `${secs} с`,
        !sent
          ? 'сигнал не отправлен'
          : st.killed
            ? `SIGKILL группе через ${p.interrupt.deadline} с`
            : '',
        sent && !st.killed
      );
    }
    if (irq) this.judgeInterrupt(irq.state, rows, push);
    if (irq && irq.state.killed && !this.network) {
      // run.sh убит до конца восстановления: стенд возвращает сессия, иначе следующие проходы
      // откажут на preflight. Не вернулся — сессия останавливается
      const lr = await this.restoreLeftover(p.name);
      push(
        'interrupt: возврат стенда сессией',
        ['PASS'],
        lr.ok ? 'PASS' : 'FAIL',
        lr.had ? `по снимку вложенного прогона: ${lr.detail}` : lr.detail,
        lr.ok
      );
      if (!lr.ok && !this.network) this.broken = true;
    }
    for (const sub of p.expect.summary) {
      const ok = md.includes(sub);
      push(`summary: ${sub}`, ['есть'], ok ? 'есть' : 'нет', md ? '' : 'summary.md не создан', ok);
    }
    const fails = new Set(
      rows
        .filter((r) => ['FAIL', 'KNOWN', 'XPASS'].includes(r.status))
        .map((r) => `${r.status}\t${r.core}\t${r.check}`)
    );
    this.suiteFails.set(p.name, fails);
    if (p.expect.same) {
      const other = this.suiteFails.get(p.expect.same);
      const test = `same_failures_as ${p.expect.same}`;
      if (!other) {
        push(test, ['совпадает'], 'нет', `прогон ${p.expect.same} не выполнялся`, false);
      } else {
        const fmt = (a, b) =>
          [...a]
            .filter((x) => !b.has(x))
            .slice(0, 10)
            .map((x) => x.replace(/\t/g, ' '))
            .join(', ') || '-';
        const onlyHere = fmt(fails, other);
        const onlyThere = fmt(other, fails);
        const same = onlyHere === '-' && onlyThere === '-';
        push(
          test,
          ['совпадает'],
          same ? 'совпадает' : 'расходится',
          same
            ? `${fails.size} строк`
            : `только в ${p.name}: ${onlyHere}; только в ${p.expect.same}: ${onlyThere}`,
          same
        );
      }
    }
    return 0;
  }

  // Стенд после вложенной сессии — по её session.json: она сама возвращает сборку HEAD и своё
  // исходное ядро (оно же активное ядро внешней сессии на момент прохода). Код 3 — сбой связи.
  nestedEnd(p, dir, rc) {
    if (rc === 3) this.network = true;
    let j = null;
    try {
      j = JSON.parse(readFileSync(path.join(dir, 'session.json'), 'utf8'));
    } catch {
      this.notes.push(`вложенная сессия ${p.name} не оставила session.json: стенд вернёт cleanup`);
      return;
    }
    const end = j.stand_end ?? {};
    this.deployed = end.product_head === true ? 'HEAD' : null;
    // ожидаемая версия — версия последнего удачного деплоя вложенной сессии (её возврат стенда)
    const last = (Array.isArray(j.steps) ? j.steps : [])
      .filter(
        (x) =>
          x &&
          x.kind === 'deploy' &&
          x.rc === 0 &&
          typeof x.dir === 'string' &&
          SAFE_PATH.test(x.dir) &&
          !x.dir.includes('..')
      )
      .pop();
    this.expectVersion =
      this.deployed === 'HEAD' && last ? readVersionFile(path.join(this.root, last.dir)) : null;
    if (CORES.includes(j.core_initial) && !this.c0) {
      this.c0 = j.core_initial;
      this.say(`исходное ядро стенда: ${this.c0}`);
    }
    this.cur = CORES.includes(end.core) ? end.core : null;
  }

  // Вердикты после прерванного вложенного прогона: этап погас, блокировка свободна, стенд
  // восстановлен, лишних падений нет. В отчёт идут только числа: командные строки процессов
  // (рабочий каталог, регэксп -test.run) не выводятся.
  judgeInterrupt(st, rows, push) {
    // процессы на ПК: группа вложенного прогона и этапы обвязки (держали бы дескриптор блокировки).
    // Проверка повторяется до трёх раз с паузой 1 с: процессы только что вышли, и зомби-сироты
    // ещё не подобраны init. Живой член группы — не зомби (ps -o stat); этап обвязки — процесс,
    // чья командная строка НАЧИНАЕТСЯ с `sh <путь>/scripts/router/<этап>.sh`, поэтому редактор,
    // tail или shellcheck с тем же путём в аргументах не считаются.
    let groupAlive = false;
    let stages = -1;
    for (let attempt = 0; attempt < 3; attempt++) {
      groupAlive = false;
      if (st.pgid > 1) {
        const ps = spawnSync('ps', ['-eo', 'pgid=,stat='], {
          encoding: 'utf8'
        });
        if (!ps.error && ps.status === 0) {
          groupAlive = ps.stdout.split('\n').some((l) => {
            const [g, stat = ''] = l.trim().split(/\s+/);
            return Number(g) === st.pgid && !stat.startsWith('Z');
          });
        } else {
          try {
            process.kill(-st.pgid, 0);
            groupAlive = true;
          } catch (e) {
            groupAlive = e.code !== 'ESRCH';
          }
        }
      }
      const pg = spawnSync(
        'pgrep',
        ['-f', '^(/[^ ]*/)?(ba|da)?sh +[^ ]*scripts/router/(go-suite|pw-suite|smoke)[.]sh'],
        { encoding: 'utf8' }
      );
      stages = -1;
      if (!pg.error && pg.status === 1) stages = 0;
      else if (!pg.error && pg.status === 0) stages = pg.stdout.split('\n').filter(Boolean).length;
      if (!groupAlive && stages === 0) break;
      if (attempt < 2) spawnSync('sleep', ['1']);
    }
    const pcOk = !groupAlive && stages === 0;
    push(
      'interrupt: процессы на ПК',
      ['нет'],
      pcOk ? 'нет' : 'есть',
      `группа: ${groupAlive ? 'жива' : 'пуста'}; этапов обвязки: ${stages < 0 ? 'не проверено' : stages}`,
      pcOk
    );

    // тестовые процессы на цели: ./<пакет>.test -test.v=test2json; шаблон со скобками не находит
    // сам себя в командной строке проверки
    const r = this.remote(
      'n=0; for f in /proc/[0-9]*/cmdline; do tr "\\000" " " <"$f" 2>/dev/null | grep -q "[.]test -test[.]" && n=$((n+1)); done; echo "n=$n"'
    );
    const m = r.out.match(/^n=(\d+)$/m);
    if (r.rc !== 0 || !m) {
      push('interrupt: тестовые процессы на цели', ['нет'], 'нет связи', `код ${r.rc}`, false);
    } else {
      const n = Number(m[1]);
      push(
        'interrupt: тестовые процессы на цели',
        ['нет'],
        n === 0 ? 'нет' : `есть: ${n}`,
        '',
        n === 0
      );
    }

    // восстановление стенда вложенным прогоном
    const restore = ['restore-verify', 'xcp-after-restore', 'core-restored'];
    for (const check of restore) {
      const own = rows.filter((x) => x.check === check);
      const row = own.find((x) => x.status !== 'PASS') ?? own[own.length - 1];
      push(
        check,
        ['PASS'],
        row ? row.status : 'нет',
        row ? row.detail : 'строки нет в results.tsv вложенного прогона',
        !!row && row.status === 'PASS'
      );
    }

    // остальные падения: при коде 130 код выхода о них не сообщает
    for (const row of rows) {
      if (restore.includes(row.check)) continue;
      if (row.status !== 'FAIL' && row.status !== 'XPASS') continue;
      push(row.check, ['PASS'], row.status, `${row.core}: ${row.detail}`, false);
    }
  }

  // Тест из ожиданий не шёл на ядре по правилу «ядра как в гейте»: ядро без полного состава, и
  // тест в состав не вошёл (Go — по именам состава, Playwright — тест без тега ядра не виден,
  // поэтому отсутствующий pw-тест на таком ядре считается не шедшим).
  notOnCore(comp, name) {
    if (!comp || comp.full) return false;
    if (name.startsWith('go:')) {
      if (!comp.go) return true;
      if (comp.go.legacy) return false;
      const top = name.slice(3).split('/')[0];
      return !comp.go.tests.some((t) => t.test === top);
    }
    return name.startsWith('pw:');
  }

  // Сверка results.tsv этапов прохода на ядре (Go и спеки — каждый в своём каталоге) с
  // ожиданиями прохода. comp — состав прохода на ядре.
  judge(p, core, dirs, comp = null) {
    const rel0 = dirs.length ? path.relative(this.root, dirs[0]) : '-';
    const got = new Map();
    const extra = [];
    for (const dir of dirs) {
      const file = path.join(dir, this.t, 'results.tsv');
      if (!existsSync(file)) continue;
      const rel = path.relative(this.root, dir);
      for (const line of readFileSync(file, 'utf8').split('\n')) {
        if (!line) continue;
        const [status, , check, detail = ''] = line.split('\t');
        if (/^(pw|go):/.test(check)) {
          // служебная строка (pw:run) есть у каждого запуска pw-suite: FAIL не затирается
          if (!(SERVICE.has(check) && got.get(check)?.status === 'FAIL')) {
            got.set(check, { status, detail, rel });
          }
        } else if (status === 'FAIL' || status === 'XPASS') {
          extra.push({ check, status, detail, rel });
        }
      }
    }
    const push = (test, expected, actual, detail, ok, rel = rel0) =>
      this.verdicts.push({
        pass: p.name,
        core,
        test,
        expected,
        actual,
        detail,
        ok,
        report: rel
      });
    const { expect } = p;
    // этап остановлен на первом несовпадении посреди вывода: тест остановки — несовпадение,
    // тесты, до которых этап не дошёл, не сверяются
    const st = this.stopped;
    const stop = st && st.live && st.pass === p.name && st.core === core ? st : null;
    // проход на этом ядре остановлен (по ходу Go или между этапами): невыполненное не сверяется
    const halted = Boolean(st && st.pass === p.name && st.core === core);
    if (stop) {
      push(
        stop.test,
        stop.expected,
        stop.actual,
        `${stop.how}: этап остановлен, следующие тесты не выполнялись`,
        false
      );
    }
    for (const [name, want] of expect.tests) {
      if (stop && name === stop.test) continue;
      const r = got.get(name);
      if (!r) {
        if (halted) continue;
        if (this.notOnCore(comp, name)) {
          push(name, want.status, 'не шёл', 'не шёл на ядре — ядро не важно', true);
          continue;
        }
        push(name, want.status, 'нет', 'тест не запускался: имя или --grep', false);
        continue;
      }
      const okStatus = want.status.includes(r.status);
      const okDetail = want.detail === null || r.detail.includes(want.detail);
      const why = okStatus && !okDetail ? `в детали нет «${want.detail}»: ${r.detail}` : r.detail;
      push(name, want.status, r.status, why, okStatus && okDetail, r.rel);
    }
    for (const [name, r] of got) {
      if (expect.tests.has(name) || (stop && name === stop.test)) continue;
      if (SERVICE.has(name)) {
        push(name, ['PASS'], r.status, r.detail, r.status === 'PASS', r.rel);
      } else if (QUARANTINE.has(r.status)) {
        // карантин (D-17): исход виден, на итог не влияет
        push(name, ['карантин'], r.status, r.detail, true, r.rel);
      } else if (expect.def) {
        push(name, expect.def, r.status, r.detail, expect.def.includes(r.status), r.rel);
      } else if (r.status === 'FAIL' || r.status === 'XPASS') {
        push(name, ['справочно'], r.status, `неожиданное падение: ${r.detail}`, false, r.rel);
      } else {
        push(name, ['справочно'], r.status, r.detail, true, r.rel);
      }
    }
    for (const e of extra) push(e.check, ['PASS'], e.status, e.detail, false, e.rel);
    if (expect.count !== null && !halted) {
      // count — по тестам, шедшим на ядре; на ядре без полного состава (только тесты с меткой
      // ядра) число справочное: count задаёт число тестов прохода целиком
      const n = [...got.keys()].filter((k) => !SERVICE.has(k)).length;
      if (comp && !comp.full) {
        push(`число тестов`, ['справочно'], String(n), 'только тесты с меткой ядра', true);
      } else {
        push(`число тестов`, [String(expect.count)], String(n), '', n === expect.count);
      }
    }
  }

  // --- возврат стенда ---

  async cleanup() {
    // возврат стенда идёт из корня рабочей копии; worktree деревьев коммитов снимаются при любом исходе
    this.leaveTree();
    try {
      await this.cleanupStand();
    } finally {
      this.cleanupTrees();
    }
  }

  async cleanupStand() {
    this.cleaning = true;
    try {
      this.restoreProduct();
    } catch (e) {
      this.notes.push(`файлы продукта не вернулись к HEAD: ${e.message}`);
    }
    if (this.network) {
      this.notes.push('сбой связи: стенд не возвращался автоматически — проверить вручную');
      return;
    }
    // снимок незавершённого прогона (сессию прервали посреди восстановления вложенного run.sh)
    const lr = await this.restoreLeftover('leftover');
    if (this.network) return;
    if (lr.had && !lr.ok)
      this.notes.push(`снимок незавершённого прогона не вернул стенд: ${lr.detail}`);
    // после прохода-прогона deployed === 'HEAD': вложенный run.sh поставил сборку HEAD и сам
    // вернул стенд по снимку, повторный деплой не нужен
    if (this.deployed !== 'HEAD') {
      const rc = await this.deployAndSmoke('HEAD', 'head-restore');
      if (rc === 3) return;
    }
    if (this.c0 && this.cur !== this.c0) await this.switchTo(this.c0);
  }

  standEnd() {
    const end = {
      tree_clean: git('status', '--porcelain', '--untracked-files=no') === ''
    };
    end.product_head = this.deployed === 'HEAD';
    const r = this.remote(
      `x=$(pidof xray | cut -d" " -f1); m=$(pidof mihomo | cut -d" " -f1); c=none; [ -n "$x" ] && c=xray; [ -n "$m" ] && c=mihomo; [ -n "$x" ] && [ -n "$m" ] && c=both; echo "$c"; pidof xcp >/dev/null && echo xcp-up || echo xcp-down; [ -d ${SNAP_BASE} ] && echo snap-left || echo snap-none`
    );
    const [core = '-', xcp = '-', snap = '-'] = r.out.split('\n');
    end.core = r.rc === 0 ? core : '-';
    end.core_initial = this.c0 ?? '-';
    end.core_ok = r.rc === 0 && core === this.c0;
    end.xcp_up = r.rc === 0 && xcp === 'xcp-up';
    end.no_snapshot = r.rc === 0 && snap === 'snap-none';
    end.ok = end.tree_clean && end.product_head && end.core_ok && end.xcp_up && end.no_snapshot;
    return end;
  }

  report(result, end) {
    // ячейка таблицы Markdown: сначала обратный слеш, иначе «\|» из детали закроет ячейку
    const esc = (s) =>
      String(s).replace(/\\/g, '\\\\').replace(/\|/g, '\\|').replace(/\s+/g, ' ').slice(0, 220);
    const L = [];
    L.push('# Сессия на стенде', '');
    L.push(`- Итог: ${result}`);
    L.push(`- HEAD: ${this.head}`);
    L.push(
      `- Цель: ${this.t}; исходное ядро: ${this.c0 ?? '-'}; ядра проходов: ${this.spec.cores.join(', ')}`
    );
    L.push(`- Отчёт: ${path.relative(this.root, this.dir)}`, '');
    const st = this.stopped;
    if (st?.reason === 'version') {
      L.push('## Остановлено: смена версии стенда', '');
      L.push(`- Проход: ${st.pass}, ядро: ${st.core}, граница: ${esc(st.boundary)}`);
      L.push(`- Ожидалась: ${esc(st.expected)}; на стенде: ${esc(st.observed)}`);
      L.push(`- Этап: ${st.stage}; падений на сменённой версии (класс stand): ${st.suspect}`);
      L.push(`- Не выполнено:${st.todo.length ? '' : ' —'}`);
      for (const t of st.todo) L.push(`  - ${esc(t)}`);
      L.push('');
    } else if (st) {
      L.push('## Остановлено на первом несовпадении', '');
      L.push(
        `- проход ${st.pass}, ядро ${st.core}: ${esc(st.test)} — ожидалось ${st.expected.join('|')}, получено ${st.actual} (${st.how})`
      );
      L.push(`- Не выполнено:${st.todo.length ? '' : ' —'}`);
      for (const t of st.todo) L.push(`  - ${t}`);
      L.push('');
    }
    L.push('## Ожидания', '');
    L.push('| Проход | Ядро | Тест | Ожидалось | Получено | Совпало | Деталь | Отчёт |');
    L.push('|---|---|---|---|---|---|---|---|');
    for (const v of this.verdicts) {
      L.push(
        `| ${v.pass} | ${v.core} | ${esc(v.test)} | ${v.expected.join('\\|')} | ${v.actual} | ${v.ok ? 'да' : '**нет**'} | ${esc(v.detail)} | ${v.report} |`
      );
    }
    if (this.suites.length) {
      L.push('', '## Прогоны наборов', '');
      L.push('| Проход | Набор | Код | Итог, с | Оболочка, с | Отчёт |');
      L.push('|---|---|---|---|---|---|');
      for (const r of this.suites) {
        const set = r.files.length ? `${r.suite} (${esc(r.files.join(', '))})` : r.suite;
        L.push(
          `| ${r.pass} | ${set} | ${r.exit} | ${r.total ?? '-'} | ${r.shell ?? '-'} | ${r.report} |`
        );
      }
    }
    L.push('', '## Этапы', '');
    for (const s of this.steps) {
      L.push(
        `- ${s.kind}${s.pass ? ` ${s.pass}` : ''}${s.core ? ` ${s.core}` : ''}${s.label ? ` ${s.label}` : ''}${s.version ? ` под номером ${s.version}` : ''}: код ${s.rc} (${path.relative(this.root, s.dir)})`
      );
    }
    L.push('', '## Стенд в конце', '');
    if (end) {
      L.push(`- рабочее дерево чистое: ${end.tree_clean ? 'да' : '**нет**'}`);
      L.push(`- на цели сборка HEAD: ${end.product_head ? 'да' : '**нет**'}`);
      L.push(
        `- активное ядро: ${end.core} (исходное ${end.core_initial}) — ${end.core_ok ? 'да' : '**нет**'}`
      );
      L.push(`- xcp запущен: ${end.xcp_up ? 'да' : '**нет**'}`);
      L.push(`- снимка незавершённого прогона нет: ${end.no_snapshot ? 'да' : '**нет**'}`);
    } else {
      L.push('- не проверялся');
    }
    for (const n of this.notes) L.push(`- ${n}`);
    const md = L.join('\n') + '\n';
    writeFileSync(path.join(this.dir, 'summary.md'), md);
    writeFileSync(
      path.join(this.dir, 'session.json'),
      JSON.stringify(
        {
          result,
          head: this.head,
          target: this.t,
          core_initial: this.c0,
          verdicts: this.verdicts,
          // номер сборки каждого прохода go/specs: разрешённый тег вышедшего релиза или null
          passes: this.spec.passes
            .filter((p) => !p.suite)
            .map((p) => ({ name: p.name, version: p.resolved ?? null, tree: p.tree ?? null })),
          suites: this.suites,
          steps: this.steps.map((s) => ({
            ...s,
            dir: path.relative(this.root, s.dir)
          })),
          stand_end: end,
          stopped: this.stopped,
          notes: this.notes
        },
        null,
        2
      ) + '\n'
    );
    process.stdout.write('\n' + md);
  }
}

async function main() {
  const argv = process.argv.slice(2);
  const args = parseArgs(argv);
  const specFile = path.resolve(args.spec);
  const root = git('rev-parse', '--show-toplevel');
  process.chdir(root);
  const spec = loadSpec(specFile, root);
  if (args.mode === 'dry-run') {
    const plan = dryRun(spec, path.relative(root, specFile), root);
    if (args.budget && plan.over && !spec.budgetReason) {
      // проверка плана (D-23): спека сверх бюджета без причины не проходит
      process.stderr.write(
        `session: оценка ${plan.minutes} мин сверх бюджета ${BUDGET_MIN} мин, budget_reason в спеке нет\n`
      );
      process.exit(4);
    }
    return;
  }
  if (args.mode === 'target') {
    process.stdout.write(spec.target + '\n');
    return;
  }
  if (process.env.RT_LOCK_HELD !== '1' || !process.env.RT_REPORT) {
    die('сессия запускается через make router-test SUITE=session SPEC=<спека>');
  }
  if (git('status', '--porcelain', '--untracked-files=no') !== '') {
    die('рабочее дерево не чистое: проходы меняют файлы продукта');
  }

  const s = new Session(spec, specFile, root);
  s.say(`отчёт ${path.relative(root, s.dir)}`);
  // оценка и бюджет (D-23): сверх бюджета — только предупреждение, сессия идёт
  try {
    const plan = planSession(spec, path.relative(root, specFile), root);
    s.say(`оценка: ${plan.minutes} мин (бюджет ${BUDGET_MIN} мин)`);
    if (plan.over) {
      s.warn(
        `предупреждение: оценка ${plan.minutes} мин сверх бюджета ${BUDGET_MIN} мин — ${spec.budgetReason ? `причина: ${spec.budgetReason}` : 'budget_reason в спеке нет'}`
      );
    }
  } catch (e) {
    s.notes.push(`оценка длительности не посчитана: ${e.message}`);
  }
  let interrupts = 0;
  const onSignal = () => {
    if (++interrupts > 1) {
      s.signalChild('SIGTERM');
      process.exit(130);
    }
    s.say('прерывание — возвращаю стенд (повторный сигнал — выход без возврата)');
    s.aborted = true;
    s.signalChild('SIGTERM');
  };
  for (const sig of ['SIGINT', 'SIGTERM', 'SIGHUP']) process.on(sig, onSignal);

  // Невыполненное при остановке: оставшиеся ядра текущего прохода и следующие проходы.
  const passTitle = (q) =>
    q.suite
      ? `проход ${q.name} (прогон набора ${q.suite})`
      : `проход ${q.name}: ядра ${passCores(spec, q).join(', ')}`;
  const stopAt = (i, cores = []) => {
    if (cores.length) {
      s.stopped.todo.push(`проход ${spec.passes[i].name}: ядра ${cores.join(', ')}`);
    }
    for (const q of spec.passes.slice(i + 1)) s.stopped.todo.push(passTitle(q));
  };

  try {
    for (let i = 0; i < spec.passes.length; i++) {
      const p = spec.passes[i];
      if (s.aborted || s.network || s.broken || s.stopped) break;
      if (p.suite) {
        // вложенный прогон деплоит рабочее дерево: файлы продукта прежнего прохода — к HEAD
        s.leaveTree();
        s.restoreProduct();
        await s.runSuite(p);
        if (s.checkStop()) stopAt(i);
        continue;
      }
      if (p.version && !s.resolveVersion(p)) break;
      // проход tree: продукт и тесты из worktree дерева коммита, остальные проходы — из корня копии
      if (p.tree) {
        if (!s.enterTree(p)) break;
      } else {
        s.leaveTree();
      }
      s.applyProduct(p);
      const key = productKey(p);
      if (s.deployed !== key) {
        const label = p.tree
          ? `${p.name}-tree-${p.treeSha.slice(0, 8)}`
          : p.version
            ? `${p.name}-${p.resolved}`
            : p.product
              ? `${p.name}-${p.product.ref.slice(0, 8)}`
              : 'head';
        const rc = await s.deployAndSmoke(key, label, p.resolved ?? null);
        if (rc === 3 || s.broken) break;
        if (!s.c0) {
          if (!s.cur) {
            s.notes.push('после деплоя смоук не нашёл активное ядро: сессия остановлена');
            s.broken = true;
            break;
          }
          s.c0 = s.cur;
          s.say(`исходное ядро стенда: ${s.c0}`);
        }
        // красный смоук — несовпадение: проход не начинается
        if (s.checkStop()) {
          stopAt(i, coreOrder(passCores(spec, p), s.cur));
          break;
        }
      }
      // ядра как в гейте: первое ядро прохода — все тесты, остальные — только с меткой ядра
      const order = coreOrder(passCores(spec, p), s.cur);
      for (let j = 0; j < order.length; j++) {
        const core = order[j];
        if (s.aborted || s.network || s.broken) break;
        // метки ядра и имена тестов — из дерева, которое идёт на стенд
        const comp = composition(p, Boolean(p.cores) || j === 0, p.tree ? s.treeWt : root, core, s.h);
        if (comp.empty) {
          s.say(`проход ${p.name} на ${core}: тестов с меткой ядра нет — ядро не важно, пропуск`);
          continue;
        }
        if (s.cur !== core) {
          const rc = await s.switchTo(core, p.name);
          if (rc !== 0) {
            // смена версии на переключении ядра: это ядро и дальше — не выполнены
            if (s.stopped) stopAt(i, order.slice(j));
            break;
          }
        }
        const rc = await s.runPass(p, core, comp);
        if (s.checkStop()) {
          stopAt(i, order.slice(j + 1));
          break;
        }
        if (rc === 3) break;
      }
    }
    // несовпадение в последнем этапе без остановки посреди вывода — не остановка: невыполненного
    // нет; смена версии стенда — остановка всегда (причина нужна в summary.md)
    if (s.stopped && s.stopped.reason !== 'version' && !s.stopped.live && !s.stopped.todo.length) {
      s.stopped = null;
    }
  } catch (e) {
    s.notes.push(`сессия упала: ${e.message}`);
    s.broken = true;
  }

  try {
    await s.cleanup();
  } catch (e) {
    s.notes.push(`возврат стенда упал: ${e.message}`);
  }
  // оценки следующих сессий опираются на эти отчёты
  s.appendHistory();
  const end = s.network ? null : s.standEnd();
  const mismatches = s.verdicts.filter((v) => !v.ok).length;
  let code = 0;
  let result;
  if (s.aborted) {
    code = 130;
    result = 'прервана';
  } else if (s.network) {
    code = 3;
    result = 'сбой связи ПК↔стенд — не красный прогон, нужен todo с разбором (AGENTS.md)';
  } else if (s.broken || s.stopped || mismatches || !end?.ok) {
    code = 1;
    const why = [];
    if (s.stopped?.reason === 'version') {
      why.push(
        `остановлено: смена версии стенда — проход ${s.stopped.pass}, ядро ${s.stopped.core}, граница ${s.stopped.boundary}`
      );
    } else if (s.stopped) {
      why.push(
        `остановлено на первом несовпадении: проход ${s.stopped.pass}, ядро ${s.stopped.core}, ${s.stopped.test}`
      );
    }
    if (s.broken) why.push('этап сломался');
    if (mismatches) why.push(`расхождений с ожиданиями: ${mismatches}`);
    if (!end?.ok) why.push('стенд не вернулся в исходное состояние');
    result = `красный (${why.join('; ')})`;
  } else {
    result = `зелёный (ожиданий: ${s.verdicts.length})`;
  }
  s.report(result, end);

  process.exit(code);
}

main().catch((e) => die(e.stack || e.message, 1));
