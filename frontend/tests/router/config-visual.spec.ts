// e2e-pages: config/nodes config/groups config/rules config/lists config/cores config/files
// e2e-kind: read
// router-area: ui-shell
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { FULL_TAG, TIER_FULL_ANNOTATION } from './lib/known';

// Визуальная проверка вкладок раздела «Конфигурация» (CFG-05, D-20).
// Read-спек обходит шесть вкладок: узлы, группы, правила, списки, ядра, файлы.
// Проверяет:
// 1. Консоль и сеть: нет console.error, pageerror, ответов 5xx или оборванных запросов.
// 2. Горизонтальная прокрутка страницы: document.scrollingElement.scrollWidth <= innerWidth.
// 3. Отсутствие обрезанных подписей без многоточия у кнопок, вкладок, бейджей, заголовков.
// 4. На экранах <= 768 px полоса config-tabs не шире окна (горизонтальный скролл полосы).
//
// В сообщения провалов попадают только testId и классы элементов (CFG-05 privacy).

const TABS = ['nodes', 'groups', 'rules', 'lists', 'cores', 'files'] as const;

type TabName = (typeof TABS)[number];

const VIEWPORTS: Record<number, { width: number; height: number }> = {
  390: { width: 390, height: 844 },
  768: { width: 768, height: 1024 },
  1280: { width: 1280, height: 800 }
};

async function layerOn(page: Page): Promise<boolean> {
  const res = await page.request.get('/api/settings');
  if (!res.ok()) return false;
  const body = (await res.json()) as { data?: { config_layer?: boolean } };
  return body.data?.config_layer === true;
}

async function checkTab(
  page: Page,
  tab: TabName,
  theme: 'light' | 'dark',
  width: 390 | 768 | 1280
): Promise<void> {
  const guard = attachConsoleGuard(page);
  const vp = VIEWPORTS[width];
  await page.setViewportSize(vp);

  await page
    .addInitScript((th) => {
      localStorage.setItem('theme', th);
    }, theme)
    .catch(() => {});

  await page.goto(`/#/config/${tab}`);

  await page.evaluate((th) => {
    localStorage.setItem('theme', th);
    document.documentElement.setAttribute('data-theme', th);
    document.documentElement.setAttribute('data-scheme', th === 'light' ? 'light' : 'dark');
  }, theme);

  await expect(page.getByTestId('config-tabs')).toBeVisible({ timeout: T.page });
  await expect(page.getByTestId(`${tab}-tab`)).toBeVisible({ timeout: T.page });
  await page.waitForLoadState('networkidle', { timeout: T.network }).catch(() => {});
  await page.waitForTimeout(300);

  const problems: string[] = [];

  // 1. Горизонтальная прокрутка страницы
  const wide = await page.evaluate(() => {
    const el = document.scrollingElement ?? document.documentElement;
    return {
      scroll: el.scrollWidth,
      inner: window.innerWidth
    };
  });
  if (wide.scroll > wide.inner + 1) {
    problems.push(`прокрутка: scrollWidth ${wide.scroll} шире окна ${wide.inner}`);
  }

  // 2. Обрезанные подписи без ellipsis
  const truncated = await page.evaluate(() => {
    const selector =
      '.tab-btn, .btn, .badge, h1, h2, h3, label, .status-line, [data-testid$="-badge"]';
    const elements = Array.from(document.querySelectorAll(selector));
    const bad: string[] = [];
    for (const el of elements) {
      if (!(el instanceof HTMLElement)) continue;
      if (el.offsetWidth === 0 || el.offsetHeight === 0) continue;
      if (el.scrollWidth > el.clientWidth + 1) {
        const style = window.getComputedStyle(el);
        if (style.textOverflow !== 'ellipsis') {
          const testId = el.getAttribute('data-testid') || '';
          const cls = el.className ? String(el.className).trim() : '';
          const tag = el.tagName.toLowerCase();
          bad.push(
            `${tag}${testId ? `[data-testid="${testId}"]` : ''}${cls ? `.${cls.split(/\s+/).join('.')}` : ''} (scrollWidth=${el.scrollWidth}, clientWidth=${el.clientWidth})`
          );
        }
      }
    }
    return bad;
  });
  if (truncated.length > 0) {
    problems.push(
      `обрезанный текст без ellipsis (${truncated.length}):\n- ${truncated.join('\n- ')}`
    );
  }

  // 3. Полоса вкладок на узком экране не шире окна
  if (width <= 768) {
    const tabsWidth = await page.evaluate(() => {
      const tabs = document.querySelector('[data-testid="config-tabs"]');
      if (!tabs) return null;
      return {
        width: tabs.getBoundingClientRect().width,
        inner: window.innerWidth
      };
    });
    if (tabsWidth && tabsWidth.width > tabsWidth.inner + 1) {
      problems.push(`config-tabs: ширина ${tabsWidth.width} шире окна ${tabsWidth.inner}`);
    }
  }

  await guard.assertClean().catch((e: Error) => problems.push(e.message));

  if (problems.length > 0) {
    throw new Error(`[tab:${tab} theme:${theme} vp:${width}]\n` + problems.join('\n'));
  }
}

// ── Ярус gate: два среза (390 светлая и 1280 тёмная) ──
for (const tab of TABS) {
  test(`config-visual tab:${tab} slices:gate`, async ({ page }) => {
    if (!(await layerOn(page))) {
      test.skip(
        true,
        'флаг config_layer выключен на этом устройстве: панель не открывает #/config'
      );
    }
    await checkTab(page, tab, 'light', 390);
    await checkTab(page, tab, 'dark', 1280);
  });
}

// ── Ярус full: полная матрица (390/768/1280 × две темы, кроме срезов gate, D-20) ──
for (const tab of TABS) {
  test(
    `config-visual tab:${tab} matrix:full`,
    {
      tag: FULL_TAG,
      annotation: {
        type: TIER_FULL_ANNOTATION,
        description: 'визуальная матрица: 390/768/1280 × две темы, кроме срезов gate (D-20)'
      }
    },
    async ({ page }) => {
      if (!(await layerOn(page))) {
        test.skip(
          true,
          'флаг config_layer выключен на этом устройстве: панель не открывает #/config'
        );
      }
      await checkTab(page, tab, 'dark', 390);
      await checkTab(page, tab, 'light', 768);
      await checkTab(page, tab, 'dark', 768);
      await checkTab(page, tab, 'light', 1280);
    }
  );
}
