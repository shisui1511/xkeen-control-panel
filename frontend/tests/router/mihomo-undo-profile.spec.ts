// e2e-kind: read
// e2e-pages: mihomo-gen
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';

// Откат конструктора Mihomo привязан к имени профиля (PART-03, D-23): снимок, сохранённый
// на другом профиле, не предлагается и стирается; снимок активного профиля доступен.
// Спек только читает состояние стенда: снимок кладётся в localStorage браузера, кнопка
// отката не нажимается, запись на роутер не идёт. Сеть не подменяется.

const KEY_YAML = 'xcp_prev_mihomo_yaml';
const KEY_PROFILE = 'xcp_prev_mihomo_profile';
const KEY_PATH = 'xcp_prev_mihomo_path';
const CONFIG_PATH = '/opt/etc/mihomo/config.yaml';
const SNAPSHOT = 'mixed-port: 7890\nmode: rule\n';
const UNDO = '[data-testid="mihomo-undo"]';

/** Имя профиля, в который пишет конструктор: активный при ссылке config.yaml, иначе config.yaml. */
async function activeProfile(page: Page): Promise<string> {
  const res = await page.request.get('/api/mihomo/profiles');
  expect(res.ok()).toBeTruthy();
  const body = (await res.json()) as { data?: { managed?: boolean; active?: string } };
  const state = body.data ?? {};
  return state.managed ? (state.active ?? '') : 'config.yaml';
}

/**
 * Конструктор Mihomo отрисован: схема загружена, сценарии на месте. При активном Xray
 * экран открывается на конструкторе Xray, вкладка Mihomo выбирается кнопкой (это только
 * переключение вида, запись на роутер не идёт).
 */
async function constructorReady(page: Page): Promise<void> {
  const workspace = page.locator('.main-content');
  const mihomoTab = workspace.getByRole('button', { name: 'Mihomo', exact: true });
  await expect(mihomoTab).toBeVisible({ timeout: T.page });
  if ((await mihomoTab.getAttribute('aria-pressed')) !== 'true') {
    await mihomoTab.click();
    await expect(mihomoTab).toHaveAttribute('aria-pressed', 'true');
  }
  await expect(workspace.locator('.loading-state-block')).toHaveCount(0, { timeout: T.page });
  await expect(workspace.locator('.gen-layout button[data-scenario-id]').first()).toBeAttached({
    timeout: T.page
  });
}

test.describe('undo', () => {
  test('undo: снимок другого профиля не предлагается', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    await page.addInitScript(
      ([yamlKey, profileKey, pathKey, snapshot, configPath]) => {
        localStorage.setItem(yamlKey, snapshot);
        localStorage.setItem(profileKey, 'rt-undo-other');
        localStorage.setItem(pathKey, configPath);
      },
      [KEY_YAML, KEY_PROFILE, KEY_PATH, SNAPSHOT, CONFIG_PATH]
    );

    await page.goto('/#/mihomo-gen');
    await constructorReady(page);

    // проверка снимка идёт при открытии экрана: чужой снимок удаляется целиком
    await expect
      .poll(() => page.evaluate((k) => localStorage.getItem(k), KEY_YAML), { timeout: T.page })
      .toBeNull();
    expect(await page.evaluate((k) => localStorage.getItem(k), KEY_PROFILE)).toBeNull();
    await expect(page.locator(UNDO)).toHaveCount(0);

    await guard.assertClean();
  });

  test('undo: снимок другого файла не предлагается и не стирается', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    const active = await activeProfile(page);
    expect(active).not.toBe('');
    // профиль тот же, а записанный файл — другой: отмена записала бы чужой YAML в этот файл
    await page.addInitScript(
      ([yamlKey, profileKey, pathKey, snapshot, profile]) => {
        localStorage.setItem(yamlKey, snapshot);
        localStorage.setItem(profileKey, profile);
        localStorage.setItem(pathKey, '/opt/etc/mihomo/profiles/rt-undo-other.yaml');
      },
      [KEY_YAML, KEY_PROFILE, KEY_PATH, SNAPSHOT, active]
    );

    await page.goto('/#/mihomo-gen');
    await constructorReady(page);

    await expect(page.locator(UNDO)).toHaveCount(0);
    // снимок остаётся: после возврата к тому файлу отмена снова нужна
    expect(await page.evaluate((k) => localStorage.getItem(k), KEY_YAML)).toBe(SNAPSHOT);

    await guard.assertClean();
  });

  test('undo: снимок активного профиля доступен', async ({ page }) => {
    const guard = attachConsoleGuard(page);
    await page.setViewportSize({ width: 1440, height: 900 });
    const active = await activeProfile(page);
    expect(active).not.toBe('');
    await page.addInitScript(
      ([yamlKey, profileKey, pathKey, snapshot, profile, configPath]) => {
        localStorage.setItem(yamlKey, snapshot);
        localStorage.setItem(profileKey, profile);
        localStorage.setItem(pathKey, configPath);
      },
      [KEY_YAML, KEY_PROFILE, KEY_PATH, SNAPSHOT, active, CONFIG_PATH]
    );

    await page.goto('/#/mihomo-gen');
    await constructorReady(page);

    // откат не нажимается: спек стенд не меняет
    await expect(page.locator(UNDO)).toBeVisible({ timeout: T.page });
    expect(await page.evaluate((k) => localStorage.getItem(k), KEY_PROFILE)).toBe(active);

    await guard.assertClean();
  });
});
