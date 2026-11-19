// e2e-pages: config/nodes config/groups config/rules config/lists config/cores config/files config/setup proxies
// e2e-kind: read
// router-area: ui-shell
import { test, expect, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { T } from './lib/env';
import { FULL_TAG, TIER_FULL_ANNOTATION } from './lib/known';

// Визуальная проверка вкладок раздела «Конфигурация» (CFG-05, D-20) и страницы мастера
// первого запуска #/config/setup (149 D-28, фаза 149.1).
// Read-спек обходит шесть вкладок: узлы, группы, правила, списки, ядра, файлы — и мастер.
// У мастера нет полосы вкладок: ждётся корень config-setup, пункт 4 пропускается. Перед
// проверками мастера шаг 7 приводится к устойчивому виду (D-09: страница сама проверяет IP при
// открытии, если шаги 5 и 6 выполнены): итог проверки или «заперт» — по ответу сервера. Затем
// каждый шаг раскрывается по очереди (нажатие заголовка — не изменяющее действие), и пункты 2
// и 3 повторяются; сообщение провала называет номер шага. Внешние запросы автопроверки IP идут
// под сборщиком консоли, как у бейджа IP на «Прокси» (pages.spec.ts).
// Страница «Прокси» (фаза 150) — отдельный срез, как мастер: корень proxies-layer-page,
// без полосы вкладок; первая карточка группы раскрывается нажатием на имя в шапке (не изменяющее
// действие), и пункты 2 и 3 повторяются на раскрытой карточке.
// Проверяет:
// 1. Консоль и сеть: нет console.error, pageerror, ответов 5xx или оборванных запросов.
// 2. Горизонтальная прокрутка страницы: document.scrollingElement.scrollWidth <= innerWidth.
// 3. Отсутствие обрезанных подписей без многоточия у кнопок, вкладок, бейджей, заголовков.
//    Подписью считается собственный видимый текст элемента: элемент отмечается, только если
//    прямоугольник его текстового узла выходит за правый край клиентской области (G149-03).
//    Сужение сознательное: нетекстовое переполнение внутри элемента эта проверка больше
//    не ловит. Это сенсорная зона переключателя (global.css .toggle-switch::after, 44×44
//    при ширине 34 px), иконки и абсолютно позиционированные декоративные слои. Содержимое,
//    вылезающее за окно, по-прежнему ловит проверка горизонтальной прокрутки (пункт 2).
// 4. На экранах <= 768 px полоса config-tabs не шире окна (горизонтальный скролл полосы).
//
// В сообщения провалов попадают только testId и классы элементов (CFG-05 privacy).

const TABS = ['nodes', 'groups', 'rules', 'lists', 'cores', 'files'] as const;

// Страница мастера — не вкладка раздела: свой корень и без полосы вкладок
const SETUP = 'setup';

// Страница «Прокси»: не вкладка раздела, свой адрес и корень
const PROXIES = 'proxies';

type TabName = (typeof TABS)[number] | typeof SETUP | typeof PROXIES;

// Итог проверки IP шага 7 мастера (IpCheckResult data-state)
const IP_RESULT = /^(proxied|direct_ok|direct_warn|failed)$/;

const VIEWPORTS: Record<number, { width: number; height: number }> = {
  390: { width: 390, height: 844 },
  768: { width: 768, height: 1024 },
  1280: { width: 1280, height: 800 }
};

/**
 * Шаг 7 мастера в устойчивом виде (D-09): шаги 5 и 6 выполнены по ответу сервера — страница
 * сама проверяет IP при открытии, ждём итог; иначе шаг заперт. Только чтение состояния.
 */
async function settleIpStep(page: Page): Promise<void> {
  const res = await page.request.get('/api/configlayer/wizard');
  expect(res.ok(), 'GET /api/configlayer/wizard').toBe(true);
  const body = (await res.json()) as { data?: { steps?: { id: string; done: boolean }[] } };
  const steps = body.data?.steps ?? [];
  const done = (id: string) => steps.find((s) => s.id === id)?.done === true;
  const box = page.getByTestId('setup-ip-result');
  if (done('apply') && done('start')) {
    await expect(box, 'setup-ip-result: итог автопроверки').toHaveAttribute(
      'data-state',
      IP_RESULT,
      {
        timeout: 20_000
      }
    );
  } else {
    await expect(box, 'setup-ip-result: шаг 7 заперт').toHaveAttribute('data-state', 'locked', {
      timeout: T.action
    });
  }
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

  await page.goto(tab === PROXIES ? '/#/proxies' : `/#/config/${tab}`);

  await page.evaluate((th) => {
    localStorage.setItem('theme', th);
    document.documentElement.setAttribute('data-theme', th);
    document.documentElement.setAttribute('data-scheme', th === 'light' ? 'light' : 'dark');
  }, theme);

  if (tab === PROXIES) {
    await expect(page.getByTestId('proxies-layer-page')).toBeVisible({ timeout: T.page });
    // вид загружен: скелетонов страницы нет
    await expect(page.locator('[data-testid="proxies-layer-page"] [aria-busy="true"]')).toHaveCount(
      0,
      { timeout: T.page }
    );
  } else if (tab === SETUP) {
    await expect(page.getByTestId('config-setup')).toBeVisible({ timeout: T.page });
    await expect(page.getByTestId('setup-steps')).toBeVisible({ timeout: T.page });
  } else {
    await expect(page.getByTestId('config-tabs')).toBeVisible({ timeout: T.page });
    await expect(page.getByTestId(`${tab}-tab`)).toBeVisible({ timeout: T.page });
  }
  await page.waitForLoadState('networkidle', { timeout: T.network }).catch(() => {});
  if (tab === SETUP) await settleIpStep(page);
  await page.waitForTimeout(300);

  const problems = await layoutProblems(page);

  // Мастер: каждый шаг раскрывается по очереди, пункты 2 и 3 повторяются на раскрытом шаге
  if (tab === SETUP) {
    for (let n = 1; n <= 7; n++) {
      const toggle = page.getByTestId(`setup-step-${n}-toggle`);
      if ((await toggle.getAttribute('aria-expanded')) !== 'true') await toggle.click();
      await expect(page.getByTestId(`setup-step-${n}-body`)).toBeVisible({ timeout: T.action });
      await page.waitForTimeout(300);
      for (const p of await layoutProblems(page)) problems.push(`шаг ${n}: ${p}`);
    }
  }

  // «Прокси»: первая карточка группы раскрывается, пункты 2 и 3 повторяются на раскрытой
  if (tab === PROXIES) {
    const card = page.locator('[data-testid^="proxy-group-"].pc-card').first();
    if ((await card.count()) > 0) {
      const head = card.locator('.pc-toggle').first();
      if ((await head.getAttribute('aria-expanded')) !== 'true') {
        await card.locator('.pc-name').first().click();
      }
      await expect(head).toHaveAttribute('aria-expanded', 'true', { timeout: T.action });
      await page.waitForTimeout(300);
      for (const p of await layoutProblems(page)) problems.push(`раскрытая карточка группы: ${p}`);
    }
  }

  // 4. Полоса вкладок на узком экране не шире окна (у мастера и «Прокси» полосы нет)
  if (width <= 768 && tab !== SETUP && tab !== PROXIES) {
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

/** Пункты 2 и 3: горизонтальная прокрутка страницы и обрезанные подписи без многоточия. */
async function layoutProblems(page: Page): Promise<string[]> {
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

  // 2. Обрезанные подписи без ellipsis. Подпись — текст элемента: переключатель без
  // текста шире своей клиентской области только из-за сенсорной зоны
  // global.css .toggle-switch::after и в провал не попадает (G149-03).
  const truncated = await page.evaluate(() => {
    // Текстовый прямоугольник элемента выходит за правый край его клиентской области.
    function textOverflows(el: HTMLElement): boolean {
      const edge = el.getBoundingClientRect().left + el.clientLeft + el.clientWidth;
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        if (!node.textContent || node.textContent.trim() === '') continue;
        const range = document.createRange();
        range.selectNodeContents(node);
        for (const rect of Array.from(range.getClientRects())) {
          if (rect.width > 0 && rect.right > edge + 1) return true;
        }
      }
      return false;
    }

    const selector =
      '.tab-btn, .btn, .badge, h1, h2, h3, label, .status-line, [data-testid$="-badge"]';
    const elements = Array.from(document.querySelectorAll(selector));
    const bad: string[] = [];
    for (const el of elements) {
      if (!(el instanceof HTMLElement)) continue;
      if (el.offsetWidth === 0 || el.offsetHeight === 0) continue;
      if (el.scrollWidth > el.clientWidth + 1) {
        const style = window.getComputedStyle(el);
        if (style.textOverflow !== 'ellipsis' && textOverflows(el)) {
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
  return problems;
}

// ── Ярус gate: два среза (390 светлая и 1280 тёмная) ──
for (const tab of TABS) {
  test(`config-visual tab:${tab} slices:gate`, async ({ page }) => {
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
      await checkTab(page, tab, 'dark', 390);
      await checkTab(page, tab, 'light', 768);
      await checkTab(page, tab, 'dark', 768);
      await checkTab(page, tab, 'light', 1280);
    }
  );
}

// ── Страница мастера #/config/setup (149 D-28): те же срезы gate и матрица full ──
test('config-visual tab:setup slices:gate', async ({ page }) => {
  await checkTab(page, SETUP, 'light', 390);
  await checkTab(page, SETUP, 'dark', 1280);
});

test(
  'config-visual tab:setup matrix:full',
  {
    tag: FULL_TAG,
    annotation: {
      type: TIER_FULL_ANNOTATION,
      description: 'визуальная матрица: 390/768/1280 × две темы, кроме срезов gate (149.1 D-28)'
    }
  },
  async ({ page }) => {
    await checkTab(page, SETUP, 'dark', 390);
    await checkTab(page, SETUP, 'light', 768);
    await checkTab(page, SETUP, 'dark', 768);
    await checkTab(page, SETUP, 'light', 1280);
  }
);

// ── Страница «Прокси» (фаза 150): те же срезы gate и матрица full ──
test('config-visual tab:proxies slices:gate', async ({ page }) => {
  await checkTab(page, PROXIES, 'light', 390);
  await checkTab(page, PROXIES, 'dark', 1280);
});

test(
  'config-visual tab:proxies matrix:full',
  {
    tag: FULL_TAG,
    annotation: {
      type: TIER_FULL_ANNOTATION,
      description: 'визуальная матрица: 390/768/1280 × две темы, кроме срезов gate (150 UI-SPEC)'
    }
  },
  async ({ page }) => {
    await checkTab(page, PROXIES, 'dark', 390);
    await checkTab(page, PROXIES, 'light', 768);
    await checkTab(page, PROXIES, 'dark', 768);
    await checkTab(page, PROXIES, 'light', 1280);
  }
);
