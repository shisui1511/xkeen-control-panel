import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Phase 111: Xray Gaps and Enhancements', () => {
  test.beforeEach(async ({ page }) => {
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

      if (url.includes('/api/auth/me')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            authenticated: true,
            setup_required: false,
            csrf_token: 'mock-csrf'
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
                xray: { installed: true, version: '1.8.24', channel: 'stable' }
              },
              active_kernel: 'xray',
              xray: { grpc_ready: true }
            }
          })
        });
      } else if (url.includes('/api/xray/stats')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            status: 'ok',
            data: {
              outbounds: {
                'proxy-out': { uplink: 1024, downlink: 2048 },
                direct: { uplink: 512, downlink: 512 }
              },
              inbounds: {
                'mixed-in': { uplink: 4096, downlink: 8192 }
              },
              users: {
                'user1@domain': { uplink: 1000, downlink: 2000 }
              }
            }
          })
        });
      } else if (url.includes('/api/xray/restart-logger') && method === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ status: 'ok', success: true })
        });
      } else if (url.includes('/api/config/read')) {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        if (path.includes('01_log')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ log: { loglevel: 'warning' } })
          });
        } else if (path.includes('02_dns')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ dns: { servers: ['8.8.8.8'] } })
          });
        } else if (path.includes('03_inbounds')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ inbounds: [] })
          });
        } else if (path.includes('04_outbounds.manual')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              outbounds: [
                {
                  tag: 'existing-proxy',
                  protocol: 'vless',
                  settings: { vnext: [{ address: '1.2.3.4', port: 443 }] }
                }
              ]
            })
          });
        } else if (path.includes('04_outbounds')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              outbounds: [
                { tag: 'direct', protocol: 'freedom' },
                { tag: 'block', protocol: 'blackhole' },
                { tag: 'dns-out', protocol: 'dns' }
              ]
            })
          });
        } else if (path.includes('05_routing')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              routing: {
                domainStrategy: 'IPIfNonMatch',
                rules: [{ type: 'field', port: '53', outboundTag: 'dns-out' }]
              }
            })
          });
        } else if (path.includes('06_policy')) {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({ policy: { levels: { '0': {} }, system: {} } })
          });
        } else {
          await route.fulfill({
            status: 200,
            contentType: 'application/json',
            body: JSON.stringify({
              routing: { rules: [] },
              dns: { servers: [] },
              inbounds: [],
              outbounds: [{ tag: 'existing-proxy', protocol: 'vless' }]
            })
          });
        }
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([
            {
              name: '04_outbounds.manual.json',
              path: '/opt/etc/xray/configs/04_outbounds.manual.json',
              size: 200
            }
          ])
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  });

  test('Traffic page: отображает многомерные вкладки статистики Xray (Outbounds, Inbounds, Users)', async ({
    page
  }) => {
    await page.goto('/#/traffic');

    const section = page.locator('[data-testid="xray-stats-section"]');
    await expect(section).toBeVisible({ timeout: 5000 });

    const tabOutbounds = page.locator('[data-testid="xray-tab-outbounds"]');
    const tabInbounds = page.locator('[data-testid="xray-tab-inbounds"]');
    const tabUsers = page.locator('[data-testid="xray-tab-users"]');

    await expect(tabOutbounds).toBeVisible();
    await expect(tabInbounds).toBeVisible();
    await expect(tabUsers).toBeVisible();

    // По умолчанию выбрана вкладка Outbounds
    const tag = page.locator('[data-testid="xray-stats-tag"]');
    await expect(tag.first()).toHaveText('proxy-out');

    // Переключаемся на Inbounds
    await tabInbounds.click();
    await expect(page.locator('[data-testid="xray-stats-tag"]').first()).toHaveText('mixed-in');

    // Переключаемся на Users
    await tabUsers.click();
    await expect(page.locator('[data-testid="xray-stats-tag"]').first()).toHaveText('user1@domain');
  });

  test('Logs page: кнопка перечитывания логов Xray доступна в тулбаре и вызывает эндпоинт', async ({
    page
  }) => {
    let restartCalled = false;
    await page.route('**/api/xray/restart-logger', async (route) => {
      restartCalled = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ status: 'ok', success: true })
      });
    });

    await page.goto('/#/logs');

    const restartBtn = page.locator('[data-testid="restart-xray-logger-btn"]');
    await expect(restartBtn).toBeVisible({ timeout: 5000 });

    await restartBtn.click();
    expect(restartCalled).toBe(true);

    // Кнопка переходит в состояние кулдауна (disabled)
    await expect(restartBtn).toBeDisabled();
  });

  test('Конструктор Xray: генерация ключа Shadowsocks 2022 заполняет поле пароля и валидирует длину', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Перейти на вкладку Outbounds
    const outboundsTab = page
      .locator(
        '[data-testid="xray-section-tabs"] button:has-text("Outbounds"), [data-tab="outbounds"]'
      )
      .first();
    await expect(outboundsTab).toBeVisible({ timeout: 5000 });
    await outboundsTab.click();

    // Открыть форму добавления узла вручную
    const addBtn = page.locator('button.add-btn:has-text("Добавить вручную")').first();
    await expect(addBtn).toBeVisible({ timeout: 5000 });
    await addBtn.click();

    // Выбрать протокол Shadowsocks
    await page.locator('#outbound-protocol').selectOption('shadowsocks');

    // Проверить доступность шифров 2022-blake3
    const cipherSelect = page.locator('#outbound-ss-cipher');
    await expect(cipherSelect).toBeVisible();
    await cipherSelect.selectOption('2022-blake3-aes-128-gcm');

    // Кнопка генерации ключа должна быть видна
    const genKeyBtn = page.locator('button:has-text("Сгенерировать ключ")');
    await expect(genKeyBtn).toBeVisible();

    // Клик по кнопке генерации ключа
    await genKeyBtn.click();

    // Поле пароля должно заполниться ключом base64 (24 символа для 16 байт)
    const passwordInput = page.locator('#outbound-ss-password');
    const val = await passwordInput.inputValue();
    expect(val.length).toBe(24);
  });

  test('Конструктор Xray: форма WireGuard валидирует поле reserved на 3 числа в диапазоне 0..255', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    const outboundsTab = page
      .locator(
        '[data-testid="xray-section-tabs"] button:has-text("Outbounds"), [data-tab="outbounds"]'
      )
      .first();
    await expect(outboundsTab).toBeVisible({ timeout: 5000 });
    await outboundsTab.click();

    const addBtn = page.locator('button.add-btn:has-text("Добавить вручную")').first();
    await addBtn.click();

    // Выбрать WireGuard
    await page.locator('#outbound-protocol').selectOption('wireguard');
    await page.locator('#outbound-tag').fill('wg-test');
    await page.locator('#outbound-wg-secret-key').fill('a'.repeat(44));
    await page.locator('#outbound-wg-public-key').fill('b'.repeat(44));
    await page.locator('#outbound-wg-endpoint').fill('192.168.1.1:51820');

    // Невалидный reserved (> 255)
    await page.locator('#outbound-wg-reserved').fill('300, 400, 500');
    await page.locator('.modal-form-card button.btn-primary').click();

    // Модальное окно не закрывается из-за ошибки валидации
    await expect(page.locator('#outbound-tag')).toBeVisible();

    // Валидный reserved (0..255)
    await page.locator('#outbound-wg-reserved').fill('10, 20, 30');
    await page.locator('.modal-form-card button.btn-primary').click();

    // Узел успешно добавлен в список
    await expect(page.locator('.outbounds-list')).toContainText('wg-test');
  });

  test('Конструктор Xray: dialerProxy отображает цепочку при выборе родительского узла', async ({
    page
  }) => {
    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    const outboundsTab = page
      .locator(
        '[data-testid="xray-section-tabs"] button:has-text("Outbounds"), [data-tab="outbounds"]'
      )
      .first();
    await expect(outboundsTab).toBeVisible({ timeout: 5000 });
    await outboundsTab.click();

    const addBtn = page.locator('button.add-btn:has-text("Добавить вручную")').first();
    await addBtn.click();

    // Проверяем наличие селектора dialerProxy
    const dialerSelect = page.locator('#outbound-dialer-proxy');
    await expect(dialerSelect).toBeVisible();

    // Выбираем существующий прокси для создания цепочки
    await dialerSelect.selectOption({ label: 'existing-proxy (vless)' });

    // Должно появиться превью цепочки dialerChainPreview
    const chainPreview = page.locator('.dialer-chain-preview');
    await expect(chainPreview).toBeVisible();
    await expect(chainPreview).toContainText('DIRECT');
  });
});
