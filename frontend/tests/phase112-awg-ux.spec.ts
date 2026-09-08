import { test, expect } from '@playwright/test';

test.use({ locale: 'ru-RU' });

test.describe('Phase 112: Import-first Node Constructor & AWG UX', () => {
  const setupRoutes = async (page: any, activeKernel = 'mihomo') => {
    await page.addInitScript(() => {
      Object.defineProperty(window.navigator, 'serviceWorker', {
        value: undefined,
        writable: false,
        configurable: true
      });
      window.localStorage.setItem('lang', 'ru');
    });

    await page.route('**/api/**', async (route: any) => {
      const url = route.request().url();

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
              active_kernel: activeKernel,
              mihomo: {
                reachable: true,
                process_running: true,
                api_reachable: true,
                api_authenticated: true
              }
            }
          })
        });
      } else if (url.includes('/api/config/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(
            activeKernel === 'mihomo'
              ? [{ name: 'config.yaml', path: '/opt/etc/mihomo/config.yaml', size: 1200 }]
              : [
                  {
                    name: 'xray-config.json',
                    path: '/opt/etc/xray/configs/xray-config.json',
                    size: 1000
                  }
                ]
          )
        });
      } else if (url.includes('/api/config/read')) {
        await route.fulfill({
          status: 200,
          contentType: 'text/plain',
          body:
            activeKernel === 'mihomo'
              ? 'port: 7890\nproxies: []\nproxy-groups: []\nrules: []\n'
              : JSON.stringify({ routing: { rules: [] }, outbounds: [], inbounds: [] })
        });
      } else if (url.includes('/api/templates/list')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify([])
        });
      } else if (url.includes('/api/config/validate')) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ valid: true })
        });
      } else {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ success: true, data: {} })
        });
      }
    });
  };

  test('Mihomo Constructor: Import-first кнопки, модалки и AWG пресеты', async ({ page }) => {
    await setupRoutes(page, 'mihomo');
    await page.goto('/#/constructor');

    // 1. Проверяем наличие Import-first кнопок в секции прокси
    const importBtn = page.locator('button.btn-action-primary:has-text("Импорт узла")');
    const manualBtn = page.locator('button.btn-secondary:has-text("+ Добавить вручную")');
    const subscrBtn = page.locator('button:has-text("Импортировать из Xray-подписок")');

    await expect(importBtn).toBeVisible();
    await expect(manualBtn).toBeVisible();
    await expect(subscrBtn).toBeVisible();

    // 2. Проверяем модалку импорта нод
    await importBtn.click();
    const importModal = page.locator('.modal-backdrop');
    await expect(importModal).toBeVisible();
    await expect(page.locator('.modal-title:has-text("Импорт")')).toBeVisible();

    // Проверяем 3 источника импорта
    await expect(page.locator('.import-source-tabs button:has-text("Ссылки")')).toBeVisible();
    await expect(
      page.locator('.import-source-tabs button:has-text("Файл конфигурации (.conf)")')
    ).toBeVisible();
    await expect(
      page.locator('.import-source-tabs button:has-text("Вставить из буфера")')
    ).toBeVisible();

    // Закрываем модалку импорта крестиком
    await page.locator('.modal-backdrop button.modal-close-btn').click();
    await expect(importModal).not.toBeVisible();

    // 3. Проверяем модалку добавления ноды вручную
    await manualBtn.click();
    await expect(importModal).toBeVisible();
    await expect(page.locator('.modal-title:has-text("Новая нода")')).toBeVisible();

    // Переключаем протокол на WireGuard
    const typeSelect = page.locator('#proxy-type');
    await typeSelect.selectOption('wireguard');

    // Включаем опцию AmneziaWG
    const awgToggle = page.locator('#proxy-awg-enabled');
    await expect(awgToggle).toBeVisible();
    await awgToggle.check();

    // Проверяем наличие блока обфускации AmneziaWG и пресетов
    const awgBlock = page.locator('.awg-options-block');
    await expect(awgBlock).toBeVisible();
    const presetSelect = page.locator('#proxy-awg-preset');
    await expect(presetSelect).toBeVisible();
    const randomBtn = page.locator('button.regenerate-btn');
    await expect(randomBtn).toBeVisible();

    // Выбираем пресет aggressive-dpi и проверяем заполнение полей
    await presetSelect.selectOption('aggressive-dpi');
    const jcInput = page.locator('#proxy-awg-jc');
    await expect(jcInput).not.toHaveValue('');

    // Закрываем модалку формы крестиком
    await page.locator('.modal-backdrop button.modal-close-btn').click();
    await expect(importModal).not.toBeVisible();

    // 4. Переключаемся на вкладку правил и проверяем закрепленную строку правила MATCH
    const rulesTab = page.locator('button.sec-tab:has-text("Правила")');
    await expect(rulesTab).toBeVisible();
    await rulesTab.click();

    const matchRow = page.locator('.match-rule-row');
    await expect(matchRow).toBeVisible();
    await expect(matchRow).toContainText('MATCH');
  });

  test('Xray Constructor: Import-first кнопки и модалки Outbounds', async ({ page }) => {
    await setupRoutes(page, 'xray');
    await page.goto('/#/constructor');

    // Переключаемся на секцию outbounds
    const outboundsTab = page.locator('button.sec-tab[data-tab="outbounds"]');
    await expect(outboundsTab).toBeVisible();
    await outboundsTab.click();

    // 1. Проверяем Import-first кнопки в шапке секции Outbounds
    const importBtn = page.locator('.constructor-outbounds-header button.btn-action-primary');
    const manualBtn = page.locator('.constructor-outbounds-header button.btn-secondary');

    await expect(importBtn).toBeVisible();
    await expect(manualBtn).toBeVisible();

    // 2. Открываем модалку импорта
    await importBtn.click();
    const importModal = page.locator('.modal-backdrop');
    await expect(importModal).toBeVisible();
    await expect(page.locator('.modal-title:has-text("Импорт")')).toBeVisible();

    // Проверяем 3 источника импорта
    await expect(page.locator('.import-source-tabs button:has-text("Ссылки")')).toBeVisible();
    await expect(
      page.locator('.import-source-tabs button:has-text("Файл конфигурации (.conf)")')
    ).toBeVisible();
    await expect(
      page.locator('.import-source-tabs button:has-text("Вставить из буфера")')
    ).toBeVisible();

    // Закрываем модалку импорта крестиком
    await page.locator('.modal-backdrop button.modal-close-btn').click();
    await expect(importModal).not.toBeVisible();

    // 3. Открываем модалку ручного добавления outbound
    await manualBtn.click();
    await expect(importModal).toBeVisible();
    await expect(page.locator('.modal-title:has-text("Новый Outbound")')).toBeVisible();

    // Закрываем модалку крестиком
    await page.locator('.modal-backdrop button.modal-close-btn').click();
    await expect(importModal).not.toBeVisible();
  });
});
