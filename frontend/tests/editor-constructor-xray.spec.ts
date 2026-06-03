/**
 * editor-constructor-xray.spec.ts — Интеграционные тесты Xray-конструктора.
 *
 * RED-тесты (падают до реализации в Plan 15.2-02/03):
 *   D-01  — конструктор загружает live-конфиги при открытии
 *   D-05  — Apply показывает confirm-диалог перед сохранением
 *   D-19  — restart не вызывается без подтверждения диалога
 */

import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

// ---------------------------------------------------------------------------
// Мок-данные для 6 файлов Xray + manual outbounds
// ---------------------------------------------------------------------------
function getMockXrayFile(path: string): string {
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
          { type: 'field', ip: ['geoip:private'], outboundTag: 'direct' },
          { type: 'field', network: 'tcp,udp', outboundTag: 'PROXY_TAG' }
        ]
      }
    });
  }
  if (path.includes('06_policy')) {
    return JSON.stringify({
      policy: {
        levels: { '0': { handshake: 4, connIdle: 300, uplinkOnly: 2, downlinkOnly: 5 } },
        system: {
          statsInboundUplink: false,
          statsInboundDownlink: false
        }
      }
    });
  }
  return JSON.stringify({});
}

// ---------------------------------------------------------------------------
// Общий beforeEach с mock setup
// ---------------------------------------------------------------------------
test.describe('Xray Constructor integration test suite', () => {
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

    // 2. Перехватить все /api/** запросы
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

      } else if (url.includes('/api/config/read') && method === 'GET') {
        // ВАЖНО: проверяем именно GET (исправление бага Phase 15.1)
        const reqUrl = new URL(url);
        const path = reqUrl.searchParams.get('path') || '';
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: getMockXrayFile(path)
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
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });

      } else if (url.includes('/api/service/control') && method === 'POST') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true })
        });

      } else if (url.includes('/api/templates/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
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

  // -------------------------------------------------------------------------
  // D-01: Конструктор загружает live-конфиги при открытии
  // -------------------------------------------------------------------------
  test('конструктор загружает live-конфиги Xray при открытии (D-01)', async ({ page }) => {
    await page.goto('/#/constructor');

    // Переключиться на Xray-конструктор
    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Конструктор должен показать вкладки разделов Xray (D-03)
    await expect(page.locator('[data-testid="xray-section-tabs"]')).toBeVisible({ timeout: 5000 });

    // Вкладка Routing должна быть видима и активна по умолчанию
    const routingTab = page.locator('[data-tab="routing"], button:has-text("Routing"), button:has-text("Маршрутизация")').first();
    await expect(routingTab).toBeVisible({ timeout: 3000 });
  });

  test('Xray-конструктор показывает вкладки всех 6 разделов (D-03)', async ({ page }) => {
    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    const tabs = page.locator('[data-testid="xray-section-tabs"]');
    await expect(tabs).toBeVisible({ timeout: 5000 });

    // Все 6 разделов должны быть доступны: Log, DNS, Inbounds, Outbounds, Routing, Policy
    for (const tab of ['log', 'dns', 'inbounds', 'outbounds', 'routing', 'policy']) {
      await expect(tabs.locator(`[data-tab="${tab}"]`).first()).toBeVisible();
    }
  });

  // -------------------------------------------------------------------------
  // D-05 + D-19: Apply показывает confirm-диалог, restart не без подтверждения
  // -------------------------------------------------------------------------
  test('Apply Changes показывает confirm-диалог перед сохранением (D-05, D-19)', async ({ page }) => {
    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Нажать кнопку Apply Changes
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible({ timeout: 5000 });
    await applyBtn.click();

    // Должен появиться confirm-диалог (D-19)
    const confirmDialog = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(confirmDialog).toBeVisible({ timeout: 3000 });
  });

  test('restart не вызывается без подтверждения confirm-диалога (D-19)', async ({ page }) => {
    let serviceControlCalled = false;

    // Перехватить вызов service/control ДО beforeEach-мока (route.fulfill первым матчит)
    await page.route('**/api/service/control', async (route) => {
      serviceControlCalled = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Нажать Apply Changes — должен открыться диалог, НЕ запустить restart
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible({ timeout: 5000 });
    await applyBtn.click();

    // Диалог открылся
    const confirmDialog = page.locator('[data-testid="apply-confirm-dialog"]');
    await expect(confirmDialog).toBeVisible({ timeout: 3000 });

    // Закрыть диалог без подтверждения
    const cancelBtn = confirmDialog.locator('button:has-text("Отмена"), button:has-text("Cancel")').first();
    await cancelBtn.click();

    // service/control НЕ должен был быть вызван
    expect(serviceControlCalled).toBe(false);
  });

  // -------------------------------------------------------------------------
  // D-07: Вкладка Outbounds показывает read-only список тегов
  // -------------------------------------------------------------------------
  test('вкладка Outbounds показывает теги из mock-файлов (D-07)', async ({ page }) => {
    await page.goto('/#/constructor');

    const xrayBtn = page.locator('.constructor-kernel-toggle button:has-text("Xray")');
    await expect(xrayBtn).toBeVisible({ timeout: 5000 });
    await xrayBtn.click();

    // Перейти на вкладку Outbounds
    const outboundsTab = page.locator(
      '[data-testid="xray-section-tabs"] button:has-text("Outbounds"), [data-tab="outbounds"]'
    ).first();
    await expect(outboundsTab).toBeVisible({ timeout: 5000 });
    await outboundsTab.click();

    // Теги из mock (direct, block, dns-out, my-proxy) должны присутствовать в списке
    await expect(page.locator('text=direct').first()).toBeVisible({ timeout: 3000 });
  });
});
