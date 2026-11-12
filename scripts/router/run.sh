#!/bin/sh
# scripts/router/run.sh [SUITE] — проверка на роутерах (make router-test SUITE=...).
#
# Наборы:
#   (без SUITE)  полный: деплой, снимок, затем матрица ядер (D-20) — для каждого ядра
#                из RT_CORES ветвь «сброс записи на носитель (sync) -> переключение ядра
#                через интерфейс панели (pw-suite.sh --switch) -> смоук без окна
#                (smoke.sh --no-observe) -> наблюдатель окна (smoke.sh --observe-only)
#                параллельно с частью Playwright только чтения (pw-suite.sh --part read) ->
#                Go-тесты на устройстве (go-suite.sh) -> ожидание стенда (settle) -> часть
#                Playwright, меняющая состояние (pw-suite.sh --part state)»; затем
#                восстановление, сверка и проверка core-restored. Параллельно окну идут только
#                спеки только чтения: Go-тесты и изменяющие спеки стартуют после ожидания
#                наблюдателя (D-07). Полный прогон — полная матрица: все тесты под обоими
#                ядрами; неразмеченный тест с разным исходом в ветвях даёт строку
#                FAIL core-mark:<проверка> «нужна метка ядра» и раздел «Нужна метка ядра» в
#                summary.md (D-05б).
#   full         то же, что запуск без SUITE: полный прогон (синоним для статуса релиза).
#   smoke        деплой и смоук (только чтение, без снимка).
#   changed      быстрый набор для каждой задачи (D-25): select-changed.mjs по diff от
#                origin/main и незакоммиченным правкам выбирает тесты затронутых областей
#                (scripts/router/areas.json, строки router-area) по именам: Go через RT_GO_SEQ,
#                спеки областей и канарейки; полный выбор — только при правке самого выбора,
#                обвязка стенда даёт RT_HARNESS=1, зависимости — pages и канарейки (D-13).
#                Порядок гейта: сначала тесты затронутых областей, затем по частоте падений из
#                истории (RTR-03); гейт идёт до конца. Вывод выбора — 13 строк eval, run.sh
#                сверяет число, порядок и форму строк и пишет его в <отчёт>/select.env. Код 3
#                выбора — выбор остановлен (нельзя надёжно получить): красный до деплоя, причины
#                в select.txt, полный выбор не подставляется. Состав этапов по выбору (вторая
#                ветвь, окно смоука по умолчанию, набор harness, самопроверка сессии) решает
#                scripts/router/run-plan.mjs — тот же план читает повтор выбора select-replay.mjs.
#                В summary.md строки «Области» и «Выбор».
#                Ветви строятся гибридом (D-01…D-03): первая — ядро из SHA HEAD (cksum SHA
#                чётный — xray, нечётный — mihomo; повтор на том же HEAD идёт под тем же
#                ядром) со всем выбором; вторая — другое ядро и только тесты с меткой ядра
#                (Go: RT_GO_CORE_PKGS по RT_GO_CORE_RUN, Playwright: RT_SPECS_CORE с отбором
#                @core). Нет размеченных тестов — второй ветви нет вовсе (ни sync, ни
#                переключения, ни смоука, ни окна). Окно смоука — 30 с, а при изменённом коде
#                запуска ядра, watchdog или полном выборе (RT_KERNEL_TOUCHED=1) — 120 с (D-07).
#                Ничего не затронуто — деплой и смоук (без снимка). Причины выбора —
#                <отчёт>/select.txt, план ветвей и окно — <отчёт>/plan.txt, оба в разделе
#                «Выбор» отчёта; раскладка тестов по ветвям — <отчёт>/<цель>/layout.tsv.
#   harness      короткая проверка механики стенда (T2h, RTR-02, D-07) для правок обвязки: деплой,
#                смоук, снимок и восстановление, затем на каждом ядре (обе ветви, гибрида нет)
#                по два Go-теста (только чтения и изменяющий) и по два спека (read и state) из
#                блока harness файла scripts/router/areas.json, и самопроверка сессии: вложенная
#                сессия по спеке блока (scripts/router/harness-session.json) обязана остановиться
#                на первом несовпадении — выйти с кодом 1, написать раздел «Остановлено на первом
#                несовпадении», не запускать второй проход и вернуть стенд. Результат цели —
#                строка PASS|FAIL harness-session-fail-fast, в summary.md — «Самопроверка
#                сессии: остановка на первом несовпадении — да|нет». Окно смоука 30 с. Нужно
#                чистое рабочее дерево (вложенная сессия его требует). Статус релиза не
#                публикуется. Тесты блока — без метки ядра, яруса full и дольше 30 с по истории.
#                В наборе changed при RT_HARNESS=1 (правка обвязки) те же Go-тесты и спеки идут
#                отдельными этапами go-harness, pw-harness-read и pw-harness-state в обеих ветвях
#                (вторая ветвь обязательна), а после ветвей идёт та же самопроверка сессии.
#   session      сессия на стенде по спеке (SPEC=<файл JSON>): проходы с ожиданиями на одной
#                цели и ядрах спеки, в том числе на файлах продукта из другого коммита
#                (воспроизведение до исправления). Проходы ведёт session.mjs (формат спеки —
#                в его заголовке), этапы — те же deploy.sh, smoke.sh, pw-suite.sh и
#                go-suite.sh. В конце рабочее дерево и стенд возвращаются: сборка HEAD,
#                исходное ядро. Итог — summary.md и session.json отчёта. Нужно чистое
#                рабочее дерево; снимка нет, как у ручной сессии на стенде.
#   rerun        перепрогон упавших тестов (RTR-05): FROM=<отчёт прогона или сессии> под build/router.
#                rerun-plan.mjs берёт из отчёта проверки go: и pw: со статусом FAIL и XPASS и
#                собирает на каждое ядро их состав (<ядро>.sel: родительские TestRouter*, спеки и
#                --grep по названиям); набор ставит сборку HEAD (или RELEASE), снимает снимок и
#                гонит на каждом ядре, где тесты упали, только их, затем восстанавливает стенд.
#                Запись — в тот же отчёт FROM, номер перепрогона N — следующий свободный:
#                <цель>/rerun-N.tsv (все строки перепрогона, включая смоук и восстановление),
#                <цель>/<ядро>/rerun-N/ (артефакты), <цель>/rerun-N.sel.d/ (состав),
#                rerun-N.meta (sha, dirty, started) и rerun-N.md; в summary.md дописывается
#                раздел «## Перепрогон N» с таблицей «проверка, ядро, было, стало» и строкой
#                «- Перепрогон: K тестов из <отчёт>». meta, results.tsv и ссылка last отчёта FROM
#                не меняются; служебные файлы цели (snapshot.log, restore.log, deploy.rc, core,
#                timings.tsv) перепрогон перезаписывает или дополняет. Нет упавших тестов — код 0
#                с сообщением, стенд не трогается. Окно смоука 30 с. Статус релиза не публикуется.
#   bisect       поиск виновного коммита (RTR-11, D-22): TEST=<go:Имя|pw:название> CORE=<ядро>
#                FROM=<ссылка> [TO=<ссылка>, по умолчанию HEAD] [MODE=regress|fix] [SPEC=<спек>]
#                [GREP=<регэксп>]. Деплой HEAD (или RELEASE) и смоук цели, затем bisect.mjs двоичным
#                поиском по first-parent коммитам (по ancestry-path, если FROM не на first-parent
#                цепочке TO) ставит на стенд сборку дерева коммита и гонит один тест (tree-step.sh:
#                продукт и тест из дерева коммита, обвязка из HEAD, отдельный worktree
#                build/bisect/wt-<sha>). MODE=regress ищет первый коммит, где тест падает, MODE=fix —
#                первый, где проходит; концы проверяются; шаг, который нельзя проверить (теста нет
#                в дереве, сборка не удалась), пропускается. Для pw: нужен SPEC; GREP по умолчанию —
#                экранированное название из TEST; для go: пакет определяется по рабочему дереву, для
#                теста, которого в дереве уже нет, — TEST=go:<пакет>:TestRouterИмя. Итог — раздел
#                «## Поиск виновного коммита» summary.md со строкой «- Итог bisect <тест>: <sha>
#                <тема>» (или «… между <a> и <b>», «… не воспроизводится»). В конце ставятся обратно
#                сборка, стоявшая до поиска (HEAD или RELEASE), и исходное ядро. Одна цель
#                (ROUTERS=<id>). Статус релиза не публикуется. Код выхода — код bisect.mjs: 0 —
#                вердикт есть и стенд вернулся, 1 — нет вердикта, 2 — неверные значения, 3 — связь.
#   delta        добор stand/<arch>-full на новый тег без повторного полного прогона (RTR-11, D-24,
#                D-25): RELEASE=<тег|latest> [DRY_RUN=1] [BASE_REPORT=<отчёт>]. Условия те же, что у
#                полного прогона (todo 11): есть отчёт полного прогона по более старому тегу той же
#                вехи (коммит — предок нового тега, новее последнего stable; delta-plan.mjs base),
#                все упавшие в нём тесты (кроме known и карантина) зелёные на новом теге, зелёные все
#                тесты, выбранные select-changed.mjs по диффу между тегами (с ярусом full для
#                затронутых областей), и смоук нового тега в этом же прогоне зелёный. Состав по ядрам
#                собирает delta-plan.mjs merge в <отчёт>/<цель>/delta.sel.d/<ядро>.sel (упавшие в базе
#                плюс выбор по диффу; первая ветвь — ядро по SHA HEAD, как у changed, вторая — только
#                тесты с меткой ядра); дальше прогон идёт как перепрогон через RT_SEL_DIR: деплой
#                сборки нового тега, смоук, снимок, ветви, подтверждающий перепрогон новых падений,
#                восстановление. Выбор по диффу полный (правка самого выбора), неизвестный файл,
#                упавший в базе тест, который нельзя перегнать, — добор невозможен (код 1, стенд не
#                трогается): нужен полный прогон. Решение — строки summary.md «- Добор: база …» и
#                «- Решение добора: success|failure»; статус stand/<arch>-full на коммите нового
#                тега публикуется с описанием «добор от <тег базы>: зелёный|красный, тестов N,
#                новых падений M» (release-status.sh). DRY_RUN=1 — статус не отправляется
#                (release-status.sh --dry-run), решение в summary.md с пометкой «dry_run: не
#                отправлено». Тег проверяется до блокировки: шаблон, наличие локально, коммит —
#                предок HEAD (иначе код 2: тесты старше релиза). Код выхода — как у полного прогона.
#   reboot       холодный старт (D-24): деплой, предполёт и снимок параллельно, затем
#                по одной цели в порядке XCP_REBOOT_ORDER (нижестоящая цель каскада первой):
#                перезагрузка, ожидание возврата, проверки автозапуска (xcp той же версии,
#                XKeen, то же ядро, правила iptables), смоук, сверка со снимком. В конце
#                все цели снова отвечают (reboot-reachable-end). Матрица ядер не применяется.
#
# Разбор падений (RTR-05, D-19…D-21), наборы full, changed и harness. Каждое новое падение теста
# (FAIL проверок go: и pw:, кроме известных, карантина и признаков потери связи) после всех ветвей и до
# восстановления стенда, в той же блокировке и в тот же отчёт, перегоняется один раз только для
# классификации (confirm-go-<ядро>, confirm-pw-<ядро>; строки — в <цель>/confirm.tsv, артефакты — в
# <цель>/<ядро>/confirm/): прошёл — класс flaky, упал снова — new. Основной проход идёт без повторов
# (retries: 0), прошедший со второго раза тест красный. Потолок — RT_CONFIRM_MAX тестов (10) на цель:
# новых падений больше — перепрогона нет вовсе (общая причина), все они new, в plan.txt строка
# «подтверждающий перепрогон пропущен». Классы падений — <цель>/classes.tsv и раздел «Классы падений»
# summary.md (branch-report.mjs), итог — строка «- Итог:»; код выхода красный при new, flaky, xpass и
# при потере связи, не догнанной после возврата связи. Счётчики report.md и summary.md показывают
# QPASS и QFAIL (карантин). В конце каждого прогона с отчётом (кроме session) отчёт добавляется в
# историю — node history.mjs append, класс падения попадает в колонку class (D-20).
#
# Поиск виновного коммита в конце полного прогона (RTR-11, D-22): для тестов класса new из
# <цель>/classes.tsv без класса flaky в истории за 30 дней (поиск по нестабильному тесту даёт ложного
# виновника) — не больше 3 тестов (RT_BISECT_MAX), по порядку классов — запускается bisect.mjs от
# sha прошлого зелёного полного прогона в истории (lastFull green), иначе от новейшего stable-тега
# (git tag --merged HEAD), до HEAD, с --assume-to: падение только что видели. Подбор тестов и записей
# для них — bisect.mjs pick (через rerun-plan.mjs). Поиск идёт до статуса релиза и удержания блокировки,
# после каждого поиска стенд возвращается к сборке прогона (RELEASE или HEAD) и исходному ядру; код
# выхода прогона не меняется. Прерванный прогон и тесты flaky поиска не получают. Итог — раздел
# «## Поиск виновного коммита» summary.md и строки «- Итог bisect …» (их подхватывает latest.md прогона
# без присмотра).
#
# Порядок: check-known.sh (метки известных падений) -> цели из локального конфига ->
# блокировка на весь прогон -> отчёт -> ротация build/router (tidy-lib.sh; сборки ротирует
# make router-<арх>) -> проверка связи -> deploy.sh -> по целям
# параллельно [preflight -> снимок -> ветви ядер (переключение, смоук, Go-тесты, Playwright)
# -> восстановление + сверка] -> report.md и summary.md в build/router/<время>-<sha>/ (ссылка build/router/last).
# Восстановление стоит в trap подпроцесса цели сразу после снимка: оно выполняется
# при любом исходе, в том числе при падении этапов посередине. Сигнал, полученный верхним
# run.sh, пересылается подпроцессам целей (под dash они сами SIGINT не перехватывают);
# повторный сигнал восстановление не прерывает: _term и восстановление его игнорируют.
#
# Окружение:
#   ROUTERS=<id,id>      выбор целей (по умолчанию все из конфига)
#   RELEASE=<тег>        ставить артефакт GitHub Release вместо сборки ветки (у набора delta —
#                        обязателен, latest — новейший релиз). У наборов smoke, full и delta (контекст
#                        stand/<arch>-full, описание «добор от <тег базы>: …») после прогона для каждой цели публикуется статус коммита тега
#                        stand/<arch>-<smoke|full> (release-status.sh): success при нуле FAIL и
#                        XPASS цели (по классам падений: красные new, flaky, xpass, не догнанный
#                        stand), иначе failure. Статуса нет при незакоммиченных правках, сбое
#                        связи ПК↔стенд (связь не вернулась или обрыв без догона), прерванном прогоне, а у full — когда коммит тега не
#                        предок HEAD. Итог — строка «Статус на GitHub» в summary.md; код выхода
#                        прогона от публикации не зависит. Сбой посреди прогона даёт красный
#                        статус; повторный прогон перекрывает его (побеждает последний статус
#                        контекста). У full и delta с RELEASE состав не сужается: заданные
#                        RT_CORES, RT_SEL_DIR, RT_GO_*, RT_SPECS*, RT_PW_GREP*, RT_PW_INVERT,
#                        RT_RESULTS_NAME или RT_ART_SUB — отказ с кодом 2 до блокировки
#   SPEC=<файл>          спека набора session (путь от корня рабочей копии или абсолютный)
#   FROM=<отчёт>         отчёт прогона или сессии набора rerun (путь от корня рабочей копии или
#                        абсолютный, только под build/router; проверка до блокировки, иначе код 2).
#                        У набора bisect FROM — ссылка git, с которой искать (TEST, CORE, TO, MODE, SPEC,
#                        GREP — там же; все значения проверяются до блокировки, иначе код 2)
#   DRY_RUN=1            набор delta: статус на GitHub не отправляется (release-status.sh --dry-run),
#                        состав и решение добора — в summary.md (0 по умолчанию)
#   BASE_REPORT=<отчёт>  набор delta: отчёт полного прогона, от которого считается добор (имя каталога
#                        под build/router или путь); по умолчанию — самый свежий подходящий в истории
#   RT_SEL_DIR=<каталог> состав ветви из файлов <ядро>.sel (rerun-plan.mjs): вместо выбора набора
#                        ветвь берёт GO_SEQ, SPECS и GREP из <ядро>.sel, а ветвь без файла
#                        пропускается; читается построчно, без eval, по шаблонам безопасных символов.
#                        Набор rerun задаёт его сам (<цель>/rerun-N.sel.d)
#   RT_LINK_WAIT=<с>     ожидание возврата связи после обрыва (rt_wait_link), 60…7200, по умолчанию 600
#   RT_HOLD_MINUTES=<N>  после прогона держать блокировку N минут для ручной
#                        проверки; снять раньше: touch <git-common-dir>/router-test.release
#   RT_LOCK_WAIT=<с>     ожидание очереди на роутеры (по умолчанию 3600)
#   RT_SMOKE_WINDOW=<с>  окно наблюдения смоука за перезапуском ядра (по умолчанию 120; в наборе
#                        changed без этой переменной — 30 с, при RT_KERNEL_TOUCHED=1 — 120 с)
#   RT_SMOKE_IPT_WAIT=<с> предел ожидания правил iptables в смоуке (по умолчанию 120); не
#                        зависит от длины окна
#   RT_CORES="<ядра>"    ветви матрицы через пробел (по умолчанию «xray mihomo»); явный RT_CORES —
#                        ручная отладка: гибрид changed выключается, все выбранные тесты идут в
#                        каждой ветви. Для гейта и обычной работы не задавать
#   RT_REBOOT_WAIT=<с>   ожидание возврата цели после перезагрузки (по умолчанию 900)
#   XCP_REBOOT_ORDER     порядок перезагрузки (локальный конфиг, id через пробел)
#   RT_BASE=<ref>        база сравнения набора changed (по умолчанию merge-base с origin/main)
#   RT_FILES=<a,b,c>     набор changed: выбор для заданного списка файлов вместо git (отладка)
#
# Раскладка отчёта: результаты всех ветвей — в <отчёт>/<id>/results.tsv (ядро ветви — во
# второй колонке), артефакты ветви (go-*.jsonl, pw-read/, pw/, pw-switch/) — в
# <отчёт>/<id>/<ядро>/: pw-read/ — часть Playwright только чтения, pw/ — изменяющая часть;
# kernel-pids.tsv (смены PID ядра на стенде от Go-тестов до конца части state) и clock.tsv
# (часы ПК и устройства) — для счёта перезапусков ядра на тест в summary.md.
# Этапы ветви в timings.tsv: sync-<ядро>, switch-<ядро>, smoke-<ядро> (проверки без окна),
# observe-<ядро> (окно, идёт параллельно pw-read-<ядро>), pw-read-<ядро>, go-<ядро>,
# settle-<ядро>, pw-<ядро> (часть state). Хвост xcp.log ветви пишет наблюдатель окна.
# Этап, оборванный сигналом, — строка с кодом 130 (её пишет _term), начало и ядро ветви те же.
#
# Потеря связи со стендом (класс stand, D-21, D-26), наборы full, changed и harness. Этап Go с
# кодом 3 или этап Playwright с ненулевым кодом при молчащем ssh — обрыв: rt_wait_link ждёт связь до
# RT_LINK_WAIT (600 с), затем один раз на ветвь догоняется сорванное — Go-пакеты вне go-done.txt
# (catchup-go-<ядро>) или та же часть Playwright (catchup-pw-<ядро>), строки — в <цель>/catchup.tsv,
# артефакты — в <цель>/<ядро>/catchup/ — и набор продолжается со следующего шага. Второй обрыв в
# ветви не догоняется. Связь не вернулась — ветвь и цель останавливаются, несделанное — в
# <цель>/notrun.txt (и в разделе «Не выполнено из-за обрыва связи» summary.md), стенд
# восстанавливается по снимку, как при любом сбое. Обрывы — в <цель>/link-loss.tsv. Набор rerun связь
# ждёт, но не догоняет.
#
# Смена версии стенда (класс stand, D-13, RTR-15), все наборы с ветвями. После деплоя ожидаемая версия
# цели пишется в <цель>/expect-version (из <отчёт>/version, через временный файл и mv -f); go-suite.sh и
# pw-suite.sh сверяют её после каждого пакета Go и каждого спека (rt_version_guard, lib.sh) и при смене
# пишут строку stop в <цель>/version-stop.tsv, строку FAIL version-guard и выходят с кодом 4. После
# каждого этапа ветви (переключение ядра, pw-read, go, pw, этапы harness, догоны, этапы подтверждающего
# перепрогона) строка stop останавливает цикл ветвей цели: следующие этапы, ветви и подтверждающий
# перепрогон не идут и записаны строками notrun в version-stop.tsv, стенд восстанавливается по снимку,
# как при любом исходе. Смоук следующей ветви сверяется с текущим ожиданием цели (rt_version_expected),
# xcp-after-restore — с версией деплоя ($EXPECT: восстановление откатывает принятый переход), а не с
# увиденной версией. Прогон со сменой версии красный (итог «красный: смена
# версии стенда» и раздел «## Остановлено: смена версии стенда» summary.md — branch-report.mjs), в
# историю не пишется, статус на GitHub не отправляется, самопроверки сессии и поиска виновного коммита
# нет.
#
# Файлы разбора падений в <отчёт>/<цель>/: confirm.tsv, confirm-count (сколько тестов подлежало
# перепрогону), confirm-skip (причина, если перепрогона не было), confirm-plan.txt, classes.tsv,
# catchup.tsv, link-loss.tsv, notrun.txt; сверки версии — expect-version, version-guard.tsv,
# version-stop.tsv.
#
# Код выхода: 0 — нет красных классов (new, flaky, xpass, не догнанный stand, stand смены версии
# стенда), 2 — неизвестный набор, иначе 1. Набор session —
# код session.mjs: 0 — ожидания совпали и стенд вернулся, 3 — сбой связи ПК↔стенд, иначе 1 или 2.
# Набор rerun: 0 — все перегнанные тесты прошли (PASS, KNOWN, QPASS, QFAIL) или упавших нет,
# 1 — есть FAIL или XPASS, 2 — неверный FROM, 3 — сбой связи ПК↔стенд.
# 130 — прерван сигналом INT, TERM или HUP: этап остановлен, стенд восстановлен по снимку,
# отчёт с пометкой «Прерван» написан, удержание блокировки (RT_HOLD_MINUTES) не применяется.

set -eu

SCRIPT_DIR=$(cd "$(dirname "$0")" && pwd)
# shellcheck disable=SC1091
. "$SCRIPT_DIR/lib.sh"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/tidy-lib.sh"

SUITE=${1:-}
case "$SUITE" in
  smoke | changed | harness | reboot | session | rerun | bisect | delta | full | '') ;;
  *)
    echo "router-test: неизвестный набор '${SUITE}'. Поддерживаются: smoke, changed, harness, reboot, session, rerun, bisect, delta и full (полный, можно без SUITE)" >&2
    echo "router-test: пример: make router-test SUITE=smoke" >&2
    exit 2
    ;;
esac
# full — полный прогон: дальше он идёт как запуск без SUITE, имя набора в отчёте остаётся full.
[ "$SUITE" != full ] || SUITE=''
SUITE_NAME=${SUITE:-full}

# Ветви матрицы ядер: только xray и mihomo, без повторов. Явно заданный RT_CORES (даже
# равный умолчанию) — ручная отладка: гибрид набора changed выключается.
RT_CORES_SET=${RT_CORES+1}
RT_CORES=${RT_CORES:-xray mihomo}
_seen=""
for _c in $RT_CORES; do
  case "$_c" in
    xray | mihomo) ;;
    *)
      echo "router-test: RT_CORES: неизвестное ядро '$_c' (допустимы xray и mihomo)" >&2
      exit 2
      ;;
  esac
  case " $_seen " in
    *" $_c "*)
      echo "router-test: RT_CORES: ядро '$_c' указано дважды" >&2
      exit 2
      ;;
  esac
  _seen="$_seen $_c"
done

# Сужение состава окружением: явный RT_CORES и непустые переменные отбора тестов, которые этапы
# читают из окружения (отладка точечного прогона). Список имён пишется в meta отчёта строкой
# narrow= (none — без сужения): базой добора годится только полный прогон с narrow=none
# (delta-plan.mjs). Имена в цикле фиксированные, eval читает только их значения.
RT_NARROW=""
[ -z "$RT_CORES_SET" ] || RT_NARROW=" RT_CORES"
for _nv in RT_SEL_DIR RT_GO_PKGS RT_GO_RUN RT_GO_RUN_SEQ RT_GO_SEQ RT_SPECS RT_SPECS_FIRST RT_PW_GREP RT_PW_GREP_INVERT RT_PW_INVERT RT_RESULTS_NAME RT_ART_SUB; do
  eval "_nvv=\${$_nv:-}"
  [ -z "$_nvv" ] || RT_NARROW="$RT_NARROW $_nv"
done
RT_NARROW=${RT_NARROW# }
# full и delta с RELEASE публикуют stand/<arch>-full: после части набора это был бы ложный зелёный.
# Сужение — только для отладки без RELEASE; отказ с кодом 2 до блокировки, стенд не трогается.
if [ -n "${RELEASE:-}" ] && [ -n "$RT_NARROW" ] && { [ "$SUITE_NAME" = full ] || [ "$SUITE_NAME" = delta ]; }; then
  echo "router-test: SUITE=$SUITE_NAME с RELEASE публикует stand/<arch>-full, а состав сужен окружением: $RT_NARROW — снимите эти переменные (сужение — только для отладки без RELEASE)" >&2
  exit 2
fi

# Ядро первой ветви набора changed (D-02): чётная контрольная сумма cksum строки SHA HEAD
# (без перевода строки) — xray, нечётная — mihomo. Зависит только от SHA: повтор на том же
# HEAD идёт под тем же ядром, а на разных коммитах ядра чередуются.
rt_sha_core() {
  _shc=$(printf '%s' "$1" | cksum | cut -d' ' -f1)
  if [ $((_shc % 2)) = 0 ]; then
    echo xray
  else
    echo mihomo
  fi
}

cd "$(rt_repo_root)"

# Начало прогона: время набора harness в summary.md
RT_T0=$(date +%s)

# Набор harness: самопроверка сессии идёт вложенной сессией, которой нужно чистое дерево;
# узнавать об этом через десять минут прогона поздно — проверка до блокировки и деплоя.
if [ "$SUITE" = harness ] && [ -n "$(git status --porcelain --untracked-files=no)" ]; then
  echo "router-test: SUITE=harness требует чистое рабочее дерево: самопроверка сессии меняет файлы продукта" >&2
  exit 2
fi

# Набор session: спека и чистое дерево проверяются до блокировки, цель берётся из спеки.
if [ "$SUITE" = session ]; then
  if [ -z "${SPEC:-}" ]; then
    echo "router-test: SUITE=session требует SPEC=<файл спеки>" >&2
    exit 2
  fi
  ROUTERS=$(node "$SCRIPT_DIR/session.mjs" --target "$SPEC") || exit 2
  export ROUTERS
  if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
    echo "router-test: SUITE=session требует чистое рабочее дерево: проходы меняют файлы продукта" >&2
    exit 2
  fi
fi

# Набор rerun: отчёт FROM проверяется до check-known.sh, целей и блокировки (код 2, стенд не
# трогается). Путь приводит к абсолютному под build/router rt_report_use: вне build/router, с
# «..» и служебные имена отвергаются. Существование каталога проверяется до неё: она создаёт его.
RT_RERUN_N=""
RT_RERUN_K=0
if [ "$SUITE" = rerun ]; then
  if [ -z "${FROM:-}" ]; then
    echo "router-test: SUITE=rerun требует FROM=<отчёт прогона или сессии под build/router>" >&2
    exit 2
  fi
  case "$FROM" in
    /*) _rr_abs=$FROM ;;
    *) _rr_abs="$(rt_repo_root)/$FROM" ;;
  esac
  if [ ! -d "$_rr_abs" ]; then
    echo "router-test: FROM: отчёт не найден: $FROM" >&2
    exit 2
  fi
  RT_REPORT=$FROM
  rt_report_use || exit 2
fi

# Набор bisect: значения проверяются до check-known.sh, целей и блокировки (код 2, стенд не трогается).
# TEST — go:TestRouterИмя (пакет определяется по рабочему дереву; можно go:<каталог пакета>:TestRouterИмя
# для теста, которого в дереве уже нет) либо pw:название; для pw: нужен SPEC. FROM — ссылка, TO — по
# умолчанию HEAD, CORE — ядро, на котором тест падает, MODE — regress (по умолчанию) или fix.
RT_BS_GO=""
RT_BS_SPEC=""
RT_BS_GREP=""
RT_BS_LABEL=""
RT_BS_FROM=""
RT_BS_TO=""
RT_BS_MODE=""
BISECT_RC=0
rt_bs_die() {
  echo "router-test: SUITE=bisect: $*" >&2
  exit 2
}
if [ "$SUITE" = bisect ]; then
  _bs_ref_re='^[A-Za-z0-9_.][A-Za-z0-9_./~^-]*$'
  [ -n "${TEST:-}" ] || rt_bs_die "нужен TEST=<go:Имя|pw:название>"
  case "${CORE:-}" in
    xray | mihomo) ;;
    *) rt_bs_die "нужен CORE=xray|mihomo" ;;
  esac
  RT_BS_MODE=${MODE:-regress}
  case "$RT_BS_MODE" in
    regress | fix) ;;
    *) rt_bs_die "MODE: regress или fix" ;;
  esac
  [ -n "${FROM:-}" ] || rt_bs_die "нужен FROM=<ссылка: с какого коммита искать>"
  RT_BS_FROM=$FROM
  RT_BS_TO=${TO:-HEAD}
  for _bs_r in "$RT_BS_FROM" "$RT_BS_TO"; do
    printf '%s\n' "$_bs_r" | grep -Eq "$_bs_ref_re" || rt_bs_die "FROM и TO: недопустимая ссылка"
    git rev-parse --verify --quiet "$_bs_r^{commit}" >/dev/null 2>&1 || rt_bs_die "FROM и TO: '$_bs_r' не указывает на коммит"
  done
  case "$TEST" in
    go:?*)
      _bs_name=${TEST#go:}
      case "$_bs_name" in
        *:*)
          RT_BS_GO=$_bs_name
          printf '%s\n' "$RT_BS_GO" | grep -Eq '^[A-Za-z0-9_./-]+:TestRouter[A-Za-z0-9_]+$' || rt_bs_die "TEST: ожидается go:<пакет>:TestRouter<Имя>"
          RT_BS_LABEL="go:${RT_BS_GO#*:}"
          ;;
        *)
          _bs_name=${_bs_name%%/*}
          printf '%s\n' "$_bs_name" | grep -Eq '^TestRouter[A-Za-z0-9_]+$' || rt_bs_die "TEST: ожидается go:TestRouter<Имя>"
          RT_BS_GO=$(node -e '
const cm = require(process.argv[1] + "/scripts/router/core-marks.cjs");
for (const t of cm.testFacts(process.argv[1])) {
  if (t.kind === "go" && t.test === process.argv[2] && t.pkg) {
    console.log(t.pkg + ":" + t.test);
    process.exit(0);
  }
}
process.exit(1);
' "$(rt_repo_root)" "$_bs_name" 2>/dev/null) || rt_bs_die "тест $_bs_name не найден в рабочем дереве: укажите TEST=go:<пакет>:$_bs_name"
          RT_BS_LABEL="go:$_bs_name"
          ;;
      esac
      ;;
    pw:?*)
      _bs_title=${TEST#pw:}
      case "$_bs_title" in
        *"
"*) rt_bs_die "TEST: перевод строки недопустим" ;;
      esac
      [ -n "${SPEC:-}" ] || rt_bs_die "для pw: нужен SPEC=<tests/router/спек.spec.ts>"
      case "$SPEC" in
        */*) RT_BS_SPEC=$SPEC ;;
        *) RT_BS_SPEC="tests/router/$SPEC" ;;
      esac
      printf '%s\n' "$RT_BS_SPEC" | grep -Eq '^tests/router/[A-Za-z0-9_.-]+\.spec\.ts$' || rt_bs_die "SPEC: ожидается tests/router/<имя>.spec.ts"
      if [ -n "${GREP:-}" ]; then
        RT_BS_GREP=$GREP
      else
        # название «описание > тест» -> «описание тест» с экранированными символами регэкспа
        RT_BS_GREP=$(printf '%s' "$_bs_title" | sed -e 's/ > / /g' -e 's/[][\.*^$(){}?+|]/\\&/g')
      fi
      case "$RT_BS_GREP" in
        *"'"* | *"
"*) rt_bs_die "GREP: одинарная кавычка и перевод строки недопустимы" ;;
      esac
      RT_BS_LABEL=$TEST
      ;;
    *) rt_bs_die "TEST: ожидается go:Имя или pw:название" ;;
  esac
fi

# Набор delta: тег, DRY_RUN, BASE_REPORT и база добора проверяются до check-known.sh, целей и блокировки
# (код 2 на неверных значениях, код 1 — нет подходящей базы; стенд не трогается). RELEASE=latest — новейший
# опубликованный релиз (как latestReleaseTag в session.mjs). Коммит тега обязан быть предком HEAD: тесты
# берутся из рабочего дерева, оно не должно быть старше релиза. База — delta-plan.mjs base: отчёт полного
# прогона по более старому тегу той же вехи (условия — в его заголовке).
DELTA_BASE_DIR=""
DELTA_BASE_TAG=""
DELTA_BASE_NAME=""
DELTA_DRY=0
rt_dl_die() {
  echo "router-test: SUITE=delta: $*" >&2
  exit 2
}
if [ "$SUITE" = delta ]; then
  [ -z "$RT_CORES_SET" ] || rt_dl_die "RT_CORES не применяется: добор идёт на обоих ядрах по составу"
  [ -n "${RELEASE:-}" ] || rt_dl_die "нужен RELEASE=<тег|latest>"
  if [ "$RELEASE" = latest ]; then
    command -v gh >/dev/null 2>&1 || rt_dl_die "RELEASE=latest: нужен gh"
    RELEASE=$(gh release list --exclude-drafts --limit 1 --json tagName --jq '.[0].tagName' 2>/dev/null) || RELEASE=""
    [ -n "$RELEASE" ] || rt_dl_die "RELEASE=latest: новейший релиз не определён (gh release list)"
  fi
  printf '%s\n' "$RELEASE" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+\.[0-9]+)?$' || rt_dl_die "RELEASE: ожидается vX.Y.Z или vX.Y.Z-<суффикс>.<N>"
  git rev-parse -q --verify "refs/tags/$RELEASE^{commit}" >/dev/null 2>&1 || rt_dl_die "тег $RELEASE не найден локально (git fetch --tags)"
  git merge-base --is-ancestor "$RELEASE" HEAD 2>/dev/null || rt_dl_die "тесты старше релиза: коммит $RELEASE не предок HEAD"
  case "${DRY_RUN:-0}" in
    0 | 1) DELTA_DRY=${DRY_RUN:-0} ;;
    *) rt_dl_die "DRY_RUN: 0 или 1" ;;
  esac
  if [ -n "${BASE_REPORT:-}" ]; then
    printf '%s\n' "$BASE_REPORT" | grep -Eq '^[A-Za-z0-9_./-]+$' || rt_dl_die "BASE_REPORT: недопустимые символы"
  fi
  _dl_rc=0
  if [ -n "${BASE_REPORT:-}" ]; then
    _dl_out=$(node "$SCRIPT_DIR/delta-plan.mjs" base --release "$RELEASE" --base-report "$BASE_REPORT") || _dl_rc=$?
  else
    _dl_out=$(node "$SCRIPT_DIR/delta-plan.mjs" base --release "$RELEASE") || _dl_rc=$?
  fi
  case "$_dl_rc" in
    0) ;;
    1)
      echo "router-test: красный — нет базы добора: нужен полный прогон на пре-релизе этой вехи (make router-test SUITE=full RELEASE=<тег>)" >&2
      exit 1
      ;;
    *) rt_dl_die "база добора не определена (delta-plan.mjs, код $_dl_rc)" ;;
  esac
  DELTA_BASE_DIR=$(printf '%s\n' "$_dl_out" | cut -f1)
  DELTA_BASE_TAG=$(printf '%s\n' "$_dl_out" | cut -f2)
  printf '%s\n' "$DELTA_BASE_TAG" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[a-z]+\.[0-9]+)?$' || rt_dl_die "тег базы добора не разобран"
  [ -d "$DELTA_BASE_DIR" ] || rt_dl_die "отчёт базы добора не найден"
  DELTA_BASE_NAME=$(basename "$DELTA_BASE_DIR")
fi

# Виды спеков рабочего дерева (WR-04): незакоммиченный спек read не уходит в несколько воркеров
# без проверки. До блокировки и деплоя, только для наборов с Playwright.
case "$SUITE" in
  '' | changed | harness | session | delta)
    if ! node scripts/check-router-kinds.js --worktree; then
      echo "router-test: красный — виды роутерных спеков рабочего дерева не прошли проверку (check-router-kinds.js --worktree)" >&2
      exit 1
    fi
    ;;
esac

# Метки известных падений должны ссылаться на живые todo (D-13): до блокировки и деплоя.
if ! sh "$SCRIPT_DIR/check-known.sh"; then
  echo "router-test: красный — метки известных падений ссылаются на закрытые или несуществующие todo" >&2
  exit 1
fi

rt_load_targets

# Набор bisect идёт на одной цели (ядро и сборка стенда возвращаются для неё).
if [ "$SUITE" = bisect ]; then
  # shellcheck disable=SC2086
  if [ "$(printf '%s\n' $RT_TARGETS | grep -c .)" != 1 ]; then
    rt_bs_die "нужна одна цель (ROUTERS=<id>), выбрано: $RT_TARGETS"
  fi
fi

# Набор rerun: состав перепрогона считается до блокировки (только чтение отчёта): в перепрогон
# идут цели, у которых есть упавшие тесты; нет ни одной — стенд не трогается, код 0.
if [ "$SUITE" = rerun ]; then
  _rr_keep=""
  _rr_tmp=$(mktemp -d)
  for _rr_t in $RT_TARGETS; do
    _rr_rc=0
    node "$SCRIPT_DIR/rerun-plan.mjs" "$RT_REPORT" "$_rr_t" --out-dir "$_rr_tmp/$_rr_t" >"$_rr_tmp/$_rr_t.out" 2>&1 || _rr_rc=$?
    case "$_rr_rc" in
      0) _rr_keep="$_rr_keep $_rr_t" ;;
      1) ;;
      *)
        cat "$_rr_tmp/$_rr_t.out" >&2
        rm -rf "$_rr_tmp"
        echo "router-test: rerun-plan.mjs не разобрал отчёт $RT_REPORT (код $_rr_rc)" >&2
        exit 2
        ;;
    esac
  done
  rm -rf "$_rr_tmp"
  if [ -z "$_rr_keep" ]; then
    echo "router-test: в отчёте $RT_REPORT нет упавших тестов (FAIL, XPASS проверок go: и pw:) для перепрогона, стенд не тронут"
    exit 0
  fi
  RT_TARGETS=${_rr_keep# }
fi

rt_lock
if [ "$SUITE" = rerun ]; then
  # Перепрогон пишет в отчёт FROM: номер — следующий свободный, meta и ссылка last не меняются.
  # Строки результатов всех стадий (включая смоук и восстановление) идут в rerun-N.tsv, артефакты
  # тестов — в <ядро>/rerun-N/ (RT_RESULTS_NAME и RT_ART_SUB читают rt_result и этапы тестов).
  RT_RERUN_N=1
  while [ -e "$RT_REPORT/rerun-$RT_RERUN_N.meta" ]; do
    RT_RERUN_N=$((RT_RERUN_N + 1))
  done
  _rr_dirty=false
  [ -z "$(git status --porcelain --untracked-files=no)" ] || _rr_dirty=true
  {
    echo "sha=$(git rev-parse HEAD)"
    echo "dirty=$_rr_dirty"
    echo "started=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  } >"$RT_REPORT/rerun-$RT_RERUN_N.meta"
  RT_RESULTS_NAME="rerun-$RT_RERUN_N.tsv"
  RT_ART_SUB="rerun-$RT_RERUN_N"
  export RT_RESULTS_NAME RT_ART_SUB
else
  rt_report_init
  echo "narrow=${RT_NARROW:-none}" >>"$RT_REPORT/meta"
fi
echo "router-test: отчёт $RT_REPORT"

# Ротация отчётов build/router стоит здесь, а не в конце: у run.sh много выходов (130, 1, код
# сессии, _finish в reboot), а hold_lock перекрывает EXIT-trap. Блокировка уже взята, текущий
# отчёт (и корень сессии при RT_REPORT_DIR) защищён; возраст считается по времени, поэтому
# результат тот же. Подоболочка и || не дают сбою ротации изменить ход и код прогона.
( tidy_rotate_reports "$(rt_repo_root)" ) || echo "router-test: предупреждение: ротация отчётов не удалась (прогон продолжается)" >&2

# Сокращённые сроки на время прогона (D-16 фазы 148.1.1, D-22): блок xcp-rt (lib.sh) идёт в наборах
# changed, full, delta, rerun, bisect и harness, но не в smoke, reboot и session. Полный прогон цели
# (full_target) пишет блок сам после снимка; шаги поиска виновного коммита (tree-step.sh) пишут его
# после своего деплоя по RT_RTENV=1; сессия ставит блок перед своими этапами сама. RT_EXPECT_RT_ENV —
# ожидание блока для проверки rt-env смоука: вне ветвей с записанным блоком блока быть не должно.
case "$SUITE" in
  '' | changed | delta | rerun | bisect | harness) RT_RTENV=1 ;;
  *) RT_RTENV=0 ;;
esac
RT_EXPECT_RT_ENV=0
export RT_RTENV RT_EXPECT_RT_ENV

# Удержание блокировки после прогона для ручной проверки (RT_HOLD_MINUTES).
hold_lock() {
  if [ -n "${RT_HOLD_MINUTES:-}" ] && [ "$RT_HOLD_MINUTES" -gt 0 ] 2>/dev/null; then
    RELEASE_FILE="$(rt_common_dir)/router-test.release"
    rm -f "$RELEASE_FILE"
    # INT/TERM/HUP прерывают удержание (код 130); EXIT убирает файл-флаг при любом выходе
    trap 'rm -f "$RELEASE_FILE"' EXIT
    trap 'exit 130' INT TERM HUP
    echo "router-test: блокировка удерживается $RT_HOLD_MINUTES мин; снять раньше: touch $RELEASE_FILE"
    END=$(($(date +%s) + RT_HOLD_MINUTES * 60))
    while [ "$(date +%s)" -lt "$END" ] && [ ! -e "$RELEASE_FILE" ]; do
      sleep 1
    done
    rm -f "$RELEASE_FILE"
  fi
}

# --- набор session ------------------------------------------------------------------
# Сессию ведёт session.mjs под блокировкой этого процесса. Сигнал передаётся ему, и run.sh
# ждёт, пока он вернёт стенд: по признаку жизни (kill -0), а не по коду wait, как у матрицы ниже.
if [ "$SUITE" = session ]; then
  node "$SCRIPT_DIR/session.mjs" "$SPEC" &
  SPID=$!
  trap 'kill -TERM "$SPID" 2>/dev/null || true' INT TERM HUP
  SRC=0
  wait "$SPID" || SRC=$?
  while kill -0 "$SPID" 2>/dev/null; do
    SRC=0
    wait "$SPID" || SRC=$?
  done
  trap - INT TERM HUP
  # Прерванная сессия (код 130) блокировку не удерживает, как и прерванная матрица
  [ "$SRC" = 130 ] || hold_lock
  if [ "$SRC" = 0 ]; then
    echo "router-test: зелёный"
  else
    echo "router-test: сессия завершилась с кодом $SRC (итог — $RT_REPORT/summary.md)" >&2
  fi
  exit "$SRC"
fi

# --- план состава прогона (T2h, D-07, D-12) -------------------------------------------
# Решения о составе наборов changed, harness и delta — блок harness файла areas.json (go, specs,
# grep, session), его добавление к changed (RT_H_EXTRA), вторая ветвь гибрида (RT_B2, RT_B2_RUN),
# окно смоука по умолчанию (RT_SMOKE_DEFAULT) и самопроверка сессии (RT_SELFCHECK) — принимает
# один модуль run-plan.mjs; тот же план читает повтор выбора select-replay.mjs. run.sh вызывает
# его через rt_harness_load <набор> [<select.env>] [<ядро b1>] и сверяет вывод по числу строк,
# порядку имён и шаблону каждой перед eval (T-148.1-28, T-148.1.1-12): значения уходят в окружение
# этапов и во вложенный run.sh. Не прошёл — код 1, значения прежние (по умолчанию), причина в
# <отчёт>/harness-load.err; что делать дальше, решает набор (changed — полный выбор, harness —
# красный до деплоя, delta — delta_abort). Наборы full, rerun, smoke, reboot и bisect модуль не
# вызывают: самопроверки у них нет (RT_SELFCHECK=0).
rt_plan_defaults() {
  RT_H_GO_SEQ=""
  RT_H_GO_PKGS=""
  RT_H_SPECS=""
  RT_H_GREP=""
  RT_H_SESSION=""
  # 1 — в changed к выбору добавлены этапы набора harness (RT_HARNESS=1)
  RT_H_EXTRA=0
  # вторая ветвь гибрида changed: ядро и признак запуска
  RT_B2=""
  RT_B2_RUN=0
  # окно смоука по умолчанию (явный RT_SMOKE_WINDOW приоритетнее — решение run.sh)
  RT_SMOKE_DEFAULT=120
  # 1 — после ветвей идёт самопроверка сессии (harness_selfcheck)
  RT_SELFCHECK=0
}
rt_plan_defaults
# итог самопроверки сессии для summary.md: «да», «нет» или пусто (не требовалась)
RT_H_SELF=""
RT_H_SELF_WHY=""
rt_harness_load() {
  _hl_suite=$1
  _hl_sel=${2:-}
  _hl_b1=${3:-}
  set -- --sh --suite "$_hl_suite"
  [ -z "$_hl_sel" ] || set -- "$@" --select "$_hl_sel"
  [ -z "$_hl_b1" ] || set -- "$@" --b1 "$_hl_b1"
  _hl_rc=0
  _hl_out=$(node "$SCRIPT_DIR/run-plan.mjs" "$@" 2>"$RT_REPORT/harness-load.err") || _hl_rc=$?
  _q="'"
  if [ "$_hl_rc" != 0 ] ||
    [ "$(printf '%s\n' "$_hl_out" | wc -l)" != 10 ] ||
    [ "$(printf '%s\n' "$_hl_out" | sed 's/=.*//' | tr '\n' ' ')" != "RT_H_EXTRA RT_B2 RT_B2_RUN RT_SMOKE_DEFAULT RT_SELFCHECK RT_H_GO_SEQ RT_H_GO_PKGS RT_H_SPECS RT_H_GREP RT_H_SESSION " ] ||
    printf '%s\n' "$_hl_out" | grep -qvE "^(RT_H_EXTRA=${_q}[01]${_q}|RT_B2=${_q}(xray|mihomo)?${_q}|RT_B2_RUN=${_q}[01]${_q}|RT_SMOKE_DEFAULT=${_q}(30|120)${_q}|RT_SELFCHECK=${_q}[01]${_q}|RT_H_GO_SEQ=${_q}[A-Za-z0-9_./: -]*${_q}|RT_H_GO_PKGS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_H_SPECS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_H_GREP=${_q}[A-Za-z0-9_|()?!^*.: -]*${_q}|RT_H_SESSION=${_q}(scripts/router/[A-Za-z0-9_./-]+)?${_q})\$"; then
    echo "план состава не получен или не прошёл проверку (run-plan.mjs --suite $_hl_suite, код $_hl_rc)" >>"$RT_REPORT/harness-load.err"
    return 1
  fi
  eval "$_hl_out"
  # Набор harness, этапы harness в changed и самопроверка сессии требуют прочитанного блока.
  if { [ "$_hl_suite" = harness ] || [ "$RT_H_EXTRA" = 1 ] || [ "$RT_SELFCHECK" = 1 ]; } &&
    { [ -z "$RT_H_GO_SEQ" ] || [ -z "$RT_H_GO_PKGS" ] || [ -z "$RT_H_SPECS" ] || [ -z "$RT_H_SESSION" ]; }; then
    echo "план состава: блок harness пуст при этапах harness или самопроверке (run-plan.mjs --suite $_hl_suite)" >>"$RT_REPORT/harness-load.err"
    rt_plan_defaults
    return 1
  fi
}

# --- выбор проверок набора changed (D-25) ------------------------------------------
# select-changed.mjs печатает тринадцать строк для eval; перед eval вывод проверяется по
# строгому шаблону на каждую строку, по их числу и порядку имён (T-144.1-23, T-145.1-18,
# T-148.1-23), при любом сомнении выбирается всё, а вторая ветвь идёт со всеми тестами
# (RT_SEL_BAD=1). Проверенный вывод — <отчёт>/select.env: по нему run-plan.mjs составляет план
# (rt_harness_load changed). Код 3 селектора — выбор остановлен (его нельзя надёжно получить,
# D-24, D-28): красный до деплоя и обращений к стенду, причины — строки «  - стоп: …» в
# select.txt; полный выбор вместо него не подставляется.
SEL_ANY=1
RT_SEL_BAD=0
RT_KERNEL_TOUCHED=0
RT_GO_CORE_PKGS=""
RT_GO_CORE_RUN=""
RT_SPECS_CORE=""
RT_GO_SEQ=""
RT_SPECS_FIRST=""
RT_PW_INVERT=""
RT_HARNESS=0
RT_AREAS=""
RT_SEL_KIND=""
if [ "$SUITE" = harness ]; then
  if ! rt_harness_load harness; then
    echo "router-test: красный — блок harness в scripts/router/areas.json не прочитан или не прошёл проверку (причина — $RT_REPORT/harness-load.err)" >&2
    exit 1
  fi
  # Все тесты набора идут в каждой ветви основными этапами: Go по именам (go-<ядро>), спеки
  # read и state (pw-read-<ядро>, pw-<ядро>); grep блока относится только к спекам набора.
  RT_SELECT_ALL=0
  RT_GO_SEQ=$RT_H_GO_SEQ
  RT_GO_PKGS=$RT_H_GO_PKGS
  RT_SPECS=$RT_H_SPECS
  RT_PW_GREP=$RT_H_GREP
  export RT_GO_PKGS RT_SPECS RT_PW_GREP
fi
if [ "$SUITE" = changed ]; then
  _sel_args=""
  [ -z "${RT_BASE:-}" ] || _sel_args="--base $RT_BASE"
  [ -z "${RT_FILES:-}" ] || _sel_args="--files $RT_FILES"
  _sel_rc=0
  # shellcheck disable=SC2086
  _sel=$(node "$SCRIPT_DIR/select-changed.mjs" --explain $_sel_args 2>"$RT_REPORT/select.txt") || _sel_rc=$?
  if [ "$_sel_rc" = 3 ]; then
    # выбор остановлен: причины уже в select.txt (stderr селектора), стенд не тронут
    _sel_stop=$(sed -n 's/^  - стоп: //p' "$RT_REPORT/select.txt" | head -n 1)
    echo "итог: выбор остановлен — прогон не идёт (полный выбор не подставляется)" >>"$RT_REPORT/select.txt"
    echo "router-test: красный — выбор остановлен: ${_sel_stop:-причина не названа} (все — $RT_REPORT/select.txt)" >&2
    exit 1
  fi
  _q="'"
  if [ "$_sel_rc" != 0 ] ||
    [ "$(printf '%s\n' "$_sel" | wc -l)" != 13 ] ||
    [ "$(printf '%s\n' "$_sel" | sed 's/=.*//' | tr '\n' ' ')" != "RT_SELECT_ALL RT_GO_PKGS RT_SPECS RT_PW_GREP RT_GO_CORE_PKGS RT_GO_CORE_RUN RT_SPECS_CORE RT_KERNEL_TOUCHED RT_GO_SEQ RT_SPECS_FIRST RT_PW_INVERT RT_HARNESS RT_AREAS " ] ||
    printf '%s\n' "$_sel" | grep -qvE "^(RT_SELECT_ALL=[01]|RT_GO_PKGS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_SPECS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_PW_GREP=${_q}[A-Za-z0-9_|()?!^*.: -]*${_q}|RT_GO_CORE_PKGS=${_q}[A-Za-z0-9_./ -]*${_q}|RT_GO_CORE_RUN=${_q}[A-Za-z0-9_|^\$()]*${_q}|RT_SPECS_CORE=${_q}[A-Za-z0-9_./ -]*${_q}|RT_KERNEL_TOUCHED=[01]|RT_GO_SEQ=${_q}[A-Za-z0-9_./: -]*${_q}|RT_SPECS_FIRST=${_q}[A-Za-z0-9_./ -]*${_q}|RT_PW_INVERT=${_q}[A-Za-z0-9_@|()?!^*.: -]*${_q}|RT_HARNESS=[01]|RT_AREAS=${_q}[a-z0-9 -]*${_q})\$"; then
    echo "всё: выбор не удался или его вывод не прошёл проверку (код $_sel_rc)" >>"$RT_REPORT/select.txt"
    RT_SELECT_ALL=1
    RT_SEL_BAD=1
    RT_KERNEL_TOUCHED=1
    RT_SEL_KIND="полный"
  else
    printf '%s\n' "$_sel" >"$RT_REPORT/select.env"
    eval "$_sel"
    # вид выбора — строка причин «вид выбора: …» (select-changed.mjs --explain)
    RT_SEL_KIND=$(sed -n 's/^  - вид выбора: //p' "$RT_REPORT/select.txt" | tail -n 1)
    case "$RT_SEL_KIND" in
      полный | обвязка | зависимости | области | смоук) ;;
      *) RT_SEL_KIND="полный" ;;
    esac
  fi
  if [ "$RT_SELECT_ALL" = 1 ]; then
    # полный набор: списки не нужны, этапы go и pw берут значения по умолчанию; списки тестов
    # с меткой ядра и RT_KERNEL_TOUCHED остаются: по ним строится вторая ветвь
    unset RT_GO_PKGS RT_SPECS RT_PW_GREP
    RT_GO_SEQ=""
    RT_SPECS_FIRST=""
    RT_PW_INVERT=""
    echo "итог: полный набор" >>"$RT_REPORT/select.txt"
  elif [ -z "$RT_GO_PKGS" ] && [ -z "$RT_SPECS" ]; then
    SEL_ANY=0
    echo "итог: затронутого нет — деплой и смоук" >>"$RT_REPORT/select.txt"
  else
    echo "итог: Go: ${RT_GO_PKGS:--}; спеки: ${RT_SPECS:--}; области: ${RT_AREAS:--}" >>"$RT_REPORT/select.txt"
    export RT_GO_PKGS RT_SPECS RT_PW_GREP
  fi
  # План состава (run-plan.mjs по select.env и ядру первой ветви из SHA HEAD): вторая ветвь
  # гибрида, окно смоука по умолчанию, самопроверка сессии. Правка обвязки стенда (RT_HARNESS=1):
  # к выбору добавляется набор harness (RT_H_EXTRA=1) — его Go-тесты и спеки идут отдельными
  # этапами в обеих ветвях (full_target, _harness_go и _harness_pw), а после ветвей идёт
  # самопроверка сессии (RT_SELFCHECK=1). Полный выбор набор уже содержит: этапы не добавляются.
  # План не получен (блок harness не прочитан, вывод модуля не прошёл проверку) — выбор
  # расширяется до полного, как при сбое самого выбора; самопроверка — по RT_HARNESS выбора.
  _rp_fail=0
  if [ "$RT_SEL_BAD" = 0 ] && ! rt_harness_load changed "$RT_REPORT/select.env" "$(rt_sha_core "$(git rev-parse HEAD)")"; then
    _rp_fail=1
    if [ "$RT_HARNESS" = 1 ]; then
      echo "всё: блок harness не прочитан (см. harness-load.err) — выбор расширен до полного" >>"$RT_REPORT/select.txt"
    else
      echo "всё: план состава не получен (см. harness-load.err) — выбор расширен до полного" >>"$RT_REPORT/select.txt"
    fi
    unset RT_GO_PKGS RT_SPECS RT_PW_GREP
    RT_SELECT_ALL=1
    RT_SEL_BAD=1
    RT_KERNEL_TOUCHED=1
    RT_SEL_KIND="полный"
    RT_GO_SEQ=""
    RT_SPECS_FIRST=""
    RT_PW_INVERT=""
    SEL_ANY=1
    RT_SELFCHECK=$RT_HARNESS
  elif [ "$RT_SEL_BAD" = 1 ]; then
    RT_SELFCHECK=$RT_HARNESS
  elif [ "$RT_HARNESS" = 1 ]; then
    if [ "$RT_H_EXTRA" != 1 ]; then
      echo "обвязка: набор harness входит в полный выбор, самопроверка сессии — в конце" >>"$RT_REPORT/select.txt"
    else
      SEL_ANY=1
      # shellcheck disable=SC2086
      _hn_go=$(printf '%s\n' $RT_H_GO_SEQ | grep -c . || true)
      # shellcheck disable=SC2086
      _hn_pw=$(printf '%s\n' $RT_H_SPECS | grep -c . || true)
      echo "обвязка: набор harness добавлен — Go: $_hn_go, спеков: $_hn_pw, оба ядра, самопроверка сессии — в конце" >>"$RT_REPORT/select.txt"
    fi
  fi
  echo "router-test: выбор — $(grep '^итог:' "$RT_REPORT/select.txt" | tail -n 1)"
  { [ "$RT_HARNESS" != 1 ] && [ "$_rp_fail" != 1 ]; } || echo "router-test: $(grep '^\(обвязка\|всё\):' "$RT_REPORT/select.txt" | tail -n 1)"
fi

# --- состав набора delta (RTR-11, D-24, D-25) ------------------------------------------------
# Добор честен только пока состав полон: невозможность составить его целиком (полный выбор по диффу,
# неизвестный файл, упавший в базе тест, который нельзя перегнать) — не «зелёный по умолчанию», а
# отказ с кодом 1 до деплоя: нужен полный прогон. Состав каждой цели — <цель>/delta.sel.d/<ядро>.sel
# (формат rerun-plan.mjs плюс CORE_ONLY), пишет delta-plan.mjs merge; run.sh читает его как состав
# перепрогона (full_target, _sel_dir). Ветвь первого ядра пишется всегда: в ней идёт смоук нового тега.
DELTA_B1=""
DELTA_B2=""
delta_abort() {
  echo "router-test: красный — добор невозможен: $*; нужен полный прогон (make router-test SUITE=full RELEASE=$RELEASE)" >&2
  exit 1
}
delta_compose() {
  _ds_rc=0
  _ds=$(node "$SCRIPT_DIR/select-changed.mjs" --explain --base "$DELTA_BASE_TAG" --head "$RELEASE" 2>"$RT_REPORT/select.txt") || _ds_rc=$?
  if [ "$_ds_rc" != 0 ] ||
    [ "$(printf '%s\n' "$_ds" | wc -l)" != 13 ] ||
    [ "$(printf '%s\n' "$_ds" | sed 's/=.*//' | tr '\n' ' ')" != "RT_SELECT_ALL RT_GO_PKGS RT_SPECS RT_PW_GREP RT_GO_CORE_PKGS RT_GO_CORE_RUN RT_SPECS_CORE RT_KERNEL_TOUCHED RT_GO_SEQ RT_SPECS_FIRST RT_PW_INVERT RT_HARNESS RT_AREAS " ]; then
    delta_abort "выбор по диффу $DELTA_BASE_TAG..$RELEASE не удался (код $_ds_rc, причины — $RT_REPORT/select.txt)"
  fi
  _ds_all=$(printf '%s\n' "$_ds" | sed -n 's/^RT_SELECT_ALL=//p')
  RT_HARNESS=$(printf '%s\n' "$_ds" | sed -n 's/^RT_HARNESS=//p')
  case "$_ds_all" in 0 | 1) ;; *) delta_abort "выбор по диффу: RT_SELECT_ALL не разобран" ;; esac
  case "$RT_HARNESS" in 0 | 1) ;; *) delta_abort "выбор по диффу: RT_HARNESS не разобран" ;; esac
  [ "$_ds_all" = 0 ] || delta_abort "выбор по диффу $DELTA_BASE_TAG..$RELEASE полный (правка самого выбора), добор не годится"
  if grep -q 'предупреждение: неизвестный файл' "$RT_REPORT/select.txt"; then
    delta_abort "в диффе $DELTA_BASE_TAG..$RELEASE есть файл, неизвестный выбору (select.txt)"
  fi
  printf '%s\n' "$_ds" >"$RT_REPORT/delta-select.env"
  DELTA_B1=$(rt_sha_core "$(git rev-parse HEAD)")
  case "$DELTA_B1" in xray) DELTA_B2=mihomo ;; *) DELTA_B2=xray ;; esac
  RT_CORES="$DELTA_B1 $DELTA_B2"
  # Правка обвязки между тегами: тесты и спеки блока harness входят в обе ветви через delta-plan.mjs
  # --extra-* (отдельных этапов harness у добора нет), самопроверка сессии идёт в конце. Из плана
  # delta (run-plan.mjs) берутся только RT_H_* и RT_SELFCHECK: ветви, состав и окно смоука — здесь.
  if [ "$RT_HARNESS" = 1 ]; then
    rt_harness_load delta "$RT_REPORT/delta-select.env" || delta_abort "блок harness в areas.json не прочитан (причина — $RT_REPORT/harness-load.err)"
  fi
  RT_PW_INVERT=""
  _dc_first=1
  for _dc_t in $RT_TARGETS; do
    mkdir -p "$RT_REPORT/$_dc_t"
    _dc_out="$RT_REPORT/$_dc_t/delta.plan.txt"
    [ -f "$DELTA_BASE_DIR/$_dc_t/results.tsv" ] || delta_abort "в базе добора $DELTA_BASE_NAME нет результатов цели $_dc_t"
    _dc_rc=0
    if [ "$RT_HARNESS" = 1 ]; then
      node "$SCRIPT_DIR/delta-plan.mjs" merge --release "$RELEASE" --base-report "$DELTA_BASE_DIR" --target "$_dc_t" \
        --out-dir "$RT_REPORT/$_dc_t/delta.sel.d" --select-env "$RT_REPORT/delta-select.env" --b1 "$DELTA_B1" --b2 "$DELTA_B2" \
        --extra-go-seq "$RT_H_GO_SEQ" --extra-specs "$RT_H_SPECS" --extra-grep "$RT_H_GREP" >"$_dc_out" 2>"$_dc_out.err" || _dc_rc=$?
    else
      node "$SCRIPT_DIR/delta-plan.mjs" merge --release "$RELEASE" --base-report "$DELTA_BASE_DIR" --target "$_dc_t" \
        --out-dir "$RT_REPORT/$_dc_t/delta.sel.d" --select-env "$RT_REPORT/delta-select.env" --b1 "$DELTA_B1" --b2 "$DELTA_B2" \
        >"$_dc_out" 2>"$_dc_out.err" || _dc_rc=$?
    fi
    if [ "$_dc_rc" != 0 ]; then
      delta_abort "состав цели $_dc_t не составлен (delta-plan.mjs, код $_dc_rc): $(head -c 300 "$_dc_out.err" | tr '\n' ' ')"
    fi
    _dc_sk=$(awk -F'\t' '$1 == "skipped" {print $2; exit}' "$_dc_out")
    [ -z "$_dc_sk" ] || delta_abort "упавший в базе тест нельзя перегнать на цели $_dc_t: $_dc_sk"
    [ -f "$RT_REPORT/$_dc_t/delta.sel.d/$DELTA_B1.sel" ] || delta_abort "состав цели $_dc_t не записан"
    # счётчики для строки «Добор»: упавших в базе, Go и спеков по диффу
    awk -F'\t' '$1 == "base-failed" {f = $2} $1 == "diff-go" {g = $2} $1 == "diff-specs" {s = $2} END {printf "%d\t%d\t%d\n", f, g, s}' "$_dc_out" >"$RT_REPORT/$_dc_t/delta.counts"
    # отсев яруса full выбора снимается, если в составе есть упавшие в базе спеки (они могли быть яруса full)
    _dc_inv=$(awk -F'\t' '$1 == "pw-invert" {print $2}' "$_dc_out")
    if [ "$_dc_first" = 1 ]; then
      RT_PW_INVERT=$_dc_inv
      _dc_first=0
    elif [ -z "$_dc_inv" ]; then
      RT_PW_INVERT=""
    fi
    awk -F'\t' -v t="$_dc_t" '{printf "router-test: добор %s — %s: %s\n", t, $1, $2}' "$_dc_out" >&2
  done
}

# --- план ветвей и окно смоука (D-01…D-03, D-07) -----------------------------------------
# changed без явного RT_CORES — гибрид: первая ветвь идёт под ядром из SHA HEAD со всем
# выбором, вторая (другое ядро) — только с тестами, размеченными как зависящие от ядра, и не
# запускается вовсе, если таких в выборе нет (ни sync, ни переключения, ни смоука, ни окна).
# Полный прогон — полная матрица (оба ядра, все тесты); явный RT_CORES — прежняя семантика
# для ручной отладки. Строки плана — в <отчёт>/plan.txt: раздел «Выбор» report.md и начало
# раздела цели в summary.md.
# Вторую ветвь гибрида (RT_B2, RT_B2_RUN) и окно смоука по умолчанию (RT_SMOKE_DEFAULT) решает план
# состава (rt_harness_load выше); RT_B2 читается только при RT_HYBRID=1.
RT_HYBRID=0
RT_B1=""
if [ "$SUITE" = harness ]; then
  # набор harness: оба ядра, все тесты набора в каждой ветви (гибрида нет), окно смоука по плану (30 с)
  : >"$RT_REPORT/plan.txt"
  # shellcheck disable=SC2086
  _hn_go=$(printf '%s\n' $RT_H_GO_SEQ | grep -c . || true)
  # shellcheck disable=SC2086
  _hn_pw=$(printf '%s\n' $RT_H_SPECS | grep -c . || true)
  echo "- Ветви: $RT_CORES (набор harness: все тесты набора в каждой ветви, гибрид выключен)" >>"$RT_REPORT/plan.txt"
  echo "- Набор harness: Go: $_hn_go, спеков: $_hn_pw, самопроверка сессии: $RT_H_SESSION" >>"$RT_REPORT/plan.txt"
  if [ -n "${RT_SMOKE_WINDOW:-}" ]; then
    echo "- Окно смоука: $RT_SMOKE_WINDOW с (RT_SMOKE_WINDOW задан явно)" >>"$RT_REPORT/plan.txt"
  else
    RT_SMOKE_WINDOW=$RT_SMOKE_DEFAULT
    echo "- Окно смоука: $RT_SMOKE_WINDOW с (набор harness)" >>"$RT_REPORT/plan.txt"
  fi
  export RT_SMOKE_WINDOW
  echo "router-test: ветви — $(head -n 1 "$RT_REPORT/plan.txt")"
elif [ "$SUITE" = rerun ]; then
  # набор rerun: состав по ядрам — файлы <ядро>.sel в <цель>/rerun-N.sel.d (plan.txt исходного
  # отчёта не трогается); окно смоука 30 с, как у узкого выбора
  _rr_keep=""
  for _rr_t in $RT_TARGETS; do
    mkdir -p "$RT_REPORT/$_rr_t"
    _rr_rc=0
    node "$SCRIPT_DIR/rerun-plan.mjs" "$RT_REPORT" "$_rr_t" --out-dir "$RT_REPORT/$_rr_t/rerun-$RT_RERUN_N.sel.d" >"$RT_REPORT/$_rr_t/rerun-$RT_RERUN_N.plan.txt" 2>&1 || _rr_rc=$?
    if [ "$_rr_rc" = 0 ]; then
      _rr_keep="$_rr_keep $_rr_t"
      for _rr_s in "$RT_REPORT/$_rr_t/rerun-$RT_RERUN_N.sel.d"/*.sel; do
        [ -f "$_rr_s" ] || continue
        _rr_c=$(sed -n 's/^COUNT=//p' "$_rr_s" | head -n 1 | tr -dc '0-9')
        RT_RERUN_K=$((RT_RERUN_K + ${_rr_c:-0}))
      done
      sed 's/^/router-test: перепрогон '"$_rr_t"' — /' "$RT_REPORT/$_rr_t/rerun-$RT_RERUN_N.plan.txt"
    fi
  done
  if [ -z "$_rr_keep" ]; then
    echo "router-test: упавших тестов для перепрогона не осталось, стенд не тронут"
    exit 0
  fi
  RT_TARGETS=${_rr_keep# }
  if [ -z "${RT_SMOKE_WINDOW:-}" ]; then
    RT_SMOKE_WINDOW=30
  fi
  export RT_SMOKE_WINDOW
elif [ "$SUITE" = delta ]; then
  # набор delta: состав по ядрам — файлы <ядро>.sel в <цель>/delta.sel.d (упавшие в базе плюс выбор по
  # диффу между тегами); ветвь без файла пропускается. Окно смоука — как у полного прогона.
  delta_compose
  : >"$RT_REPORT/plan.txt"
  echo "- Ядро ветви: $DELTA_B1 (правило: cksum SHA HEAD чётный — xray, нечётный — mihomo)" >>"$RT_REPORT/plan.txt"
  echo "- Состав добора: база $DELTA_BASE_TAG ($DELTA_BASE_NAME), упавшие в базе и выбор по диффу $DELTA_BASE_TAG..$RELEASE; вторая ветвь ($DELTA_B2) — по составу цели" >>"$RT_REPORT/plan.txt"
  echo "- Окно смоука: ${RT_SMOKE_WINDOW:-120} с (добор: смоук нового тега $RELEASE в этом же прогоне)" >>"$RT_REPORT/plan.txt"
  [ -z "${RT_SMOKE_WINDOW:-}" ] || export RT_SMOKE_WINDOW
  echo "router-test: ветви — $(head -n 1 "$RT_REPORT/plan.txt")"
elif [ "$SUITE" = changed ] || [ -z "$SUITE" ]; then
  : >"$RT_REPORT/plan.txt"
  if [ -n "$RT_CORES_SET" ] && [ "$SUITE" = changed ] && [ "$SEL_ANY" = 0 ]; then
    echo "- Ветви: нет (затронутого нет — деплой и смоук; RT_CORES=$RT_CORES не применяется)" >>"$RT_REPORT/plan.txt"
  elif [ -n "$RT_CORES_SET" ]; then
    echo "- Ветви: $RT_CORES (отладка RT_CORES: все выбранные тесты в каждой ветви, гибрид выключен)" >>"$RT_REPORT/plan.txt"
  elif [ "$SUITE" = changed ] && [ "$SEL_ANY" = 1 ] && [ "$RT_SEL_BAD" = 0 ]; then
    # вторую ветвь (другое ядро) и её запуск (тесты с меткой ядра или набор harness) решил план
    # состава по тому же ядру первой ветви
    RT_B1=$(rt_sha_core "$(git rev-parse HEAD)")
    if [ "$RT_B2_RUN" = 1 ] && [ -n "$RT_B2" ]; then
      RT_CORES="$RT_B1 $RT_B2"
    else
      RT_CORES="$RT_B1"
    fi
    RT_HYBRID=1
    echo "- Ядро ветви: $RT_B1 (правило: cksum SHA HEAD чётный — xray, нечётный — mihomo)" >>"$RT_REPORT/plan.txt"
    if [ "$RT_B2_RUN" = 1 ]; then
      _n_go=0
      if [ -n "$RT_GO_CORE_RUN" ]; then
        _n_go=$(($(printf '%s' "$RT_GO_CORE_RUN" | tr -cd '|' | wc -c) + 1))
      elif [ -n "$RT_GO_CORE_PKGS" ]; then
        # откат на «все роутерные тесты пакетов»: список тестов пуст, пакеты идут целиком
        # shellcheck disable=SC2086
        _n_go="все тесты пакетов ($(printf '%s\n' $RT_GO_CORE_PKGS | grep -c . || true))"
      fi
      # shellcheck disable=SC2086
      _n_pw=$(printf '%s\n' $RT_SPECS_CORE | grep -c . || true)
      echo "- Вторая ветвь ($RT_B2): только тесты с меткой ядра (Go: $_n_go, Playwright: $_n_pw спеков)$([ "$RT_H_EXTRA" != 1 ] || printf ' и все тесты набора harness')" >>"$RT_REPORT/plan.txt"
    else
      echo "- Вторая ветвь ($RT_B2): не запускалась — в выборе нет тестов с меткой ядра" >>"$RT_REPORT/plan.txt"
    fi
  elif [ "$SUITE" = changed ] && [ "$RT_SEL_BAD" = 1 ]; then
    echo "- Ветви: $RT_CORES (выбор не прошёл проверку: вторая ветвь со всеми тестами)" >>"$RT_REPORT/plan.txt"
  elif [ "$SUITE" = changed ]; then
    echo "- Ветви: нет (затронутого нет — деплой и смоук)" >>"$RT_REPORT/plan.txt"
  else
    echo "- Ветви: $RT_CORES (полная матрица: все тесты под каждым ядром)" >>"$RT_REPORT/plan.txt"
  fi
  # окно смоука: в changed — по плану состава (120 с при затронутом коде запуска ядра или полном
  # выборе, иначе 30 с); заданный в окружении RT_SMOKE_WINDOW приоритетнее; в полном прогоне —
  # RT_SMOKE_WINDOW или 120 с (D-07)
  if [ "$SUITE" = changed ]; then
    if [ -n "${RT_SMOKE_WINDOW:-}" ]; then
      echo "- Окно смоука: $RT_SMOKE_WINDOW с (RT_SMOKE_WINDOW задан явно)" >>"$RT_REPORT/plan.txt"
    elif [ "$RT_SMOKE_DEFAULT" = 120 ]; then
      RT_SMOKE_WINDOW=120
      echo "- Окно смоука: 120 с (затронут код запуска ядра или выбор полный)" >>"$RT_REPORT/plan.txt"
    else
      RT_SMOKE_WINDOW=30
      echo "- Окно смоука: 30 с (код запуска ядра и watchdog не затронуты)" >>"$RT_REPORT/plan.txt"
    fi
  else
    echo "- Окно смоука: ${RT_SMOKE_WINDOW:-120} с (полный прогон)" >>"$RT_REPORT/plan.txt"
  fi
  [ -z "${RT_SMOKE_WINDOW:-}" ] || export RT_SMOKE_WINDOW
  [ "$RT_H_EXTRA" != 1 ] || echo "- Обвязка изменена: набор harness добавлен (в обеих ветвях и самопроверка сессии $RT_H_SESSION)" >>"$RT_REPORT/plan.txt"
  echo "router-test: ветви — $(head -n 1 "$RT_REPORT/plan.txt")"
fi
# Строка о сокращённых сроках — в plan.txt прогона с ветвями (через него в report.md и summary.md).
# Перепрогон plan.txt исходного отчёта не трогает: строка — только в выводе.
if [ "$RT_RTENV" = 1 ]; then
  if [ "$SUITE" = rerun ]; then
    echo "router-test: сокращённые сроки: $RT_RTENV_VALUES"
  elif [ -z "$SUITE" ] || [ "$SUITE" = harness ] || [ "$SUITE" = delta ] || { [ "$SUITE" = changed ] && [ "$SEL_ANY" = 1 ]; }; then
    echo "- Сокращённые сроки: $RT_RTENV_VALUES" >>"$RT_REPORT/plan.txt"
  fi
fi

# Файл сэмплера PID ядра на устройстве (full_target, _kpid_start): вне /opt/tmp/xcp-rt, который
# чистит смоук, и вне каталога снимка.
RT_KPID=/opt/tmp/xcp-rt-kpid.tsv

# Проверки смоука, которые пропускаются, если цель недоступна или деплой не удался.
SKIP_CHECKS="kernel-running iptables-rules api-version version xcp-pid api-me api-service-status rt-env log-panic log-error kernel-restart"

# Состав ветви из файла <ядро>.sel (rerun-plan.mjs, RT_SEL_DIR). Читается без eval: значение —
# первая строка «КЛЮЧ=значение» (sed по ключу), безопасные символы проверяет rt_sel_ok.
rt_sel_val() {
  sed -n "s/^$2=//p" "$1" | head -n 1
}
# rt_sel_ok <GO_SEQ|SPECS|COUNT> <значение>: код 0 — значение состоит только из допустимых символов
# (T-148.1-33). GREP не проверяется по шаблону: он уходит этапу аргументом env и не раскрывается
# оболочкой; его экранирует rerun-plan.mjs, а запретные для строки символы (перевод строки)
# исключены самим чтением одной строки.
rt_sel_ok() {
  case "$1" in
    GO_SEQ) [ -z "$(printf '%s' "$2" | tr -d 'A-Za-z0-9_./: -')" ] ;;
    SPECS) [ -z "$(printf '%s' "$2" | tr -d 'A-Za-z0-9_./ -')" ] ;;
    COUNT) [ -n "$2" ] && [ -z "$(printf '%s' "$2" | tr -d '0-9')" ] ;;
    *) return 1 ;;
  esac
}

# Потолок подтверждающего перепрогона (D-19): новых падений цели больше — общая причина
# (случай 148-25), перепрогона нет вовсе, все падения остаются new. Решение принимается одним
# сравнением общего числа новых падений по всем ветвям цели с этим значением (full_target).
RT_CONFIRM_MAX=10

# Признаки потери связи в детали строки результата (класс stand, D-21); тот же список — в
# branch-report.mjs (STAND_RE). Такие падения в подтверждающий перепрогон не идут.
RT_STAND_RE='нет связи|связь потеряна|ERR_NETWORK_CHANGED|net::ERR_INTERNET_DISCONNECTED|net::ERR_NAME_NOT_RESOLVED|ECONNRESET|ETIMEDOUT'

# Красные классы цели одной строкой «new flaky xpass stand_open»: из classes.tsv (stand_open — класс
# stand с причиной «не догнан…»: связь не вернулась или догона не было, — и класс stand смены версии
# стенда: «смена версии стенда», «падение на сменённой версии стенда», D-13), без него — прежний
# подсчёт по results.tsv (FAIL как new, XPASS как xpass); нет результатов — «1 0 0 0».
rt_class_counts() {
  if [ -f "$RT_REPORT/$1/classes.tsv" ]; then
    # подтест Go и его родитель — один тест
    awk -F'\t' '
      { c = $3; if (c ~ /^go:/) sub(/\/.*/, "", c); k = $1 "\t" $2 "\t" c }
      k in seen { next }
      { seen[k] = 1 }
      $1 == "new" {n++}
      $1 == "flaky" {f++}
      $1 == "xpass" {x++}
      $1 == "stand" && ($4 ~ /^не догнан/ || $4 == "смена версии стенда" || $4 == "падение на сменённой версии стенда") {s++}
      END {printf "%d %d %d %d\n", n, f, x, s}' "$RT_REPORT/$1/classes.tsv"
  elif [ -f "$RT_REPORT/$1/results.tsv" ]; then
    awk -F'\t' '$1 == "FAIL" {n++} $1 == "XPASS" {x++} END {printf "%d 0 %d 0\n", n, x}' "$RT_REPORT/$1/results.tsv"
  else
    echo "1 0 0 0"
  fi
}

# Красные классы цели в переменных: RT_RC_NEW (new), RT_RC_RED (new, flaky и не догнанный stand),
# RT_RC_XPASS (xpass). Разбор счётчиков — в функции: set -- не должен менять позиционные параметры
# вызывающего.
rt_red_counts() {
  set -- $(rt_class_counts "$1")
  RT_RC_NEW=$1
  RT_RC_RED=$(($1 + $2 + $4))
  RT_RC_XPASS=$3
}

# --- проверка связи -----------------------------------------------------------

REACHABLE=""
for t in $RT_TARGETS; do
  mkdir -p "$RT_REPORT/$t"
  rc=0
  rt_stage "$t" reachable rt_ssh "$t" "true" || rc=$?
  if [ "$rc" = 0 ]; then
    REACHABLE="$REACHABLE $t"
  else
    rt_result "$t" FAIL reachable "нет связи"
    for c in $SKIP_CHECKS; do
      rt_result "$t" SKIP "$c" "нет связи"
    done
  fi
done

# --- деплой ---------------------------------------------------------------------

DEPLOY_ARGS=""
if [ -n "${RELEASE:-}" ]; then
  DEPLOY_ARGS="--release $RELEASE"
fi
EXPECT=""
# Блок сокращённых сроков, оставшийся после обрыва прошлого прогона (D-23), снимается в начале
# любого прогона, даже если деплой цели не дойдёт до установки; без перезапуска — деплой перезапустит
# панель сам и тоже снимает блок. Сбой — строка в лог прогона, итог не меняется.
for t in $REACHABLE; do
  rc=0
  rt_stage "$t" rt-env-clear rt_rtenv_clear "$t" --no-restart >/dev/null || rc=$?
  [ "$rc" = 0 ] || echo "router-test: цель $t: оставшийся блок сокращённых сроков не снят до деплоя (код $rc), его снимет установка" >&2
done
if [ -n "$REACHABLE" ]; then
  rc=0
  # shellcheck disable=SC2086
  ROUTERS=$(printf '%s' "$REACHABLE" | sed 's/^ //; s/ /,/g') sh "$SCRIPT_DIR/deploy.sh" $DEPLOY_ARGS || rc=$?
  [ -f "$RT_REPORT/version" ] && EXPECT=$(cat "$RT_REPORT/version")
  for t in $REACHABLE; do
    drc=1
    [ -f "$RT_REPORT/$t/deploy.rc" ] && drc=$(cat "$RT_REPORT/$t/deploy.rc")
    if [ "$drc" = 0 ]; then
      DEPLOYED="${DEPLOYED:-} $t"
    elif [ "$drc" = 3 ]; then
      rt_result "$t" FAIL reachable "нет связи"
      for c in $SKIP_CHECKS; do
        rt_result "$t" SKIP "$c" "нет связи"
      done
    else
      rt_result "$t" FAIL deploy "деплой завершился с кодом $drc"
      for c in $SKIP_CHECKS; do
        rt_result "$t" SKIP "$c" "деплой не выполнен"
      done
    fi
  done
fi
# Ожидаемая версия каждой развёрнутой цели (D-13, контракт сверки версии) — из результата этого
# деплоя ($RT_REPORT/version), через временный файл и mv -f. Её сверяет rt_version_guard (lib.sh) на
# границе пакета Go и спека, меняет только он же при принятом объявленном переходе; смоук ветвей
# читает текущее ожидание через rt_version_expected, проверка xcp-after-restore — версию деплоя ($EXPECT).
if [ -n "$EXPECT" ]; then
  for t in ${DEPLOYED:-}; do
    if printf '%s\n' "$EXPECT" >"$RT_REPORT/$t/expect-version.tmp"; then
      mv -f "$RT_REPORT/$t/expect-version.tmp" "$RT_REPORT/$t/expect-version"
    fi
  done
fi

# --- смоук параллельно ------------------------------------------------------------

# Полный набор на одной цели: preflight -> снимок -> ветви ядер -> восстановление + сверка.
# Ветвь ядра: переключение через интерфейс панели -> смоук -> Go-тесты -> Playwright.
# Работает в подпроцессе. Восстановление — в EXIT-trap, который ставится сразу после
# успешного снимка: оно выполняется при любом исходе, включая прерывание.
full_target() {
  _t=$1
  _snapped=0
  _orig=""
  # набор changed: этап go или pw пропускается, если выбор его не затронул
  _do_go=1
  _do_pw=1
  if { [ "$SUITE" = changed ] || [ "$SUITE" = harness ]; } && [ "${RT_SELECT_ALL:-1}" != 1 ]; then
    [ -n "${RT_GO_PKGS:-}" ] || _do_go=0
    [ -n "${RT_SPECS:-}" ] || _do_pw=0
  fi
  # Срок ожидания связи после обрыва (D-21): целое 60…7200, иначе 600 с.
  _link_wait=${RT_LINK_WAIT:-600}
  case "$_link_wait" in '' | *[!0-9]*) _link_wait=600 ;; esac
  if [ "$_link_wait" -lt 60 ] || [ "$_link_wait" -gt 7200 ]; then
    _link_wait=600
  fi
  # 1 — в ветви догон уже был (или набор rerun, где догона нет): второй обрыв не догоняется
  _cu_done=0
  # исход _stand_gate: none (обрыва не было), catchup (связь вернулась, догонять), nocatchup (связь
  # вернулась, догона нет), gone (связь не вернулась, несделанное записано в notrun.txt)
  _sg_state=none
  # Состав ветвей из файлов <ядро>.sel (rerun-plan.mjs): набор rerun берёт каталог своей цели,
  # иначе — RT_SEL_DIR из окружения; нет каталога — выбор набора, как раньше.
  _sel_dir=${RT_SEL_DIR:-}
  [ "$SUITE" != rerun ] || _sel_dir="$RT_REPORT/$_t/rerun-$RT_RERUN_N.sel.d"
  # набор delta: состав цели — упавшие в базе плюс выбор по диффу (delta-plan.mjs merge)
  [ "$SUITE" != delta ] || _sel_dir="$RT_REPORT/$_t/delta.sel.d"
  # Повторный сигнал (TERM группе плюс TERM, пересланный верхним run.sh, или второй Ctrl+C) не
  # должен обрывать восстановление: игнорирование наследуют rt_restore, _after_restore и ssh.
  _finish() {
    trap '' INT TERM HUP
    # прерванный подтверждающий перепрогон не должен утащить строки восстановления в confirm.tsv
    [ "$SUITE" = rerun ] || unset RT_RESULTS_NAME RT_ART_SUB
    # сэмплер PID ядра (если ветвь оборвалась между Go-тестами и частью state) — до восстановления
    _kpid_stop "$_t" "$_ks"
    _ks=""
    if [ "$_snapped" = 1 ]; then
      _snapped=0
      # строки восстановления и сверки несут исходное ядро
      RT_CORE=""
      [ -z "$_orig" ] || printf '%s\n' "$_orig" >"$RT_REPORT/$_t/core"
      rt_restore "$_t" || true
      _after_restore "$_t"
    fi
  }
  # Проверка после восстановления: xcp запущен и версия равна установленной деплоем,
  # активное ядро совпадает с исходным из снимка (core-restored, D-19).
  # Снимок включает /opt/sbin/xcp и возвращает бинарник деплоя, поэтому ожидание — версия деплоя
  # ($EXPECT), а не expect-version цели: принятый объявленный переход переписывает expect-version
  # на версию перехода, а восстановление её откатывает.
  _after_restore() {
    _rc=0
    _exp_ar=$EXPECT
    _out=$(rt_ssh "$1" "/opt/sbin/xcp -v; pidof xcp" 2>/dev/null) || _rc=$?
    _ver=$(printf '%s\n' "$_out" | sed -n '1p')
    _pid=$(printf '%s\n' "$_out" | sed -n '2p')
    if [ "$_rc" = 0 ] && [ -n "$_pid" ] && { [ -z "$_exp_ar" ] || [ "$_ver" = "$_exp_ar" ]; }; then
      rt_result "$1" PASS xcp-after-restore "xcp запущен, версия $_ver"
    else
      rt_result "$1" FAIL xcp-after-restore "после восстановления xcp не запущен или версия $_ver, ожидалась ${_exp_ar:--}"
    fi
    _rc=0
    _now=$(rt_ssh "$1" 'x=$(pidof xray | cut -d" " -f1); m=$(pidof mihomo | cut -d" " -f1); if [ -n "$x" ] && [ -n "$m" ]; then echo both; elif [ -n "$x" ]; then echo xray; elif [ -n "$m" ]; then echo mihomo; else echo none; fi' 2>/dev/null) || _rc=$?
    if [ -z "$_orig" ]; then
      rt_result "$1" FAIL core-restored "исходное ядро не найдено в журнале снимка"
    elif [ "$_rc" = 0 ] && [ "$_now" = "$_orig" ]; then
      rt_result "$1" PASS core-restored "активное ядро $_now равно исходному"
    else
      rt_result "$1" FAIL core-restored "после восстановления ядро ${_now:--}, исходное $_orig"
    fi
  }
  # Ожидание рабочего состояния стенда после разрушающих Go-тестов (остановка, запуск и
  # перезапуск XKeen, D-04): на медленном устройстве перехват и статус XKeen приходят в норму
  # не сразу, а Playwright должен начинать с рабочей панели. Результат — строка stand-ready.
  # Свои имена переменных (_sl_*): sh не имеет локальных, а rt_stage держит _t0 и _rc и
  # по ним пишет код этапа; общие имена портили бы код и обрывали подпроцесс цели под set -e.
  _settle() {
    _sl_t0=$(date +%s)
    _sl_rc=0
    RT_SSH_STDIN=1 rt_ssh "$1" "sh -s" <"$SCRIPT_DIR/settle.sh" >/dev/null 2>&1 || _sl_rc=$?
    _sl_dt=$(($(date +%s) - _sl_t0))
    if [ "$_sl_rc" = 0 ]; then
      rt_result "$1" PASS stand-ready "после Go-тестов ядро запущено и перехват на месте за $_sl_dt с"
    else
      rt_result "$1" FAIL stand-ready "после Go-тестов стенд не пришёл в рабочее состояние за $_sl_dt с (код $_sl_rc)"
    fi
  }
  # Сэмплер PID ядра на стенде (D-06, todo 01): замер перезапусков ядра на тест. Фоновый цикл на
  # устройстве раз в секунду берёт `pidof xray mihomo` и при смене значения дописывает строку
  # «<эпоха устройства>\t<PID через запятую или ->» в $RT_KPID. Идёт от Go-тестов ветви до конца её
  # части Playwright state: стартует после окна смоука и останавливается до смоука следующей ветви,
  # поэтому проверки процессов смоука (pidof ядра и xcp) его не видят; имя процесса — sh. Файл
  # забирается в <отчёт>/<цель>/<ядро>/kernel-pids.tsv, рядом clock.tsv — «<эпоха ПК>\t<эпоха
  # устройства>» в момент запуска: по ним branch-report.mjs переводит смены PID на часы ПК и
  # считает перезапуски в окне каждого теста. Нет связи — сэмплер не запускается, ветвь идёт как
  # раньше. _ks — PID цикла на устройстве (пусто — сэмплер не идёт).
  _ks=""
  _kpid_start() {
    RT_SSH_STDIN=1 rt_ssh "$1" "sh -s" 2>/dev/null <<EOF | sed -n 's/^kpid=//p' | tr -dc '0-9'
F=$RT_KPID
rm -f "\$F"
kpid_loop() {
  l=""
  while :; do
    p=\$(pidof xray mihomo | tr ' ' ',')
    [ -n "\$p" ] || p=-
    if [ "\$p" != "\$l" ]; then
      printf '%s\t%s\n' "\$(date +%s)" "\$p" >>"\$F"
      l=\$p
    fi
    sleep 1
  done
}
(
  trap '' HUP
  kpid_loop
) </dev/null >/dev/null 2>&1 &
echo "kpid=\$!"
EOF
  }
  # Остановка сэмплера: _kpid_stop <цель> <PID> [<каталог ветви>] — с каталогом файл забирается в
  # kernel-pids.tsv; без него (_term, _finish) только остановка и удаление файла, best effort.
  _kpid_stop() {
    [ -n "$2" ] || return 0
    if [ -n "${3:-}" ]; then
      rt_ssh "$1" "kill $2 2>/dev/null; cat $RT_KPID 2>/dev/null; rm -f $RT_KPID" >"$3/kernel-pids.tsv" 2>/dev/null || true
    else
      rt_ssh "$1" "kill $2 2>/dev/null; rm -f $RT_KPID" >/dev/null 2>&1 || true
    fi
  }
  # Этапы набора harness, добавленного к changed (RT_H_EXTRA=1, правка обвязки): Go-тесты блока
  # (go-harness-<ядро>) и ожидание стенда, затем спеки блока частями read (в окне наблюдателя) и
  # state (pw-harness-read-<ядро>, pw-harness-state-<ядро>) — в каждой ветви, без отбора @core.
  # Артефакты — в <ядро>/harness/, чтобы не затирать этапы выбора. В самом наборе harness блок
  # идёт основными этапами go, pw-read и pw. Вызываются из ветви, поэтому _t и _c видны.
  # Код 4 — после этапа сменилась версия стенда (_version_stop_check): вызывающий выходит из цикла.
  _harness_go() {
    [ "$RT_H_EXTRA" = 1 ] || return 0
    rt_stage "$_t" "go-harness-$_c" _bg env "RT_GO_PKGS=" "RT_GO_RUN=" "RT_GO_RUN_SEQ=$RT_H_GO_SEQ" "RT_ART_SUB=harness" sh "$SCRIPT_DIR/go-suite.sh" "$_t" "$_c" || true
    if _version_stop_check "go-harness-$_c" "$_brc" "$(_vs_rest h-go)"; then
      return 4
    fi
    rt_stage "$_t" "settle-harness-$_c" _bg _settle "$_t" || true
  }
  _harness_pw() {
    [ "$RT_H_EXTRA" = 1 ] || return 0
    rt_stage "$_t" "pw-harness-$1-$_c" _bg env "RT_SPECS=$RT_H_SPECS" "RT_PW_GREP=$RT_H_GREP" "RT_PW_GREP_INVERT=" "RT_PW_CORE_ONLY=" "RT_SPECS_FIRST=" "RT_ART_SUB=harness" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part "$1" || true
    if _version_stop_check "pw-harness-$1-$_c" "$_brc" "$(_vs_rest "h-$1")"; then
      return 4
    fi
  }
  # --- смена версии стенда (D-13, контракт сверки версии) -------------------------------------
  # Строку stop в <цель>/version-stop.tsv пишет rt_version_guard (lib.sh) на границе пакета Go или
  # спека, этап выходит с кодом 4; строки suspect и notrun внутри этапа пишут go-suite.sh и
  # pw-suite.sh. _vs_base — число строк stop до ветвей: перепрогон пишет в отчёт FROM, где может
  # остаться остановка исходного прогона, поэтому считаются только новые строки. _vstop=1 — цикл
  # ветвей цели остановлен сменой версии (следующие этапы, ветви и подтверждающий перепрогон не идут,
  # восстановление по снимку — в _finish, как при любом исходе).
  _vs_file="$RT_REPORT/$_t/version-stop.tsv"
  _vs_count() {
    if [ -f "$_vs_file" ]; then
      awk -F'\t' '$1 == "stop" {n++} END {print n + 0}' "$_vs_file"
    else
      echo 0
    fi
  }
  _vs_base=$(_vs_count)
  # эпоха старта цели: rt_version_guard не считает строку stop прежнего прогона (из отчёта FROM) уже
  # записанной остановкой текущего, иначе новая смена версии не даёт строки stop (счёт выше _vs_base)
  RT_VSTOP_SINCE=$(date +%s)
  export RT_VSTOP_SINCE
  _vstop=0
  # Что в ветви идёт после этапа: _vs_rest <switch|pw-read|h-read|go|pw|h-go|h-state> — по флагам
  # ветви (_b_go, _b_pw) и набору harness, добавленному к changed (RT_H_EXTRA); пусто — ничего.
  _vs_rest() {
    _vr=""
    case "$1" in switch) _vr="смоук" ;; esac
    case "$1" in switch) [ "$_b_pw" != 1 ] || _vr="$_vr, часть read Playwright" ;; esac
    case "$1" in switch | pw-read) [ "$RT_H_EXTRA" != 1 ] || _vr="$_vr, часть read набора harness" ;; esac
    case "$1" in switch | pw-read | h-read) [ "$_b_go" != 1 ] || _vr="$_vr, Go-тесты" ;; esac
    case "$1" in switch | pw-read | h-read | go) [ "$_b_pw" != 1 ] || _vr="$_vr, часть state Playwright" ;; esac
    case "$1" in switch | pw-read | h-read | go | pw) [ "$RT_H_EXTRA" != 1 ] || _vr="$_vr, Go-тесты набора harness" ;; esac
    case "$1" in switch | pw-read | h-read | go | pw | h-go) [ "$RT_H_EXTRA" != 1 ] || _vr="$_vr, часть state набора harness" ;; esac
    printf '%s' "${_vr#, }"
  }
  # _version_stop_check <этап> <код этапа> <невыполненное в ветви> [confirm] — сразу после этапа
  # ветви (с confirm — этапа подтверждающего перепрогона), раньше _stand_gate. Новая строка stop или
  # код этапа 4 без неё (тогда строки FAIL version-guard и stop пишутся здесь, граница «этап:<имя>») —
  # смена версии: строки notrun<TAB><ядро><TAB><что> для невыполненного в ветви, для каждой
  # оставшейся ветви и для подтверждающего перепрогона; _vstop=1, _stop=1, код 0 — вызывающий
  # выходит из цикла (break). Повторный вызов после остановки ничего не пишет и даёт код 0. Иначе код 1.
  _version_stop_check() {
    [ "$_vstop" = 0 ] || return 0
    if [ "$(_vs_count)" -le "$_vs_base" ]; then
      [ "$2" = 4 ] || return 1
      rt_result "$_t" FAIL version-guard "этап $1 вернул код 4 без строки stop"
      _vs_exp=$(rt_version_expected "$_t")
      printf 'stop\t%s\t%s\t%s\t%s\t%s\n' "${RT_CORE:--}" "этап:$1" "$_vs_exp" "неизвестно" "$(date +%s)" >>"$_vs_file"
    fi
    _vs_core=${RT_CORE:--}
    [ -z "$3" ] || printf 'notrun\t%s\t%s\n' "$_vs_core" "$3" >>"$_vs_file"
    if [ "${4:-}" != confirm ]; then
      for _vs_k in ${RT_CORES#*"$_c"}; do
        # ветвь без файла состава (перепрогон, добор) и так не пошла бы
        if [ -n "$_sel_dir" ] && [ ! -f "$_sel_dir/$_vs_k.sel" ]; then
          continue
        fi
        printf 'notrun\t%s\t%s\n' "$_vs_core" "ветвь $_vs_k: не выполнена" >>"$_vs_file"
      done
      [ "$SUITE" = rerun ] || printf 'notrun\t%s\t%s\n' "$_vs_core" "подтверждающий перепрогон: не выполнен" >>"$_vs_file"
    fi
    _vstop=1
    _stop=1
    echo "router-test: цель $_t: версия стенда сменилась (этап $1) — цикл ветвей остановлен, стенд восстанавливается по снимку" >&2
    return 0
  }
  _sp=""
  # имя и время начала этапа под _bg (см. _bg), время начала наблюдателя окна (шаг 2а)
  _bg_stage=""
  _bg_t0=""
  _ot0=""
  # PID фоновых этапов, идущих одновременно с основным (их запускает план параллельных
  # этапов); _term гасит и их
  _sps=""
  # Остановить этап целиком: он стартует отдельной группой процессов (setsid, PID этапа равен
  # номеру группы), поэтому TERM группе настигает и ssh, и node этапа. Без setsid группы нет —
  # тогда TERM только самому процессу. Запись `kill -TERM -- -N` dash не принимает, форма
  # `kill -s TERM -- -N` работает и в dash, и в bash.
  # Функция оболочки (_settle, rt_ssh) идёт в фоновом подпроцессе без своей группы: ему шлётся
  # TERM, а его дочерние процессы (ssh) собираются заранее и получают TERM отдельно, иначе после
  # смерти подпроцесса они осиротели бы.
  _stop_stage() {
    if kill -s TERM -- "-$1" 2>/dev/null; then
      return 0
    fi
    _st_kids=$(pgrep -P "$1" 2>/dev/null || true)
    kill "$1" 2>/dev/null || true
    for _st_k in $_st_kids; do
      kill -s TERM "$_st_k" 2>/dev/null || true
    done
  }
  # То же с KILL: для этапа, не завершившегося по TERM за отведённый срок.
  _kill_stage() {
    if kill -s KILL -- "-$1" 2>/dev/null; then
      return 0
    fi
    _st_kids=$(pgrep -P "$1" 2>/dev/null || true)
    kill -s KILL "$1" 2>/dev/null || true
    for _st_k in $_st_kids; do
      kill -s KILL "$_st_k" 2>/dev/null || true
    done
  }
  # сигнал: остановить текущий этап целиком и выйти — EXIT-trap восстановит стенд, когда
  # потомки этапа уже не меняют его. Повторный сигнал игнорируется (первая команда): он не
  # входит в _term заново. Оборванные этапы — основной под _bg и наблюдатель окна — пишутся в
  # timings.tsv строкой с кодом 130 в формате rt_stage (цель, этап, секунды, код, начало,
  # ядро): по ним summary.md видит, где оборвался прогон. Что шло, запоминается до остановки.
  _term() {
    trap '' INT TERM HUP
    _tm_stage=""
    _tm_obs=0
    [ -z "$_sp" ] || _tm_stage=$_bg_stage
    [ -z "$_sps" ] || _tm_obs=1
    for _p in $_sp $_sps; do
      _stop_stage "$_p"
    done
    # Ожидание этапов ограничено 60 с: этап, не реагирующий на TERM (ssh к недоступной цели),
    # иначе держал бы подпроцесс цели и блокировку неограниченно, а повторный сигнал игнорируется.
    # Сторож по истечении срока посылает KILL; игнорируемые сигналы он наследует, поэтому его
    # самого и его sleep гасит только KILL.
    (
      sleep 60
      for _p in $_sp $_sps; do
        _kill_stage "$_p"
      done
    ) &
    _tm_kw=$!
    for _p in $_sp $_sps; do
      wait "$_p" 2>/dev/null || true
    done
    pkill -KILL -P "$_tm_kw" 2>/dev/null || true
    kill -s KILL "$_tm_kw" 2>/dev/null || true
    _tm_now=$(date +%s)
    if [ -n "$_tm_stage" ]; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$_t" "$_tm_stage" "$((_tm_now - _bg_t0))" 130 "$_bg_t0" "${RT_CORE:--}" >>"$RT_REPORT/timings.tsv"
    fi
    if [ "$_tm_obs" = 1 ] && [ -n "$_ot0" ]; then
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$_t" "observe-$_c" "$((_tm_now - _ot0))" 130 "$_ot0" "${RT_CORE:--}" >>"$RT_REPORT/timings.tsv"
    fi
    _kpid_stop "$_t" "$_ks"
    _ks=""
    exit 130
  }
  # Этап ветви в фоне с ожиданием: код этапа попадает в _brc и возвращается как код функции
  # (вызовы под rt_stage дополняются `|| true`: run.sh идёт под set -e), сигнал прерывает
  # этап целиком. Вызывается только как `rt_stage … _bg …`: rt_stage до вызова кладёт имя
  # этапа в _stage, а время начала — в _t0, и _bg копирует их в свои _bg_stage и _bg_t0 (в sh
  # нет локальных переменных, а rt_stage при восстановлении перезапишет общие): по ним _term
  # пишет строку оборванного этапа. Свои имена — по той же причине, что и _sl_* у _settle.
  _bg() {
    _bg_stage=${_stage:-}
    _bg_t0=${_t0:-}
    # Функцию оболочки (_settle, rt_ssh) setsid запустить не может: она идёт в фоне как есть.
    case "$(type "$1" 2>&1)" in
      *"shell function"* | *"is a function"*) "$@" & ;;
      *)
        if command -v setsid >/dev/null 2>&1; then
          setsid "$@" &
        else
          "$@" &
        fi
        ;;
    esac
    _sp=$!
    _brc=0
    wait "$_sp" || _brc=$?
    _sp=""
    _bg_stage=""
    return "$_brc"
  }
  # --- потеря связи со стендом (класс stand, D-21, D-26) --------------------------------------
  # Строка в <цель>/notrun.txt: несделанное из-за обрыва.
  _notrun() {
    printf '%s\n' "$1" >>"$RT_REPORT/$_t/notrun.txt"
  }
  # Обрыв связи после этапа ветви: _stand_gate <этап> <код этапа> <что не выполнено, если связь не
  # вернётся>. Обрыв — код 3 либо ненулевой код этапа и молчащий ssh (Playwright кода 3 не даёт).
  # Ждёт связь до _link_wait; результат — _sg_state: none, catchup (первый обрыв ветви: догнать),
  # nocatchup (второй обрыв или набор rerun: связь вернулась, но сорванное не догоняется — оно в
  # notrun.txt) или gone (связь не вернулась: несделанное, остальные ветви и подтверждающий
  # перепрогон — в notrun.txt, вызывающий выходит из цикла ветвей). Каждый обрыв — строка в
  # link-loss.tsv (ядро, этап, исход).
  _stand_gate() {
    _sg_state=none
    if [ "$2" != 3 ]; then
      [ "$2" != 0 ] || return 0
      if rt_ssh "$_t" true >/dev/null 2>&1; then
        return 0
      fi
    fi
    if rt_wait_link "$_t" "$_link_wait"; then
      if [ "$_cu_done" = 0 ]; then
        _cu_done=1
        _sg_state=catchup
        printf '%s\t%s\t%s\n' "$_c" "$1" "догон" >>"$RT_REPORT/$_t/link-loss.tsv"
        # файл есть уже при пустом догоне: по нему видно, что связь возвращалась и догон был
        : >>"$RT_REPORT/$_t/catchup.tsv"
      else
        _sg_state=nocatchup
        printf '%s\t%s\t%s\n' "$_c" "$1" "без догона" >>"$RT_REPORT/$_t/link-loss.tsv"
        _notrun "$_c: этап $1 оборван (код $2), связь вернулась, сорванное не догоняется (второй обрыв в ветви или набор rerun): $3"
      fi
      return 0
    fi
    _sg_state=gone
    printf '%s\t%s\t%s\n' "$_c" "$1" "связь не вернулась" >>"$RT_REPORT/$_t/link-loss.tsv"
    _notrun "$_c: этап $1 оборван (код $2), связь не вернулась за $_link_wait с: $3"
    for _sg_k in ${RT_CORES#*"$_c"}; do
      _notrun "ветвь $_sg_k: не выполнена"
    done
    if [ "$SUITE" != rerun ]; then
      _notrun "подтверждающий перепрогон: не выполнен"
    fi
    return 0
  }
  # Что в Go-этапе ветви не доделано: записи RT_GO_RUN_SEQ (_gl_seq) или пакеты (_gl_pkgs), которых
  # нет в go-done.txt этапа (go-suite.sh пишет его на пакет, обработка которого закончилась без
  # обрыва). Нет файла — не доделано всё. Выбор ветви — _g_seq, _g_pkgs; без них — все пакеты с
  # роутерными тестами (тот же поиск, что в go-suite.sh).
  _go_left() {
    _gl_seq=""
    _gl_pkgs=""
    _gl_done="$_bdir/go-done.txt"
    if [ -n "$_g_seq" ]; then
      for _gl_e in $_g_seq; do
        grep -qxF -e "${_gl_e%%:*}" "$_gl_done" 2>/dev/null || _gl_seq="$_gl_seq $_gl_e"
      done
      _gl_seq=${_gl_seq# }
      return 0
    fi
    _gl_all=""
    if [ -n "$_g_pkgs" ]; then
      for _gl_p in $_g_pkgs; do
        _gl_all="$_gl_all ${_gl_p#./}"
      done
    else
      for _gl_f in $(find internal cmd -name '*_test.go' 2>/dev/null | sort); do
        if [ "$(head -n 1 "$_gl_f")" = "//go:build router" ]; then
          _gl_d=$(dirname "$_gl_f")
          case " $_gl_all " in
            *" $_gl_d "*) ;;
            *) _gl_all="$_gl_all $_gl_d" ;;
          esac
        fi
      done
    fi
    for _gl_p in $_gl_all; do
      grep -qxF -e "$_gl_p" "$_gl_done" 2>/dev/null || _gl_pkgs="$_gl_pkgs $_gl_p"
    done
    _gl_pkgs=${_gl_pkgs# }
  }
  # Догон части Playwright: _cu_pw <read|state> — та же часть набора ветви, строки в catchup.tsv.
  _cu_pw() {
    rt_stage "$_t" "catchup-pw-$_c" _bg env "RT_SPECS=$_p_specs" "RT_PW_GREP=$_p_grep" "RT_SPECS_FIRST=$2" "RT_PW_GREP_INVERT=${RT_PW_INVERT:-}" "RT_PW_CORE_ONLY=$_p_core" "RT_RESULTS_NAME=catchup.tsv" "RT_ART_SUB=catchup" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part "$1" || true
  }
  # Подтверждающий перепрогон новых падений (RTR-05, D-19). Идёт после цикла ветвей и до
  # восстановления стенда (его делает _finish в EXIT-ловушке), в той же блокировке и в тот же отчёт.
  # Вход — FAIL проверок go: и pw: без признаков потери связи (confirm-src.tsv). Решение принимается
  # по общему числу таких падений цели, одним сравнением с RT_CONFIRM_MAX: больше — перепрогона нет
  # ни на одном ядре (confirm-skip, строка в plan.txt), падения остаются new. Иначе для каждого
  # ядра с падениями (сначала то, что сейчас на стенде) rerun-plan.mjs --core <ядро> --max <остаток
  # потолка> собирает состав, ядро переключается так же, как в ветвях, и тесты идут этапами
  # confirm-go-<ядро> и confirm-pw-<ядро> со строками в confirm.tsv и артефактами в <ядро>/confirm/.
  # Прошедший тест — flaky, упавший снова — new (branch-report.mjs); retries основного прохода не
  # меняются, результат перепрогона не делает тест зелёным.
  _confirm_run() {
    [ -f "$RT_REPORT/$_t/results.tsv" ] || return 0
    awk -F'\t' -v re="$RT_STAND_RE" '$1 == "FAIL" && $3 ~ /^(go|pw):/ && $4 !~ re' "$RT_REPORT/$_t/results.tsv" >"$RT_REPORT/$_t/confirm-src.tsv"
    _cf_cd="$RT_REPORT/$_t/confirm-count.d"
    rm -rf "$_cf_cd"
    _cf_prc=0
    node "$SCRIPT_DIR/rerun-plan.mjs" "$RT_REPORT" "$_t" --results confirm-src.tsv --out-dir "$_cf_cd" >"$RT_REPORT/$_t/confirm-plan.txt" 2>&1 || _cf_prc=$?
    if [ "$_cf_prc" != 0 ]; then
      rm -rf "$_cf_cd"
      [ "$_cf_prc" = 1 ] || echo "router-test: цель $_t: rerun-plan.mjs не составил состав подтверждающего перепрогона (код $_cf_prc, confirm-plan.txt), падения остаются new" >&2
      return 0
    fi
    _cf_n=0
    for _cf_s in "$_cf_cd"/*.sel; do
      [ -f "$_cf_s" ] || continue
      _cf_v=$(rt_sel_val "$_cf_s" COUNT | tr -dc '0-9')
      _cf_n=$((_cf_n + ${_cf_v:-0}))
    done
    rm -rf "$_cf_cd"
    printf '%s\n' "$_cf_n" >"$RT_REPORT/$_t/confirm-count"
    if [ "$_cf_n" -gt "$RT_CONFIRM_MAX" ]; then
      printf '%s\n' "без перепрогона: падений больше $RT_CONFIRM_MAX" >"$RT_REPORT/$_t/confirm-skip"
      echo "- Цель $_t: подтверждающий перепрогон пропущен: новых падений $_cf_n больше $RT_CONFIRM_MAX — общая причина" >>"$RT_REPORT/plan.txt"
      echo "router-test: цель $_t: подтверждающий перепрогон пропущен: новых падений $_cf_n больше $RT_CONFIRM_MAX — общая причина" >&2
      return 0
    fi
    _cf_left=$RT_CONFIRM_MAX
    _cf_order=""
    [ -z "$_cur" ] || _cf_order=$_cur
    for _cf_k in xray mihomo; do
      [ "$_cf_k" = "$_cur" ] || _cf_order="$_cf_order $_cf_k"
    done
    _cf_sd="$RT_REPORT/$_t/confirm.sel.d"
    rm -rf "$_cf_sd"
    # строки результатов и артефакты перепрогона — отдельно от основного прохода (в том числе
    # stand-ready после Go-тестов перепрогона)
    RT_RESULTS_NAME=confirm.tsv
    RT_ART_SUB=confirm
    export RT_RESULTS_NAME RT_ART_SUB
    for _cf_k in $_cf_order; do
      [ "$_cf_left" -gt 0 ] || break
      _cf_prc=0
      node "$SCRIPT_DIR/rerun-plan.mjs" "$RT_REPORT" "$_t" --core "$_cf_k" --max "$_cf_left" --results confirm-src.tsv --out-dir "$_cf_sd" >>"$RT_REPORT/$_t/confirm-plan.txt" 2>&1 || _cf_prc=$?
      if [ "$_cf_prc" != 0 ] || [ ! -f "$_cf_sd/$_cf_k.sel" ]; then
        continue
      fi
      _cf_f="$_cf_sd/$_cf_k.sel"
      _cf_seq=$(rt_sel_val "$_cf_f" GO_SEQ)
      _cf_specs=$(rt_sel_val "$_cf_f" SPECS)
      _cf_grep=$(rt_sel_val "$_cf_f" GREP)
      _cf_cnt=$(rt_sel_val "$_cf_f" COUNT)
      if ! rt_sel_ok GO_SEQ "$_cf_seq" || ! rt_sel_ok SPECS "$_cf_specs" || ! rt_sel_ok COUNT "$_cf_cnt"; then
        echo "router-test: цель $_t: файл состава $_cf_k.sel подтверждающего перепрогона не прошёл проверку, ядро пропущено" >&2
        continue
      fi
      _cf_left=$((_cf_left - _cf_cnt))
      RT_CORE=$_cf_k
      export RT_CORE
      mkdir -p "$RT_REPORT/$_t/$_cf_k/confirm"
      if [ "$_cur" != "$_cf_k" ]; then
        rt_stage "$_t" "confirm-switch-$_cf_k" _bg sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_cf_k" --switch "$_cf_k" || true
        # смена версии стенда (D-13): остаток перепрогона не идёт, его падения остаются new
        if _version_stop_check "confirm-switch-$_cf_k" "$_brc" "подтверждающий перепрогон: ядро $_cf_k и следующие не выполнены" confirm; then
          break
        fi
        if [ "$_brc" != 0 ]; then
          echo "router-test: цель $_t: переключение на $_cf_k для подтверждающего перепрогона не удалось" >&2
          _cur=""
          continue
        fi
        _cur=$_cf_k
        printf '%s\n' "$_cf_k" >"$RT_REPORT/$_t/core"
      fi
      if [ -n "$_cf_seq" ]; then
        rt_stage "$_t" "confirm-go-$_cf_k" _bg env "RT_GO_PKGS=" "RT_GO_RUN=" "RT_GO_RUN_SEQ=$_cf_seq" sh "$SCRIPT_DIR/go-suite.sh" "$_t" "$_cf_k" || true
        if _version_stop_check "confirm-go-$_cf_k" "$_brc" "подтверждающий перепрогон: спеки ядра $_cf_k и следующие ядра не выполнены" confirm; then
          break
        fi
        rt_stage "$_t" "confirm-settle-$_cf_k" _bg _settle "$_t" || true
      fi
      if [ -n "$_cf_specs" ]; then
        rt_stage "$_t" "confirm-pw-$_cf_k" _bg env "RT_SPECS=$_cf_specs" "RT_PW_GREP=$_cf_grep" "RT_PW_GREP_INVERT=" "RT_PW_CORE_ONLY=" "RT_SPECS_FIRST=" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_cf_k" || true
        if _version_stop_check "confirm-pw-$_cf_k" "$_brc" "подтверждающий перепрогон: следующие ядра не выполнены" confirm; then
          break
        fi
      fi
    done
    unset RT_RESULTS_NAME RT_ART_SUB
    RT_CORE=""
  }
  trap '_finish' EXIT
  trap '_term' INT TERM HUP
  # Флаг ставится до снимка: сигнал, пришедший сразу после возврата ssh снимка, обрабатывается
  # ещё до следующей строки, и без флага восстановление не запустилось бы. Снимок не снят —
  # флаг сбрасывается, восстанавливать нечего.
  _snapped=1
  rt_snapshot "$_t" || {
    _snapped=0
    exit 1
  }
  # Исходное ядро — из журнала снимка (строка core=…): к нему вернёт восстановление.
  _orig=$(sed -n 's/^core=//p' "$RT_REPORT/$_t/snapshot.log" | tail -n 1)

  # Сокращённые сроки (D-22): блок xcp-rt пишется только после снимка — восстановление возвращает
  # xcp.env без него. Сбой — строка FAIL rt-env-write, ветви идут дальше: тесты читают действующий
  # срок у процесса панели. Записанный блок ждёт смоук каждой ветви (RT_EXPECT_RT_ENV=1).
  _rtenv_ok=0
  if [ "${RT_RTENV:-0}" = 1 ]; then
    _rw=0
    rt_stage "$_t" rt-env-write rt_rtenv_write "$_t" >"$RT_REPORT/$_t/rtenv.log" 2>&1 || _rw=$?
    if [ "$_rw" = 0 ]; then
      _rtenv_ok=1
    elif [ "$_rw" = 3 ]; then
      rt_result "$_t" FAIL rt-env-write "нет связи, блок сокращённых сроков не записан"
    else
      rt_result "$_t" FAIL rt-env-write "блок сокращённых сроков не записан или панель не поднялась после перезапуска (код $_rw, rtenv.log)"
    fi
  fi

  _first=1
  # ядро, которое сейчас на стенде (после последнего успешного переключения): с него начинается
  # подтверждающий перепрогон
  _cur=""
  # 1 — цикл ветвей прерван: связь со стендом не вернулась или сменилась версия стенда (_vstop=1);
  # подтверждающего перепрогона нет
  _stop=0
  for _c in $RT_CORES; do
    # Состав из <ядро>.sel: у ветви без файла тестов нет — она пропускается целиком
    if [ -n "$_sel_dir" ] && [ ! -f "$_sel_dir/$_c.sel" ]; then
      continue
    fi
    # Ядро ветви: колонка results.tsv и файл core (селекторы меток arch/core)
    RT_CORE=$_c
    export RT_CORE
    printf '%s\n' "$_c" >"$RT_REPORT/$_t/core"
    mkdir -p "$RT_REPORT/$_t/$_c"
    # Каталог ветви для файлов сэмплера и хвоста лога: перепрогон — внутри <ядро>/rerun-N/,
    # чтобы не затирать файлы исходного отчёта
    _bdir="$RT_REPORT/$_t/$_c"
    [ "$SUITE" != rerun ] || _bdir="$_bdir/rerun-$RT_RERUN_N"
    mkdir -p "$_bdir"

    # Тесты ветви. Гибрид (changed): в ветви второго ядра идут только тесты с меткой ядра
    # (Go — RT_GO_CORE_PKGS по RT_GO_CORE_RUN, Playwright — RT_SPECS_CORE с отбором @core);
    # пустой список — этап пропускается. Значения этапам передаёт env: _bg запускает этап в
    # фоне отдельной группой процессов. Пустые значения этапы читают как «по умолчанию».
    # догон после обрыва — один на ветвь; в наборе rerun его нет
    _cu_done=0
    [ "$SUITE" != rerun ] || _cu_done=1
    _b_go=$_do_go
    _b_pw=$_do_pw
    _b_skip="роутерные Go-пакеты набора changed не затронуты"
    _b_skip_pw="роутерные спеки набора changed не затронуты"
    _g_pkgs=${RT_GO_PKGS:-}
    _g_run=${RT_GO_RUN:-}
    _g_seq=${RT_GO_SEQ:-}
    _p_specs=${RT_SPECS:-}
    _p_first=${RT_SPECS_FIRST:-}
    _p_core=""
    if [ "${RT_HYBRID:-0}" = 1 ] && [ "$_c" = "$RT_B2" ]; then
      _g_pkgs=$RT_GO_CORE_PKGS
      _g_run=$RT_GO_CORE_RUN
      _g_seq=""
      _p_specs=$RT_SPECS_CORE
      _p_first=""
      _p_core=1
      _b_go=1
      _b_pw=1
      [ -n "$RT_GO_CORE_PKGS" ] || _b_go=0
      [ -n "$RT_SPECS_CORE" ] || _b_pw=0
      _b_skip="во второй ветви нет тестов с меткой ядра"
      _b_skip_pw="во второй ветви нет тестов с меткой ядра"
    fi

    # Состав ветви из <ядро>.sel: Go по именам (GO_SEQ), спеки (SPECS) и --grep (GREP) заменяют
    # выбор набора; значения проверяются по шаблонам, не прошло — ветвь пропускается с FAIL rerun:sel.
    _p_grep=${RT_PW_GREP:-}
    if [ -n "$_sel_dir" ]; then
      _sel_f="$_sel_dir/$_c.sel"
      _sel_seq=$(rt_sel_val "$_sel_f" GO_SEQ)
      _sel_specs=$(rt_sel_val "$_sel_f" SPECS)
      _sel_grep=$(rt_sel_val "$_sel_f" GREP)
      _sel_cnt=$(rt_sel_val "$_sel_f" COUNT)
      if ! rt_sel_ok GO_SEQ "$_sel_seq" || ! rt_sel_ok SPECS "$_sel_specs" || ! rt_sel_ok COUNT "$_sel_cnt"; then
        rt_result "$_t" FAIL rerun:sel "файл состава $_c.sel не прошёл проверку, ветвь пропущена"
        continue
      fi
      _g_pkgs=""
      _g_run=""
      _g_seq=$_sel_seq
      _p_specs=$_sel_specs
      _p_first=""
      _p_core=""
      # CORE_ONLY=1 (набор delta, вторая ветвь): из спеков идут только тесты с меткой ядра
      [ "$(rt_sel_val "$_sel_f" CORE_ONLY)" != 1 ] || _p_core=1
      _p_grep=$_sel_grep
      _b_go=0
      [ -z "$_sel_seq" ] || _b_go=1
      _b_pw=0
      [ -z "$_sel_specs" ] || _b_pw=1
      _b_skip="в составе (перепрогон, добор) нет Go-тестов этого ядра"
      _b_skip_pw="в составе (перепрогон, добор) нет спеков этого ядра"
    fi

    # Выбор Go по именам (RT_GO_SEQ, первая ветвь): порядок и состав задаёт список, пакеты и
    # выражение отбора go-suite.sh не нужны. Пустой список — пакеты целиком, как раньше.
    if [ -n "$_g_seq" ]; then
      _g_pkgs=""
      _g_run=""
    fi

    # Номер последней строки xcp.log до начала ветви: смоук смотрит только строки после неё,
    # поэтому ошибки, вызванные разрушающими тестами предыдущей ветви (D-04), не
    # засчитываются этой ветви. В первой ветви смоук смотрит от баннера запуска.
    # Вместе с номером строки запоминается inode файла: при размере 1 МБ xcp.log
    # ротируется (RotateWriter: xcp.log -> xcp.log.1 -> xcp.log.2), и без inode номер
    # строки указывал бы уже на другой файл. Смоук по inode находит файл ветви среди
    # xcp.log, .1 и .2 и берёт хвост после запомненной строки (WR-01). Номер inode
    # освобождается после третьей ротации и может достаться новому xcp.log, поэтому
    # запоминается ещё и md5 первой строки: найденный файл с другой первой строкой — не файл ветви.
    _from=""
    _ino=""
    _fp=""
    if [ "$_first" = 0 ]; then
      _lraw=$(rt_ssh "$_t" 'L=/opt/var/log/xcp.log; echo "xcp_lines=$(wc -l <$L)"; set -- $(ls -i $L); echo "xcp_ino=$1"; echo "xcp_fp=$(head -n 1 $L | md5sum | cut -c1-32)"' 2>/dev/null) || _lraw=""
      _from=$(printf '%s\n' "$_lraw" | sed -n 's/^xcp_lines=//p' | tr -dc '0-9')
      _ino=$(printf '%s\n' "$_lraw" | sed -n 's/^xcp_ino=//p' | tr -dc '0-9')
      _fp=$(printf '%s\n' "$_lraw" | sed -n 's/^xcp_fp=//p' | tr -dc '0-9a-f')
    fi
    _first=0

    # 0. Сброс записи на носитель до начала ветви. Установка бинарника, снимок стенда и
    # разрушающие тесты прошлой ветви оставляют в кэше страниц десятки МБ; ядро
    # сбрасывает их примерно через 30 с, и на флешке стенда это застой носителя на
    # десятки секунд — смоук ветви попадал в него: ssh без баннера, выход из панели
    # 36–41 с (гейты 20261020-203115-fdaccf9d, 20261020-214913-b9421f42). sync сбрасывает
    # всё сейчас, до переключения ядра; нет связи — смоук ветви это и покажет.
    rt_stage "$_t" "sync-$_c" _bg rt_ssh "$_t" sync >/dev/null 2>&1 || true

    # 1. переключение ядра интерфейсом панели
    rt_stage "$_t" "switch-$_c" _bg sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --switch "$_c" || true
    _src=$_brc
    # смена версии стенда на границе pw:core-switch.spec.ts (D-13): цикл ветвей останавливается
    if _version_stop_check "switch-$_c" "$_src" "$(_vs_rest switch)"; then
      break
    fi
    if [ "$_src" != 0 ]; then
      for _s in smoke go pw; do
        rt_result "$_t" SKIP "$_s" "переключение на $_c не удалось"
      done
      continue
    fi
    _cur=$_c

    # 2. смоук без окна (только чтение; строки лога — после начала ветви). Ключи --from-* общие
    # для проверок и наблюдателя: числа и шестнадцатеричный отпечаток, кавычки не нужны.
    _fargs=""
    if [ -n "$_from" ] && [ -n "$_ino" ] && [ -n "$_fp" ]; then
      _fargs="--from-line $_from --from-inode $_ino --from-fp $_fp"
    elif [ -n "$_from" ] && [ -n "$_ino" ]; then
      _fargs="--from-line $_from --from-inode $_ino"
    elif [ -n "$_from" ]; then
      _fargs="--from-line $_from"
    fi
    # Ожидаемая версия — текущее ожидание цели (D-13), а не увиденная: принятый объявленный переход
    # прошлой ветви его меняет, посторонняя установка — нет.
    # rt_version_expected всегда возвращает 0: пустой вывод проверяется явно
    _exp=$(rt_version_expected "$_t")
    [ -n "$_exp" ] || _exp=$EXPECT
    # Проверка rt-env ждёт блок, если он записан после снимка (RT_EXPECT_RT_ENV=1), иначе его отсутствия.
    # shellcheck disable=SC2086
    rt_stage "$_t" "smoke-$_c" _bg env "RT_EXPECT_RT_ENV=$_rtenv_ok" sh "$SCRIPT_DIR/smoke.sh" "$_t" "$_exp" --no-observe $_fargs || true
    _smrc=$_brc
    # смоук записывает в core определённое им ядро: ветвь продолжается с ядром ветви
    printf '%s\n' "$_c" >"$RT_REPORT/$_t/core"

    _pwr_rc=0
    # 2а. окно наблюдения (smoke.sh --observe-only: smoke-settle 30 с, окно RT_SMOKE_WINDOW, вердикт
    # kernel-restart и правила логов) фоном параллельно с частью Playwright только чтения
    # (D-07): окно перекрыто обходом страниц, а не простоем. Параллельно окну идут только спеки
    # только чтения; Go-тесты и изменяющие спеки стартуют после ожидания наблюдателя.
    # Цель недоступна (код 3) — наблюдать нечего: смоук уже записал SKIP по этим проверкам.
    if [ "$_smrc" != 3 ]; then
      _ot0=$(date +%s)
      if command -v setsid >/dev/null 2>&1; then
        # shellcheck disable=SC2086
        setsid sh "$SCRIPT_DIR/smoke.sh" "$_t" "$_exp" --observe-only $_fargs &
      else
        # shellcheck disable=SC2086
        sh "$SCRIPT_DIR/smoke.sh" "$_t" "$_exp" --observe-only $_fargs &
      fi
      _osp=$!
      _sps="$_sps $_osp"
      # смена версии в части read (D-13) отмечается сразу (_vstop), а из цикла ветвь выходит после
      # ожидания наблюдателя окна: он идёт фоном и не должен остаться без ожидания
      if [ "$_b_pw" = 1 ]; then
        rt_stage "$_t" "pw-read-$_c" _bg env "RT_SPECS=$_p_specs" "RT_PW_GREP=$_p_grep" "RT_PW_GREP_INVERT=${RT_PW_INVERT:-}" "RT_PW_CORE_ONLY=$_p_core" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part read || true
        _pwr_rc=$_brc
        _version_stop_check "pw-read-$_c" "$_pwr_rc" "$(_vs_rest pw-read)" || true
      fi
      [ "$_vstop" = 1 ] || _harness_pw read || true
      _orc=0
      wait "$_osp" || _orc=$?
      _sps=""
      printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$_t" "observe-$_c" "$(($(date +%s) - _ot0))" "$_orc" "$_ot0" "$_c" >>"$RT_REPORT/timings.tsv"
    else
      if [ "$_b_pw" = 1 ]; then
        rt_stage "$_t" "pw-read-$_c" _bg env "RT_SPECS=$_p_specs" "RT_PW_GREP=$_p_grep" "RT_PW_GREP_INVERT=${RT_PW_INVERT:-}" "RT_PW_CORE_ONLY=$_p_core" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part read || true
        _pwr_rc=$_brc
        _version_stop_check "pw-read-$_c" "$_pwr_rc" "$(_vs_rest pw-read)" || true
      fi
      [ "$_vstop" = 1 ] || _harness_pw read || true
    fi
    if [ "$_vstop" = 1 ]; then
      break
    fi
    # 2а'. обрыв связи в части read: ждём связь, один раз догоняем часть (D-21)
    if [ "$_b_pw" = 1 ]; then
      _stand_gate "pw-read-$_c" "$_pwr_rc" "часть read Playwright, Go-тесты, часть state Playwright"
      if [ "$_sg_state" = gone ]; then
        _stop=1
        break
      fi
      if [ "$_sg_state" = catchup ]; then
        _cu_pw read ""
        if _version_stop_check "catchup-pw-$_c" "$_brc" "$(_vs_rest pw-read)"; then
          break
        fi
        _stand_gate "catchup-pw-$_c" "$_brc" "часть read Playwright (догон), Go-тесты, часть state Playwright"
        if [ "$_sg_state" = gone ]; then
          _stop=1
          break
        fi
      fi
    fi
    # хвост xcp.log пишет наблюдатель (без него — смоук без окна не пишет)
    [ ! -f "$RT_REPORT/$_t/xcp-log-tail.txt" ] || mv "$RT_REPORT/$_t/xcp-log-tail.txt" "$_bdir/xcp-log-tail.txt"

    # 2б. сэмплер PID ядра (см. _kpid_start) и сверка часов ПК и устройства: clock.tsv — середина
    # интервала вызова ssh на ПК и эпоха устройства. Нет связи (смоук вернул 3) — не запускается.
    if [ "$_smrc" != 3 ]; then
      _ks=$(_kpid_start "$_t") || _ks=""
      if [ -n "$_ks" ]; then
        _kc0=$(date +%s)
        _kdev=$(rt_ssh "$_t" 'date +%s' 2>/dev/null | tr -dc '0-9') || _kdev=""
        _kc1=$(date +%s)
        [ -z "$_kdev" ] || printf '%s\t%s\n' "$(((_kc0 + _kc1) / 2))" "$_kdev" >"$_bdir/clock.tsv"
      fi
    fi

    # 3. Go-тесты на устройстве с этим ядром
    if [ "$_b_go" = 1 ]; then
      rt_stage "$_t" "go-$_c" _bg env "RT_GO_PKGS=$_g_pkgs" "RT_GO_RUN=$_g_run" "RT_GO_RUN_SEQ=$_g_seq" sh "$SCRIPT_DIR/go-suite.sh" "$_t" "$_c" || true
      # обрыв связи в Go-этапе: ждём связь, один раз догоняем пакеты вне go-done.txt (D-21)
      _gorc=$_brc
      # смена версии стенда (D-13): оставшиеся группы Go go-suite.sh уже записал строками notrun
      if _version_stop_check "go-$_c" "$_gorc" "$(_vs_rest go)"; then
        break
      fi
      _go_left
      _stand_gate "go-$_c" "$_gorc" "Go: ${_gl_seq:-${_gl_pkgs:--}}, часть state Playwright"
      if [ "$_sg_state" = gone ]; then
        _stop=1
        break
      fi
      if [ "$_sg_state" = catchup ] && { [ -n "$_gl_seq" ] || [ -n "$_gl_pkgs" ]; }; then
        rt_stage "$_t" "catchup-go-$_c" _bg env "RT_GO_PKGS=$_gl_pkgs" "RT_GO_RUN=$_g_run" "RT_GO_RUN_SEQ=$_gl_seq" "RT_RESULTS_NAME=catchup.tsv" "RT_ART_SUB=catchup" sh "$SCRIPT_DIR/go-suite.sh" "$_t" "$_c" || true
        if _version_stop_check "catchup-go-$_c" "$_brc" "$(_vs_rest go)"; then
          break
        fi
        _stand_gate "catchup-go-$_c" "$_brc" "Go (догон): ${_gl_seq:-$_gl_pkgs}, часть state Playwright"
        if [ "$_sg_state" = gone ]; then
          _stop=1
          break
        fi
      fi

      # 3а. стенд после разрушающих тестов должен прийти в рабочее состояние
      rt_stage "$_t" "settle-$_c" _bg _settle "$_t" || true
    else
      rt_result "$_t" SKIP go "$_b_skip"
    fi

    # 4. Playwright, часть state: спеки, меняющие состояние стенда, один воркер. Часть только
    # чтения прошла в шаге 2а
    if [ "$_b_pw" = 1 ]; then
      rt_stage "$_t" "pw-$_c" _bg env "RT_SPECS=$_p_specs" "RT_PW_GREP=$_p_grep" "RT_SPECS_FIRST=$_p_first" "RT_PW_GREP_INVERT=${RT_PW_INVERT:-}" "RT_PW_CORE_ONLY=$_p_core" sh "$SCRIPT_DIR/pw-suite.sh" "$_t" "$_c" --part state || true
      # смена версии стенда (D-13): оставшиеся спеки pw-suite.sh уже записал строками notrun
      if _version_stop_check "pw-$_c" "$_brc" "$(_vs_rest pw)"; then
        break
      fi
      # обрыв связи в части state: ждём связь, один раз догоняем часть (D-21)
      _stand_gate "pw-$_c" "$_brc" "часть state Playwright"
      if [ "$_sg_state" = gone ]; then
        _stop=1
        break
      fi
      if [ "$_sg_state" = catchup ]; then
        _cu_pw state "$_p_first"
        if _version_stop_check "catchup-pw-$_c" "$_brc" "$(_vs_rest pw)"; then
          break
        fi
        _stand_gate "catchup-pw-$_c" "$_brc" "часть state Playwright (догон)"
        if [ "$_sg_state" = gone ]; then
          _stop=1
          break
        fi
      fi
    else
      rt_result "$_t" SKIP pw "$_b_skip_pw"
    fi

    # 4б. набор harness, добавленный к выбору changed (RT_H_EXTRA=1): Go, ожидание стенда, state;
    # код 4 — смена версии стенда после этапа (D-13)
    _harness_go || break
    _harness_pw state || break

    # 4а. остановка сэмплера и забор смен PID ядра ветви: до смоука следующей ветви
    _kpid_stop "$_t" "$_ks" "$_bdir"
    _ks=""
  done
  RT_CORE=""
  # подтверждающий перепрогон новых падений (D-19): до восстановления стенда, кроме набора rerun
  if [ "$SUITE" != rerun ] && [ "$_stop" = 0 ]; then
    _confirm_run
  fi
  exit 0
}

# --- набор reboot (D-24) -------------------------------------------------------------
# Предполёт и снимок идут параллельно (D-23), перезагрузка — строго по одной цели в порядке
# XCP_REBOOT_ORDER: перезагрузка вышестоящей цели каскада обрывает связь ПК и upstream
# нижестоящей, параллельная перезагрузка дала бы ложные ошибки в xcp.log и недетерминированный
# смоук (D-16). Проверки цели завершаются до перезагрузки следующей.

RB_CHECKS="reboot-back reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify reboot-reachable-end"
TAB=$(printf '\t')

# Метка известного падения проверки перезагрузки: запись `reboot:<проверка>` в known-failures
# (проверка — точно или по префиксу, селектор — по архитектуре и ядру цели). Печатает slug.
rb_label() {
  rb_arch=$(rt_get "$1" ARCH)
  rb_core=-
  [ ! -f "$RT_REPORT/$1/core" ] || rb_core=$(cat "$RT_REPORT/$1/core")
  while IFS="$TAB" read -r rb_lc rb_ls rb_lslug || [ -n "$rb_lc" ]; do
    case "$rb_lc" in reboot:?*) ;; *) continue ;; esac
    rb_p=${rb_lc#reboot:}
    case "$2" in "$rb_p"*) ;; *) continue ;; esac
    rt_selector_match "$rb_ls" "$rb_arch" "$rb_core" || continue
    printf '%s\n' "$rb_lslug"
    return 0
  done <"$SCRIPT_DIR/known-failures"
  return 1
}

# Строка результата: rb_res <цель> <PASS|FAIL|SKIP> <проверка> <деталь>; FAIL с применимой
# меткой становится KNOWN со slug.
rb_res() {
  if [ "$2" = FAIL ]; then
    rb_slug=$(rb_label "$1" "$3") || rb_slug=""
    if [ -n "$rb_slug" ]; then
      rt_result "$1" KNOWN "$3" "$4 [известное падение: $rb_slug]"
      return 0
    fi
  fi
  rt_result "$1" "$2" "$3" "$4"
}

# Остальные проверки цели пропущены: rb_skip <цель> <причина> <проверки…>
rb_skip() {
  rb_sk_t=$1
  rb_sk_why=$2
  shift 2
  for rb_sk_c in "$@"; do
    rt_result "$rb_sk_t" SKIP "$rb_sk_c" "$rb_sk_why"
  done
}

# Снимок на устройстве больше не нужен.
rb_cleanup() {
  rt_ssh "$1" "sh $RT_SNAP_DIR/snapshot.sh cleanup; rm -rf $RT_SNAP_DIR" >/dev/null 2>&1 || true
}

# Состояние автозапуска на устройстве (reboot-state.sh); печатает строки ключ=значение.
# Сразу после холодного старта сеть может моргнуть, поэтому до трёх попыток; вывод считается
# полным, если дошёл до последней строки (nat=). Ошибки ssh — в <отчёт>/<цель>/reboot-state.err.
rb_state() {
  rb_tries=0
  while [ "$rb_tries" -lt 3 ]; do
    rb_out=$(RT_SSH_STDIN=1 rt_ssh "$1" "sh -s${2:+ $2}" <"$SCRIPT_DIR/reboot-state.sh" 2>>"$RT_REPORT/$1/reboot-state.err") || rb_out=""
    if printf '%s\n' "$rb_out" | grep -q '^nat='; then
      printf '%s\n' "$rb_out"
      return 0
    fi
    rb_tries=$((rb_tries + 1))
    sleep 10
  done
  return 1
}

# Ожидание рабочего XKeen после холодного старта (settle.sh), время — в timings.tsv.
rb_settle() {
  rb_s0=$(date +%s)
  RT_SSH_STDIN=1 rt_ssh "$1" "sh -s" <"$SCRIPT_DIR/settle.sh" >/dev/null 2>&1 || true
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$1" reboot-settle "$(($(date +%s) - rb_s0))" 0 "$rb_s0" - >>"$RT_REPORT/timings.tsv"
}

# Полный цикл одной цели: состояние до -> перезагрузка -> возврат -> состояние после ->
# смоук -> сверка со снимком.
rb_one() {
  rb_t=$1
  rb_b=$(rb_state "$rb_t") || rb_b=""
  printf '%s\n' "$rb_b" >"$RT_REPORT/$rb_t/reboot-state-before.txt"
  rb_bid=$(printf '%s\n' "$rb_b" | rt_kv boot_id)
  rb_bxcp=$(printf '%s\n' "$rb_b" | rt_kv xcp)
  rb_bxk=$(printf '%s\n' "$rb_b" | rt_kv xkeen)
  rb_bcore=$(printf '%s\n' "$rb_b" | rt_kv core)
  rb_bm=$(printf '%s\n' "$rb_b" | rt_kv mangle)
  rb_bn=$(printf '%s\n' "$rb_b" | rt_kv nat)
  rb_start=$(date -u +%Y-%m-%dT%H:%M:%SZ)
  if [ -z "$rb_bid" ] || [ -z "$rb_bxcp" ]; then
    rb_res "$rb_t" FAIL reboot-back "не удалось снять состояние до перезагрузки"
    rb_skip "$rb_t" "состояние до перезагрузки не снято" reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify
    rb_cleanup "$rb_t"
    printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" - "$rb_start" >>"$RT_REPORT/reboot.tsv"
    return 0
  fi
  printf '%s\n' "$rb_bcore" >"$RT_REPORT/$rb_t/core"

  # Команда перезагрузки KeeneticOS из Entware; соединение обрывается — это ожидаемо (код 3)
  rb_rc=0
  rt_ssh "$rb_t" "ndmc -c 'system reboot'" >/dev/null 2>&1 || rb_rc=$?
  if [ "$rb_rc" != 0 ] && [ "$rb_rc" != 3 ]; then
    rb_res "$rb_t" FAIL reboot-back "команда перезагрузки не принята (код $rb_rc)"
    rb_skip "$rb_t" "перезагрузка не выполнена" reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify
    rb_cleanup "$rb_t"
    printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" - "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$RT_REPORT/reboot.tsv"
    return 0
  fi

  rb_rc=0
  rt_stage "$rb_t" reboot-wait rt_wait_back "$rb_t" "$rb_bid" || rb_rc=$?
  rb_back=$RT_BACK_SECS
  if [ "$rb_rc" != 0 ]; then
    rb_res "$rb_t" FAIL reboot-back "нет связи после перезагрузки: за $rb_back с цель не вернулась (снимок остался на устройстве)"
    rb_skip "$rb_t" "нет связи после перезагрузки" reboot-xcp reboot-xkeen reboot-core reboot-iptables reboot-verify
    printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" "$rb_back" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$RT_REPORT/reboot.tsv"
    return 0
  fi
  rb_res "$rb_t" PASS reboot-back "вернулась за $rb_back с, загрузка новая"

  [ "$rb_bxk" != 1 ] || rb_settle "$rb_t"
  rb_a=$(rb_state "$rb_t" stable) || rb_a=""
  printf '%s\n' "$rb_a" >"$RT_REPORT/$rb_t/reboot-state-after.txt"
  rb_axcp=$(printf '%s\n' "$rb_a" | rt_kv xcp)
  rb_apid=$(printf '%s\n' "$rb_a" | rt_kv xcp_pid)
  rb_axk=$(printf '%s\n' "$rb_a" | rt_kv xkeen)
  rb_acore=$(printf '%s\n' "$rb_a" | rt_kv core)
  rb_am=$(printf '%s\n' "$rb_a" | rt_kv mangle)
  rb_an=$(printf '%s\n' "$rb_a" | rt_kv nat)
  if [ -z "$rb_a" ]; then
    rb_res "$rb_t" FAIL reboot-xcp "не удалось снять состояние после перезагрузки"
    rb_skip "$rb_t" "состояние после перезагрузки не снято" reboot-xkeen reboot-core reboot-iptables
  else
    # xcp запущен той же версии (S99xcp)
    if [ -n "$rb_apid" ] && [ "$rb_axcp" = "$rb_bxcp" ]; then
      rb_res "$rb_t" PASS reboot-xcp "xcp запущен автозапуском, версия $rb_axcp"
    else
      rb_res "$rb_t" FAIL reboot-xcp "после перезагрузки xcp: PID ${rb_apid:--}, версия ${rb_axcp:--}; до перезагрузки версия $rb_bxcp"
    fi
    # XKeen запущен, если был запущен
    if [ "$rb_bxk" != 1 ]; then
      rb_res "$rb_t" PASS reboot-xkeen "XKeen не работал до перезагрузки — не проверяется"
    elif [ "$rb_axk" = 1 ]; then
      rb_res "$rb_t" PASS reboot-xkeen "XKeen запущен автозапуском"
    else
      rb_res "$rb_t" FAIL reboot-xkeen "XKeen работал до перезагрузки и не запущен после"
    fi
    # то же ядро
    if [ "$rb_acore" = "$rb_bcore" ]; then
      rb_res "$rb_t" PASS reboot-core "ядро $rb_acore запущено, как до перезагрузки"
    else
      rb_res "$rb_t" FAIL reboot-core "после перезагрузки ядро ${rb_acore:--}, до перезагрузки $rb_bcore"
    fi
    # правила XKeen восстановлены
    if [ "$((rb_bm + rb_bn))" = 0 ]; then
      rb_res "$rb_t" PASS reboot-iptables "правил XKeen до перезагрузки не было — не проверяется"
    elif [ "$rb_am" = "$rb_bm" ] && [ "$rb_an" = "$rb_bn" ]; then
      rb_res "$rb_t" PASS reboot-iptables "правила XKeen на месте: mangle $rb_am, nat $rb_an"
    else
      rb_res "$rb_t" FAIL reboot-iptables "правил XKeen mangle ${rb_am:--}, nat ${rb_an:--}; до перезагрузки mangle $rb_bm, nat $rb_bn"
    fi
  fi

  # смоук (D-34): строки лога — с баннера запуска после перезагрузки
  sh "$SCRIPT_DIR/smoke.sh" "$rb_t" "$EXPECT" || true

  # файлы стенда не изменились: сверка со снимком, расхождение — восстановление и падение
  rb_rc=0
  rt_stage "$rb_t" reboot-verify rt_snap_remote "$rb_t" verify >"$RT_REPORT/$rb_t/reboot-verify.log" 2>&1 || rb_rc=$?
  if [ "$rb_rc" = 0 ]; then
    rb_res "$rb_t" PASS reboot-verify "файлы, версии и ядро равны снимку"
    rb_cleanup "$rb_t"
  else
    rb_res "$rb_t" FAIL reboot-verify "расхождение со снимком после перезагрузки (код $rb_rc): $(grep 'ОШИБКА' "$RT_REPORT/$rb_t/reboot-verify.log" | head -n 2)"
    rt_restore "$rb_t" || true
  fi
  printf '%s\t%s\t%s\t%s\n' "$rb_t" "$rb_start" "$rb_back" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >>"$RT_REPORT/reboot.tsv"
}

reboot_suite() {
  # 1. предполёт и снимок параллельно
  for rb_t in ${DEPLOYED:-}; do
    (
      rb_rc=0
      rt_snapshot "$rb_t" || rb_rc=$?
      echo "$rb_rc" >"$RT_REPORT/$rb_t/snap.rc"
    ) &
  done
  wait
  rb_ready=""
  for rb_t in $RT_TARGETS; do
    if [ "$(cat "$RT_REPORT/$rb_t/snap.rc" 2>/dev/null || echo 1)" = 0 ]; then
      rb_ready="$rb_ready $rb_t"
    else
      # shellcheck disable=SC2086
      rb_skip "$rb_t" "цель не готова: нет связи, деплоя или снимка" $RB_CHECKS
    fi
  done

  # 2. порядок: XCP_REBOOT_ORDER, затем цели набора, которых в нём нет
  rb_order=""
  if [ -n "${XCP_REBOOT_ORDER:-}" ]; then
    for rb_o in $XCP_REBOOT_ORDER; do
      case " $rb_ready " in
        *" $rb_o "*) case " $rb_order " in *" $rb_o "*) ;; *) rb_order="$rb_order $rb_o" ;; esac ;;
      esac
    done
  else
    echo "XCP_REBOOT_ORDER не задан: порядок XCP_TARGETS (нижестоящая цель каскада должна идти первой)" >"$RT_REPORT/reboot-warn"
  fi
  for rb_o in $rb_ready; do
    case " $rb_order " in *" $rb_o "*) ;; *) rb_order="$rb_order $rb_o" ;; esac
  done
  rb_order=${rb_order# }
  printf '%s\n' "$rb_order" >"$RT_REPORT/reboot-order"
  : >"$RT_REPORT/reboot.tsv"

  # 3. строго по одной цели
  for rb_t in $rb_order; do
    rb_one "$rb_t"
  done

  # 4. в конце все цели снова отвечают (нижестоящая видна с ПК после перезагрузки вышестоящей)
  for rb_t in $rb_order; do
    rb_rc=0
    rt_stage "$rb_t" reboot-end rt_wait_back "$rb_t" || rb_rc=$?
    if [ "$rb_rc" = 0 ]; then
      rb_res "$rb_t" PASS reboot-reachable-end "отвечает по ssh, панель отвечает (ожидание $RT_BACK_SECS с)"
    else
      rb_res "$rb_t" FAIL reboot-reachable-end "нет связи с целью в конце набора (ожидание $RT_BACK_SECS с)"
    fi
  done

  # 5. метка reboot:* без падения проверки — XPASS (метка больше не нужна)
  for rb_t in $rb_order; do
    rb_arch=$(rt_get "$rb_t" ARCH)
    rb_core=-
    [ ! -f "$RT_REPORT/$rb_t/core" ] || rb_core=$(cat "$RT_REPORT/$rb_t/core")
    while IFS="$TAB" read -r rb_lc rb_ls rb_lslug || [ -n "$rb_lc" ]; do
      case "$rb_lc" in reboot:?*) ;; *) continue ;; esac
      rt_selector_match "$rb_ls" "$rb_arch" "$rb_core" || continue
      rb_p=${rb_lc#reboot:}
      if awk -F'\t' -v p="$rb_p" 'index($3, p) == 1 { if ($1 == "PASS") ok = 1; if ($1 == "KNOWN") k = 1 } END { exit !(ok && !k) }' "$RT_REPORT/$rb_t/results.tsv"; then
        rt_result "$rb_t" XPASS "known:$rb_p" "метка больше не нужна: снять и закрыть todo $rb_lslug"
      fi
    done <"$SCRIPT_DIR/known-failures"
  done
}

PIDS=""
RT_INTR=0
if [ "$SUITE" = reboot ]; then
  reboot_suite
else
  # Сигнал ставится до запуска подпроцессов целей: между запуском и ожиданием он не убивает
  # run.sh и не оставляет цели без ожидания. Под dash сигнал, игнорируемый при старте
  # асинхронного списка (`( full_target ) &`), подпроцесс цели перехватить не может: его
  # trap INT не срабатывает, и Ctrl+C не гасил бы этап. TERM он перехватывает, поэтому
  # обработчик пересылает TERM каждому подпроцессу цели (его PID, не группе: подпроцесс не
  # лидер группы, и TERM группе задел бы сам run.sh). Цель останавливает этап, восстанавливает
  # стенд и выходит с кодом 130.
  trap 'RT_INTR=1; echo "router-test: прерывание — останавливаю этапы и жду восстановления стенда" >&2; for _kp in $PIDS; do kill -s TERM "$_kp" 2>/dev/null || true; done' INT TERM HUP
  for t in ${DEPLOYED:-}; do
    if [ -z "$SUITE" ] || [ "$SUITE" = harness ] || [ "$SUITE" = rerun ] || [ "$SUITE" = delta ] || { [ "$SUITE" = changed ] && [ "$SEL_ANY" = 1 ]; }; then
      (full_target "$t") &
    else
      sh "$SCRIPT_DIR/smoke.sh" "$t" "$EXPECT" &
    fi
    PIDS="$PIDS $!"
  done
fi
# Ждём подпроцессы целей по признаку жизни, а не по коду wait. Прерванный перехваченным
# сигналом wait возвращает больше 128, пока подпроцесс жив; повторный wait завершённого
# подпроцесса в dash и bash снова даёт его сохранённый код, а не 127, поэтому условие по
# величине кода зацикливалось бы навсегда (G-145.1-5). Конец ожидания — kill -0: пока
# подпроцесс жив (в том числе зомби), wait повторяется, и последний wait забирает его код.
# Код 130 — цель прервали напрямую, мимо этого обработчика.
for p in $PIDS; do
  wrc=0
  wait "$p" || wrc=$?
  while kill -0 "$p" 2>/dev/null; do
    wrc=0
    wait "$p" || wrc=$?
  done
  if [ "$wrc" = 130 ]; then
    RT_INTR=1
  fi
done
# На время построения отчёта сигнал не убивает run.sh: он только запоминается (RT_INTR), отчёт
# доводится до конца, и прогон выходит с кодом 130 без удержания блокировки.
trap 'RT_INTR=1' INT TERM HUP

# Цели, где прогон остановлен сменой версии стенда (D-13): строка stop в <цель>/version-stop.tsv; у
# перепрогона (отчёт FROM мог хранить остановку исходного прогона) — строка FAIL version-guard в его
# rerun-N.tsv. Прогон со сменой версии неполный: зелёного нет (класс stand, открытый), самопроверка
# сессии и поиск виновного коммита не идут, в историю он не пишется, статус на GitHub не отправляется.
RT_VSTOP=""
for t in $RT_TARGETS; do
  if [ "$SUITE" = rerun ]; then
    _vs_f="$RT_REPORT/$t/${RT_RESULTS_NAME:-results.tsv}"
    if [ -f "$_vs_f" ] && awk -F'\t' '$1 == "FAIL" && $3 == "version-guard" {f = 1} END {exit !f}' "$_vs_f"; then
      RT_VSTOP="$RT_VSTOP $t"
    fi
  elif [ -f "$RT_REPORT/$t/version-stop.tsv" ] && awk -F'\t' '$1 == "stop" {f = 1} END {exit !f}' "$RT_REPORT/$t/version-stop.tsv"; then
    RT_VSTOP="$RT_VSTOP $t"
  fi
done
RT_VSTOP=${RT_VSTOP# }

# История результатов (D-20): history.mjs append добавляет отчёт (строки результатов и классы
# падений) в build/router/history.tsv; повторное добавление того же отчёта ничего не пишет. Вызывается
# в конце каждого прогона с отчётом (все наборы, кроме session: её историю пишет session.mjs) до
# hold_lock; сбой — предупреждение, код выхода прогона не меняется.
rt_history_append() {
  node "$SCRIPT_DIR/history.mjs" append "$RT_REPORT" >/dev/null || echo "router-test: предупреждение: запись отчёта в историю не удалась (node scripts/router/history.mjs append $RT_REPORT)" >&2
}

# --- отчёт набора rerun -------------------------------------------------------------------
# Итог перепрогона дописывается в отчёт FROM: раздел «## Перепрогон N» в summary.md (копия в
# rerun-N.md) с таблицей «проверка, ядро, было, стало» и строкой «- Перепрогон: K тестов из
# <отчёт>». «Было» — статус упавшей проверки в исходных результатах отчёта (results.tsv прогона или
# всех этапов сессии), «стало» — худший статус той же проверки в rerun-N.tsv; подтест Go сводится к
# родительскому TestRouter*. Остальные файлы отчёта (meta, report.md, results.tsv) не меняются.
# Код выхода: 130 — прерван; 3 — сбой связи (FAIL reachable или код деплоя 3); 1 — есть FAIL или
# XPASS в rerun-N.tsv (нет файла — тоже); иначе 0.
rerun_finish() {
  _rf_n=$RT_RERUN_N
  _rf_name=$(basename "$RT_REPORT")
  _rf_md="$RT_REPORT/rerun-$_rf_n.md"
  _rf_bad=0
  _rf_link=0
  {
    echo "## Перепрогон $_rf_n ($(date -u +%Y-%m-%dT%H:%M:%SZ), SHA $(git rev-parse --short HEAD))"
    echo
    echo "- Перепрогон: $RT_RERUN_K тестов из $_rf_name"
    [ "$RT_INTR" != 1 ] || echo "- Прерван: да (сигнал)"
    [ -z "$RT_VSTOP" ] || echo "- Остановлен: смена версии стенда ($RT_VSTOP); история: не записано — смена версии стенда, прогон неполный"
    for _rf_t in $RT_TARGETS; do
      _rf_res="$RT_REPORT/$_rf_t/rerun-$_rf_n.tsv"
      echo
      echo "### Цель $_rf_t"
      echo
      if [ ! -f "$_rf_res" ]; then
        echo "- результатов нет"
        _rf_bad=$((_rf_bad + 1))
        continue
      fi
      awk -F'\t' '{c[$1]++} END {printf "- PASS: %d, FAIL: %d, KNOWN: %d, XPASS: %d, SKIP: %d\n", c["PASS"], c["FAIL"], c["KNOWN"], c["XPASS"], c["SKIP"]}' "$_rf_res"
      _rf_orig=""
      if [ -f "$RT_REPORT/$_rf_t/results.tsv" ]; then
        _rf_orig="$RT_REPORT/$_rf_t/results.tsv"
      else
        for _rf_f in "$RT_REPORT"/[0-9]*-*/"$_rf_t"/results.tsv; do
          if [ -f "$_rf_f" ]; then _rf_orig="$_rf_orig $_rf_f"; fi
        done
      fi
      echo
      echo "| Проверка | Ядро | Было | Стало |"
      echo "|---|---|---|---|"
      # shellcheck disable=SC2086
      awk -F'\t' -v new="$_rf_res" '
        function pkey(core, chk) { if (chk ~ /^go:/) sub(/\/.*$/, "", chk); return core "\t" chk }
        function esc(s,   o, i, ch) {
          o = ""
          for (i = 1; i <= length(s); i++) {
            ch = substr(s, i, 1)
            if (ch == "\\" || ch == "|") o = o "\\"
            o = o ch
          }
          return o
        }
        function bad(s) { return s == "FAIL" || s == "XPASS" }
        FILENAME == new {
          if ($3 !~ /^(go|pw):/) next
          k = pkey($2, $3)
          ran[$2] = 1
          if (!(k in now) || (bad($1) && !bad(now[k]))) now[k] = $1
          next
        }
        bad($1) && $3 ~ /^(go|pw):/ {
          k = pkey($2, $3)
          if (!(k in was)) { was[k] = $1; order[++n] = k }
        }
        END {
          for (i = 1; i <= n; i++) {
            k = order[i]
            split(k, a, "\t")
            if (!(a[1] in ran)) continue
            printf "| %s | %s | %s | %s |\n", esc(a[2]), a[1], was[k], (k in now) ? now[k] : "нет строки"
          }
        }
      ' $_rf_orig "$_rf_res"
      if awk -F'\t' '$1 == "FAIL" && $3 == "reachable" {f = 1} END {exit !f}' "$_rf_res"; then
        _rf_link=1
      fi
      if [ -f "$RT_REPORT/$_rf_t/deploy.rc" ] && [ "$(cat "$RT_REPORT/$_rf_t/deploy.rc")" = 3 ]; then
        _rf_link=1
      fi
      _rf_b=$(awk -F'\t' '$1 == "FAIL" || $1 == "XPASS" {n++} END {print n + 0}' "$_rf_res")
      _rf_bad=$((_rf_bad + _rf_b))
    done
  } >"$_rf_md"
  {
    echo
    cat "$_rf_md"
  } >>"$RT_REPORT/summary.md"
  echo
  cat "$_rf_md"
  if [ "$RT_INTR" = 1 ]; then
    echo "router-test: прерван (код 130), итог — $_rf_md" >&2
    exit 130
  fi
  # перепрогон, остановленный сменой версии стенда, неполный: в историю не пишется (D-13)
  [ -n "$RT_VSTOP" ] || rt_history_append
  hold_lock
  if [ "$_rf_link" = 1 ]; then
    echo "router-test: сбой связи ПК↔стенд (итог — $_rf_md)" >&2
    exit 3
  fi
  if [ "$_rf_bad" -gt 0 ]; then
    echo "router-test: красный (FAIL и XPASS перепрогона: $_rf_bad)" >&2
    exit 1
  fi
  echo "router-test: зелёный"
  exit 0
}
if [ "$SUITE" = rerun ]; then
  rerun_finish
fi

# --- самопроверка сессии (RT_SELFCHECK=1: набор harness; changed и delta при RT_HARNESS=1) ---
# Сама остановка на первом несовпадении — механика сессии (session.mjs), и править обвязку, не
# сломав её, нельзя. Вложенная сессия по спеке блока harness (проход fail-fast-probe нарочно ждёт
# FAIL у проходящего дешёвого теста) обязана: выйти с кодом 1, написать в summary.md раздел
# «Остановлено на первом несовпадении», не выполнить второй проход (must-not-run) и вернуть
# стенд («## Стенд в конце» без «нет»). Иначе — FAIL harness-session-fail-fast. Вложенный run.sh
# идёт под блокировкой этого процесса (RT_LOCK_HELD=1), настройки прогона не наследует, отчёт —
# <отчёт>-01-harness-session (ключ отчёта tidy: <ключ>-NN-<имя>). Итог — RT_H_SELF (да|нет) и
# причина в RT_H_SELF_WHY для summary.md; строка результата — в results.tsv цели.
_hs_fail() {
  RT_H_SELF=нет
  RT_H_SELF_WHY=$1
  rt_result "$_hs_rt" FAIL harness-session-fail-fast "$1"
}
harness_selfcheck() {
  RT_H_SELF=нет
  RT_H_SELF_WHY=""
  _hs_rt=${RT_TARGETS%% *}
  [ -n "$_hs_rt" ] || return 0
  if [ -z "$RT_H_SESSION" ]; then
    _hs_fail "блок harness в areas.json не прочитан: спеки самопроверки нет"
    return 0
  fi
  _hs_tg=$(node "$SCRIPT_DIR/session.mjs" --target "$RT_H_SESSION" 2>/dev/null) || _hs_tg=""
  if [ -z "$_hs_tg" ]; then
    _hs_fail "спека самопроверки не разобрана (session.mjs --target)"
    return 0
  fi
  # строка результата — в цели спеки, если она в прогоне, иначе в первой цели прогона
  case " $RT_TARGETS " in *" $_hs_tg "*) _hs_rt=$_hs_tg ;; esac
  if [ -n "$(git status --porcelain --untracked-files=no)" ]; then
    _hs_fail "рабочее дерево не чистое: вложенная сессия меняет файлы продукта"
    return 0
  fi
  if [ -z "${DEPLOYED:-}" ]; then
    _hs_fail "ни одна цель не развёрнута: проверять нечем"
    return 0
  fi
  _hs_dir="$RT_REPORT-01-harness-session"
  _hs_rel=${_hs_dir#"$(rt_repo_root)"/}
  echo "router-test: самопроверка сессии (остановка на первом несовпадении) → $_hs_rel"
  env -u RT_CORES -u RT_HOLD_MINUTES -u RT_BASE -u RT_FILES -u RELEASE -u SUITE \
    -u RT_SMOKE_WINDOW -u RT_SMOKE_IPT_WAIT -u RT_GO_PKGS -u RT_GO_RUN -u RT_GO_RUN_SEQ \
    -u RT_GO_CORE_PKGS -u RT_GO_CORE_RUN -u RT_SPECS -u RT_SPECS_CORE -u RT_SPECS_FIRST \
    -u RT_PW_GREP -u RT_PW_GREP_INVERT -u RT_PW_CORE_ONLY -u RT_ART_SUB -u RT_KERNEL_TOUCHED \
    -u RT_SEL_DIR -u RT_GO_SEQ -u RT_PW_INVERT -u RT_RESULTS_NAME \
    -u RT_SELECT_ALL -u RT_CORE -u RT_REPORT -u XCP_WANT_CORE -u XCP_VERSION \
    SPEC="$RT_H_SESSION" RT_LOCK_HELD=1 RT_REPORT_DIR="$_hs_dir" \
    sh "$SCRIPT_DIR/run.sh" session >"$RT_REPORT/harness-session.log" 2>&1 &
  _hs_pid=$!
  trap 'RT_INTR=1; kill -TERM "$_hs_pid" 2>/dev/null || true' INT TERM HUP
  _hs_rc=0
  wait "$_hs_pid" || _hs_rc=$?
  while kill -0 "$_hs_pid" 2>/dev/null; do
    _hs_rc=0
    wait "$_hs_pid" || _hs_rc=$?
  done
  trap 'RT_INTR=1' INT TERM HUP
  # прерванный прогон отчёт доводит до конца без результата самопроверки
  if [ "$RT_INTR" = 1 ]; then
    RT_H_SELF=""
    return 0
  fi
  if [ "$_hs_rc" = 3 ]; then
    _hs_fail "сбой связи ПК↔стенд во вложенной сессии (код 3), отчёт $_hs_rel"
    return 0
  fi
  if [ "$_hs_rc" != 1 ]; then
    _hs_fail "вложенная сессия вышла с кодом $_hs_rc, ожидался 1 (остановка на первом несовпадении), отчёт $_hs_rel"
    return 0
  fi
  if [ ! -f "$_hs_dir/summary.md" ] || [ ! -f "$_hs_dir/session.json" ]; then
    _hs_fail "у вложенной сессии нет summary.md или session.json, отчёт $_hs_rel"
    return 0
  fi
  if ! grep -q '^## Остановлено на первом несовпадении' "$_hs_dir/summary.md"; then
    _hs_fail "в summary.md вложенной сессии нет раздела «Остановлено на первом несовпадении», отчёт $_hs_rel"
    return 0
  fi
  if grep -q '^| must-not-run |' "$_hs_dir/summary.md"; then
    _hs_fail "второй проход (must-not-run) выполнялся: сессия не остановилась, отчёт $_hs_rel"
    return 0
  fi
  _hs_chk_rc=0
  _hs_chk=$(node -e '
const fs = require("fs");
const s = JSON.parse(fs.readFileSync(process.argv[1] + "/session.json", "utf8"));
const bad = [];
if (!s.stopped) bad.push("сессия не остановилась на первом несовпадении");
else if (s.stopped.pass !== "fail-fast-probe") bad.push("остановка не на проходе fail-fast-probe, а на " + s.stopped.pass);
const ran = [...(s.verdicts || []), ...(s.steps || [])].filter((x) => x.pass === "must-not-run");
if (ran.length) bad.push("второй проход (must-not-run) выполнялся");
if (!s.stand_end || !s.stand_end.ok) bad.push("стенд не вернулся в исходное состояние");
if (bad.length) {
  console.log(bad.join("; "));
  process.exit(1);
}
' "$_hs_dir" 2>&1 | head -c 400) || _hs_chk_rc=$?
  if [ "$_hs_chk_rc" != 0 ] || [ -n "$_hs_chk" ]; then
    _hs_fail "${_hs_chk:-session.json вложенной сессии не разобран}, отчёт $_hs_rel"
    return 0
  fi
  if ! awk '/^## Стенд в конце/ {f = 1; next} /^## / {f = 0} f && /\*\*нет\*\*/ {bad = 1} END {exit bad}' "$_hs_dir/summary.md" ||
    ! grep -q '^## Стенд в конце' "$_hs_dir/summary.md"; then
    _hs_fail "раздел «Стенд в конце» вложенной сессии пуст или содержит «нет», отчёт $_hs_rel"
    return 0
  fi
  RT_H_SELF=да
  rt_result "$_hs_rt" PASS harness-session-fail-fast "сессия остановилась на первом несовпадении (код 1), второй проход не выполнялся, стенд вернулся; отчёт $_hs_rel"
}

# Самопроверка сессии — по плану состава (RT_SELFCHECK: набор harness, changed и delta при правке
# обвязки); наборы без плана состава её не запускают.
if [ "$RT_INTR" != 1 ] && [ "$RT_SELFCHECK" = 1 ]; then
  if [ -n "$RT_VSTOP" ]; then
    # смена версии стенда остановила прогон (D-13): самопроверка — тоже невыполненный этап
    printf 'notrun\t-\t%s\n' "самопроверка сессии: не выполнена" >>"$RT_REPORT/${RT_VSTOP%% *}/version-stop.tsv"
  else
    harness_selfcheck
  fi
fi

# --- отчёт -------------------------------------------------------------------------

SHA=$(git rev-parse HEAD)
DIRTY=$(sed -n 's/^dirty=//p' "$RT_REPORT/meta")
FAILS=0
XPASSES=0

# Прерванный прогон: пометка в отчётах и список оборванных этапов (строки timings.tsv с кодом
# 130 пишет _term, D-19). Нет таких строк — «-».
if [ "$RT_INTR" = 1 ]; then
  INTR_STAGES=""
  for t in $RT_TARGETS; do
    _is=""
    if [ -f "$RT_REPORT/timings.tsv" ]; then
      _is=$(awk -F'\t' -v t="$t" '$1 == t && $4 == 130 {printf "%s%s/%s", sep, t, $2; sep = ", "}' "$RT_REPORT/timings.tsv") || _is=""
    fi
    [ -z "$_is" ] || INTR_STAGES="${INTR_STAGES:+$INTR_STAGES, }$_is"
  done
fi

# Разделы по ветвям (этапы, восстановление, долгие тесты, метки ядра) строятся до отчётов и
# до подсчёта FAIL: --write дописывает в results.tsv строки FAIL core-mark:… (D-05б, полный
# прогон краснеет на неразмеченном тесте с разным исходом) и пишет <цель>/layout.tsv (D-19).
# Строки плана ветвей (plan.txt) идут первыми в разделе цели.
for t in $RT_TARGETS; do
  mkdir -p "$RT_REPORT/$t"
  node "$SCRIPT_DIR/branch-report.mjs" "$RT_REPORT" "$t" --suite "$SUITE_NAME" --write --plan "$RT_REPORT/plan.txt" >"$RT_REPORT/$t/branch.md" 2>"$RT_REPORT/$t/branch.err" || true
done

{
  echo "# Прогон проверки на роутерах"
  echo
  echo "- Набор: $SUITE_NAME"
  echo "- SHA: $SHA (незакоммиченные правки: $DIRTY)"
  [ "$RT_INTR" != 1 ] || echo "- Прерван: да (сигнал); оборваны этапы: ${INTR_STAGES:--}"
  [ -z "${RELEASE:-}" ] || echo "- Релиз: $RELEASE"
  echo
  if [ "$SUITE" = changed ] && [ -f "$RT_REPORT/select.txt" ]; then
    echo "## Выбор"
    echo
    echo '```'
    cat "$RT_REPORT/select.txt"
    echo '```'
    echo
    if [ -s "$RT_REPORT/plan.txt" ]; then
      echo "### Ветви"
      echo
      cat "$RT_REPORT/plan.txt"
      echo
    fi
  elif [ -s "$RT_REPORT/plan.txt" ]; then
    echo "## Ветви"
    echo
    cat "$RT_REPORT/plan.txt"
    echo
  fi
  if [ "$SUITE" = reboot ] && [ -f "$RT_REPORT/reboot-order" ]; then
    echo "## Перезагрузки"
    echo
    echo "- Порядок (XCP_REBOOT_ORDER): $(cat "$RT_REPORT/reboot-order")"
    [ ! -f "$RT_REPORT/reboot-warn" ] || echo "- Предупреждение: $(cat "$RT_REPORT/reboot-warn")"
    if [ -s "$RT_REPORT/reboot.tsv" ]; then
      awk -F'\t' '{printf "- %s: начало перезагрузки %s (UTC), возврат через %s с, конец проверок %s (UTC)\n", $1, $2, $3, $4}' "$RT_REPORT/reboot.tsv"
    fi
    echo
  fi
  for t in $RT_TARGETS; do
    arch=$(rt_get "$t" ARCH)
    ver=""
    [ -f "$RT_REPORT/$t/xcp_version" ] && ver=$(cat "$RT_REPORT/$t/xcp_version")
    echo "## Цель $t ($arch)"
    echo
    echo "- Версия xcp: ${ver:--}"
    if [ -f "$RT_REPORT/$t/results.tsv" ]; then
      awk -F'\t' '{c[$1]++} END {printf "- PASS: %d, FAIL: %d, KNOWN: %d, XPASS: %d, SKIP: %d, QPASS: %d, QFAIL: %d\n", c["PASS"], c["FAIL"], c["KNOWN"], c["XPASS"], c["SKIP"], c["QPASS"], c["QFAIL"]}' "$RT_REPORT/$t/results.tsv"
    else
      echo "- результатов нет"
    fi
    echo
    echo "Время этапов (секунды, код):"
    echo
    if [ -f "$RT_REPORT/timings.tsv" ]; then
      awk -F'\t' -v t="$t" '$1 == t || $1 == "all" {printf "- %s: %s с (код %s)\n", $2, $3, $4}' "$RT_REPORT/timings.tsv"
    fi
    echo
    if [ -f "$RT_REPORT/$t/results.tsv" ] && awk -F'\t' '$1 == "FAIL" || $1 == "XPASS" {f = 1} END {exit !f}' "$RT_REPORT/$t/results.tsv"; then
      echo "Падения:"
      echo
      # класс падения — из classes.tsv цели (нет файла — без класса)
      _cls_f="$RT_REPORT/$t/classes.tsv"
      [ -f "$_cls_f" ] || _cls_f=/dev/null
      awk -F'\t' -v cf="$_cls_f" '
        FILENAME == cf { cls[$2 "\t" $3] = $1; next }
        $1 == "FAIL" || $1 == "XPASS" {
          c = cls[$2 "\t" $3]
          printf "- %s %s%s: %s\n", $1, $3, (c != "" ? " [" c "]" : ""), $4
        }' "$_cls_f" "$RT_REPORT/$t/results.tsv"
      echo
    fi
    if [ -f "$RT_REPORT/$t/results.tsv" ] && awk -F'\t' '$1 == "KNOWN" {f = 1} END {exit !f}' "$RT_REPORT/$t/results.tsv"; then
      echo "Известные падения (не влияют на код выхода):"
      echo
      awk -F'\t' '$1 == "KNOWN" {printf "- %s: %s\n", $3, $4}' "$RT_REPORT/$t/results.tsv"
      echo
    fi
  done
} >"$RT_REPORT/report.md"

{
  echo "# Сводка прогона"
  echo
  echo "- Набор: $SUITE_NAME"
  if [ "$SUITE" = changed ]; then
    echo "- Области: ${RT_AREAS:-нет}"
    echo "- Выбор: ${RT_SEL_KIND:-полный}"
    [ "$RT_H_EXTRA" != 1 ] || echo "- Обвязка изменена: набор harness добавлен"
  fi
  if [ "$SUITE" = harness ]; then
    _el=$(($(date +%s) - RT_T0))
    echo "- Время набора: $((_el / 60)) мин $((_el % 60)) с ($_el с)"
  fi
  if [ -n "$RT_H_SELF" ]; then
    echo "- Самопроверка сессии: остановка на первом несовпадении — $RT_H_SELF"
    [ -z "$RT_H_SELF_WHY" ] || echo "- Самопроверка сессии, причина: $RT_H_SELF_WHY"
  fi
  echo "- SHA: $SHA"
  [ -z "${RELEASE:-}" ] || echo "- Релиз: $RELEASE"
  echo "- Проверены цели: $(printf '%s' "$RT_TARGETS" | tr ' ' ',')"
  [ "$RT_INTR" != 1 ] || echo "- Прерван: да (сигнал); оборваны этапы: ${INTR_STAGES:--}"
  echo
  for t in $RT_TARGETS; do
    arch=$(rt_get "$t" ARCH)
    ver=""
    [ -f "$RT_REPORT/$t/xcp_version" ] && ver=$(cat "$RT_REPORT/$t/xcp_version")
    printf '%s: xcp %s, ' "$arch" "${ver:--}"
    if [ -f "$RT_REPORT/$t/results.tsv" ]; then
      awk -F'\t' '{c[$1]++} END {printf "passed %d, failed %d, known %d, xpass %d, skipped %d, quarantine %d (QPASS %d, QFAIL %d)\n", c["PASS"], c["FAIL"], c["KNOWN"], c["XPASS"], c["SKIP"], c["QPASS"] + c["QFAIL"], c["QPASS"], c["QFAIL"]}' "$RT_REPORT/$t/results.tsv"
    else
      echo "результатов нет"
    fi
    # план ветвей, этапы по ветвям, итог и критический путь, оболочка, восстановление, долгие
    # тесты, метки ядра (D-19)
    [ ! -f "$RT_REPORT/$t/branch.md" ] || cat "$RT_REPORT/$t/branch.md"
  done
} >"$RT_REPORT/summary.md"

# Красные классы (D-19…D-21): new, flaky и не догнанный stand — в FAILS, xpass — в XPASSES. Нет
# classes.tsv (разбор не сработал) — прежний подсчёт FAIL и XPASS по results.tsv.
for t in $RT_TARGETS; do
  rt_red_counts "$t"
  FAILS=$((FAILS + RT_RC_RED))
  XPASSES=$((XPASSES + RT_RC_XPASS))
done
# Прогон со сменой версии стенда зелёным не бывает (D-13): класс stand смены версии в classes.tsv
# красный; без разбора (classes.tsv не записан) — всё равно красный.
if [ -n "$RT_VSTOP" ] && [ "$FAILS" -eq 0 ]; then
  FAILS=1
fi

# --- решение добора (набор delta, RTR-11, D-24, D-25) --------------------------------------
# Решение по цели: success только при всех условиях todo 11 — найденная база (иначе прогона не было),
# нуль красных классов (new, flaky, не догнанный stand, xpass: упавшие в базе, тесты по диффу и
# подтверждающий перепрогон), зелёный смоук нового тега (хотя бы одна пройденная проверка смоука этого
# прогона и нет FAIL), полный состав (непустой notrun.txt — часть не выполнена из-за обрыва связи).
# Результат — RT_DD_STATE (success|failure) и RT_DD_TESTS (число разных тестов go: и pw: прогона).
DELTA_RED=0
delta_decide() {
  RT_DD_STATE=failure
  RT_DD_TESTS=0
  _dd_res="$RT_REPORT/$1/results.tsv"
  [ -f "$_dd_res" ] || return 0
  RT_DD_TESTS=$(awk -F'\t' '
    $1 != "SKIP" && ($3 ~ /^go:/ || $3 ~ /^pw:/) && $3 != "go:setup" && $3 != "pw:run" && $3 !~ /^pw:core-switch:/ {
      c = $3; if (c ~ /^go:/) sub(/\/.*/, "", c)
      if (!(c in seen)) { seen[c] = 1; n++ }
    }
    END { print n + 0 }' "$_dd_res")
  rt_red_counts "$1"
  [ "$RT_RC_RED" -eq 0 ] && [ "$RT_RC_XPASS" -eq 0 ] || return 0
  [ ! -s "$RT_REPORT/$1/notrun.txt" ] || return 0
  awk -F'\t' '$1 == "PASS" && $3 == "kernel-running" {f = 1} END {exit !f}' "$_dd_res" || return 0
  RT_DD_STATE=success
}
# Строки «- Добор:» и «- Решение добора:» в summary.md по каждой цели; при DRY_RUN=1 решение с пометкой
# «dry_run: не отправлено» (статус на GitHub не отправляется).
delta_report() {
  for _dr_t in $RT_TARGETS; do
    _dr_f=0
    _dr_g=0
    _dr_s=0
    if [ -f "$RT_REPORT/$_dr_t/delta.counts" ]; then
      IFS="$(printf '\t')" read -r _dr_f _dr_g _dr_s <"$RT_REPORT/$_dr_t/delta.counts" || true
    fi
    delta_decide "$_dr_t"
    [ "$RT_DD_STATE" = success ] || DELTA_RED=1
    echo "- Добор: база $DELTA_BASE_TAG ($DELTA_BASE_NAME), упавших в базе ${_dr_f:-0}, по диффу $((${_dr_g:-0} + ${_dr_s:-0})), всего тестов $RT_DD_TESTS" >>"$RT_REPORT/summary.md"
    if [ "$DELTA_DRY" = 1 ]; then
      echo "- Решение добора: $RT_DD_STATE (dry_run: не отправлено)" >>"$RT_REPORT/summary.md"
    else
      echo "- Решение добора: $RT_DD_STATE" >>"$RT_REPORT/summary.md"
    fi
  done
}
if [ "$SUITE" = delta ]; then
  delta_report
fi

echo
cat "$RT_REPORT/summary.md"

# --- статус стенда на коммите тега релиза (D-10, D-13) --------------------------------

# Для каждой цели ставит статус stand/<arch>-<smoke|full> на коммите RELEASE и дописывает в
# summary.md строку «Статус на GitHub: <контекст> <состояние>» (при отказе — «не отправлен
# (<причина>)»). Статуса нет при смене версии стенда в прогоне (RT_VSTOP, D-13), незакоммиченных
# правках, сбое связи ПК↔стенд (непустой
# <цель>/notrun.txt, строка FAIL reachable без догона — нет catchup.tsv, а также когда у красного
# прогона в конце нет связи с целью; обрыв, после которого связь вернулась и был догон, статус не
# останавливает), отсутствии результатов, деплое, не дошедшем до
# цели (сбой на ПК), сбое подготовки стенда (deploy.rc=2), у smoke — когда ветка меняла
# scripts/router относительно origin/main (или сравнить не удалось), а у full — когда коммит
# тега не предок HEAD. Прерванный прогон досюда не доходит (выход по RT_INTR раньше). Код выхода
# прогона не меняется.
publish_release_status() {
  [ -n "${RELEASE:-}" ] || return 0
  case "$SUITE_NAME" in
    smoke | full | delta) ;;
    *) return 0 ;;
  esac
  for _pt in $RT_TARGETS; do
    _parch=$(rt_get "$_pt" ARCH)
    _pctx="stand/${_parch}-${SUITE_NAME}"
    # добор публикует тот же контекст, что полный прогон: хук закрытия вехи читает stand/arm64-full
    [ "$SUITE_NAME" != delta ] || _pctx="stand/${_parch}-full"
    _preason=""
    _pres="$RT_REPORT/$_pt/results.tsv"
    if [ -n "$RT_VSTOP" ]; then
      # прогон остановлен сменой версии стенда (D-13): неполный, ни зелёного, ни красного статуса
      _preason="смена версии стенда"
    elif [ "$DIRTY" = true ]; then
      _preason="незакоммиченные правки"
    elif [ -f "$RT_REPORT/$_pt/deploy.rc" ] && [ "$(cat "$RT_REPORT/$_pt/deploy.rc")" = 3 ]; then
      _preason="сбой связи ПК↔стенд"
    elif [ ! -f "$_pres" ]; then
      _preason="результатов нет"
    elif [ -s "$RT_REPORT/$_pt/notrun.txt" ]; then
      # связь не вернулась или часть набора после обрыва не выполнена (D-21, D-26)
      _preason="сбой связи ПК↔стенд"
    elif awk -F'\t' '$1 == "FAIL" && $3 == "reachable" {f = 1} END {exit !f}' "$_pres" && [ ! -f "$RT_REPORT/$_pt/catchup.tsv" ]; then
      # обрыв без догона; обрыв, после которого связь вернулась и был догон, статус не останавливает
      _preason="сбой связи ПК↔стенд"
    elif [ ! -f "$RT_REPORT/$_pt/deploy.rc" ]; then
      # deploy.sh вышел до установки на цель: нет ассета, сбой gh или sha256 на ПК
      _preason="деплой не начат (сбой на ПК)"
    elif [ "$(cat "$RT_REPORT/$_pt/deploy.rc")" = 2 ]; then
      _preason="сбой подготовки стенда"
    elif [ "$SUITE_NAME" = smoke ] && ! git diff --quiet origin/main...HEAD -- scripts/router >/dev/null 2>&1; then
      # Смоук ветки с изменённым кодом стенда проверяет не то, что идёт в main; не удалось
      # сравнить с origin/main (нет ссылки) — тоже не ставим
      _preason="код стенда ветки отличается от main"
    elif [ "$SUITE_NAME" = full ] || [ "$SUITE_NAME" = delta ]; then
      if ! git rev-parse -q --verify "refs/tags/$RELEASE^{commit}" >/dev/null 2>&1; then
        _preason="тег релиза не найден локально"
      elif ! git merge-base --is-ancestor "$RELEASE" HEAD 2>/dev/null; then
        _preason="тесты старше релиза"
      fi
    fi
    if [ -z "$_preason" ]; then
      # красные классы цели: new, flaky, не догнанный stand (_pf) и xpass (_px); «новых падений» в
      # описании — new плюс xpass
      rt_red_counts "$_pt"
      _pnew=$RT_RC_NEW
      _pf=$RT_RC_RED
      _px=$RT_RC_XPASS
      # Красный прогон без связи в конце — вероятный обрыв посреди прогона, а не регрессия
      if [ "$_pf" -gt 0 ] && ! rt_ssh "$_pt" true >/dev/null 2>&1; then
        _preason="сбой связи ПК↔стенд"
      fi
    fi
    if [ -z "$_preason" ]; then
      _pcore=-
      [ ! -s "$RT_REPORT/$_pt/core" ] || _pcore=$(head -n 1 "$RT_REPORT/$_pt/core")
      if [ "$SUITE_NAME" = smoke ]; then _pword="смоук"; else _pword="полный прогон"; fi
      if [ "$_pf" -eq 0 ] && [ "$_px" -eq 0 ]; then
        _pstate=success
        _pverdict="зелёный"
      else
        _pstate=failure
        _pverdict="красный"
      fi
      _pdesc="$_pword $_parch: $_pverdict, ядро $_pcore, новых падений $((_pnew + _px))"
      if [ "$SUITE_NAME" = delta ]; then
        # добор: решение — delta_decide (все условия todo 11), описание называет тег базы (D-25)
        delta_decide "$_pt"
        _pstate=$RT_DD_STATE
        _pverdict="красный"
        [ "$_pstate" != success ] || _pverdict="зелёный"
        _pdesc="добор от $DELTA_BASE_TAG: $_pverdict, тестов $RT_DD_TESTS, новых падений $((_pnew + _px))"
      fi
      _prc=0
      if [ "$DELTA_DRY" = 1 ]; then
        # сухой режим: release-status.sh --dry-run проверяет коммит тега и текст, ничего не отправляя
        sh "$SCRIPT_DIR/release-status.sh" --dry-run "$RELEASE" "$_pctx" "$_pstate" "$_pdesc" || _prc=$?
        if [ "$_prc" -eq 0 ]; then
          _pline="- Статус на GitHub: не отправлен (dry_run: $_pctx $_pstate)"
        else
          _pline="- Статус на GitHub: не отправлен (dry_run, проверка статуса не прошла)"
        fi
      else
        sh "$SCRIPT_DIR/release-status.sh" "$RELEASE" "$_pctx" "$_pstate" "$_pdesc" || _prc=$?
        if [ "$_prc" -eq 0 ]; then
          _pline="- Статус на GitHub: $_pctx $_pstate"
        else
          _pline="- Статус на GitHub: не отправлен (ошибка отправки)"
        fi
      fi
    else
      _pline="- Статус на GitHub: не отправлен ($_preason)"
    fi
    echo "$_pline" >>"$RT_REPORT/summary.md"
    echo "router-test: ${_pline#- }"
  done
}

# --- удержание блокировки для ручной проверки ---------------------------------------

# Прерванный прогон блокировку не удерживает: RT_HOLD_MINUTES — для ручной проверки после
# завершённого прогона, а здесь блокировка снимается выходом процесса.
if [ "$RT_INTR" = 1 ]; then
  echo "router-test: прерван (код 130), итог — $RT_REPORT/summary.md" >&2
  exit 130
fi

# История результатов (D-20): отчёт со строками и классами добавляется в build/router/history.tsv.
# Сбой записи прогон не краснит и код выхода не меняет. Прогон, остановленный сменой версии стенда
# (D-13), неполный: в историю не пишется (иначе база добора и bisect видели бы часть набора).
if [ -n "$RT_VSTOP" ]; then
  echo "- История: не записано — смена версии стенда, прогон неполный" >>"$RT_REPORT/summary.md"
  echo "router-test: история: не записано — смена версии стенда, прогон неполный" >&2
else
  rt_history_append
fi

# --- поиск виновного коммита (RTR-11, D-22, todo 11) ----------------------------------------
# Набор bisect: после деплоя и смоука bisect.mjs ищет коммит для заданного теста. Полный прогон:
# в конце, после статуса релиза и до hold_lock, поиск идёт для новых падений (класс new в
# <цель>/classes.tsv). Вне окна поиска не запускается при прерванном прогоне (выход 130 раньше) и
# для теста с классом flaky в истории за 30 дней (отбор — bisect.mjs pick). Раздел «## Поиск
# виновного коммита» и строки «- Итог bisect» пишет bisect.mjs в summary.md, их подхватывает latest.md
# прогона без присмотра. Поиск код выхода полного прогона не меняет.

# Потолок автоматического поиска в конце полного прогона: не больше 3 тестов (D-22) — каждый поиск
# это несколько сборок на стенде, а виновник новых падений обычно общий.
RT_BISECT_MAX=3

# Строка в разделе «## Поиск виновного коммита» summary.md (заголовок один на отчёт).
bs_note() {
  grep -q '^## Поиск виновного коммита$' "$RT_REPORT/summary.md" 2>/dev/null || printf '\n## Поиск виновного коммита\n' >>"$RT_REPORT/summary.md"
  printf '%s\n' "$1" >>"$RT_REPORT/summary.md"
}

# Набор bisect: код — код bisect.mjs (BISECT_RC). Деплой или смоук цели не прошли — поиска нет.
bisect_suite() {
  BISECT_RC=1
  _bs_t=${RT_TARGETS%% *}
  case " ${DEPLOYED:-} " in
    *" $_bs_t "*) ;;
    *)
      echo "router-test: красный — деплой цели не выполнен, поиск не запускался" >&2
      return 0
      ;;
  esac
  if [ ! -f "$RT_REPORT/$_bs_t/results.tsv" ] || awk -F'\t' '$1 == "FAIL" {f = 1} END {exit !f}' "$RT_REPORT/$_bs_t/results.tsv"; then
    echo "router-test: красный — смоук цели не прошёл, поиск не запускался" >&2
    return 0
  fi
  # исходное ядро стенда — по смоуку (<отчёт>/<цель>/core)
  _bs_orig=""
  [ ! -s "$RT_REPORT/$_bs_t/core" ] || _bs_orig=$(head -n 1 "$RT_REPORT/$_bs_t/core")
  set -- --target "$_bs_t" --core "$CORE" --from "$RT_BS_FROM" --to "$RT_BS_TO" --mode "$RT_BS_MODE" --report "$RT_REPORT" --label "$RT_BS_LABEL"
  if [ -n "$RT_BS_GO" ]; then
    set -- "$@" --go "$RT_BS_GO"
  else
    set -- "$@" --spec "$RT_BS_SPEC"
    [ -z "$RT_BS_GREP" ] || set -- "$@" --grep "$RT_BS_GREP"
  fi
  [ -z "${RELEASE:-}" ] || set -- "$@" --restore-release "$RELEASE"
  [ -z "$_bs_orig" ] || set -- "$@" --restore-core "$_bs_orig"
  BISECT_RC=0
  node "$SCRIPT_DIR/bisect.mjs" "$@" || BISECT_RC=$?
}

# Полный прогон: поиск для новых падений без класса flaky в истории за 30 дней.
auto_bisect() {
  [ "$SUITE_NAME" = full ] || return 0
  # прогон остановлен сменой версии стенда (D-13): неполный, виновника не ищут
  [ -z "$RT_VSTOP" ] || return 0
  _ab_left=$RT_BISECT_MAX
  for _ab_t in $RT_TARGETS; do
    [ "$_ab_left" -gt 0 ] || break
    _ab_cls="$RT_REPORT/$_ab_t/classes.tsv"
    [ -s "$_ab_cls" ] || continue
    awk -F'\t' '$1 == "new" {f = 1} END {exit !f}' "$_ab_cls" || continue
    if ! rt_ssh "$_ab_t" true >/dev/null 2>&1; then
      bs_note "- Цель $_ab_t: поиск не запускался — нет связи со стендом"
      continue
    fi
    _ab_orig=""
    [ ! -s "$RT_REPORT/$_ab_t/core" ] || _ab_orig=$(head -n 1 "$RT_REPORT/$_ab_t/core")
    _ab_tsv="$RT_REPORT/$_ab_t/bisect-pick.tsv"
    _ab_rc=0
    node "$SCRIPT_DIR/bisect.mjs" pick --report "$RT_REPORT" --target "$_ab_t" --max "$_ab_left" >"$_ab_tsv" 2>"$_ab_tsv.err" || _ab_rc=$?
    if [ "$_ab_rc" != 0 ]; then
      bs_note "- Цель $_ab_t: поиск не запускался — подбор тестов не удался (код $_ab_rc)"
      continue
    fi
    _ab_from=""
    _ab_tab=$(printf '\t')
    while IFS="$_ab_tab" read -r _ab_k _ab_a _ab_b _ab_c _ab_d _ab_e || [ -n "$_ab_k" ]; do
      case "$_ab_k" in
        skip) bs_note "- Цель $_ab_t: поиск не запускался — $_ab_a" ;;
        from) _ab_from=$_ab_a ;;
        skipped) bs_note "- Пропущено: $_ab_a ($_ab_b)" ;;
        test)
          [ "$_ab_left" -gt 0 ] || continue
          [ -n "$_ab_from" ] || continue
          # _ab_a ядро, _ab_b go|pw, _ab_c запись теста, _ab_d grep или -, _ab_e имя теста
          set -- --target "$_ab_t" --core "$_ab_a" --from "$_ab_from" --to HEAD --mode regress --assume-to --report "$RT_REPORT" --label "$_ab_e"
          if [ "$_ab_b" = go ]; then
            set -- "$@" --go "$_ab_c"
          else
            set -- "$@" --spec "$_ab_c"
            [ "$_ab_d" = - ] || set -- "$@" --grep "$_ab_d"
          fi
          [ -z "${RELEASE:-}" ] || set -- "$@" --restore-release "$RELEASE"
          [ -z "$_ab_orig" ] || set -- "$@" --restore-core "$_ab_orig"
          _ab_left=$((_ab_left - 1))
          node "$SCRIPT_DIR/bisect.mjs" "$@" || echo "router-test: поиск виновного коммита для $_ab_e завершился с кодом $? (итог — summary.md)" >&2
          ;;
      esac
    done <"$_ab_tsv"
    rm -f "$_ab_tsv" "$_ab_tsv.err"
  done
}

# Статус релиза от поиска не зависит и ставится до него: поиск меняет стенд (сборки старых коммитов),
# и обрыв связи после многочасового поиска не должен отменять статус честного прогона.
publish_release_status

if [ "$SUITE" = bisect ]; then
  bisect_suite
elif [ -z "$SUITE" ]; then
  auto_bisect
fi
if [ "$RT_INTR" = 1 ]; then
  echo "router-test: прерван (код 130), итог — $RT_REPORT/summary.md" >&2
  exit 130
fi

hold_lock

if [ "$SUITE" = bisect ]; then
  if [ "$BISECT_RC" = 0 ]; then
    echo "router-test: поиск завершён (итог — строки «Итог bisect» в $RT_REPORT/summary.md)"
  else
    echo "router-test: поиск завершился с кодом $BISECT_RC (итог — $RT_REPORT/summary.md)" >&2
  fi
  exit "$BISECT_RC"
fi

if [ "$DELTA_RED" = 1 ] && [ "$FAILS" -eq 0 ] && [ "$XPASSES" -eq 0 ]; then
  echo "router-test: красный — решение добора failure (строки «Решение добора» в $RT_REPORT/summary.md)" >&2
  exit 1
fi
if [ "$FAILS" -gt 0 ] || [ "$XPASSES" -gt 0 ]; then
  # причина — строки «- Итог:» целей (branch-report.mjs), иначе счётчики
  _itog=$(grep -h '^- Итог: красный' "$RT_REPORT"/summary.md | sed 's/^- Итог: //' | paste -sd ';' - | sed 's/;/; /g' || true)
  echo "router-test: ${_itog:-красный (FAIL: $FAILS, XPASS: $XPASSES)}" >&2
  exit 1
fi
echo "router-test: зелёный"
