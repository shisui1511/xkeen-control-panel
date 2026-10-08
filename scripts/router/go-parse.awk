# scripts/router/go-parse.awk — разбор потока `go tool test2json` (go-suite.sh).
#
# Вход: JSON-строки test2json. Параметр: -v pkg=<пакет>.
# Выход: строки «статус<TAB>go:<Тест><TAB>деталь» (PASS, FAIL, SKIP, KNOWN, XPASS).
# KNOWN и XPASS определяются по маркерам «KNOWN-FAILURE slug=» и «XPASS slug=» в выводе
# теста (internal/routertest). Родитель, упавший из-за подтеста, отдельной строкой не
# выводится: падение несёт подтест.

function unesc(s) {
  gsub(/\\n/, " ", s); gsub(/\\t/, " ", s); gsub(/\\"/, "\"", s); gsub(/\\\\/, "\\", s)
  return s
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
    sub(/"\}$/, "", text)
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
    if (s == "pass") {
      if (t in kn) printf "KNOWN\tgo:%s\t%s [известное падение: %s]\n", t, d, kn[t]
      else printf "PASS\tgo:%s\t\n", t
    } else if (s == "skip") {
      printf "SKIP\tgo:%s\t%s\n", t, d
    } else if (s == "fail") {
      if (t in xp) printf "XPASS\tgo:%s\tметка больше не нужна: снять и закрыть todo %s\n", t, xp[t]
      else if (!(t in badchild)) printf "FAIL\tgo:%s\t%s\n", t, d
    } else {
      printf "FAIL\tgo:%s\tтест не завершился: %s %s\n", t, d, pkgout
      anyfail = 1
    }
    if (s == "fail" && !(t in badchild) && !(t in xp)) anyfail = 1
    if (s == "fail" && (t in xp)) anyfail = 1
  }
  if (pkgfail && !anyfail) printf "FAIL\tgo:%s\tпакет завершился ошибкой: %s\n", pkg, pkgout
}
