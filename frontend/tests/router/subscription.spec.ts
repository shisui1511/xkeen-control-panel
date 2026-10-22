// e2e-pages: proxies
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test, expect, type Request } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { CORE_TAG } from './lib/known';

// Обновление настоящей подписки через интерфейс (RT-05): страница «Прокси», вкладка подписок,
// кнопка обновления на карточке. Подписка уже добавлена на стенде; обновление идёт к настоящему
// провайдеру. Условие успеха: все ответы обновления 2xx, нет уведомления об ошибке, баннера
// отказа устройства и ошибки на карточке, кнопка снова доступна, число узлов в карточке больше нуля.
//
// Вердикт детерминирован (три исхода):
//   - PASS — условие успеха выполнено;
//   - SKIP с причиной «провайдер подписки недоступен: <класс>» — есть ошибка на карточке, и она
//     относится к закрытому списку классов недоступного провайдера (тайм-аут, соединение, DNS,
//     HTTP 5xx провайдера, «сервер подписки недоступен»); причина видна в results.tsv;
//   - FAIL — любой другой отказ: ответ не 2xx или уведомление об ошибке без такого класса.
//
// D-21: в вывод попадает только число узлов. Имена узлов, адреса подписки и содержимое страницы
// подписок в вывод и в вложения теста не попадают (вложений testInfo у этого спека нет;
// трассы и снимки при падении остаются только в build/router/).
// D-33: сырой текст ошибки карточки, URL, домены и имена провайдеров в сообщения и вывод не
// попадают: печатается только класс ошибки, методы, пути без имён, статусы и секунды.

const LOCALES = ['ru', 'en'].map((lang) =>
  fileURLToPath(new URL(`../../src/locales/${lang}.json`, import.meta.url))
);

/** Переводы ключа subscr.error_upstream_unreachable читаются из файлов локалей при загрузке спека. */
function upstreamUnreachableTexts(): string[] {
  const found: string[] = [];
  for (const file of LOCALES) {
    const dict = JSON.parse(readFileSync(file, 'utf8')) as Record<string, string>;
    const text = dict['subscr.error_upstream_unreachable'];
    if (!text) throw new Error(`в ${file} нет ключа subscr.error_upstream_unreachable`);
    found.push(text);
  }
  return found;
}

const UPSTREAM_TEXTS = upstreamUnreachableTexts();

/**
 * Класс недоступного провайдера по тексту ошибки на карточке; null — ошибка не из закрытого
 * списка (отказ панели или неизвестная причина). Сам текст дальше не передаётся (D-33).
 */
function providerDownClass(text: string): string | null {
  const masked = text.replace(/https?:\/\/\S+/g, '<url>');
  if (UPSTREAM_TEXTS.some((u) => masked.includes(u))) return 'сервер подписки недоступен';
  if (/upstream (unavailable|unreachable)/i.test(masked)) return 'сервер подписки недоступен';
  if (/context deadline exceeded|Client\.Timeout|i\/o timeout|TLS handshake timeout/i.test(masked))
    return 'тайм-аут';
  if (/connection refused|connection reset|\bEOF\b/i.test(masked)) return 'соединение';
  if (/no such host|server misbehaving/i.test(masked)) return 'DNS';
  if (/\bHTTP 5\d\d\b/.test(masked)) return 'HTTP 5xx провайдера';
  return null;
}

interface RefreshCall {
  method: string;
  path: string;
  xrayOnly: boolean;
  status: number | null;
  seconds: number | null;
}

function describeCalls(calls: RefreshCall[]): string {
  if (calls.length === 0) return 'запросов обновления нет';
  return calls
    .map(
      (c) =>
        `${c.method} ${c.path}${c.xrayOnly ? ' kernel=xray' : ''} ` +
        `${c.status ?? 'без ответа'}${c.seconds === null ? '' : ` за ${c.seconds.toFixed(1)} с`}`
    )
    .join(', ');
}

test(`subscription-refresh core:${RT.core || 'unknown'}`, { tag: CORE_TAG }, async ({ page }) => {
  // обновление ходит к провайдеру и пересобирает конфиги: на медленном устройстве это минуты
  const ACTION = 60_000 * RT.slow;
  const cards = page.locator('.subscriptions-list .sub-card');

  test.setTimeout(T.page + 6 * ACTION);
  const guard = attachConsoleGuard(page);

  // Запросы обновления, ушедшие за одно нажатие (сеть только наблюдается, не подменяется).
  // Одно нажатие — одна команда обновления провайдера Mihomo: PUT /api/proxy-providers/<имя>/refresh;
  // запрос Xray в этом случае идёт с kernel=xray и Mihomo не трогает.
  const calls: RefreshCall[] = [];
  const started = new Map<Request, { at: number; call: RefreshCall }>();
  page.on('request', (req) => {
    let u: URL;
    try {
      u = new URL(req.url());
    } catch {
      return;
    }
    let call: RefreshCall | null = null;
    if (u.pathname === '/api/subscriptions/refresh') {
      call = {
        method: req.method(),
        path: u.pathname,
        xrayOnly: u.searchParams.get('kernel') === 'xray',
        status: null,
        seconds: null
      };
    } else if (/^\/api\/proxy-providers\/[^/]+\/refresh$/.test(u.pathname)) {
      // имя провайдера в отчёт не попадает (D-21)
      call = {
        method: req.method(),
        path: '/api/proxy-providers/<имя>/refresh',
        xrayOnly: false,
        status: null,
        seconds: null
      };
    }
    if (!call) return;
    calls.push(call);
    started.set(req, { at: Date.now(), call });
  });
  page.on('response', (res) => {
    const s = started.get(res.request());
    if (!s) return;
    s.call.status = res.status();
    s.call.seconds = (Date.now() - s.at) / 1000;
  });

  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/#/proxies?tab=providers');

  await expect(cards.first(), 'на стенде нет ни одной подписки').toBeVisible({ timeout: T.page });
  const total = await cards.count();

  for (let i = 0; i < total; i++) {
    const card = cards.nth(i);
    const badge = card.locator('.nodes-count-badge');
    const refresh = card.locator('.sub-header-right button.action-icon-btn').first();
    const label = `подписка ${i + 1} из ${total}`;

    // прежние уведомления не должны засчитаться за результат этого обновления
    const closers = page.locator('.toast__close');
    for (let n = await closers.count(); n > 0; n--) {
      await closers
        .first()
        .click({ timeout: 2000 })
        .catch(() => {});
    }
    await expect(page.locator('.toast')).toHaveCount(0, { timeout: T.action });

    await expect(refresh, `${label}: кнопка обновления`).toBeEnabled({ timeout: T.action });
    calls.length = 0;
    started.clear();
    await refresh.click();

    // обновление закончено, когда пришло уведомление (успех, предупреждение или ошибка)
    await expect(
      page.locator('.toast--success, .toast--warning, .toast--error').first(),
      `${label}: нет уведомления об итоге обновления`
    ).toBeVisible({ timeout: ACTION });
    await expect(refresh, `${label}: кнопка обновления осталась занятой`).toBeEnabled({
      timeout: ACTION
    });

    // форма запросов нажатия: если ушёл PUT обновления провайдера Mihomo, то он один, а запрос
    // Xray идёт с kernel=xray (иначе панель сама шлёт Mihomo вторую команду)
    const puts = calls.filter((c) => c.method === 'PUT');
    const posts = calls.filter((c) => c.method === 'POST');
    const doubled = puts.length > 1 || (puts.length === 1 && posts.some((c) => !c.xrayOnly));
    expect(
      doubled,
      `${label}: одно нажатие — больше одной команды обновления провайдера Mihomo (${describeCalls(calls)})`
    ).toBe(false);

    // итог нажатия: ответы, уведомления об ошибке, ошибка на карточке — разовый подсчёт, без повторов
    const toastErrors = await page.locator('.toast--error').count();
    const errorBox = card.locator('.sub-error-details');
    const cardError =
      (await errorBox.count()) > 0 ? ((await errorBox.first().textContent()) ?? '') : '';
    const allOk = calls.length > 0 && calls.every((c) => c.status !== null && c.status < 300);

    if (!allOk || toastErrors > 0 || cardError.trim() !== '') {
      const cls = cardError.trim() === '' ? null : providerDownClass(cardError);
      if (cls) {
        // 5xx в этом случае — следствие недоступного провайдера, а не отказ панели
        guard.detach();
        test.skip(true, `провайдер подписки недоступен: ${cls}, статусы ${describeCalls(calls)}`);
      }
      const shown =
        cardError.trim() === ''
          ? 'ошибки на карточке нет'
          : `не распознана (${cardError.length} символов)`;
      expect(
        false,
        `${label}: обновление не удалось; ответы: ${describeCalls(calls)}; уведомлений об ошибке: ${toastErrors}; ошибка на карточке: ${shown}`
      ).toBe(true);
    }

    await expect(
      card.getByTestId('device-rejected-banner'),
      `${label}: провайдер отклонил устройство`
    ).toHaveCount(0);

    const text = (await badge.first().textContent())?.trim() ?? '';
    const nodes = Number(text);
    expect(Number.isFinite(nodes), `${label}: число узлов не число`).toBe(true);
    expect(nodes, `${label}: после обновления нет узлов`).toBeGreaterThan(0);
    console.log(`subscription-refresh: ${label}, узлов ${nodes}, ${describeCalls(calls)}`);
  }

  await guard.assertClean();
});
