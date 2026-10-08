import type { ConsoleMessage, Page, Request, Response } from '@playwright/test';

// Проверка консоли и сети (D-14). Подключается ДО перехода на страницу, чтобы ранние
// ошибки не терялись, и собирает только нарушения:
//   - console.error и pageerror;
//   - ответы со статусом 5xx;
//   - оборванные запросы (requestfailed).
// Предупреждения и 4xx не считаются (ожидаемые 409 от гейта ядра и подобные отказы).
// Списков исключений нет: известное падение размечается меткой knownFailure с todo.
//
// Запрос, оборванный самим браузером при уходе со страницы, не нарушение: assertClean
// вызывается до навигации, и после неё сборщики отключаются.

// Chrome дублирует каждый ответ 4xx строкой console.error «Failed to load resource: the
// server responded with a status of 4xx». Статус отражает собственное правило D-14 для
// ответов (4xx не считаются, 5xx ловит сборщик response), поэтому дубль не складывается
// в нарушения второй раз. Любой другой текст console.error — нарушение.
const RESOURCE_4XX = /^Failed to load resource: the server responded with a status of 4\d\d\b/;

export interface ConsoleGuard {
  /** Падает со списком нарушений, затем отключает сборщики. */
  assertClean(): Promise<void>;
  /** Отключает сборщики (перед уходом со страницы). */
  detach(): void;
}

function pathOf(url: string): string {
  try {
    const u = new URL(url);
    return u.pathname + u.search;
  } catch {
    return url;
  }
}

export function attachConsoleGuard(page: Page): ConsoleGuard {
  const violations: string[] = [];

  const onConsole = (msg: ConsoleMessage) => {
    if (msg.type() !== 'error') return;
    if (RESOURCE_4XX.test(msg.text())) return;
    violations.push(`console.error: ${msg.text()}`);
  };
  const onPageError = (err: Error) => {
    violations.push(`pageerror: ${err.message}`);
  };
  const onResponse = (res: Response) => {
    if (res.status() >= 500) {
      violations.push(`HTTP ${res.status()} ${res.request().method()} ${pathOf(res.url())}`);
    }
  };
  const onRequestFailed = (req: Request) => {
    const why = req.failure()?.errorText ?? 'без причины';
    violations.push(`requestfailed ${req.method()} ${pathOf(req.url())}: ${why}`);
  };

  page.on('console', onConsole);
  page.on('pageerror', onPageError);
  page.on('response', onResponse);
  page.on('requestfailed', onRequestFailed);

  const detach = () => {
    page.off('console', onConsole);
    page.off('pageerror', onPageError);
    page.off('response', onResponse);
    page.off('requestfailed', onRequestFailed);
  };

  return {
    detach,
    async assertClean() {
      // отложенные события консоли успевают дойти до Node
      await page.evaluate(() => new Promise<void>((r) => setTimeout(r, 0))).catch(() => {});
      detach();
      if (violations.length > 0) {
        throw new Error(
          `консоль и сеть не чисты (${violations.length}):\n- ${violations.join('\n- ')}`
        );
      }
    }
  };
}
