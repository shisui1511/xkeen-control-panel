# scripts/router/go-parse.awk — разбор потока `go tool test2json` (go-suite.sh).
#
# Вход: JSON-строки test2json. Параметр: -v pkg=<пакет>.
# Выход: строки «статус<TAB>go:<Тест><TAB>деталь» (PASS, FAIL, SKIP, KNOWN, XPASS, QPASS,
# QFAIL). KNOWN и XPASS определяются по маркерам «KNOWN-FAILURE slug=» и «XPASS slug=» в
# выводе теста (internal/routertest). Родитель, упавший из-за подтеста, отдельной строкой
# не выводится: падение несёт подтест.
#
# Карантин (routertest.Quarantine): тест с маркером «QUARANTINE slug=» в начале строки t.Logf
# («<файл>.go:<строка>: QUARANTINE slug=<slug>») и все его подтесты
# дают QPASS при прохождении и QFAIL при падении (вместо PASS и FAIL); деталь называет
# slug. QFAIL на итог не влияет: go-suite.sh считает в BAD только FAIL и XPASS, а строка
# «пакет завершился ошибкой» не выводится, если ошибку пакета объясняют падения в карантине.

function unesc(s) {
  gsub(/\\n/, " ", s); gsub(/\\t/, " ", s); gsub(/\\"/, "\"", s); gsub(/\\\\/, "\\", s)
  return s
}
# slug карантина теста: маркер у самого теста или у ближайшего предка; "" — не в карантине
function qslug(t,    q) {
  q = t
  while (1) {
    if (q in qr) return qr[q]
    if (!sub(/\/[^\/]*$/, "", q)) return ""
  }
}
function field(re, skip, tail,    r) {
  if (match($0, re)) return substr($0, RSTART + skip, RLENGTH - skip - tail)
  return ""
}
{
  action = field("\"Action\":\"[a-z]+\"", 10, 1)
  test = field("\"Test\":\"[^\"]*\"", 8, 1)
  text = ""
  p = index($0, "\"Output\":\"")
  if (p > 0) {
    text = substr($0, p + 10)
    # новые версии Go добавляют после Output поле OutputType
    q = index(text, "\",\"OutputType\"")
    if (q > 0) text = substr(text, 1, q - 1)
    else sub(/"\}$/, "", text)
    text = unesc(text)
  }
  if (test == "") {
    if (action == "fail") pkgfail = 1
    if (action == "output" && length(pkgout) < 300 && text !~ /^(FAIL|PASS|ok)/ ) pkgout = pkgout text
    next
  }
  if (!(test in seen)) { seen[test] = 1; order[n++] = test }
  if (action == "pass" || action == "fail" || action == "skip") st[test] = action
  if (action == "output") {
    if (match(text, /KNOWN-FAILURE slug=[a-z0-9-]+/)) kn[test] = substr(text, RSTART + 19, RLENGTH - 19)
    if (match(text, /XPASS slug=[a-z0-9-]+/)) xp[test] = substr(text, RSTART + 11, RLENGTH - 11)
    # маркер карантина — только строка t.Logf самого routertest.Quarantine («<файл>.go:<строка>:
    # QUARANTINE slug=…» в начале вывода), а не такая подстрока где-то в логе теста
    if (match(text, /^ *[A-Za-z0-9_.-]+\.go:[0-9]+: QUARANTINE slug=[a-z0-9][a-z0-9-]*/)) {
      qt = substr(text, RSTART, RLENGTH)
      sub(/^.*QUARANTINE slug=/, "", qt)
      qr[test] = qt
    }
    if (text !~ /^(=== |--- |PASS|FAIL|ok)/ && length(out[test]) < 400) out[test] = out[test] text
  }
}
END {
  for (i = 0; i < n; i++) {
    t = order[i]
    if (st[t] == "fail") {
      q = t
      while (sub(/\/[^\/]*$/, "", q)) badchild[q] = 1
    }
  }
  for (i = 0; i < n; i++) {
    t = order[i]
    s = st[t]
    d = out[t]
    gsub(/^ +| +$/, "", d)
    qs = qslug(t)
    if (s == "pass") {
      if (t in kn) printf "KNOWN\tgo:%s\t%s [известное падение: %s]\n", t, d, kn[t]
      else if (qs != "") printf "QPASS\tgo:%s\tкарантин: %s\n", t, qs
      else printf "PASS\tgo:%s\t\n", t
    } else if (s == "skip") {
      printf "SKIP\tgo:%s\t%s\n", t, d
    } else if (s == "fail") {
      if (t in xp) printf "XPASS\tgo:%s\tметка больше не нужна: снять и закрыть todo %s\n", t, xp[t]
      else if (!(t in badchild) && qs != "") printf "QFAIL\tgo:%s\tкарантин: %s: %s\n", t, qs, d
      else if (!(t in badchild)) printf "FAIL\tgo:%s\t%s\n", t, d
    } else if (qs != "") {
      printf "QFAIL\tgo:%s\tкарантин: %s: тест не завершился: %s %s\n", t, qs, d, pkgout
      anyqfail = 1
    } else {
      printf "FAIL\tgo:%s\tтест не завершился: %s %s\n", t, d, pkgout
      anyfail = 1
    }
    if (s == "fail" && !(t in badchild) && !(t in xp) && qs == "") anyfail = 1
    if (s == "fail" && !(t in xp) && qs != "") anyqfail = 1
    if (s == "fail" && (t in xp)) anyfail = 1
  }
  # ошибку пакета, которую объясняют только падения в карантине, отдельно не выводить
  if (pkgfail && !anyfail && !anyqfail) printf "FAIL\tgo:%s\tпакет завершился ошибкой: %s\n", pkg, pkgout
}
