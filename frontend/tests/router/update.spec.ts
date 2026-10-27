import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { test, expect, type Page } from '@playwright/test';
import AxeBuilder from '@axe-core/playwright';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { knownFailure } from './lib/known';
import { api } from './lib/nodes-api';

// Каналы и тип версии на карточке «Обновление» настоящей панели (D-28, D-29, D-32): страница
// «Настройки», вкладка обновлений. Источник истины — сама панель стенда и её API; сеть не
// подменяется. Ожидаемые подписи читаются из словарей ru и en (в спеке они не дублируются);
// вид версии для ожидания спек определяет по суффиксу номера сборки сам, функции продукта
// не импортируются.
//
// Состояние автоустановки (D-15, D-26, D-27): тесты `update auto-status:` сверяют строки
// карточки автообновлений, блока «Доступна версия» и баннера на главной с полем auto_state из
// GET /api/update/state. Сроки и даты спек не считает: берёт код состояния у самой панели.
// Кандидат автоустановки (auto_version) на Основном может быть старше новейшей версии: тогда
// строка называет его номер, а «Срочное» у блока новейшей версии не рисуется.
// Тесты `update axe:` проверяют доступность и горизонтальную прокрутку в двух темах.
//
// Спек изменяющий (меняет канал панели, на время теста включает автоустановку), поэтому
// строки вида в шапке нет. Канал, автоустановка и окно, прочитанные в beforeEach, возвращаются
// в afterEach. Вместе с автоустановкой тест ставит минутное окно, которое начинается через
// 12 ч 30 мин от текущего момента по UTC: пояса с таким сдвигом нет, ближайшие пояса отстоят
// на 15–30 мин, а включение длится секунды, поэтому фоновая установка во время теста не начнётся
// ни при каком поясе стенда.
//
// Бюджет обращений панели к GitHub API без токена (60 в час с адреса стенда): каждая загрузка
// вкладки обновлений и каждое сохранение канала — одно обращение. Счёт за прогон всего спека:
//   update channels: — 6 (на язык: переход, сохранение канала с повторной проверкой, перезагрузка);
//   update kinds: — 1;
//   update auto-status: — 2;
//   update axe: — 4;
// итого 13. Новые загрузки вкладки в этот спек добавляются только с пересчётом счёта.

const DICT = Object.fromEntries(
  ['ru', 'en'].map((lang) => [
    lang,
    JSON.parse(
      readFileSync(
        fileURLToPath(new URL(`../../src/locales/${lang}.json`, import.meta.url)),
        'utf8'
      )
    ) as Record<string, string>
  ])
) as Record<'ru' | 'en', Record<string, string>>;

/** Подпись из словаря языка; отсутствие ключа — ошибка спека, а не повод подставить текст. */
function text(lang: 'ru' | 'en', key: string): string {
  const value = DICT[lang][key];
  if (!value) throw new Error(`в словаре ${lang} нет ключа ${key}`);
  return value;
}

type Channel = 'stable' | 'beta';

interface AutoSaved {
  auto_install: boolean;
  install_window: string;
}

interface UpdateStateApi {
  channel?: string;
  has_update?: boolean;
  latest_version?: string;
  error?: string;
  auto_install?: boolean;
  install_window?: string;
  auto_state?: string;
  auto_version?: string;
}

let savedChannel: Channel | null = null;
let savedAuto: AutoSaved | null = null;
let autoTouched = false;

/** Вид версии по суффиксу номера: без суффикса — stable, beta и rc — beta, остальное — alpha. */
function kindOf(version: string): 'stable' | 'beta' | 'alpha' {
  const pre = version.replace(/^v/, '').split('+')[0].split('-')[1] ?? '';
  if (pre === '') return 'stable';
  if (pre.startsWith('beta') || pre.startsWith('rc')) return 'beta';
  return 'alpha';
}

async function readChannel(page: Page): Promise<Channel> {
  const res = await page.request.get('/api/update/channel');
  expect(res.ok(), `GET /api/update/channel: http ${res.status()}`).toBe(true);
  const body = (await res.json()) as { data?: { channel?: string }; channel?: string };
  const value = body.data?.channel ?? body.channel;
  return value === 'beta' ? 'beta' : 'stable';
}

async function readUpdateState(page: Page): Promise<UpdateStateApi> {
  const res = await page.request.get('/api/update/state');
  expect(res.ok(), `GET /api/update/state: http ${res.status()}`).toBe(true);
  const body = (await res.json()) as { data?: UpdateStateApi } & UpdateStateApi;
  return body.data ?? body;
}

test.beforeEach(async ({ page }) => {
  savedChannel = await readChannel(page);
  const st = await readUpdateState(page);
  savedAuto = { auto_install: st.auto_install === true, install_window: st.install_window ?? '' };
  autoTouched = false;
});

test.afterEach(async ({ page }) => {
  const channel = savedChannel;
  const auto = savedAuto;
  const touched = autoTouched;
  savedChannel = null;
  savedAuto = null;
  autoTouched = false;
  // страница панели открыта — токен CSRF в хранилище есть; если до перехода не дошло,
  // ничего не менялось
  if (!channel || !page.url().includes('/#/')) return;
  if (touched && auto) {
    await api(page, 'POST', '/api/update/settings', {
      auto_install: auto.auto_install,
      ...(auto.install_window ? { install_window: auto.install_window } : {})
    });
  }
  if ((await readChannel(page)) !== channel) {
    await api(page, 'POST', '/api/update/channel', { channel });
  }
});

async function openUpdates(page: Page, lang: 'ru' | 'en'): Promise<void> {
  await page.addInitScript((l) => localStorage.setItem('lang', l), lang);
  await page.goto('/#/settings?tab=updates');
  await expect(page.locator('[data-testid="update-channel-row"]')).toBeVisible({
    timeout: T.page
  });
}

/** Минутное окно установки, начинающееся через 12 ч 30 мин от текущего момента по UTC. */
function farWindow(): string {
  const start = new Date(Date.now() + (12 * 60 + 30) * 60_000);
  const end = new Date(start.getTime() + 60_000);
  const hm = (d: Date) =>
    `${String(d.getUTCHours()).padStart(2, '0')}:${String(d.getUTCMinutes()).padStart(2, '0')}`;
  return `${hm(start)}-${hm(end)}`;
}

/** Ждёт конца проверки обновлений: виден блок «Доступна версия», «последняя версия» или ошибка. */
async function waitCheckDone(page: Page): Promise<void> {
  const done = page
    .locator('.update-available')
    .or(page.getByText(text('ru', 'settings.update_up_to_date'), { exact: true }))
    .or(page.locator('.update-state.state-error'))
    .first();
  await expect(done).toBeVisible({ timeout: T.page });
  // после проверки панель обновляет общее состояние отдельным запросом
  await page.waitForLoadState('networkidle', { timeout: T.network }).catch(() => {});
}

// Коды, для которых панель рисует строку; остальные (none, failed_version, неизвестные) — нет
const DATED = ['delayed', 'scheduled', 'urgent'];
const SHOWN = [...DATED, 'held', 'dev_build'];

/** Ожидаемый код строки по данным API: null — строки быть не должно. */
function shownCode(st: UpdateStateApi): string | null {
  const code = st.auto_state ?? '';
  if (!SHOWN.includes(code)) return null;
  if (code !== 'dev_build' && st.auto_install !== true) return null;
  if (DATED.includes(code) && st.error) return null;
  return code;
}

/** Кандидат автоустановки, отличный от новейшей версии; пусто — совпадает или поля нет. */
function otherVersion(st: UpdateStateApi): string {
  const v = st.auto_version ?? '';
  return v && v !== st.latest_version ? v : '';
}

/**
 * Строка состояния: непустая, без необработанных подстановок, для срочных — с окном установки,
 * при кандидате старше новейшей версии — с его номером.
 */
async function expectStateText(
  line: ReturnType<Page['locator']>,
  code: string,
  st: UpdateStateApi
): Promise<void> {
  const body = ((await line.textContent()) ?? '').trim();
  expect(body, `строка состояния ${code}: пустой текст`).not.toBe('');
  expect(body, `строка состояния ${code}: остались подстановки`).not.toMatch(/[{}]/);
  if (DATED.includes(code)) {
    expect(body, `строка состояния ${code}: нет окна установки`).toContain(st.install_window ?? '');
    const other = otherVersion(st);
    if (other) expect(body, `строка состояния ${code}: нет версии кандидата`).toContain(other);
  }
}

for (const channel of ['stable', 'beta'] as const) {
  test(`update auto-status: ${channel}`, async ({ page }) => {
    test.setTimeout(T.page * 4 + T.action * 10);
    const guard = attachConsoleGuard(page);
    await page.addInitScript(() => localStorage.removeItem('xcp.update.dismissed'));
    await page.addInitScript(() => localStorage.setItem('lang', 'ru'));

    // Настройки меняются со страницы, которая обращений к GitHub не делает: токен CSRF уже есть
    await page.goto('/#/dashboard');
    await page.waitForFunction(() => !!localStorage.getItem('csrf_token'), null, {
      timeout: T.page
    });
    autoTouched = true;
    const ch = await api(page, 'POST', '/api/update/channel', { channel });
    expect(ch.status, 'POST /api/update/channel').toBe(200);
    const win = farWindow();
    const set = await api(page, 'POST', '/api/update/settings', {
      auto_install: true,
      install_window: win
    });
    expect(set.status, 'POST /api/update/settings').toBe(200);

    // загрузка вкладки сама проверяет релизы и обновляет состояние планировщика
    await page.goto('/#/settings?tab=updates');
    await expect(page.locator('[data-testid="update-channel-row"]')).toBeVisible({
      timeout: T.page
    });
    await waitCheckDone(page);
    const st = await readUpdateState(page);
    const code = shownCode(st);

    const card = page.locator('[data-testid="update-auto-status"]');
    if (code) {
      await expect(card, `карточка автообновлений: ожидалась строка ${code}`).toBeVisible({
        timeout: T.action
      });
      await expect(card).toHaveAttribute('data-state', code);
      await expect(card).toHaveAttribute('role', 'status');
      await expectStateText(card, code, st);
    } else {
      await expect(
        card,
        `карточка автообновлений: строки быть не должно (auto_state=${st.auto_state ?? 'нет'})`
      ).toHaveCount(0);
    }

    // Блок «Доступна версия»: строка и «Срочное» только у версии и канала из состояния
    const avail = page.locator('.update-available');
    const availAuto = page.locator('[data-testid="update-available-auto"]');
    const urgentBadge = page.locator('[data-testid="update-urgent-badge"]');
    if ((await avail.count()) > 0 && (await avail.isVisible())) {
      const title = (await avail.locator('.available-title').textContent()) ?? '';
      const same =
        !!st.latest_version && title.includes(st.latest_version) && st.channel === channel;
      if (same && code && code !== 'dev_build') {
        await expect(availAuto).toBeVisible({ timeout: T.action });
        await expect(availAuto).toHaveAttribute('data-state', code);
        await expectStateText(availAuto, code, st);
      } else {
        await expect(availAuto).toHaveCount(0);
      }
      if (same && st.auto_state === 'urgent' && !otherVersion(st)) {
        await expect(urgentBadge).toBeVisible({ timeout: T.action });
      } else {
        await expect(urgentBadge).toHaveCount(0);
      }
      // ручная установка доступна при любом состоянии
      await expect(
        avail.getByRole('button', { name: text('ru', 'settings.install_update') })
      ).toBeEnabled();
    } else {
      await expect(availAuto).toHaveCount(0);
      await expect(urgentBadge).toHaveCount(0);
    }

    // Баннер на главной: код состояния или manual
    await page.goto('/#/dashboard');
    const banner = page.locator('[data-testid="update-banner-auto"]');
    const after = await readUpdateState(page);
    if (after.has_update) {
      await expect(banner).toBeVisible({ timeout: T.page });
      const bannerCode = shownCode(after) ?? 'manual';
      await expect(banner).toHaveAttribute('data-state', bannerCode, { timeout: T.action });
      if (bannerCode !== 'manual') await expectStateText(banner, bannerCode, after);
    }

    await guard.assertClean();
  });
}

for (const theme of ['light', 'dark'] as const) {
  for (const vp of [
    { width: 1280, height: 900 },
    { width: 390, height: 844 }
  ]) {
    test(`update axe: theme:${theme} vp:${vp.width}`, async ({ page }) => {
      test.setTimeout(T.page * 3 + T.action * 4);
      const guard = attachConsoleGuard(page);
      const problems: string[] = [];
      await page.setViewportSize(vp);
      await page.addInitScript(
        ({ th }) => {
          localStorage.setItem('theme', th);
          localStorage.setItem('lang', 'ru');
        },
        { th: theme }
      );
      await page.goto('/#/settings?tab=updates');
      await expect(page.locator('[data-testid="update-channel-row"]')).toBeVisible({
        timeout: T.page
      });
      await waitCheckDone(page);
      // переходы между вкладками идут с затуханием: axe не должен видеть полупрозрачный кадр
      await page.waitForTimeout(500);

      await guard.assertClean().catch((e: Error) => problems.push(e.message));

      const scan = await new AxeBuilder({ page })
        .withTags(['wcag2a', 'wcag2aa', 'wcag21a', 'wcag21aa'])
        .exclude('.cm-editor')
        .exclude('.xterm')
        .analyze();
      const severe = scan.violations.filter(
        (v) => v.impact === 'critical' || v.impact === 'serious'
      );
      // Метка только когда axe сам нашёл контраст на .badge-primary и такой бейдж есть на
      // странице: без бейджа нарушения нет, и метка дала бы XPASS
      if (
        theme === 'light' &&
        severe.some(
          (v) =>
            v.id === 'color-contrast' &&
            v.nodes.some((n) => n.target.join(' ').includes('.badge-primary'))
        ) &&
        (await page.locator('.badge-primary').count()) > 0
      ) {
        knownFailure('*', 'light-tinted-badge-text-contrast');
      }
      // Капсула состояния на узком экране в светлой теме: метка только когда axe нашёл
      // контраст на самом .mobile-traffic
      if (
        theme === 'light' &&
        vp.width === 390 &&
        severe.some(
          (v) =>
            v.id === 'color-contrast' &&
            v.nodes.some((n) => n.target.join(' ').includes('.mobile-traffic'))
        )
      ) {
        knownFailure('*', 'status-capsule-mobile-traffic-contrast');
      }
      if (severe.length > 0) {
        // Только правило, селекторы и числа контраста: HTML узлов в вывод не попадает
        const list = severe.map((v) => {
          const nodes = v.nodes.slice(0, 3).map((n) => {
            const msg = (n.any[0]?.message ?? '')
              .replace(/^Element has insufficient color contrast of /, 'контраст ')
              .replace(/, font size:.*?\)/, ')')
              .replace(/\. Expected contrast ratio of /, ', нужно ');
            return `${n.target.join(' ')} [${msg}]`;
          });
          return `${v.id} (${v.impact}, узлов ${v.nodes.length}): ${nodes.join(' | ')}`;
        });
        problems.push(`axe: нарушений critical/serious ${severe.length}:\n- ${list.join('\n- ')}`);
      }

      if (vp.width === 390) {
        const wide = await page.evaluate(() => ({
          scroll: document.documentElement.scrollWidth,
          inner: window.innerWidth
        }));
        if (wide.scroll > wide.inner + 1) {
          problems.push(`прокрутка: scrollWidth ${wide.scroll} шире окна ${wide.inner}`);
        }
      }

      if (problems.length > 0) throw new Error(problems.join('\n'));
    });
  }
}

for (const lang of ['ru', 'en'] as const) {
  test(`update channels: подписи и выбор (${lang})`, async ({ page }) => {
    test.setTimeout(T.page * 3 + T.action * 4);
    const guard = attachConsoleGuard(page);
    await openUpdates(page, lang);

    const row = page.locator('[data-testid="update-channel-row"]');
    const main = row.getByRole('button', { name: text(lang, 'settings.channel_stable') });
    const preview = row.getByRole('button', { name: text(lang, 'settings.channel_beta') });
    await expect(main).toBeVisible({ timeout: T.action });
    await expect(preview).toBeVisible({ timeout: T.action });

    // выбираем сегмент, отличный от сохранённого, чтобы путь сохранения точно выполнялся
    const target: Channel = (savedChannel ?? 'stable') === 'beta' ? 'stable' : 'beta';
    const segment = target === 'beta' ? preview : main;
    await segment.click();

    await expect
      .poll(async () => readChannel(page), {
        timeout: T.action,
        message: 'GET /api/update/channel не вернул выбранный канал'
      })
      .toBe(target);
    await expect(row.locator('.field-row-desc')).toHaveText(
      text(lang, `settings.channel_${target}_desc`),
      { timeout: T.action }
    );

    await page.reload();
    await expect(row).toBeVisible({ timeout: T.page });
    await expect(
      row.getByRole('button', { name: text(lang, `settings.channel_${target}`) })
    ).toHaveAttribute('aria-pressed', 'true', { timeout: T.action });

    await guard.assertClean();
  });
}

test('update kinds: бейдж текущей версии', async ({ page }) => {
  test.setTimeout(T.page * 2 + T.action * 2);
  const guard = attachConsoleGuard(page);
  // номер сборки — из /api/version панели: состояние обновлений его может ещё не нести
  const res = await page.request.get('/api/version');
  expect(res.ok(), `GET /api/version: http ${res.status()}`).toBe(true);
  const body = (await res.json()) as {
    data?: { panel_version?: string };
    panel_version?: string;
  };
  const version = body.data?.panel_version ?? body.panel_version ?? '';
  expect(version, 'панель не отдала номер сборки').not.toBe('');
  const kind = kindOf(version);

  await openUpdates(page, 'ru');
  const badge = page.locator('[data-testid="update-kind-current"]');
  await expect(badge).toHaveText(text('ru', `settings.update_kind_${kind}`), {
    timeout: T.page
  });
  const cls = { stable: 'badge-success', beta: 'badge-primary', alpha: 'badge-warning' }[kind];
  await expect(badge).toHaveClass(new RegExp(`\\b${cls}\\b`));

  await guard.assertClean();
});
