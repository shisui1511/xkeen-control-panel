import { test, expect } from '@playwright/test';

// Pre-flight check E2E spec.
// Fully mocked — no real backend or router is contacted.
// Validates:
//  - API-unreachable badge in the Mihomo kernel card
//  - Pre-flight dialog gate before XKeen start
//  - «Исправить → Конфигуратор» branch (no start, navigate to #/editor)
//  - «Запустить всё равно» branch (start is POSTed)
//  - Silent start when preflight returns valid:true

test.describe('Pre-flight check — badge and dialog gate', () => {
  // Closure state reset per test
  let startPosted = false;

  // Per-test configurable payloads
  let preflightBody: {
    valid: boolean;
    errors: { code: string; message: string }[];
    warnings: { code?: string; message?: string }[];
  };

  let mihomoCaps: {
    reachable: boolean;
    process_running: boolean;
    api_reachable: boolean;
    api_authenticated: boolean;
  };

  test.beforeEach(async ({ page }) => {
    // Reset per-test state
    startPosted = false;

    // Default: preflight OK, Mihomo not running
    preflightBody = { valid: true, errors: [], warnings: [] };
    mihomoCaps = {
      reachable: true,
      process_running: false,
      api_reachable: true,
      api_authenticated: true
    };

    // Отключаем Service Worker и форсируем русский язык интерфейса
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      // Форсируем русский язык для стабильных текстовых ассертов
      localStorage.setItem('lang', 'ru');
    });

    // Перехватываем все запросы к API
    await page.route('**/api/**', async (route) => {
      const url = route.request().url();

      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf-token'
          })
        });
      } else if (url.includes('/api/capabilities')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              kernels: {
                xray: { installed: true, version: '1.8.4', channel: 'stable' },
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'xray',
              mihomo: mihomoCaps
            }
          })
        });
      } else if (url.includes('/api/settings')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: { dev_mode: false } })
        });
      } else if (url.includes('/api/version')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: 'v0.15.1' })
        });
      } else if (url.includes('/api/service/restart-log')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/kernels') && !url.includes('/api/kernels/')) {
        // Оба ядра установлены; mihomo process_status зависит от mihomoCaps.process_running
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: [
              {
                name: 'xray',
                display_name: 'Xray-core',
                binary_path: '/opt/bin/xray',
                current_version: '1.8.4',
                latest_version: '1.8.4',
                has_update: false,
                channel: 'stable',
                status: 'idle',
                process_status: 'stopped',
                message: 'stopped'
              },
              {
                name: 'mihomo',
                display_name: 'Mihomo',
                binary_path: '/opt/bin/mihomo',
                current_version: '1.18.0',
                latest_version: '1.18.0',
                has_update: false,
                channel: 'stable',
                status: 'idle',
                process_status: mihomoCaps.process_running ? 'running' : 'stopped',
                message: mihomoCaps.process_running ? 'running' : 'stopped'
              }
            ]
          })
        });
      } else if (url.includes('/api/service/status')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              is_running: false,
              active_kernel: 'xray',
              pid: 0,
              uptime: '',
              binary_path: '/opt/sbin/xkeen',
              raw: 'XKeen not running'
            }
          })
        });
      } else if (url.includes('/api/config/preflight')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: preflightBody })
        });
      } else if (url.includes('/api/service/control') && url.includes('action=start')) {
        startPosted = true;
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body: 'OK'
        });
      } else {
        // Заглушка для прочих запросов
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  // -------------------------------------------------------------------------
  // Тест 1: Бейдж «API недоступен» виден, когда Mihomo запущен, но API не достигает
  // -------------------------------------------------------------------------
  test('badge visible when mihomo running but api unreachable', async ({ page }) => {
    mihomoCaps = {
      reachable: true,
      process_running: true,
      api_reachable: false,
      api_authenticated: false
    };

    await page.goto('/#/services');

    // Ждём загрузки данных ядер (Mihomo-карточка с текстом running)
    await expect(page.locator('.kernel-card').first()).toBeVisible();

    const badge = page.locator('a.badge.badge-warning[href="#/editor"]');
    await expect(badge).toBeVisible();
    await expect(badge).toContainText('API недоступен');
  });

  // -------------------------------------------------------------------------
  // Тест 2: Бейдж отсутствует, когда Mihomo запущен и API доступен
  // -------------------------------------------------------------------------
  test('badge hidden when mihomo running and api reachable', async ({ page }) => {
    mihomoCaps = {
      reachable: true,
      process_running: true,
      api_reachable: true,
      api_authenticated: true
    };

    await page.goto('/#/services');

    // Ждём загрузки страницы (status-badge в XKeen card появляется только после загрузки ядер)
    await expect(page.locator('.kernel-card').first()).toBeVisible();

    const badge = page.locator('a.badge.badge-warning[href="#/editor"]');
    await expect(badge).toHaveCount(0);
  });

  // -------------------------------------------------------------------------
  // Тест 3: Preflight ошибки → диалог блокирует, «Исправить» навигирует без старта
  // -------------------------------------------------------------------------
  test('preflight errors show dialog; Исправить navigates to editor without start', async ({
    page
  }) => {
    preflightBody = {
      valid: false,
      errors: [{ code: 'no_external_controller', message: 'Отсутствует external-controller' }],
      warnings: []
    };

    await page.goto('/#/services');

    // Ждём кнопку «Запустить» в XKeen карточке (isRunning=false)
    // SVG внутри кнопки, поэтому используем getByText или locator с классом
    const startButton = page.locator('.kernel-card').first().locator('button.btn-primary');
    await expect(startButton).toBeVisible();

    // Кликаем по кнопке Запустить
    await startButton.click();

    // Диалог с заголовком «Ошибка конфигурации» должен появиться
    const dialogTitle = page.locator('h2.modal-title');
    await expect(dialogTitle).toBeVisible();
    await expect(dialogTitle).toHaveText('Ошибка конфигурации');

    // Нажимаем «Исправить → Конфигуратор» (cancelLabel → resolve(false))
    const fixButton = page.getByRole('button', { name: 'Исправить → Конфигуратор' });
    await expect(fixButton).toBeVisible();
    await fixButton.click();

    // Должна произойти навигация к #/editor
    await expect(page).toHaveURL(/#\/editor/);

    // Start не должен был POST-иться
    expect(startPosted).toBe(false);
  });

  // -------------------------------------------------------------------------
  // Тест 4: Preflight ошибки → «Запустить всё равно» — старт выполняется
  // -------------------------------------------------------------------------
  test('preflight errors: Запустить всё равно proceeds and posts start', async ({ page }) => {
    preflightBody = {
      valid: false,
      errors: [{ code: 'no_external_controller', message: 'Отсутствует external-controller' }],
      warnings: []
    };

    await page.goto('/#/services');

    // Ждём кнопку «Запустить» в XKeen карточке
    const startButton = page.locator('.kernel-card').first().locator('button.btn-primary');
    await expect(startButton).toBeVisible();
    await startButton.click();

    // Диалог появляется
    const dialogTitle = page.locator('h2.modal-title');
    await expect(dialogTitle).toBeVisible();
    await expect(dialogTitle).toHaveText('Ошибка конфигурации');

    // Нажимаем «Запустить всё равно» (confirmLabel → resolve(true))
    const startAnywayButton = page.getByRole('button', { name: 'Запустить всё равно' });
    await expect(startAnywayButton).toBeVisible();
    await startAnywayButton.click();

    // Диалог закрывается
    await expect(dialogTitle).not.toBeVisible();

    // Start должен был POST-иться
    expect(startPosted).toBe(true);
  });

  // -------------------------------------------------------------------------
  // Тест 5: Preflight valid:true → тихий старт без диалога
  // -------------------------------------------------------------------------
  test('preflight valid true: silent start without dialog', async ({ page }) => {
    preflightBody = { valid: true, errors: [], warnings: [] };

    await page.goto('/#/services');

    // Ждём кнопку «Запустить» в XKeen карточке (isRunning=false, btn-primary)
    const startButton = page.locator('.kernel-card').first().locator('button.btn-primary');
    await expect(startButton).toBeVisible();
    await startButton.click();

    // Диалог не должен появляться
    const dialogTitle = page.locator('h2.modal-title');

    // Небольшая задержка для реакции UI, затем проверяем отсутствие диалога
    await page.waitForTimeout(300);
    await expect(dialogTitle).not.toBeVisible();

    // Start должен был POST-иться
    expect(startPosted).toBe(true);
  });

  // -------------------------------------------------------------------------
  // Тест 6: Preflight только предупреждения → тихий старт без диалога
  // -------------------------------------------------------------------------
  test('preflight warnings only: silent start without dialog', async ({ page }) => {
    preflightBody = {
      valid: true,
      errors: [],
      warnings: [{ code: 'no_rules', message: 'Правила не заданы' }]
    };

    await page.goto('/#/services');

    // Ждём кнопку «Запустить» в XKeen карточке (isRunning=false, btn-primary)
    const startButton = page.locator('.kernel-card').first().locator('button.btn-primary');
    await expect(startButton).toBeVisible();
    await startButton.click();

    // Диалог не должен появляться
    const dialogTitle = page.locator('h2.modal-title');

    await page.waitForTimeout(300);
    await expect(dialogTitle).not.toBeVisible();

    // Start должен был POST-иться
    expect(startPosted).toBe(true);
  });
});
