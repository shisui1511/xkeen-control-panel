/**
 * constructor-apply.spec.ts — исходы «Применить» обоих конструкторов на моках.
 *
 * Сервер решает, перезапускать ли ядро (action=apply), а UI выбирает тост по
 * фактическому исходу. Проверяем, что остановленное ядро не запускается,
 * чужое не перезапускается и ошибка перезапуска не глотается.
 *
 * Маршрут: /#/constructor
 */

import { test, expect, type Page } from '@playwright/test';

test.use({ locale: 'ru-RU' });

interface ControlCall {
  action: string;
  kernel: string;
  url: string;
}

interface SetupOptions {
  activeKernel?: 'xray' | 'mihomo';
  applyRestarts?: { xray?: boolean; mihomo?: boolean };
  /** Ответ action=apply (поле data). */
  applyData?: Record<string, unknown>;
  applyDelayMs?: number;
  /** Входящие в 03_inbounds.json, загруженном с роутера. */
  xrayInbounds?: unknown[];
}

const MIHOMO_YAML = `mixed-port: 7890
proxies:
  - name: node-a
    type: ss
    server: 1.2.3.4
    port: 8388
    cipher: aes-128-gcm
    password: secret
proxy-groups:
  - name: Selective
    type: select
    proxies:
      - node-a
      - DIRECT
rules:
  - MATCH,Selective
`;

function xrayFile(path: string, inbounds: unknown[] = []): string {
  if (path.includes('01_log')) {
    return JSON.stringify({ log: { loglevel: 'warning', dnsLog: false } });
  }
  if (path.includes('02_dns')) {
    return JSON.stringify({
      dns: { tag: 'dns-in', servers: ['8.8.8.8'], queryStrategy: 'UseIP', hosts: {} }
    });
  }
  if (path.includes('03_inbounds')) {
    return JSON.stringify({ inbounds });
  }
  if (path.includes('04_outbounds')) {
    return JSON.stringify({
      outbounds: [
        { tag: 'direct', protocol: 'freedom' },
        { tag: 'block', protocol: 'blackhole' },
        { tag: 'dns-out', protocol: 'dns' }
      ]
    });
  }
  if (path.includes('05_routing')) {
    return JSON.stringify({
      routing: {
        domainStrategy: 'IPIfNonMatch',
        rules: [
          { type: 'field', port: '53', outboundTag: 'dns-out' },
          { type: 'field', network: 'tcp,udp', outboundTag: 'direct' }
        ]
      }
    });
  }
  if (path.includes('06_policy')) {
    return JSON.stringify({
      policy: {
        levels: { '0': { handshake: 4, connIdle: 300, uplinkOnly: 2, downlinkOnly: 5 } },
        system: { statsInboundUplink: false, statsInboundDownlink: false }
      }
    });
  }
  return JSON.stringify({});
}

async function setup(page: Page, opts: SetupOptions = {}) {
  const calls: ControlCall[] = [];
  const saves: string[] = [];
  // Записанные файлы отдаются обратно при следующем чтении, как на роутере
  const written = new Map<string, string>();
  const activeKernel = opts.activeKernel ?? 'xray';

  await page.addInitScript(() => {
    Object.defineProperty(window.navigator, 'serviceWorker', {
      value: undefined,
      writable: false,
      configurable: true
    });
    window.localStorage.setItem('lang', 'ru');
  });

  await page.route('**/api/**', async (route) => {
    const url = route.request().url();
    const method = route.request().method();
    const json = (body: unknown, status = 200) =>
      route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(body) });

    if (url.includes('/api/auth/me')) {
      return json({ authenticated: true, setup_required: false, csrf_token: 'mock-csrf' });
    }
    if (url.includes('/api/capabilities')) {
      return json({
        success: true,
        data: {
          kernels: {
            xray: { installed: true, version: '1.8.24', channel: 'stable' },
            mihomo: { installed: true, version: '1.19.0', channel: 'stable' }
          },
          active_kernel: activeKernel,
          apply_restarts: {
            xray: opts.applyRestarts?.xray ?? false,
            mihomo: opts.applyRestarts?.mihomo ?? false
          }
        }
      });
    }
    if (url.includes('/api/service/control') && method === 'POST') {
      const u = new URL(url);
      const action = u.searchParams.get('action') ?? '';
      calls.push({ action, kernel: u.searchParams.get('kernel') ?? '', url });
      if (action === 'apply') {
        if (opts.applyDelayMs) {
          await new Promise((r) => setTimeout(r, opts.applyDelayMs));
        }
        return json({
          success: true,
          data: opts.applyData ?? {
            outcome: 'restarted',
            kernel: u.searchParams.get('kernel') ?? 'xray',
            active_kernel: activeKernel,
            active_running: true
          }
        });
      }
      return json({ success: true });
    }
    if (url.includes('/api/config/read') && method === 'GET') {
      const path = new URL(url).searchParams.get('path') || '';
      const stored = written.get(path);
      if (stored !== undefined) {
        return route.fulfill({ status: 200, contentType: 'text/plain', body: stored });
      }
      if (path.includes('mihomo')) {
        return route.fulfill({ status: 200, contentType: 'text/plain', body: MIHOMO_YAML });
      }
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: xrayFile(path, opts.xrayInbounds)
      });
    }
    if (url.includes('/api/config/list')) {
      return json([]);
    }
    if (url.includes('/api/config/smart-merge') && method === 'POST') {
      return json({
        success: true,
        data: { content: MIHOMO_YAML, stats: { proxies: 1, proxy_providers: 0, rules: 1 } }
      });
    }
    if (url.includes('/api/config/save') && method === 'POST') {
      const savedPath = new URL(url).searchParams.get('path') ?? '';
      saves.push(savedPath);
      written.set(savedPath, route.request().postData() ?? '');
      return json({ success: true });
    }
    if (url.includes('/api/templates/list')) {
      return json([]);
    }
    return json({ success: true, data: {} });
  });

  return { calls, saves };
}

async function openXrayConstructor(page: Page) {
  await page.goto('/#/constructor');
  const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
  await expect(xrayBtn).toBeVisible({ timeout: 15000 });
  await xrayBtn.click();
  await expect(page.locator('[data-testid="xray-section-tabs"]')).toBeVisible({ timeout: 15000 });
}

async function openApplyDialog(page: Page) {
  const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
  await expect(applyBtn).toBeVisible({ timeout: 10000 });
  await applyBtn.click();
  const dialog = page.locator('[data-testid="apply-confirm-dialog"]');
  await expect(dialog).toBeVisible({ timeout: 5000 });
  return dialog;
}

function confirmButton(dialog: ReturnType<Page['locator']>) {
  return dialog.locator('button.btn-primary');
}

test.describe('Применение конструктора Xray', () => {
  test('ядро остановлено: без restart, тост «вступят в силу при запуске» и «Запустить сейчас»', async ({
    page
  }) => {
    const { calls, saves } = await setup(page, {
      applyRestarts: { xray: false },
      applyData: {
        outcome: 'saved_kernel_stopped',
        kernel: 'xray',
        active_kernel: 'xray',
        active_running: false
      }
    });
    await openXrayConstructor(page);

    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toHaveText('Применить');

    const dialog = await openApplyDialog(page);
    await expect(dialog).not.toContainText('перезапустить');
    await expect(confirmButton(dialog)).toHaveText('Применить');
    await confirmButton(dialog).click();

    const toast = page.locator('.toast', { hasText: 'вступят в силу при запуске' });
    await expect(toast).toBeVisible({ timeout: 5000 });
    expect(saves.length).toBeGreaterThan(0);
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
    const applies = calls.filter((c) => c.action === 'apply');
    expect(applies).toHaveLength(1);
    expect(applies[0].kernel).toBe('xray');

    await toast.getByRole('button', { name: 'Запустить сейчас' }).click();
    await expect.poll(() => calls.filter((c) => c.action === 'start').length).toBe(1);
  });

  test('ядро запущено: подпись «Применить и перезапустить» и успешный тост', async ({ page }) => {
    const { calls } = await setup(page, { applyRestarts: { xray: true } });
    await openXrayConstructor(page);

    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toHaveText('Применить и перезапустить');

    const dialog = await openApplyDialog(page);
    await expect(dialog).toContainText('перезапустить');
    await confirmButton(dialog).click();

    await expect(page.locator('.toast', { hasText: 'ядро перезапущено' })).toBeVisible({
      timeout: 5000
    });
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
    expect(calls.filter((c) => c.action === 'apply')).toHaveLength(1);
  });

  test('рестарт упал: тост-ошибка с причиной и «Логи», флаг несохранённого снят', async ({
    page
  }) => {
    const { calls } = await setup(page, {
      applyRestarts: { xray: true },
      applyData: {
        outcome: 'restart_failed',
        kernel: 'xray',
        active_kernel: 'xray',
        active_running: true,
        error: 'xray failed to start: port 1181 busy'
      }
    });
    await openXrayConstructor(page);

    const dialog = await openApplyDialog(page);
    await confirmButton(dialog).click();

    const toast = page.locator('.toast--error', { hasText: 'перезапуск не удался' });
    await expect(toast).toBeVisible({ timeout: 5000 });
    await expect(toast).toContainText('port 1181 busy');
    await expect(toast.getByRole('button', { name: 'Логи' })).toBeVisible();
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);

    // Файлы записаны и перечитаны: повторное применение не находит изменений
    await expect(dialog).toBeHidden({ timeout: 5000 });
    const dialog2 = await openApplyDialog(page);
    await expect(dialog2).toContainText('02_dns.json: Без изменений');
    await expect(dialog2.locator('.diff-stat')).toHaveCount(0);
  });

  test('активно другое ядро: тост «после переключения на Xray», без действий и switch_kernel', async ({
    page
  }) => {
    const { calls } = await setup(page, {
      activeKernel: 'mihomo',
      applyData: {
        outcome: 'saved_kernel_inactive',
        kernel: 'xray',
        active_kernel: 'mihomo',
        active_running: true
      }
    });
    await openXrayConstructor(page);

    const dialog = await openApplyDialog(page);
    await confirmButton(dialog).click();

    const toast = page.locator('.toast', { hasText: 'после переключения на Xray' });
    await expect(toast).toBeVisible({ timeout: 5000 });
    await expect(toast.locator('button.toast__action')).toHaveCount(0);
    expect(calls.filter((c) => c.action === 'switch_kernel')).toHaveLength(0);
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
  });

  test('двойной клик по подтверждению отправляет один action=apply', async ({ page }) => {
    const { calls } = await setup(page, { applyDelayMs: 1000 });
    await openXrayConstructor(page);

    const dialog = await openApplyDialog(page);
    const btn = confirmButton(dialog);
    await btn.dblclick();

    await expect(page.locator('.toast', { hasText: 'Сохранено' })).toBeVisible({ timeout: 8000 });
    expect(calls.filter((c) => c.action === 'apply')).toHaveLength(1);
  });

  test('правка входящего попадает в запись: черновик не делит объекты с загруженным файлом', async ({
    page
  }) => {
    const { saves } = await setup(page, {
      xrayInbounds: [
        {
          tag: 'redirect',
          port: 61219,
          protocol: 'dokodemo-door',
          listen: '127.0.0.1',
          settings: {}
        }
      ]
    });
    await openXrayConstructor(page);

    await page.locator('[data-testid="xray-section-tabs"] [data-tab="inbounds"]').first().click();
    const port = page.locator('#xray-inbound-port-redirect');
    await expect(port).toBeVisible({ timeout: 5000 });
    await port.fill('61220');

    const dialog = await openApplyDialog(page);
    await expect(dialog).toContainText('03_inbounds.json');
    await expect(
      dialog.locator('li', { hasText: '03_inbounds.json' }).locator('.diff-stat').first()
    ).toBeVisible();
    await confirmButton(dialog).click();

    await expect.poll(() => saves.some((p) => p.endsWith('03_inbounds.json'))).toBe(true);
  });
});

async function openMihomoConstructor(page: Page) {
  await page.goto('/#/constructor');
  const mihomoBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
  await expect(mihomoBtn).toBeVisible({ timeout: 15000 });
  await mihomoBtn.click();
  await expect(page.locator('[data-testid="apply-changes-btn"]')).toBeVisible({ timeout: 15000 });
}

/** Открывает диалог применения Mihomo и подтверждает его; возвращает кнопки диалога переключения. */
async function applyMihomo(page: Page) {
  await page.locator('[data-testid="apply-changes-btn"]').click();
  const dialog = page.locator('[data-testid="apply-confirm-dialog"]');
  await expect(dialog).toBeVisible({ timeout: 5000 });
  await confirmButton(dialog).click();
}

const switchPrompt = 'Сейчас работает Xray — переключить на Mihomo?';

test.describe('Применение конструктора Mihomo', () => {
  const inactiveRunning = {
    outcome: 'saved_kernel_inactive',
    kernel: 'mihomo',
    active_kernel: 'xray',
    active_running: true
  };

  test('«Только сохранить» не трогает ядра, повторное применение тоже', async ({ page }) => {
    const { calls, saves } = await setup(page, {
      activeKernel: 'xray',
      applyData: inactiveRunning
    });
    await openMihomoConstructor(page);

    for (let attempt = 0; attempt < 2; attempt++) {
      await applyMihomo(page);
      const prompt = page.getByText(switchPrompt);
      await expect(prompt).toBeVisible({ timeout: 5000 });
      await expect(page.getByRole('button', { name: 'Переключить и запустить' })).toBeVisible();
      await page.getByRole('button', { name: 'Только сохранить' }).click();

      const toast = page.locator('.toast', {
        hasText: 'Конфиг Mihomo сохранён. Вступит в силу после переключения на Mihomo'
      });
      await expect(toast.first()).toBeVisible({ timeout: 5000 });
      await expect(toast.first().locator('button.toast__action')).toHaveCount(0);
      await expect(prompt).toBeHidden();
    }

    expect(saves.some((p) => p.endsWith('config.yaml'))).toBe(true);
    expect(calls.filter((c) => c.action === 'apply')).toHaveLength(2);
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
    expect(calls.filter((c) => c.action === 'switch_kernel')).toHaveLength(0);
  });

  test('«Переключить и запустить» вызывает один switch_kernel на mihomo', async ({ page }) => {
    const { calls } = await setup(page, { activeKernel: 'xray', applyData: inactiveRunning });
    await openMihomoConstructor(page);

    await applyMihomo(page);
    await expect(page.getByText(switchPrompt)).toBeVisible({ timeout: 5000 });
    await page.getByRole('button', { name: 'Переключить и запустить' }).click();

    await expect.poll(() => calls.filter((c) => c.action === 'switch_kernel').length).toBe(1);
    expect(calls.find((c) => c.action === 'switch_kernel')?.kernel).toBe('mihomo');
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
  });

  test('Xray не запущен: диалога нет, конфиг сохранён, без switch_kernel', async ({ page }) => {
    const { calls } = await setup(page, {
      activeKernel: 'xray',
      applyData: { ...inactiveRunning, active_running: false }
    });
    await openMihomoConstructor(page);

    await applyMihomo(page);

    await expect(
      page.locator('.toast', { hasText: 'Вступит в силу после переключения на Mihomo' }).first()
    ).toBeVisible({ timeout: 5000 });
    await expect(page.getByText(switchPrompt)).toHaveCount(0);
    expect(calls.filter((c) => c.action === 'switch_kernel')).toHaveLength(0);
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
  });

  test('Mihomo активен и остановлен: без запуска, тост «Запустить сейчас»', async ({ page }) => {
    const { calls } = await setup(page, {
      activeKernel: 'mihomo',
      applyData: {
        outcome: 'saved_kernel_stopped',
        kernel: 'mihomo',
        active_kernel: 'mihomo',
        active_running: false
      }
    });
    await openMihomoConstructor(page);

    await applyMihomo(page);

    const toast = page.locator('.toast', { hasText: 'вступят в силу при запуске' });
    await expect(toast).toBeVisible({ timeout: 5000 });
    await expect(toast.getByRole('button', { name: 'Запустить сейчас' })).toBeVisible();
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
    expect(calls.filter((c) => c.action === 'start')).toHaveLength(0);
  });

  test('подпись кнопки и диалога зависят от apply_restarts.mihomo', async ({ page }) => {
    await setup(page, { activeKernel: 'mihomo', applyRestarts: { mihomo: false } });
    await openMihomoConstructor(page);
    const btn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(btn).toHaveText('Применить');
    await btn.click();
    const dialog = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 5000 });
    await expect(dialog).not.toContainText('перезапустить');
    await expect(confirmButton(dialog)).toHaveText('Применить');
  });

  test('запущенный Mihomo: «Применить и перезапустить» и тост об успехе', async ({ page }) => {
    const { calls } = await setup(page, {
      activeKernel: 'mihomo',
      applyRestarts: { mihomo: true },
      applyData: {
        outcome: 'restarted',
        kernel: 'mihomo',
        active_kernel: 'mihomo',
        active_running: true
      }
    });
    await openMihomoConstructor(page);
    const btn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(btn).toHaveText(/Применить и перезапустить/);
    await applyMihomo(page);
    await expect(page.locator('.toast--success').first()).toBeVisible({ timeout: 5000 });
    expect(calls.filter((c) => c.action === 'restart')).toHaveLength(0);
    expect(calls.filter((c) => c.action === 'apply')).toHaveLength(1);
  });

  test('рестарт Mihomo упал: ошибка с причиной и «Логи», не «Failed to restart service»', async ({
    page
  }) => {
    await setup(page, {
      activeKernel: 'mihomo',
      applyRestarts: { mihomo: true },
      applyData: {
        outcome: 'restart_failed',
        kernel: 'mihomo',
        active_kernel: 'mihomo',
        active_running: true,
        error: 'mihomo failed: bad config'
      }
    });
    await openMihomoConstructor(page);
    await applyMihomo(page);

    const toast = page.locator('.toast--error', { hasText: 'перезапуск не удался' });
    await expect(toast).toBeVisible({ timeout: 5000 });
    await expect(toast).toContainText('bad config');
    await expect(toast.getByRole('button', { name: 'Логи' })).toBeVisible();
    await expect(page.getByText('Failed to restart service')).toHaveCount(0);
  });

  test('двойной клик по подтверждению отправляет один action=apply', async ({ page }) => {
    const { calls } = await setup(page, {
      activeKernel: 'mihomo',
      applyDelayMs: 1000,
      applyData: {
        outcome: 'restarted',
        kernel: 'mihomo',
        active_kernel: 'mihomo',
        active_running: true
      }
    });
    await openMihomoConstructor(page);

    await page.locator('[data-testid="apply-changes-btn"]').click();
    const dialog = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(dialog).toBeVisible({ timeout: 5000 });
    await confirmButton(dialog).dblclick();

    await expect(page.locator('.toast--success').first()).toBeVisible({ timeout: 8000 });
    expect(calls.filter((c) => c.action === 'apply')).toHaveLength(1);
  });
});
