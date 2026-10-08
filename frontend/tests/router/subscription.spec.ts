// e2e-pages: proxies
import { test, expect } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';

// Обновление настоящей подписки через интерфейс (RT-05): страница «Прокси», вкладка подписок,
// кнопка обновления на карточке. Подписка уже добавлена на стенде; обновление идёт к настоящему
// провайдеру. Условие успеха: нет уведомления об ошибке и баннера отказа устройства, кнопка
// снова доступна, число узлов в карточке больше нуля.
//
// D-21: в вывод попадает только число узлов. Имена узлов, адреса подписки и содержимое страницы
// подписок в вывод и в вложения теста не попадают (вложений testInfo у этого спека нет;
// трассы и снимки при падении остаются только в build/router/).

test(`subscription-refresh core:${RT.core || 'unknown'}`, async ({ page }) => {
  // обновление ходит к провайдеру и пересобирает конфиги: на медленном устройстве это минуты
  const ACTION = 60_000 * RT.slow;
  const cards = page.locator('.subscriptions-list .sub-card');

  test.setTimeout(T.page + 6 * ACTION);
  const guard = attachConsoleGuard(page);
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
    await refresh.click();

    // обновление закончено, когда пришло уведомление (успех, предупреждение или ошибка)
    await expect(
      page.locator('.toast--success, .toast--warning, .toast--error').first(),
      `${label}: нет уведомления об итоге обновления`
    ).toBeVisible({ timeout: ACTION });
    await expect(refresh, `${label}: кнопка обновления осталась занятой`).toBeEnabled({
      timeout: ACTION
    });

    // ошибки: уведомление, баннер отказа устройства, сообщение об ошибке на карточке
    await expect(page.locator('.toast--error'), `${label}: уведомление об ошибке`).toHaveCount(0);
    await expect(
      card.getByTestId('device-rejected-banner'),
      `${label}: провайдер отклонил устройство`
    ).toHaveCount(0);
    await expect(card.locator('.sub-error-details'), `${label}: ошибка на карточке`).toHaveCount(0);

    const text = (await badge.first().textContent())?.trim() ?? '';
    const nodes = Number(text);
    expect(Number.isFinite(nodes), `${label}: число узлов не число`).toBe(true);
    expect(nodes, `${label}: после обновления нет узлов`).toBeGreaterThan(0);
    console.log(`subscription-refresh: ${label}, узлов ${nodes}`);
  }

  await guard.assertClean();
});
