# router-watch — ход роутерного прогона в Claude Code

Плагин Claude Code (мод с хуками) показывает ход прогона на стенде рядом с чатом. Панель «Прогон стенда» содержит:

- текущий этап;
- тесты идущего Go- или Playwright-запуска (n из N, текущий тест);
- упавшие тесты, по мере падения;
- прошедшее время и оставшееся по оценке;
- итог законченного прогона.

Пока прогон идёт, та же сводка в одну строку видна в строке состояния. Панель открывается сама, когда прогон начинается; вручную — командой `/router-watch`.

Данные берёт из `node scripts/router/progress.mjs --json` рабочей копии сессии (раз в 3 с), то есть из журнала `build/router/<отчёт>/progress.tsv`. В проекте без `scripts/router/progress.mjs` опроса нет. Без Claude Code тот же ход виден в терминале: `make router-watch`.

## Установка

Плагин ставится из копии вне рабочей копии: при переключении на ветку без этого каталога установленный плагин не пропадёт. Из корня репозитория, в терминале WSL:

```bash
mkdir -p ~/.claude/local-plugins
rsync -a --delete --exclude .claude-plugin/types tools/router-watch-plugin/ ~/.claude/local-plugins/router-watch/
claude plugin marketplace add ~/.claude/local-plugins/router-watch
claude plugin install router-watch@local-router-tools --scope user
```

Плагин загружается в новых сессиях Claude Code, в том числе во вкладке Code приложения.

## Обновление

После правки файлов в `tools/router-watch-plugin/`:

```bash
rsync -a --delete --exclude .claude-plugin/types tools/router-watch-plugin/ ~/.claude/local-plugins/router-watch/
claude plugin marketplace update local-router-tools
```

Затем открыть новую сессию.

## Проверка

```bash
claude plugin validate tools/router-watch-plugin
claude plugin list
```

Каталог `.claude-plugin/types/` (типы API и `tsconfig.json`, от которого наследует `tsconfig.json` плагина) движок пишет сам при загрузке плагина; в репозиторий он не попадает.
