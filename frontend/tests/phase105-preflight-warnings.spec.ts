import { test, expect } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

test.use({ locale: 'ru-RU' });

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
    return JSON.stringify({
      inbounds: [
        {
          tag: 'tproxy-in',
          port: 12345,
          protocol: 'dokodemo-door'
        }
      ]
    });
  }
  if (path.includes('04_outbounds')) {
    return JSON.stringify({
      outbounds: [
        { tag: 'direct', protocol: 'freedom' },
        { tag: 'block', protocol: 'blackhole' },
        { tag: 'my-proxy', protocol: 'vless' }
      ]
    });
  }
  if (path.includes('05_routing')) {
    // Empty stub to trigger applyTemplateFiles on load (TMPL-01 pattern)
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

test.describe('Phase 105: Preflight Validation Warnings (TMPL-08, D-07, D-08)', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
    });
  });

  test('Сценарий 1: Сохранение в Редакторе отображает список предупреждений в порядке бэкенда', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    await page.route('**/api/config/list**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          { name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 128 }
        ])
      });
    });

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'port: 7890\nmode: rule\n'
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            warnings: [
              {
                code: 'preflight.lan_rdp'
              },
              {
                code: 'preflight.dns_loop'
              }
            ]
          }
        })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/editor');

    // Открываем файл в редакторе
    const fileRow = page.locator('.file-row:has-text("config.yaml")').first();
    await expect(fileRow).toBeVisible({ timeout: 10000 });
    await fileRow.click();

    await expect(page.locator('.editor-tab:has-text("config.yaml")')).toBeVisible({
      timeout: 10000
    });

    // Нажимаем Сохранить и применить
    const saveBtn = page.locator('button[title="Сохранить и применить"]');
    await expect(saveBtn).toBeVisible({ timeout: 10000 });
    await saveBtn.click();

    // Проверяем появление блока предупреждений
    const warningsBlock = page.locator('.preflight-warnings');
    await expect(warningsBlock).toBeVisible({ timeout: 10000 });

    const items = warningsBlock.locator('li');
    await expect(items).toHaveCount(2);

    // Проверяем порядок и перевод по кодам
    await expect(items.nth(0)).toHaveText(
      'Обнаружен риск перехвата локального трафика или портов управления (LAN/RDP/SSH)'
    );
    await expect(items.nth(1)).toHaveText(
      'Обнаружена петля DNS: upstream DNS направлен на собственный порт прокси'
    );
  });

  test('Сценарий 2: Сохранение в конструкторе Mihomo отображает предупреждения в порядке бэкенда', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    const existingYAML = 'mixed-port: 7890\nproxies:\n  - name: Node1\n    type: direct\n';
    const mergedYAML = 'mixed-port: 7890\nproxies:\n  - name: MergedNode\n    type: direct\n';

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/yaml',
        body: existingYAML
      });
    });

    await page.route('**/api/config/smart-merge', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            content: mergedYAML,
            stats: { proxies: 1, rules: 5 }
          }
        })
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            warnings: [
              {
                code: 'preflight.awg_flat_fields'
              },
              {
                code: 'preflight.dns_loop'
              }
            ]
          }
        })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/editor');

    // Переходим на вкладку Конструктор
    const constructorTab = page.locator('button.tab-btn:has-text("Конструктор")');
    await expect(constructorTab).toBeVisible({ timeout: 10000 });
    await constructorTab.click();

    // Выбираем ядро Mihomo
    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible({ timeout: 10000 });
    await mihomoKernelBtn.click();

    // Выбираем пресет
    const presetSelect = page.locator('select.preset-select, select#preset-select');
    await expect(presetSelect).toBeVisible({ timeout: 10000 });
    await presetSelect.selectOption('zkeen-selective');

    // Кликаем применить
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible({ timeout: 10000 });
    await applyBtn.click();

    // Если открылся модал подтверждения, подтверждаем
    const confirmModal = page.locator('[data-testid="apply-confirm-dialog"]');
    if (await confirmModal.isVisible({ timeout: 2000 }).catch(() => false)) {
      const confirmBtn = confirmModal.locator('button.btn-primary');
      await confirmBtn.click();
    }

    // Проверяем предупреждения
    const warningsBlock = page.locator('.preflight-warnings');
    await expect(warningsBlock).toBeVisible({ timeout: 10000 });

    const items = warningsBlock.locator('li');
    await expect(items).toHaveCount(2);

    await expect(items.nth(0)).toHaveText(
      'Параметры AmneziaWG должны быть вложены в блок amnezia-wg-option'
    );
    await expect(items.nth(1)).toHaveText(
      'Обнаружена петля DNS: upstream DNS направлен на собственный порт прокси'
    );
  });

  test('Сценарий 3: Сохранение в конструкторе Xray отображает предупреждения в порядке бэкенда', async ({
    page
  }) => {
    await setupMocks(page, 'xray');

    await page.route('**/api/config/read**', async (route) => {
      const url = route.request().url();
      const reqUrl = new URL(url);
      const path = reqUrl.searchParams.get('path') || '';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: getStubXrayFile(path)
      });
    });

    await page.route('**/api/config/smart-merge', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            content: JSON.stringify({ routing: { rules: [] } }),
            stats: { rules: 4, user_rules: 0 }
          }
        })
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      const url = route.request().url();
      const isRouting = url.includes('05_routing');
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            warnings: isRouting
              ? [
                  {
                    code: 'preflight.dns_over_vless'
                  },
                  {
                    code: 'preflight.lan_rdp'
                  }
                ]
              : []
          }
        })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    // Переход в конструктор: пустой rules в 05_routing.json автоматически запускает applyTemplateFiles
    await page.goto('/#/constructor');

    const xrayKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    if (await xrayKernelBtn.isVisible({ timeout: 3000 }).catch(() => false)) {
      await xrayKernelBtn.click();
    }

    // Проверяем появление предупреждений после сохранения
    const warningsBlock = page.locator('.preflight-warnings');
    await expect(warningsBlock).toBeVisible({ timeout: 10000 });

    const items = warningsBlock.locator('li');
    await expect(items).toHaveCount(2);

    await expect(items.nth(0)).toHaveText(
      'DNS-over-VLESS требует прямого резолвера для сервера прокси'
    );
    await expect(items.nth(1)).toHaveText(
      'Обнаружен риск перехвата локального трафика или портов управления (LAN/RDP/SSH)'
    );
  });

  test('Сценарий 4: Ответ с пустым списком замечаний не показывает панель в DOM', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    await page.route('**/api/config/list**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          { name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 128 }
        ])
      });
    });

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'port: 7890\nmode: rule\n'
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            warnings: []
          }
        })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")').first();
    await expect(fileRow).toBeVisible({ timeout: 10000 });
    await fileRow.click();

    await expect(page.locator('.editor-tab:has-text("config.yaml")')).toBeVisible({
      timeout: 10000
    });

    const saveBtn = page.locator('button[title="Сохранить и применить"]');
    await expect(saveBtn).toBeVisible({ timeout: 10000 });
    await saveBtn.click();

    // Проверяем, что блок предупреждений полностью отсутствует в DOM
    await expect(page.locator('.preflight-warnings')).toHaveCount(0);
  });

  test('Сценарий 5: Закрытие панели убирает её, а повторное сохранение показывает снова', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    await page.route('**/api/config/list**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          { name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 128 }
        ])
      });
    });

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'port: 7890\nmode: rule\n'
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            warnings: [
              {
                code: 'preflight.lan_rdp'
              }
            ]
          }
        })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")').first();
    await expect(fileRow).toBeVisible({ timeout: 10000 });
    await fileRow.click();

    await expect(page.locator('.editor-tab:has-text("config.yaml")')).toBeVisible({
      timeout: 10000
    });

    const saveBtn = page.locator('button[title="Сохранить и применить"]');
    await expect(saveBtn).toBeVisible({ timeout: 10000 });
    await saveBtn.click();

    // 1. Появилась панель
    const warningsBlock = page.locator('.preflight-warnings');
    await expect(warningsBlock).toBeVisible({ timeout: 10000 });
    await expect(warningsBlock.locator('li')).toHaveCount(1);

    // 2. Закрываем панель
    const closeBtn = warningsBlock.locator('.alert-close-btn');
    await expect(closeBtn).toBeVisible();
    await closeBtn.click();

    // 3. Панель исчезла из DOM
    await expect(page.locator('.preflight-warnings')).toHaveCount(0);

    // 4. Повторное сохранение показывает панель снова
    await saveBtn.click();
    await expect(page.locator('.preflight-warnings')).toBeVisible({ timeout: 10000 });
    await expect(page.locator('.preflight-warnings li')).toHaveCount(1);
  });

  test('Сценарий 6: Несколько предупреждений с одним кодом, но разными сообщениями отображаются без дедупликации (WR-01)', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    await page.route('**/api/config/list**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify([
          { name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 128 }
        ])
      });
    });

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/plain',
        body: 'port: 7890\nmode: rule\n'
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            warnings: [
              {
                code: 'preflight.port_conflict',
                message: 'Port conflict: port 1053 used by dns.listen'
              },
              {
                code: 'preflight.port_conflict',
                message: 'Port conflict: port 5000 used by redir-port'
              }
            ]
          }
        })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/editor');

    const fileRow = page.locator('.file-row:has-text("config.yaml")').first();
    await expect(fileRow).toBeVisible({ timeout: 10000 });
    await fileRow.click();

    const saveBtn = page.locator('button[title="Сохранить и применить"]');
    await expect(saveBtn).toBeVisible({ timeout: 10000 });
    await saveBtn.click();

    const warningsBlock = page.locator('.preflight-warnings');
    await expect(warningsBlock).toBeVisible({ timeout: 10000 });

    const items = warningsBlock.locator('li');
    await expect(items).toHaveCount(2);
    await expect(items.nth(0)).toHaveText('Port conflict: port 1053 used by dns.listen');
    await expect(items.nth(1)).toHaveText('Port conflict: port 5000 used by redir-port');
  });
});
