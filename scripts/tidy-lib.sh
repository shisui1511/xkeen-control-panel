# scripts/tidy-lib.sh — функции уборки build/ на ПК: план и исполнение ротации.
#
# Подключается через `. "$SCRIPT_DIR/tidy-lib.sh"` после scripts/router/lib.sh
# (нужны rt_repo_root, rt_common_dir, rt_main_root) из scripts/tidy.sh и скриптов стенда.
# POSIX sh; сам ничего не запускает и `set` не меняет. Функции — tidy_*, внутренние
# переменные — с префиксом _tl.
#
# Пороги (переменные окружения, в скобках умолчания; проверяются — целое число, для сборок, кэша
# релизов, дней и числа групп не меньше 1; при неверном значении план не строится, код 2):
#   XCP_TIDY_BUILDS            сколько новейших build/xcp_v*_<арх> оставлять на архитектуру (5)
#   XCP_TIDY_RELEASES          сколько новейших тегов кэша релизов оставлять (3)
#   XCP_TIDY_REPORTS_KEEP      сколько новейших групп отчётов всегда остаются целиком (10)
#   XCP_TIDY_REPORT_LIGHT_DAYS через сколько дней группа отчётов удаляется целиком (30)
#   XCP_TIDY_REPORT_HEAVY_DAYS через сколько дней с отчёта снимается всё, кроме сводок (3)
#   XCP_TIDY_LIGHT_MAX_KB      сводка крупнее этого размера (КиБ) считается тяжёлой (512)
#   TIDY_SIZES                 1 — считать размеры (du) для плана; иначе размеры 0
#
# Запись плана — одна строка из пяти полей через TAB:
#   <действие>  <категория>  <абсолютный путь>  <КиБ>  <примечание>
# Действия библиотеки: rm, thin, skip. Остальные (mv, git-действия) исполняет
# обработчик tidy_exec_hook, если вызывающий скрипт его определил.

_TL_TAB=$(printf '\t')

# Сообщение об отказе: _tl_refuse <путь> <причина>; всегда код 1.
_tl_refuse() {
  echo "tidy: отказ удалять: ${1} — ${2}" >&2
  return 1
}

# Проверка порога: _tl_num <имя переменной> <значение> [<минимум>]. Целое не меньше минимума
# (по умолчанию 0); иначе сообщение в stderr и код 2 — план не строится, ничего не удаляется.
_tl_num() {
  case "$2" in
    '' | *[!0-9]*)
      echo "tidy: $1 должна быть целым числом: $2" >&2
      return 2
      ;;
  esac
  # 10 цифр и больше не сравниваются как число: заведомо вне разумных порогов
  if [ "${#2}" -gt 9 ] || [ "$2" -lt "${3:-0}" ]; then
    echo "tidy: $1 должна быть от ${3:-0} до 999999999: $2" >&2
    return 2
  fi
}

# Записи плана для путей со stdin (по одному в строке):
# _tl_emit_paths <действие> <категория> <примечание>. Размеры — одним вызовом du.
_tl_emit_paths() {
  _tle_f=$(mktemp)
  cat >"$_tle_f"
  if [ -s "$_tle_f" ]; then
    if [ "${TIDY_SIZES:-0}" = 1 ]; then
      xargs -d '\n' du -sk -- <"$_tle_f" 2>/dev/null |
        awk -F '\t' -v a="$1" -v c="$2" -v n="$3" 'BEGIN { OFS = "\t" } { k = $1; sub(/^[^\t]*\t/, ""); print a, c, $0, k, n }'
    else
      awk -v a="$1" -v c="$2" -v n="$3" 'BEGIN { OFS = "\t" } { print a, c, $0, 0, n }' "$_tle_f"
    fi
  fi
  rm -f "$_tle_f"
}

# Единственное место, где удаляется файл или каталог. Удаляет путь только если он
# лежит внутри корня, не входит в защищённые и подходит под одну из категорий уборки.
# tidy_safe_rm <корень> <абсолютный путь>
tidy_safe_rm() {
  _tls_root=$1
  _tls_p=${2%/}
  _tls_rel=""
  case "$_tls_root" in
    /?*) ;;
    *)
      _tl_refuse "$_tls_p" "корень не абсолютный"
      return 1
      ;;
  esac
  case "$_tls_p" in
    '')
      _tl_refuse "(пусто)" "пустой путь"
      return 1
      ;;
    "$_tls_root")
      _tl_refuse "." "это корень репозитория"
      return 1
      ;;
    "$_tls_root"/?*) ;;
    *)
      _tl_refuse "$_tls_p" "путь вне корня репозитория"
      return 1
      ;;
  esac
  _tls_rel=${_tls_p#"$_tls_root"/}
  case "/$_tls_rel/" in
    */../* | */./* | *//*)
      _tl_refuse "$_tls_rel" "недопустимый компонент пути"
      return 1
      ;;
  esac
  # запретный список — раньше любых разрешений
  for _tls_x in .git .planning .claude AGENTS.md CLAUDE.md .mcp.json .gsd .agents \
    scripts/local scripts/router/targets.local.env \
    frontend/node_modules frontend/dist build/router/bin; do
    case "$_tls_rel" in
      "$_tls_x" | "$_tls_x"/*)
        _tl_refuse "$_tls_rel" "защищённый путь"
        return 1
        ;;
    esac
  done
  _tls_ok=0
  case "$_tls_rel" in
    build/*)
      _tls_b=${_tls_rel#build/}
      case "$_tls_b" in
        xcp | xcp_v* | xcp_test_* | *.log)
          case "$_tls_b" in */*) ;; *) _tls_ok=1 ;; esac
          ;;
        router/release/v[0-9]*)
          case "${_tls_b#router/release/}" in */*) ;; *) _tls_ok=1 ;; esac
          ;;
        router/* | router)
          _tls_n=${_tls_b#router/}
          case "$_tls_n" in
            '' | router | */* | bin | release) ;;
            *) _tls_ok=1 ;;
          esac
          ;;
      esac
      ;;
  esac
  # обрывки отчётов вне build/router и корневые хвосты (точный список имён)
  case "$_tls_rel" in
    frontend/build | frontend/build/?*) _tls_ok=1 ;;
    frontend/playwright-report | frontend/test-results | frontend/test-results-router | \
      frontend/test-results-router.json)
      _tls_ok=1
      ;;
    xcp | *.test | *.out | node_modules | commands.md | .go)
      case "$_tls_rel" in */*) ;; *) _tls_ok=1 ;; esac
      ;;
  esac
  if [ "$_tls_ok" != 1 ]; then
    _tl_refuse "$_tls_rel" "не относится к уборке"
    return 1
  fi
  # вне build/ отслеживаемое git не удаляется никогда
  case "$_tls_rel" in
    build/*) ;;
    *)
      if [ -n "$(git -C "$_tls_root" ls-files -- "$_tls_rel" 2>/dev/null)" ]; then
        _tl_refuse "$_tls_rel" "отслеживается git"
        return 1
      fi
      ;;
  esac
  # родитель должен лежать внутри корня и после разыменования (ссылка на чужой каталог)
  _tls_pc=0
  _tl_check_parent "$_tls_root" "$_tls_p" || _tls_pc=$?
  case "$_tls_pc" in
    0) ;;
    2) return 0 ;;
    *)
      _tl_refuse "$_tls_rel" "каталог выходит за корень через ссылку"
      return 1
      ;;
  esac
  if [ ! -e "$_tls_p" ] && [ ! -L "$_tls_p" ]; then
    return 0
  fi
  if [ "$_tls_rel" = .go ]; then
    # кэш модулей Go хранит каталоги только для чтения: без chmod rm -rf падает
    chmod -R u+w -- "$_tls_p" 2>/dev/null || true
  fi
  rm -rf -- "$_tls_p"
}

# Родитель пути после разыменования лежит внутри корня: _tl_check_parent <корень> <путь>.
# Код 0 — да; 1 — нет; 2 — родителя не существует (удалять нечего).
_tl_check_parent() {
  _tlc_rr=$(cd "$1" 2>/dev/null && pwd -P) || return 1
  _tlc_par=$(cd "$(dirname "$2")" 2>/dev/null && pwd -P) || return 2
  case "$_tlc_par" in
    "$_tlc_rr" | "$_tlc_rr"/*) return 0 ;;
  esac
  return 1
}

# Записи rm для сборок build/xcp_v*_<арх>: кроме XCP_TIDY_BUILDS новейших на архитектуру.
# Маска по хвосту: *_mips не совпадает с *_mipsle.
tidy_plan_builds() {
  _tlb_root=$1
  _tlb_keep=${XCP_TIDY_BUILDS:-5}
  _tl_num XCP_TIDY_BUILDS "$_tlb_keep" 1 || return 2
  for _tlb_a in arm64 mipsle mips; do
    ls -t -- "$_tlb_root"/build/xcp_v*_"$_tlb_a" 2>/dev/null |
      while IFS= read -r _tlb_f; do
        if [ -f "$_tlb_f" ] && [ ! -L "$_tlb_f" ]; then
          printf '%s\n' "$_tlb_f"
        fi
      done |
      awk -v k="$_tlb_keep" 'NR > k' |
      _tl_emit_paths rm сборки "старше $_tlb_keep новейших ($_tlb_a)"
  done
}

# Исполняет записи плана со stdin: rm, thin; skip пропускает; прочие действия отдаёт
# в tidy_exec_hook (если определена). Итог по категориям — в stderr. Код 1, если хоть
# одна операция не удалась.
# tidy_exec <корень>
tidy_exec() {
  _tlx_root=$1
  _tlx_res=$(mktemp)
  _tlx_rc=0
  while IFS="$_TL_TAB" read -r _tlx_a _tlx_c _tlx_p _tlx_k _tlx_n; do
    [ -n "$_tlx_a" ] || continue
    case "$_tlx_a" in
      skip) continue ;;
      rm | thin)
        if _tl_apply "$_tlx_a" "$_tlx_root" "$_tlx_c" "$_tlx_p"; then
          _tlx_st=ok
        else
          _tlx_st=fail
          _tlx_rc=1
        fi
        ;;
      *)
        if command -v tidy_exec_hook >/dev/null 2>&1 && tidy_exec_hook "$_tlx_a" "$_tlx_c" "$_tlx_p" "$_tlx_k" "$_tlx_n"; then
          _tlx_st=ok
        else
          _tlx_st=fail
          _tlx_rc=1
        fi
        ;;
    esac
    printf '%s\t%s\t%s\t%s\n' "$_tlx_c" "$_tlx_a" "$_tlx_st" "${_tlx_k:-0}" >>"$_tlx_res"
  done
  awk -F '\t' '
    { if (!($1 in seen)) { seen[$1] = 1; order[++n] = $1 }
      if ($3 == "ok") {
        if ($2 == "rm") rm[$1]++; else if ($2 == "thin") th[$1]++; else if ($2 == "mv") mv[$1]++; else ot[$1]++
        kb[$1] += $4
      } else fail[$1]++ }
    END { for (i = 1; i <= n; i++) {
        c = order[i]
        s = "tidy: " c ": удалено " rm[c] + 0 ", утончено " th[c] + 0
        if (mv[c]) s = s ", перенесено " mv[c]
        if (ot[c]) s = s ", прочих " ot[c]
        if (kb[c] > 0) s = s sprintf(", ≈ %.1f МБ", kb[c] / 1024)
        if (fail[c]) s = s ", ошибок " fail[c]
        print s } }' "$_tlx_res" >&2
  rm -f "$_tlx_res"
  return "$_tlx_rc"
}

# Одна операция rm/thin над путём. Для корня группы отчёта (имя = ключ) операция
# применяется и ко всем вложенным <ключ>-NN-*: _tl_apply <действие> <корень> <категория> <путь>
_tl_apply() {
  _tla_rc=0
  _tla_name=$(basename "$4")
  if [ "$3" = "отчёты" ] && [ "$(tidy_report_key "$_tla_name")" = "$_tla_name" ]; then
    _tla_list=$(mktemp)
    _tl_members "$(dirname "$4")" "$_tla_name" >"$_tla_list"
    while IFS= read -r _tla_m; do
      _tl_apply1 "$1" "$2" "$_tla_m" || _tla_rc=1
    done <"$_tla_list"
    rm -f "$_tla_list"
  else
    _tl_apply1 "$1" "$2" "$4" || _tla_rc=1
  fi
  return "$_tla_rc"
}

_tl_apply1() { # <действие> <корень> <путь>
  case "$1" in
    rm) tidy_safe_rm "$2" "$3" ;;
    thin) tidy_thin "$2" "$3" ;;
    *) return 1 ;;
  esac
}

# --- отчёты ---------------------------------------------------------------------------

_TL_KEY_RE='[0-9]{8}-[0-9]{6}-[0-9a-f]{7,40}(-dirty)?'

# Ключ группы отчёта: <ts>-<sha>[-dirty] из имени каталога (корня или вложенного
# <ключ>-NN-<проход>); не отчёт — ничего.
tidy_report_key() {
  printf '%s\n' "$1" | sed -nE 's/^([0-9]{8}-[0-9]{6}-[0-9a-f]{7,40}(-dirty)?)(-[0-9]{2}-.+)?$/\1/p'
}

# Существующие члены группы: корень <ключ> и вложенные <ключ>-NN-*: _tl_members <каталог> <ключ>
_tl_members() {
  for _tlm_m in "$1/$2" "$1/$2"-[0-9][0-9]-*; do
    if [ -e "$_tlm_m" ] || [ -L "$_tlm_m" ]; then
      printf '%s\n' "$_tlm_m"
    fi
  done
}

# find по «тяжёлым» файлам каталога: всё, кроме сводок; сводка крупнее
# XCP_TIDY_LIGHT_MAX_KB тоже тяжёлая. Файлы с cookie и CSRF (state.json*, .go-cookies,
# .go-session.env, .cookies) вне списка сводок, значит тяжёлые.
# _tl_find_heavy <каталог> <действие find…>
_tl_find_heavy() {
  _tlh_d=$1
  shift
  find "$_tlh_d" -type f \( ! \( -name meta -o -name report.md -o -name summary.md \
    -o -name select.txt -o -name plan.txt -o -name version -o -name session.json \
    -o -name spec.json -o -name session.log -o -name reboot-order -o -name RECOVERED.txt \
    -o -name core -o -name core.detected -o -name xcp_version -o -name branch.md \
    -o -name stand-evidence.txt -o -name '*.tsv' -o -name '*.rc' \) \
    -o -size +"${XCP_TIDY_LIGHT_MAX_KB:-512}"k \) "$@"
}

# Снимает с каталога отчёта (верхнего уровня build/router, имя отчёта или старого формата)
# всё тяжёлое и пустые каталоги. tidy_thin <корень> <каталог>
tidy_thin() {
  _tlt_root=$1
  _tlt_d=${2%/}
  case "$_tlt_d" in
    "$_tlt_root"/build/router/?*) ;;
    *)
      _tl_refuse "$_tlt_d" "утончать можно только отчёты в build/router"
      return 1
      ;;
  esac
  _tlt_n=${_tlt_d#"$_tlt_root"/build/router/}
  case "$_tlt_n" in
    */* | bin | release | last | session-last | . | ..)
      _tl_refuse "build/router/$_tlt_n" "не отчёт"
      return 1
      ;;
  esac
  if [ ! -d "$_tlt_d" ] || [ -L "$_tlt_d" ]; then
    _tl_refuse "build/router/$_tlt_n" "не каталог"
    return 1
  fi
  _tlt_pc=0
  _tl_check_parent "$_tlt_root" "$_tlt_d" || _tlt_pc=$?
  if [ "$_tlt_pc" != 0 ]; then
    _tl_refuse "build/router/$_tlt_n" "каталог выходит за корень через ссылку"
    return 1
  fi
  _tl_find_heavy "$_tlt_d" -exec rm -f {} +
  find "$_tlt_d" -depth -mindepth 1 -type d -empty -delete
}

# Запись плана для одного пути: _tl_emit1 <действие> <категория> <путь> <примечание> [<КиБ>]
_tl_emit1() {
  _tl1_k=${5:-}
  if [ -z "$_tl1_k" ]; then
    _tl1_k=0
    if [ "${TIDY_SIZES:-0}" = 1 ]; then
      _tl1_k=$(du -sk -- "$3" 2>/dev/null | awk '{ print $1 }')
    fi
  fi
  printf '%s\t%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "${_tl1_k:-0}" "$4"
}

# Размер группы (КиБ): _tl_group_kb <каталог> <ключ> [heavy]; heavy — только тяжёлое.
_tl_group_kb() {
  if [ "${TIDY_SIZES:-0}" != 1 ]; then
    echo 0
    return 0
  fi
  _tlg_list=$(mktemp)
  _tl_members "$1" "$2" >"$_tlg_list"
  if [ "${3:-}" = heavy ]; then
    while IFS= read -r _tlg_m; do
      if [ -d "$_tlg_m" ] && [ ! -L "$_tlg_m" ]; then
        _tl_find_heavy "$_tlg_m" -exec du -k {} +
      fi
    done <"$_tlg_list" | awk '{ s += $1 } END { print s + 0 }'
  elif [ -s "$_tlg_list" ]; then
    xargs -d '\n' du -sk -- <"$_tlg_list" 2>/dev/null | awk '{ s += $1 } END { print s + 0 }'
  else
    echo 0
  fi
  rm -f "$_tlg_list"
}

# В группе есть тяжёлые файлы (код 0) или нет (1): _tl_group_has_heavy <каталог> <ключ>
_tl_group_has_heavy() {
  _tlq_list=$(mktemp)
  _tlq_r=1
  _tl_members "$1" "$2" >"$_tlq_list"
  while IFS= read -r _tlq_m; do
    if [ -d "$_tlq_m" ] && [ ! -L "$_tlq_m" ] && [ -n "$(_tl_find_heavy "$_tlq_m" -print -quit)" ]; then
      _tlq_r=0
      break
    fi
  done <"$_tlq_list"
  rm -f "$_tlq_list"
  return "$_tlq_r"
}

# Защищённые ключи групп отчётов: строки <ключ><TAB><причина>.
# Цели ссылок last и session-last, текущий прогон (RT_REPORT, RT_REPORT_DIR),
# ссылки из pending todo основной копии, XCP_TIDY_REPORTS_KEEP новейших групп.
tidy_protected_keys() {
  _tlp_root=$1
  _tlp_rd=$_tlp_root/build/router
  for _tlp_l in last session-last; do
    if [ -L "$_tlp_rd/$_tlp_l" ]; then
      _tlp_k=$(tidy_report_key "$(basename "$(readlink "$_tlp_rd/$_tlp_l")")")
      if [ -n "$_tlp_k" ]; then
        printf '%s\tцель %s\n' "$_tlp_k" "$_tlp_l"
      fi
    fi
  done
  if [ -d "$_tlp_rd" ]; then
    _tlp_rr=$(cd "$_tlp_rd" && pwd -P)
    for _tlp_v in "${RT_REPORT:-}" "${RT_REPORT_DIR:-}"; do
      [ -n "$_tlp_v" ] || continue
      case "$_tlp_v" in
        /*) ;;
        *) _tlp_v=$_tlp_root/$_tlp_v ;;
      esac
      _tlp_d=$(cd "$_tlp_v" 2>/dev/null && pwd -P) || continue
      case "$_tlp_d" in
        "$_tlp_rr"/?*)
          _tlp_rel=${_tlp_d#"$_tlp_rr"/}
          _tlp_k=$(tidy_report_key "${_tlp_rel%%/*}")
          if [ -n "$_tlp_k" ]; then
            printf '%s\tтекущий прогон\n' "$_tlp_k"
          fi
          ;;
      esac
    done
  fi
  _tlp_td=$(rt_main_root)/.planning/todos/pending
  if [ -d "$_tlp_td" ]; then
    grep -rHEo --include='*.md' "$_TL_KEY_RE" "$_tlp_td" 2>/dev/null |
      sed -E "s#^.*/([^/:]*)\\.md:(.*)\$#\\2${_TL_TAB}ссылка из todo \\1#" || true
  fi
  if [ -d "$_tlp_rd" ]; then
    LC_ALL=C ls -1A "$_tlp_rd" 2>/dev/null |
      sed -nE "s/^(${_TL_KEY_RE})(-[0-9]{2}-.+)?\$/\\1/p" |
      LC_ALL=C sort -u | tail -n "${XCP_TIDY_REPORTS_KEEP:-10}" |
      awk -v n="${XCP_TIDY_REPORTS_KEEP:-10}" '{ print $0 "\t" n " новейших" }'
  fi
}

# Записи плана для отчётов (категория «отчёты») по группам <ключ> + <ключ>-NN-*.
# Защищённая группа старше тяжёлой отсечки — одна запись skip с причиной; незащищённая
# старше XCP_TIDY_REPORT_LIGHT_DAYS — rm целиком; старше XCP_TIDY_REPORT_HEAVY_DAYS и
# с тяжёлыми файлами — thin. Защита любого члена защищает группу.
tidy_plan_reports() {
  _tlr_root=$1
  _tlr_rd=$_tlr_root/build/router
  [ -d "$_tlr_rd" ] || return 0
  _tl_num XCP_TIDY_REPORT_LIGHT_DAYS "${XCP_TIDY_REPORT_LIGHT_DAYS:-30}" 1 || return 2
  _tl_num XCP_TIDY_REPORT_HEAVY_DAYS "${XCP_TIDY_REPORT_HEAVY_DAYS:-3}" 1 || return 2
  _tl_num XCP_TIDY_REPORTS_KEEP "${XCP_TIDY_REPORTS_KEEP:-10}" 1 || return 2
  _tl_num XCP_TIDY_LIGHT_MAX_KB "${XCP_TIDY_LIGHT_MAX_KB:-512}" 0 || return 2
  _tlr_prot=$(mktemp)
  _tlr_list=$(mktemp)
  _tlr_cls=$(mktemp)
  tidy_protected_keys "$_tlr_root" >"$_tlr_prot"
  LC_ALL=C ls -1A "$_tlr_rd" | sed -nE "s/^(${_TL_KEY_RE})(-[0-9]{2}-.+)?\$/\\1${_TL_TAB}&/p" >"$_tlr_list"
  _tlr_cl=$(date -u -d "${XCP_TIDY_REPORT_LIGHT_DAYS:-30} days ago" +%Y%m%d-%H%M%S)
  _tlr_ch=$(date -u -d "${XCP_TIDY_REPORT_HEAVY_DAYS:-3} days ago" +%Y%m%d-%H%M%S)
  awk -v cl="$_tlr_cl" -v ch="$_tlr_ch" '
    BEGIN { FS = "\t" }
    FILENAME == ARGV[1] { if (!($1 in reason)) reason[$1] = $2; next }
    { key = $1
      if (!(key in seen)) { seen[key] = 1; order[++n] = key }
      if ($2 != key) nested[key]++ }
    END {
      for (i = 1; i <= n; i++) {
        k = order[i]; ts = substr(k, 1, 15)
        if (k in reason) { if (ts < ch) print "skip\t" k "\t" reason[k] "\t" nested[k] + 0 }
        else if (ts < cl) print "rm\t" k "\t-\t" nested[k] + 0
        else if (ts < ch) print "thinchk\t" k "\t-\t" nested[k] + 0
      }
    }' "$_tlr_prot" "$_tlr_list" >"$_tlr_cls"
  while IFS="$_TL_TAB" read -r _tlr_c _tlr_k _tlr_why _tlr_nest; do
    _tlr_note=""
    if [ "${_tlr_nest:-0}" -gt 0 ]; then
      _tlr_note="+$_tlr_nest вложенных"
    fi
    case "$_tlr_c" in
      skip)
        _tl_emit1 skip отчёты "$_tlr_rd/$_tlr_k" "$_tlr_why" 0
        ;;
      rm)
        _tl_emit1 rm отчёты "$_tlr_rd/$_tlr_k" "$_tlr_note" "$(_tl_group_kb "$_tlr_rd" "$_tlr_k")"
        ;;
      thinchk)
        if _tl_group_has_heavy "$_tlr_rd" "$_tlr_k"; then
          _tl_emit1 thin отчёты "$_tlr_rd/$_tlr_k" "${_tlr_note:-тяжёлое старше ${XCP_TIDY_REPORT_HEAVY_DAYS:-3} дн.}" "$(_tl_group_kb "$_tlr_rd" "$_tlr_k" heavy)"
        fi
        ;;
    esac
  done <"$_tlr_cls"
  rm -f "$_tlr_prot" "$_tlr_list" "$_tlr_cls"
}

# --- старый формат build/router ---------------------------------------------------------

# Имена верхнего уровня build/router, не относящиеся ни к служебным (bin, release, last,
# session-last), ни к отчётам нового формата: dbg*, session-2026…, скрытые .session-*.
_tl_legacy_names() {
  LC_ALL=C ls -1A "$1" 2>/dev/null |
    sed -E "/^${_TL_KEY_RE}(-[0-9]{2}-.+)?\$/d" |
    grep -vxE 'bin|release|last|session-last' || true
}

# Записи плана старого формата (категория «старый формат build/router») по mtime тем же
# двухуровневым правилом: старше лёгкой отсечки — rm, старше тяжёлой и с тяжёлыми
# файлами — thin. Цели ссылок last и session-last — skip. Висячие last и session-last — rm.
tidy_plan_legacy() {
  _tll_root=$1
  _tll_rd=$_tll_root/build/router
  [ -d "$_tll_rd" ] || return 0
  _tl_num XCP_TIDY_REPORT_LIGHT_DAYS "${XCP_TIDY_REPORT_LIGHT_DAYS:-30}" 1 || return 2
  _tl_num XCP_TIDY_REPORT_HEAVY_DAYS "${XCP_TIDY_REPORT_HEAVY_DAYS:-3}" 1 || return 2
  _tl_num XCP_TIDY_LIGHT_MAX_KB "${XCP_TIDY_LIGHT_MAX_KB:-512}" 0 || return 2
  _tll_cat="старый формат build/router"
  _tll_tsl=""
  _tll_tl=""
  if [ -L "$_tll_rd/session-last" ]; then
    _tll_tsl=$(basename "$(readlink "$_tll_rd/session-last")")
  fi
  if [ -L "$_tll_rd/last" ]; then
    _tll_tl=$(basename "$(readlink "$_tll_rd/last")")
  fi
  _tll_o30=$(mktemp)
  _tll_o3=$(mktemp)
  _tll_names=$(mktemp)
  find "$_tll_rd" -mindepth 1 -maxdepth 1 -mtime +"${XCP_TIDY_REPORT_LIGHT_DAYS:-30}" >"$_tll_o30"
  find "$_tll_rd" -mindepth 1 -maxdepth 1 -mtime +"${XCP_TIDY_REPORT_HEAVY_DAYS:-3}" >"$_tll_o3"
  _tl_legacy_names "$_tll_rd" >"$_tll_names"
  while IFS= read -r _tll_n; do
    _tll_p=$_tll_rd/$_tll_n
    if [ "$_tll_n" = "$_tll_tsl" ]; then
      _tl_emit1 skip "$_tll_cat" "$_tll_p" "цель session-last" 0
    elif [ "$_tll_n" = "$_tll_tl" ]; then
      _tl_emit1 skip "$_tll_cat" "$_tll_p" "цель last" 0
    elif grep -qxF -- "$_tll_p" "$_tll_o30"; then
      _tl_emit1 rm "$_tll_cat" "$_tll_p" "старше ${XCP_TIDY_REPORT_LIGHT_DAYS:-30} дн."
    elif grep -qxF -- "$_tll_p" "$_tll_o3" && [ -d "$_tll_p" ] && [ ! -L "$_tll_p" ] &&
      [ -n "$(_tl_find_heavy "$_tll_p" -print -quit)" ]; then
      _tl_emit1 thin "$_tll_cat" "$_tll_p" "тяжёлое старше ${XCP_TIDY_REPORT_HEAVY_DAYS:-3} дн." \
        "$(_tl_heavy_kb "$_tll_p")"
    fi
  done <"$_tll_names"
  for _tll_l in last session-last; do
    if [ -L "$_tll_rd/$_tll_l" ] && [ ! -e "$_tll_rd/$_tll_l" ]; then
      _tl_emit1 rm "$_tll_cat" "$_tll_rd/$_tll_l" "висячая ссылка" 0
    fi
  done
  rm -f "$_tll_o30" "$_tll_o3" "$_tll_names"
}

# Размер тяжёлого в каталоге (КиБ): _tl_heavy_kb <каталог>
_tl_heavy_kb() {
  if [ "${TIDY_SIZES:-0}" = 1 ]; then
    _tl_find_heavy "$1" -exec du -k {} + | awk '{ s += $1 } END { print s + 0 }'
  else
    echo 0
  fi
}

# Для --drop-legacy: rm на каждую запись старого формата, включая цель session-last и
# саму ссылку session-last, если она указывает на старый формат или висит. Служебные bin,
# release, last и группы отчётов не затрагиваются.
tidy_plan_legacy_all() {
  _tld_root=$1
  _tld_rd=$_tld_root/build/router
  [ -d "$_tld_rd" ] || return 0
  _tld_cat="старый формат build/router"
  _tld_names=$(mktemp)
  _tl_legacy_names "$_tld_rd" >"$_tld_names"
  while IFS= read -r _tld_n; do
    _tl_emit1 rm "$_tld_cat" "$_tld_rd/$_tld_n" "разовое: --drop-legacy"
  done <"$_tld_names"
  if [ -L "$_tld_rd/session-last" ]; then
    _tld_t=$(basename "$(readlink "$_tld_rd/session-last")")
    if [ ! -e "$_tld_rd/session-last" ] || grep -qxF -- "$_tld_t" "$_tld_names"; then
      _tl_emit1 rm "$_tld_cat" "$_tld_rd/session-last" "разовое: --drop-legacy" 0
    fi
  fi
  if [ -L "$_tld_rd/last" ]; then
    # как у session-last: ссылка на уходящий старый формат удаляется вместе с целью, иначе повиснет
    _tld_t=$(basename "$(readlink "$_tld_rd/last")")
    if [ ! -e "$_tld_rd/last" ] || grep -qxF -- "$_tld_t" "$_tld_names"; then
      _tl_emit1 rm "$_tld_cat" "$_tld_rd/last" "разовое: --drop-legacy" 0
    fi
  fi
  rm -f "$_tld_names"
}

# --- кэш релизов -----------------------------------------------------------------------

# Записи rm для build/router/release/<тег>: кроме XCP_TIDY_RELEASES новейших тегов по
# версии (пре-релиз раньше финального) и защищённого тега.
# tidy_plan_releases <корень> [<защищённый тег>]
tidy_plan_releases() {
  _tlv_root=$1
  _tlv_prot=${2:-}
  _tlv_d=$_tlv_root/build/router/release
  [ -d "$_tlv_d" ] || return 0
  _tlv_keep=${XCP_TIDY_RELEASES:-3}
  _tl_num XCP_TIDY_RELEASES "$_tlv_keep" 1 || return 2
  ls -1 "$_tlv_d" 2>/dev/null | grep -E '^v[0-9]' | sed 's/-/~/' | sort -V | sed 's/~/-/' |
    awk -v k="$_tlv_keep" -v d="$_tlv_d" '{ a[NR] = $0 } END { for (i = 1; i <= NR - k; i++) print d "/" a[i] }' |
    grep -vxF -- "$_tlv_d/$_tlv_prot" |
    _tl_emit_paths rm "кэш релизов" "старше $_tlv_keep новейших тегов"
}

# --- ротация для скриптов стенда (без du) ----------------------------------------------

# Ротация сборок: план и исполнение.
tidy_rotate_builds() {
  tidy_plan_builds "$1" | tidy_exec "$1"
}

# Ротация отчётов и старого формата build/router (без --drop-legacy).
tidy_rotate_reports() {
  {
    tidy_plan_reports "$1"
    tidy_plan_legacy "$1"
  } | tidy_exec "$1"
}

# Ротация кэша релизов: tidy_rotate_releases <корень> <защищённый тег>
tidy_rotate_releases() {
  tidy_plan_releases "$1" "${2:-}" | tidy_exec "$1"
}
