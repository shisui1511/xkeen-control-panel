import { test, expect } from '@playwright/test';
import { setupMocks } from './helpers/api-mocks';

test.use({ locale: 'ru-RU' });

test.describe('Phase 106: вкладка слушателей Mihomo (MIHO-07)', () => {
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

  test('Сценарий 1: Конфиг без слушателей отображает пустое состояние без счетчика', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    const sampleYaml = 'mixed-port: 7890\nproxies:\n  - name: DirectNode\n    type: direct\n';

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/yaml',
        body: sampleYaml
      });
    });

    await page.goto('/#/constructor');

    // Выбираем ядро Mihomo
    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible({ timeout: 10000 });
    await mihomoKernelBtn.click();

    // Кликаем по вкладке Слушатели
    const listenersTab = page.locator('button.sec-tab:has-text("Слушатели")');
    await expect(listenersTab).toBeVisible({ timeout: 10000 });
    await listenersTab.click();

    // Счетчик на вкладке отсутствует
    const badge = listenersTab.locator('.sec-count');
    await expect(badge).toHaveCount(0);

    // Подсказка и кнопка добавления видны
    const hint = page.locator('.rulesets-hint');
    await expect(hint).toBeVisible();
    await expect(hint).toContainText('дополнительных входящих портов');

    const addBtn = page.locator('button.add-btn:has-text("Добавить слушатель")');
    await expect(addBtn).toBeVisible();
  });

  test('Сценарий 2: Добавление слушателя обновляет счетчик и предпросмотр YAML', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    const sampleYaml = 'mixed-port: 7890\nproxies:\n  - name: DirectNode\n    type: direct\n';

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/yaml',
        body: sampleYaml
      });
    });

    await page.goto('/#/constructor');

    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible({ timeout: 10000 });
    await mihomoKernelBtn.click();

    const listenersTab = page.locator('button.sec-tab:has-text("Слушатели")');
    await expect(listenersTab).toBeVisible({ timeout: 10000 });
    await listenersTab.click();

    // Открываем форму добавления
    const addBtn = page.locator('button.add-btn:has-text("Добавить слушатель")');
    await expect(addBtn).toBeVisible();
    await addBtn.click();

    // Заполняем имя и порт
    const nameInput = page.locator('#listener-name');
    await expect(nameInput).toBeVisible();
    await nameInput.fill('tv-in');

    const portInput = page.locator('#listener-port');
    await expect(portInput).toBeVisible();
    await portInput.fill('7899');

    // Сохраняем форму
    const saveBtn = page.locator('.form-actions button.btn-primary');
    await expect(saveBtn).toBeVisible();
    await saveBtn.click();

    // В списке появился элемент
    const itemRow = page.locator('.item-row');
    await expect(itemRow).toHaveCount(1);
    await expect(itemRow).toContainText('tv-in');
    await expect(itemRow).toContainText('7899');

    // На вкладке появился счетчик 1
    const badge = listenersTab.locator('.sec-count');
    await expect(badge).toBeVisible();
    await expect(badge).toHaveText('1');

    // Проверяем предпросмотр YAML
    const yamlPreview = page.locator('#mihomo-yaml-preview, .yaml-preview');
    await expect(yamlPreview).toBeVisible();
    const previewText = await yamlPreview.innerText();
    expect(previewText).toContain('listeners:');
    expect(previewText).toContain('name: "tv-in"');
    expect(previewText).toContain('port: 7899');
  });

  test('Сценарий 3: Коллизия порта слушателя порождает мягкое предупреждение без прерывания сохранения', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    // Конфиг с mixed-port: 7890
    const sampleYaml = 'mixed-port: 7890\nproxies:\n  - name: DirectNode\n    type: direct\n';
    let smartMergeCalled = false;
    let saveConfigCalled = false;
    let interceptedTemplateContent = '';

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/yaml',
        body: sampleYaml
      });
    });

    await page.route('**/api/config/smart-merge', async (route) => {
      smartMergeCalled = true;
      const body = route.request().postDataJSON();
      interceptedTemplateContent = body?.template_content || '';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            content: 'mixed-port: 7890\nlisteners:\n  - name: conflict-in\n    port: 7890\n',
            stats: { proxies: 1 }
          }
        })
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      saveConfigCalled = true;
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: { warnings: [] }
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

    await page.goto('/#/constructor');

    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible({ timeout: 10000 });
    await mihomoKernelBtn.click();

    const listenersTab = page.locator('button.sec-tab:has-text("Слушатели")');
    await expect(listenersTab).toBeVisible({ timeout: 10000 });
    await listenersTab.click();

    // Добавляем слушатель с портом 7890, конфликтующим с mixed-port: 7890
    const addBtn = page.locator('button.add-btn:has-text("Добавить слушатель")');
    await expect(addBtn).toBeVisible();
    await addBtn.click();

    await page.locator('#listener-name').fill('conflict-in');
    await page.locator('#listener-port').fill('7890');
    await page.locator('.form-actions button.btn-primary').click();

    // Нажимаем Применить
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible();
    await applyBtn.click();

    // Если открылся модал подтверждения, подтверждаем
    const confirmModal = page.locator('[data-testid="apply-confirm-dialog"]');
    if (await confirmModal.isVisible({ timeout: 2000 }).catch(() => false)) {
      const confirmBtn = confirmModal.locator('button.btn-primary');
      await confirmBtn.click();
    }

    // Проверяем наличие мягкого предупреждения о коллизии портов
    const warningsBlock = page.locator('.preflight-warnings');
    await expect(warningsBlock).toBeVisible({ timeout: 10000 });
    await expect(warningsBlock).toContainText('7890');

    // Проверяем, что запрос слияния и запрос сохранения были выполнены
    expect(smartMergeCalled).toBe(true);
    expect(saveConfigCalled).toBe(true);
    expect(interceptedTemplateContent).toContain('name: "conflict-in"');
  });

  test('Сценарий 4: Экзотический слушатель переводит секцию в read-only и сохраняется при слиянии', async ({
    page
  }) => {
    await setupMocks(page, 'mihomo');

    // Конфиг с экзотическим типом tuic
    const exoticYaml = `mixed-port: 7890
listeners:
  - name: tuic-in
    type: tuic
    port: 8443
    listen: 0.0.0.0
proxies:
  - name: DirectNode
    type: direct
`;
    let interceptedTemplateContent = '';

    await page.route('**/api/config/read**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'text/yaml',
        body: exoticYaml
      });
    });

    await page.route('**/api/config/smart-merge', async (route) => {
      const body = route.request().postDataJSON();
      interceptedTemplateContent = body?.template_content || '';
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          success: true,
          data: {
            content: exoticYaml,
            stats: { proxies: 1 }
          }
        })
      });
    });

    await page.route('**/api/config/save**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true, data: {} })
      });
    });

    await page.route('**/api/service/control**', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ success: true })
      });
    });

    await page.goto('/#/constructor');

    const mihomoKernelBtn = page.locator('.constructor-kernel-toggle button:has-text("Mihomo")');
    await expect(mihomoKernelBtn).toBeVisible({ timeout: 10000 });
    await mihomoKernelBtn.click();

    const listenersTab = page.locator('button.sec-tab:has-text("Слушатели")');
    await expect(listenersTab).toBeVisible({ timeout: 10000 });
    await listenersTab.click();

    // Проверяем видимость баннера read-only (.alert.alert-warning)
    const alertWarning = page.locator('.sec-body .alert.alert-warning');
    await expect(alertWarning).toBeVisible();
    await expect(alertWarning).toContainText('только для чтения');

    // Кнопки добавления нет
    const addBtn = page.locator('button.add-btn:has-text("Добавить слушатель")');
    await expect(addBtn).toHaveCount(0);

    // Применяем изменения
    const applyBtn = page.locator('[data-testid="apply-changes-btn"]');
    await expect(applyBtn).toBeVisible();
    await applyBtn.click();

    const confirmModal = page.locator('[data-testid="apply-confirm-dialog"]');
    if (await confirmModal.isVisible({ timeout: 2000 }).catch(() => false)) {
      const confirmBtn = confirmModal.locator('button.btn-primary');
      await confirmBtn.click();
    }

    // Проверяем, что экзотическая секция listeners осталась в template_content
    await expect.poll(() => interceptedTemplateContent).toContain('listeners:');
    expect(interceptedTemplateContent).toContain('name: tuic-in');
    expect(interceptedTemplateContent).toContain('type: tuic');
    expect(interceptedTemplateContent).toContain('port: 8443');
  });
});
