// e2e-kind: read
// e2e-pages: rules proxies config/nodes
// router-area: routing
import AxeBuilder from '@axe-core/playwright';
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { CORE_TAG } from './lib/known';

// Страница «Правила» без API Mihomo (фаза 147.1, план 19, D-18…D-22): при остановленном
// Mihomo первой открывается вкладка «Провайдеры» по файлам профиля, «Диагностика» и «Все
// правила» видны и неактивны с постоянным пояснением, баннера «API недоступен» нет.
// Ответы сети не подменяются. Спек только читает: «Проверить URL» — безопасная проба
// ссылки, состояние стенда она не меняет. Сами случаи диагностики ссылки (через прокси,
// напрямую, оба не прошли) подтверждает TestRouterRuleProviderCheckURL на отдельном ядре.
// Имена и адреса провайдеров стенда в сообщения проверок не попадают.

const PROVIDER_CARD = '[data-testid^="provider-card-"]';

/** Страница «Правила» открыта и загружена: скелетона состояния ядра и списка провайдеров нет. */
async function openRules(page: Page, hash = '#/rules'): Promise<void> {
  await page.goto(`/${hash}`, { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('tab-providers')).toBeVisible({ timeout: T.page });
  await expect(page.locator('.rules-page .tab-content .skeleton')).toHaveCount(0, {
    timeout: T.page
  });
}

/** Вкладка «Провайдеры» выбрана и список прочитан (индикатора загрузки нет). */
async function providersReady(page: Page): Promise<void> {
  await expect(page.getByTestId('tab-providers')).toHaveAttribute('aria-selected', 'true', {
    timeout: T.page
  });
  await expect(page.locator('.providers-tab .loading-state')).toHaveCount(0, { timeout: T.page });
}

/** Прежний API Mihomo отвечает: по /api/capabilities (чтение). */
async function mihomoApiUp(page: Page): Promise<boolean> {
  const res = await page.request.get('/api/capabilities');
  if (!res.ok()) return false;
  const body = (await res.json()) as {
    data?: { mihomo?: { api_reachable?: boolean } };
    mihomo?: { api_reachable?: boolean };
  };
  return Boolean((body.data ?? body).mihomo?.api_reachable);
}

/**
 * Флаг слоя «Конфигурация» на устройстве (чтение с сессией панели). Неудачный ответ или
 * конверт без булева config_layer — красный сценарий, а не «флаг выключен».
 */
async function configLayerFlag(page: Page): Promise<boolean> {
  const res = await page.request.get('/api/settings');
  expect(res.status(), 'GET /api/settings').toBe(200);
  const body = (await res.json()) as { data?: { config_layer?: unknown } };
  const flag = body.data?.config_layer;
  expect(typeof flag, 'config_layer в конверте data ответа /api/settings').toBe('boolean');
  return flag === true;
}

/** Критичные и серьёзные нарушения axe (WCAG 2.0/2.1 A и AA) в рабочей области страницы. */
async function axeSevere(page: Page): Promise<string[]> {
  // переход между вкладками идёт с затуханием: axe не должен видеть полупрозрачный кадр
  await page.waitForTimeout(500);
  const scan = await new AxeBuilder({ page })
    .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
    .include('.rules-page')
    .analyze();
  return scan.violations
    .filter((v) => v.impact === 'critical' || v.impact === 'serious')
    .map((v) => {
      const nodes = v.nodes
        .slice(0, 3)
        .map((n) => `${n.target.join(' ')} [${n.any[0]?.message ?? ''}]`);
      return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
    });
}

test('rules-offline: при Xray вкладка Провайдеры', { tag: CORE_TAG }, async ({ page }) => {
  test.skip(RT.core !== 'xray', 'страница без API проверяется при активном Xray');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openRules(page);
  await providersReady(page);

  await expect(page.getByTestId('tab-diagnostic')).toBeDisabled();
  await expect(page.getByTestId('tab-all_rules')).toBeDisabled();
  // D-16: при включённом слое старой вкладки «Исключения» нет (её адрес ведёт в «Конфигурацию»),
  // без слоя она на месте. Оба режима проходит config-entries legacy-redirects.
  const exceptions = page.getByTestId('tab-exceptions');
  if (await configLayerFlag(page)) {
    await expect(exceptions).toHaveCount(0);
  } else {
    await expect(exceptions).toBeVisible();
    await expect(exceptions).toBeEnabled();
  }
  await expect(page.getByTestId('rules-tabs-note')).toBeVisible();
  await expect(page.getByTestId('rules-offline-badge')).toBeVisible();
  await expect(page.getByTestId('rules-offline-note')).toBeVisible();
  await expect(page.locator('.api-offline')).toHaveCount(0);

  await guard.assertClean();
});

test('rules-offline: ссылка tab=providers', async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openRules(page, '#/rules?tab=providers');
  await providersReady(page);
  await expect(page.locator('.providers-tab')).toBeVisible();
  await guard.assertClean();
});

test('rules-offline: карточка провайдера', { tag: CORE_TAG }, async ({ page }) => {
  test.skip(RT.core !== 'xray', 'страница без API проверяется при активном Xray');
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openRules(page);
  await providersReady(page);

  const cards = page.locator(PROVIDER_CARD);
  if ((await cards.count()) === 0) {
    // у активного профиля нет rule-provider'ов: пустое состояние и пометка причины
    await expect(page.locator('.providers-tab .header-badge')).toHaveText('0');
    await expect(page.getByTestId('rules-offline-badge')).toBeVisible();
  } else {
    // вместо числа правил и «обновлено» — размер и дата файла либо статус файла
    const first = cards.first();
    await expect(
      first
        .locator('.provider-meta .meta-item, [data-testid^="provider-status-"], .badge-danger')
        .first()
    ).toBeVisible();
    await expect(
      first.locator('.updated-time', { hasText: /назад|ago|never|Никогда/ })
    ).toHaveCount(0);
  }

  await guard.assertClean();
});

test('rules-offline: проверка URL', { tag: CORE_TAG }, async ({ page }) => {
  test.skip(RT.core !== 'xray', 'страница без API проверяется при активном Xray');
  test.setTimeout(T.page * 3);
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await openRules(page);
  await providersReady(page);

  const buttons = page.locator('[data-testid^="provider-check-"]');
  test.skip(
    (await buttons.count()) === 0,
    'у активного профиля нет http-провайдеров: случаи проверки подтверждает TestRouterRuleProviderCheckURL'
  );

  const button = buttons.first();
  const name = ((await button.getAttribute('data-testid')) ?? '').replace('provider-check-', '');
  const info = await page.request.get('/api/rule-providers/info');
  expect(info.ok(), 'GET /api/rule-providers/info').toBe(true);
  const body = (await info.json()) as { data?: { name: string; proxy?: string }[] };
  const proxy = (body.data ?? []).find((d) => d.name === name)?.proxy ?? '';
  const viaProxy = proxy !== '' && proxy.toUpperCase() !== 'DIRECT';

  await button.click();
  await expect(page.locator(`[data-testid="provider-url-result-${name}"]`)).toBeVisible({
    timeout: T.page
  });
  await expect(page.locator(`[data-testid="provider-url-direct-${name}"]`)).toBeVisible();
  // ядро остановлено: проверка только напрямую, с пометкой, что результат может отличаться
  if (viaProxy) {
    await expect(page.locator(`[data-testid="provider-url-direct-only-${name}"]`)).toBeVisible();
  }
  // код HTTP через ядро не обещается: строки «через прокси» при остановленном ядре нет
  await expect(page.locator(`[data-testid="provider-url-proxy-${name}"]`)).toHaveCount(0);

  await guard.assertClean();
});

for (const theme of ['dark', 'light'] as const) {
  test(`rules-offline axe: тема ${theme}`, { tag: CORE_TAG }, async ({ page }) => {
    test.skip(RT.core !== 'xray', 'страница без API проверяется при активном Xray');
    const guard = attachConsoleGuard(page);
    await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
    await page.setViewportSize({ width: 1280, height: 900 });
    await openRules(page);
    await providersReady(page);
    expect(
      await page.evaluate(() => document.documentElement.getAttribute('data-theme')),
      'применена тема'
    ).toBe(theme);

    const problems = (await axeSevere(page)).map((v) => `axe ${v}`);
    expect(problems, problems.join('\n')).toEqual([]);

    await guard.assertClean();
  });
}

for (const theme of ['dark', 'light'] as const) {
  test(
    `rules-offline: 390 px, тема ${theme} — без горизонтальной прокрутки, цели не меньше 44 px`,
    { tag: CORE_TAG },
    async ({ page }) => {
      test.skip(RT.core !== 'xray', 'страница без API проверяется при активном Xray');
      const guard = attachConsoleGuard(page);
      await page.addInitScript((th) => localStorage.setItem('theme', th), theme);
      await page.setViewportSize({ width: 390, height: 844 });
      await openRules(page);
      await providersReady(page);

      const pageScroll = await page.evaluate(
        () => document.documentElement.scrollWidth - document.documentElement.clientWidth
      );
      expect(pageScroll, 'горизонтальная прокрутка страницы').toBeLessThanOrEqual(0);

      const buttons = page.locator(`.providers-tab .tab-header .btn, ${PROVIDER_CARD} .btn`);
      const count = await buttons.count();
      for (let i = 0; i < count; i++) {
        const box = await buttons.nth(i).boundingBox();
        expect(box, `кнопка ${i} видна`).not.toBeNull();
        expect(box!.height, `высота кнопки ${i}`).toBeGreaterThanOrEqual(44);
        expect(box!.x + box!.width, `кнопка ${i} в пределах экрана`).toBeLessThanOrEqual(390);
      }

      await guard.assertClean();
    }
  );
}

test(
  'rules-offline: при живом API вкладки активны, пометки нет',
  { tag: CORE_TAG },
  async ({ page }) => {
    test.skip(RT.core !== 'mihomo', 'проверка живого API идёт при активном Mihomo');
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await openRules(page);
    test.skip(!(await mihomoApiUp(page)), 'Clash API Mihomo не отвечает: это не режим живого API');

    await expect(page.getByTestId('tab-diagnostic')).toBeEnabled();
    await expect(page.getByTestId('tab-all_rules')).toBeEnabled();
    await expect(page.getByTestId('rules-tabs-note')).toHaveCount(0);
    await expect(page.getByTestId('rules-offline-badge')).toHaveCount(0);

    await guard.assertClean();
  }
);

/**
 * Mihomo установлен на устройстве (чтение /api/capabilities). Сбой самой панели или ответ
 * без булева installed — красный сценарий, а не «Mihomo не установлен».
 */
async function mihomoInstalled(page: Page): Promise<boolean> {
  const res = await page.request.get('/api/capabilities');
  expect(res.status(), 'GET /api/capabilities').toBe(200);
  const body = (await res.json()) as {
    data?: { kernels?: { mihomo?: { installed?: unknown } } };
    kernels?: { mihomo?: { installed?: unknown } };
  };
  const installed = (body.data ?? body).kernels?.mihomo?.installed;
  expect(typeof installed, 'kernels.mihomo.installed в ответе /api/capabilities').toBe('boolean');
  return installed === true;
}

const CONFIG_TABS = ['nodes', 'groups', 'rules', 'lists', 'cores', 'files'];

// CFG-01: плашка «Mihomo API оффлайн» пишет причину по состоянию ядер и не предлагает запуск
// второго ядра; раздел «Конфигурация» открыт при любом ядре и плашкой не закрывается.
test(`proxies offline reason core:${RT.core}`, { tag: CORE_TAG }, async ({ page }) => {
  const guard = attachConsoleGuard(page);
  await page.setViewportSize({ width: 1440, height: 900 });
  await page.goto('/#/proxies', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.locator('.main-content .page-header h1').first()).toBeVisible({
    timeout: T.page
  });

  if (RT.core === 'xray') {
    test.skip(!(await mihomoInstalled(page)), 'Mihomo не установлен: плашки нет по замыслу');
    const banner = page.locator('.api-offline');
    await expect(banner).toHaveAttribute('data-reason', 'xray', { timeout: T.page });
    await expect(banner.getByTestId('api-offline-services')).toHaveAttribute('href', '#/services');
    await expect(page.getByTestId('api-offline-start')).toHaveCount(0);
    await expect(page.getByTestId('api-offline-restart')).toHaveCount(0);
  } else {
    // Mihomo активен: API отвечает, плашки нет (ждём ответа capabilities)
    await page.waitForLoadState('networkidle', { timeout: T.network }).catch(() => {});
    await expect(page.locator('.api-offline')).toHaveCount(0, { timeout: T.page });
  }

  if (!(await configLayerFlag(page))) {
    test.info().annotations.push({
      type: 'skip-part',
      description: 'флаг config_layer выключен на стенде: раздел #/config не проверен'
    });
  } else {
    await page.goto('/#/config/nodes', { timeout: T.page, waitUntil: 'domcontentloaded' });
    for (const tab of CONFIG_TABS) {
      await expect(page.getByTestId(`config-tab-${tab}`)).toBeVisible({ timeout: T.page });
    }
    await expect(page.locator('.api-offline')).toHaveCount(0);
    await expect(page.getByTestId('kernel-conflict-gate')).toHaveCount(0);
  }

  await guard.assertClean();
});
