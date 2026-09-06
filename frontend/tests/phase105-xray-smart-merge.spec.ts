import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// Заглушечные файлы для тестирования авто-инициализации шаблона
function getStubXrayFile(path: string): string {
  if (path.includes('01_log')) {
    return JSON.stringify({ log: { loglevel: 'warning', dnsLog: false } });
  }
  if (path.includes('02_dns')) {
    return JSON.stringify({
      dns: {
        tag: 'dns-in',
        servers: ['8.8.8.8'],
        queryStrategy: 'UseIP',
        hosts: {}
      }
    });
  }
  if (path.includes('03_inbounds')) {
    return JSON.stringify({ inbounds: [] });
  }
  if (path.includes('04_outbounds.manual')) {
    return JSON.stringify({
      outbounds: [{ tag: 'my-proxy', protocol: 'vless' }]
    });
  }
  if (path.includes('04_outbounds')) {
    // Кастомный outbound пользователя + базовые
    return JSON.stringify({
      outbounds: [
        { tag: 'direct', protocol: 'freedom' },
        { tag: 'block', protocol: 'blackhole' },
        { tag: 'my-proxy', protocol: 'vless' }
      ]
    });
  }
  if (path.includes('05_routing')) {
    // Пустой stub без правил (trigger для applyTemplateFiles)
    return JSON.stringify({
      routing: {
        domainStrategy: 'IPIfNonMatch',
        rules: []
      }
    });
  }
  if (path.includes('06_policy')) {
    return JSON.stringify({
      policy: {
        levels: { '0': { handshake: 4, connIdle: 300, uplinkOnly: 2, downlinkOnly: 5 } }
      }
    });
  }
  return JSON.stringify({});
}

test.describe('Phase 105: Xray Constructor Smart-Merge (TMPL-01, TMPL-07)', () => {
  test.beforeEach(async ({ page }) => {
    // 1. Отключить Service Worker
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
    });
  });

  test('применение шаблона Xray вызывает smart-merge, сохраняет исключения TMPL-07 и показывает toast со статистикой', async ({
    page
  }) => {
    let smartMergeCalled = false;
    let smartMergePayload: any = null;
    const saveCalls: Array<{ path: string; body: string }> = [];

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
                xray: { installed: true, version: '1.8.24', channel: 'stable' },
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'xray'
            }
          })
        });
      } else if (url.includes('/api/rules/custom')) {
        // Пользовательские исключения TMPL-07
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            data: [
              {
                id: 'r1',
                type: 'domain_suffix',
                value: 'instagram.com',
                target: 'proxy',
                comment: 'Instagram exception',
                enabled: true
              }
            ]
          })
        });
      } else if (url.includes('/api/config/smart-merge') && method === 'POST') {
        smartMergeCalled = true;
        smartMergePayload = route.request().postDataJSON();

        // Ответ сервера: объединенный routing.rules с instagram.com и замененным PROXY_TAG
        const mergedContent = JSON.stringify(
          {
            routing: {
              domainStrategy: 'IPIfNonMatch',
              rules: [
                {
                  type: 'field',
                  domain: ['instagram.com'],
                  outboundTag: 'my-proxy'
                },
                {
                  type: 'field',
                  ip: ['geoip:private'],
                  outboundTag: 'direct'
                },
                {
                  type: 'field',
                  domain: ['geosite:category-ads-all'],
                  outboundTag: 'block'
                },
                {
                  type: 'field',
                  domain: ['geosite:geolocation-!cn'],
                  outboundTag: 'my-proxy'
                }
              ]
            }
          },
          null,
          2
        );

        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({
            success: true,
            data: {
              content: mergedContent,
              stats: {
                rules: 4,
                user_rules: 1
              }
            }
          })
        });
      } else if (url.includes('/api/config/read') && method === 'GET') {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: getStubXrayFile(path)
        });
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
      } else if (url.includes('/api/config/save') && method === 'POST') {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        const body = route.request().postData() || '';
        saveCalls.push({ path, body });
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    await page.goto('/#/constructor');

    // Переключиться на Xray-конструктор
    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Ожидаем появления тоста с успешным слиянием (D-04)
    const toast = page.locator('.toast--success, [role="alert"]');
    await expect(toast.first()).toBeVisible({ timeout: 5000 });
    const toastText = await toast.first().textContent();
    expect(toastText).toContain('Конфигурация применена через Smart Merge');
    expect(toastText).toContain('4 правила');
    expect(toastText).toContain('из них 1 ваше исключение');

    // 1. Проверяем вызов smart-merge
    expect(smartMergeCalled).toBe(true);
    expect(smartMergePayload).not.toBeNull();
    expect(smartMergePayload.type).toBe('xray');
    expect(smartMergePayload.target_file).toBe('05_routing.json');
    expect(smartMergePayload.active_outbound_tag).toBe('my-proxy');
    // template_content на этапе отправки обязан содержать плейсхолдер PROXY_TAG
    expect(smartMergePayload.template_content).toContain('PROXY_TAG');

    // 2. Проверяем сохранение 05_routing.json через save
    const routingSave = saveCalls.find((c) => c.path.includes('05_routing.json'));
    expect(routingSave).toBeDefined();
    // Доказательство D-06: пользовательское правило для instagram.com присутствует в сохраненном файле
    expect(routingSave?.body).toContain('instagram.com');
    // Доказательство подстановки тега бэкендом
    expect(routingSave?.body).toContain('my-proxy');
  });

  test('при ошибке smart-merge (500) сохранение 05_routing.json отменяется без fallback (D-05)', async ({
    page
  }) => {
    let smartMergeCalled = false;
    const saveCalls: Array<{ path: string; body: string }> = [];

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
                xray: { installed: true, version: '1.8.24', channel: 'stable' },
                mihomo: { installed: true, version: '1.18.0', channel: 'stable' }
              },
              active_kernel: 'xray'
            }
          })
        });
      } else if (url.includes('/api/rules/custom')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: [] })
        });
      } else if (url.includes('/api/config/smart-merge') && method === 'POST') {
        smartMergeCalled = true;
        // Симулируем 500 Internal Server Error
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({
            success: false,
            error: 'Backend merge failed'
          })
        });
      } else if (url.includes('/api/config/read') && method === 'GET') {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: getStubXrayFile(path)
        });
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/config/save') && method === 'POST') {
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        const body = route.request().postData() || '';
        saveCalls.push({ path, body });
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });

    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Ожидаем появления тоста об ошибке smart-merge
    const errorToast = page.locator('.toast--error, [role="alert"]');
    await expect(errorToast.first()).toBeVisible({ timeout: 5000 });
    const errorText = await errorToast.first().textContent();
    expect(errorText).toContain('Не удалось выполнить безопасное слияние конфигурации');

    // Проверяем: smart-merge был вызван
    expect(smartMergeCalled).toBe(true);

    // Доказательство D-05: сохранение 05_routing.json НЕ выполнялось (0 вызовов)
    const routingSaveCalls = saveCalls.filter((c) => c.path.includes('05_routing.json'));
    expect(routingSaveCalls.length).toBe(0);
  });
});
