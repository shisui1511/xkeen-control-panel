# scripts/tidy-lib.sh — функции уборки build/ на ПК: план и исполнение ротации.
#
# Подключается через `. "$SCRIPT_DIR/tidy-lib.sh"` после scripts/router/lib.sh
# (нужны rt_repo_root, rt_common_dir, rt_main_root) из scripts/tidy.sh и скриптов стенда.
# POSIX sh; сам ничего не запускает и `set` не меняет. Функции — tidy_*, внутренние
# переменные — с префиксом _tl.
#
# Пороги (переменные окружения, в скобках умолчания):
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
  if [ "$_tls_ok" != 1 ]; then
    _tl_refuse "$_tls_rel" "не относится к уборке"
    return 1
  fi
  # родитель должен лежать внутри корня и после разыменования (ссылка на чужой каталог)
  _tls_rr=$(cd "$_tls_root" 2>/dev/null && pwd -P) || {
    _tl_refuse "$_tls_rel" "корень недоступен"
    return 1
  }
  _tls_par=$(cd "$(dirname "$_tls_p")" 2>/dev/null && pwd -P) || return 0
  case "$_tls_par" in
    "$_tls_rr" | "$_tls_rr"/*) ;;
    *)
      _tl_refuse "$_tls_rel" "каталог выходит за корень через ссылку"
      return 1
      ;;
  esac
  if [ ! -e "$_tls_p" ] && [ ! -L "$_tls_p" ]; then
    return 0
  fi
  rm -rf -- "$_tls_p"
}

# Записи rm для сборок build/xcp_v*_<арх>: кроме XCP_TIDY_BUILDS новейших на архитектуру.
# Маска по хвосту: *_mips не совпадает с *_mipsle.
tidy_plan_builds() {
  _tlb_root=$1
  _tlb_keep=${XCP_TIDY_BUILDS:-5}
  for _tlb_a in arm64 mipsle mips; do
    ls -t -- "$_tlb_root"/build/xcp_v*_"$_tlb_a" 2>/dev/null |
      while IFS= read -r _tlb_f; do
        if [ -f "$_tlb_f" ] && [ ! -L "$_tlb_f" ]; then
          printf '%s\n' "$_tlb_f"
        fi
      done |
      sed -n "$((_tlb_keep + 1)),\$p" |
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

# Одна операция rm/thin: _tl_apply <действие> <корень> <категория> <путь>
_tl_apply() {
  case "$1" in
    rm) tidy_safe_rm "$2" "$4" ;;
    *) return 1 ;;
  esac
}

# Ротация сборок: план и исполнение для скриптов стенда (без du).
tidy_rotate_builds() {
  tidy_plan_builds "$1" | tidy_exec "$1"
}
