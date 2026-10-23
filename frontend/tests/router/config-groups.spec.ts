// e2e-pages: config/groups
import { test, expect, type Locator, type Page } from '@playwright/test';
import { attachConsoleGuard } from './lib/console';
import { RT, T } from './lib/env';
import { CORE_TAG } from './lib/known';
import {
  addToDraft,
  api,
  installedKernels,
  layerFlag,
  nodeLink,
  randomPort,
  resetDraft
} from './lib/nodes-api';

// Вкладка «Группы» раздела «Конфигурация» на настоящей панели (фаза 146, план 19): карточки
// групп со сводкой по ядрам, группы по умолчанию после первого источника, строка «Сейчас» ручной
// группы (выбор участника без черновика и диалога, D-38) и диалог группы. Ответы сети не
// подменяются. Флаг слоя включается на время прогона и возвращается прежним, черновик
// сбрасывается, взятая маршрутизация возвращается. В вывод не попадают имена узлов.

const layer = layerFlag();

test.describe.configure({ mode: 'serial' });

test.beforeAll(async ({ browser }) => {
  await layer.enable(browser);
});

test.afterAll(async ({ browser }) => {
  await layer.restore(browser);
});

async function startPage(page: Page): Promise<void> {
  await page.goto('/', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await page.locator('.main-content').first().waitFor({ state: 'visible', timeout: T.page });
}

async function openGroups(page: Page, width = 1440): Promise<void> {
  await page.setViewportSize({ width, height: width > 600 ? 900 : 844 });
  await page.goto('/#/config/groups', { timeout: T.page, waitUntil: 'domcontentloaded' });
  await expect(page.getByTestId('config-page')).toBeVisible({ timeout: T.page });
  const tab = page.getByTestId('groups-tab');
  await expect(tab).toBeVisible({ timeout: T.action });
  await expect(tab.locator('.skeleton')).toHaveCount(0, { timeout: T.action });
}

interface GroupInfo {
  id: string;
  name: string;
  mode: string;
  current: string;
}

interface RoutingInfo {
  revision: number;
  desired: boolean;
  hasSources: boolean;
  defaultsDone: boolean;
  groups: GroupInfo[];
}

async function routingInfo(page: Page): Promise<RoutingInfo> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  const d = view.data ?? {};
  return {
    revision: Number(d.revision ?? 0),
    desired: d.desired === true,
    hasSources: d.has_sources === true,
    defaultsDone: d.defaults_done === true,
    groups: ((d.groups ?? []) as GroupInfo[]).map((g) => ({
      id: g.id,
      name: g.name,
      mode: g.mode,
      current: g.current ?? ''
    }))
  };
}

/** Идентификаторы записей «Моих узлов» в черновике. */
async function myEntryIds(page: Page): Promise<string[]> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const sources = (view.data?.sources ?? []) as { id: string; nodes?: { entry_id?: string }[] }[];
  const my = sources.find((s) => s.id === 'my');
  return (my?.nodes ?? []).map((n) => n.entry_id ?? '').filter((id) => id !== '');
}

/** Убирает из черновика записи «Моих узлов», которых не было в списке `keep`. */
async function removeMyEntries(page: Page, keep: string[]): Promise<void> {
  for (const entryId of await myEntryIds(page)) {
    if (keep.includes(entryId)) continue;
    const view = await api(page, 'GET', '/api/configlayer/sources');
    await api(page, 'POST', '/api/configlayer/sources/nodes/delete', {
      revision: Number(view.data?.draft_revision ?? 0),
      entry_id: entryId
    });
  }
}

/** Запускает применение и ждёт его конца: идёт ли применение, видно в снимке слоя. */
async function applyAndWait(page: Page): Promise<void> {
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  expect(start.status, 'запуск применения').toBe(200);
  await expect
    .poll(
      async () => {
        const st = await api(page, 'GET', '/api/configlayer/state');
        const apply = (st.data?.apply ?? {}) as { running?: boolean };
        return apply.running === false && Number(st.data?.draft_changes ?? -1) === 0;
      },
      { timeout: T.save * 3, intervals: [2000] }
    )
    .toBe(true);
}

test('groups: группы по умолчанию — «Авто» и «Прокси» после первого источника, чип режима и сводка по ядрам', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await resetDraft(page);
  const before = await routingInfo(page);
  // группы по умолчанию создаются один раз и только с первым источником (D-09, D-37)
  test.skip(
    before.defaultsDone || before.hasSources,
    'на стенде уже есть источники или группы по умолчанию создавались'
  );
  const inst = await installedKernels(page);
  const port = randomPort();
  try {
    await addToDraft(
      page,
      [nodeLink('vless_ws.txt', port, 'GrpA'), nodeLink('socks.txt', port + 1, 'GrpB')].join('\n')
    );
    const after = await routingInfo(page);
    const auto = after.groups.find((g) => g.name === 'Авто');
    const proxy = after.groups.find((g) => g.name === 'Прокси');
    expect(auto?.mode, 'группа «Авто»').toBe('auto');
    expect(proxy?.mode, 'группа «Прокси»').toBe('manual');

    await openGroups(page);
    await expect(page.getByTestId('groups-count')).toContainText('2');
    const autoCard = page.getByTestId(`group-card-${auto!.id}`);
    const proxyCard = page.getByTestId(`group-card-${proxy!.id}`);
    await expect(autoCard).toBeVisible({ timeout: T.action });
    await expect(proxyCard).toBeVisible();
    await expect(page.getByTestId(`group-mode-${auto!.id}`)).toHaveText(/Авто|Auto/);
    await expect(page.getByTestId(`group-mode-${proxy!.id}`)).toHaveText(/Вручную|Manual/);

    // сводка называет только установленные ядра и содержит числа
    for (const id of [auto!.id, proxy!.id]) {
      const summary = page.getByTestId(`group-summary-${id}`);
      await expect(summary).toContainText(/\d/);
      if (inst.xray && inst.mihomo) {
        await expect(summary).toContainText('Xray');
        await expect(summary).toContainText('Mihomo');
      } else {
        await expect(summary).not.toContainText('Xray:');
        await expect(summary).not.toContainText('Mihomo:');
      }
    }
    // сегмент ядра — только при двух установленных ядрах
    await expect(page.getByTestId('groups-kernel-segment')).toHaveCount(
      inst.xray && inst.mihomo ? 1 : 0
    );
    // «Авто» не имеет строки «Сейчас», «Прокси» — имеет
    await expect(page.getByTestId(`group-current-${auto!.id}`)).toHaveCount(0);
    await expect(page.getByTestId(`group-current-${proxy!.id}`)).toHaveCount(1);
    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test(
  'groups: «Сейчас» — выбор участника ручной группы без черновика, подсказка о перезапуске Xray',
  { tag: CORE_TAG },
  async ({ page }) => {
    test.setTimeout(T.save * 10);
    const guard = attachConsoleGuard(page);
    await startPage(page);
    await resetDraft(page);
    const inst = await installedKernels(page);
    const wasManaged = (await routingInfo(page)).desired;
    const myBefore = await myEntryIds(page);
    const port = randomPort();
    let groupId = '';
    try {
      await addToDraft(page, nodeLink('vless_ws.txt', port, 'GrpCur'));
      // собственная ручная группа: участники DIRECT и REJECT дают два заведомо допустимых выбора
      const saved = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
        revision: (await routingInfo(page)).revision,
        group: {
          id: '',
          name: 'RT Сейчас',
          mode: 'manual',
          sources: { all: true, ids: [] },
          include: [],
          exclude: [],
          regex: false,
          picks: [],
          members: [{ kind: 'direct' }, { kind: 'reject' }],
          reserve: ''
        }
      });
      expect(saved.status, 'сохранение группы').toBe(200);
      groupId = String((saved.data?.group as { id?: string } | undefined)?.id ?? '');
      expect(groupId, 'идентификатор группы').not.toBe('');
      const take = await api(page, 'POST', '/api/configlayer/routing/take', {
        revision: (await routingInfo(page)).revision
      });
      expect(take.status, 'взятие маршрутизации').toBe(200);
      await applyAndWait(page);

      await openGroups(page);
      const select = page.getByTestId(`group-current-${groupId}`);
      await expect(select).toBeVisible({ timeout: T.action });
      await expect(select).toBeEnabled({ timeout: T.action });
      // постоянная подсказка про Xray и отсутствие диалога подтверждения
      if (inst.xray) {
        await expect(page.getByTestId(`group-hint-xray-${groupId}`)).toBeVisible();
      } else {
        await expect(page.getByTestId(`group-hint-xray-${groupId}`)).toHaveCount(0);
      }

      const current = await select.inputValue();
      const target = current === 'direct' ? 'reject' : 'direct';
      const draftBefore = Number(
        (await api(page, 'GET', '/api/configlayer/state')).data?.draft_changes ?? 0
      );
      await select.selectOption(target);
      await expect(
        page.locator('.toast--success').filter({ hasText: /Выбрано|Selected/ })
      ).toBeVisible({
        timeout: T.save
      });
      await expect(select).toBeEnabled({ timeout: T.save });
      await expect(page.getByRole('dialog')).toHaveCount(0);
      // выбор не попал в черновик
      const draftAfter = Number(
        (await api(page, 'GET', '/api/configlayer/state')).data?.draft_changes ?? 0
      );
      expect(draftAfter, 'выбор не создаёт изменений черновика').toBe(draftBefore);

      // после перечитывания страницы выбран тот же участник
      await openGroups(page);
      await expect(page.getByTestId(`group-current-${groupId}`)).toHaveValue(target, {
        timeout: T.action
      });
      const now = (await routingInfo(page)).groups.find((g) => g.id === groupId);
      expect(now?.current, `выбор (${RT.core || 'ядро'}) сохранён`).toBe(target);
      await guard.assertClean();
    } finally {
      // возврат стенда: своя группа удаляется, маршрутизация отпускается, если не была взята
      const rev = async () => (await routingInfo(page)).revision;
      if (groupId) {
        await api(page, 'POST', '/api/configlayer/routing/groups/delete', {
          revision: await rev(),
          id: groupId,
          cascade: { mode: '' }
        });
      }
      await removeMyEntries(page, myBefore);
      if (!wasManaged) {
        await api(page, 'POST', '/api/configlayer/routing/release', { revision: await rev() });
      }
      const st = await api(page, 'GET', '/api/configlayer/state');
      if (Number(st.data?.draft_changes ?? 0) > 0) await applyAndWait(page);
      await resetDraft(page);
    }
  }
);

// --- Диалог группы ---

const addGroupButton = (page: Page): Locator =>
  page.getByRole('button', { name: /^(Добавить группу|Add group)$/ }).first();

const modalOf = (page: Page): Locator => page.getByTestId('group-modal');

/** Открывает диалог новой группы и ждёт его появления. */
async function openNewGroup(page: Page): Promise<Locator> {
  await addGroupButton(page).click();
  const modal = modalOf(page);
  await expect(modal).toBeVisible({ timeout: T.action });
  return modal;
}

/** Числа строки предпросмотра по ядрам после того, как расчёт закончился. */
async function previewCounts(page: Page): Promise<number[]> {
  await expect(page.getByTestId('group-preview-busy')).toHaveCount(0, { timeout: T.action });
  const line = page.getByTestId('group-preview-line');
  await expect(line).toBeVisible({ timeout: T.action });
  return ((await line.innerText()).match(/\d+/g) ?? []).map(Number);
}

/** Сохраняет группу через API панели (в черновик); возвращает её идентификатор. */
async function saveGroupApi(page: Page, group: Record<string, unknown>): Promise<string> {
  const res = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
    revision: (await routingInfo(page)).revision,
    group: {
      id: '',
      sources: { all: true, ids: [] },
      include: [],
      exclude: [],
      regex: false,
      picks: [],
      members: [],
      reserve: '',
      ...group
    }
  });
  expect(res.status, 'сохранение группы через API').toBe(200);
  return String((res.data?.group as { id?: string } | undefined)?.id ?? '');
}

interface GroupFull {
  id: string;
  name: string;
  mode: string;
  exclude: string[];
  reserve: string;
  members: { kind: string; id?: string }[];
}

async function groupsFull(page: Page): Promise<GroupFull[]> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  return (view.data?.groups ?? []) as GroupFull[];
}

test('groups: диалог группы — пустое имя блокирует сохранение, дубль имени, «Авто» с исключением по подстроке в другом регистре', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await resetDraft(page);
  const inst = await installedKernels(page);
  const port = randomPort();
  try {
    await addToDraft(
      page,
      [
        nodeLink('vless_ws.txt', port, 'DlgAlpha'),
        nodeLink('vless_ws.txt', port + 1, 'DlgBeta')
      ].join('\n')
    );
    await openGroups(page);
    const modal = await openNewGroup(page);
    const nameField = modal.getByRole('textbox', { name: /^(Имя|Name)$/ });
    const save = page.getByTestId('group-save');

    // пустое имя: ошибка под полем, «Сохранить в черновик» неактивна
    await expect(save).toBeDisabled();
    await expect(modal.locator('.error-text').first()).toContainText(
      /Введите имя группы|Enter a group name/
    );
    await expect(nameField).toHaveAttribute('aria-describedby', /-error$/);

    // дубль имени существующей группы (регистр не важен)
    const existing = (await routingInfo(page)).groups[0];
    if (existing) {
      await nameField.fill(existing.name.toUpperCase());
      await expect(modal.locator('.error-text').first()).toContainText(/уже есть|already/);
      await expect(save).toBeDisabled();
    }

    // режим «Авто»: предпросмотр считает состав по каждому установленному ядру
    const name = `RT Авто ${port}`;
    await nameField.fill(name);
    await expect(modal.locator('.error-text')).toHaveCount(0);
    await expect(save).toBeEnabled();
    await modal
      .getByTestId('group-mode')
      .getByRole('button', { name: /^(Авто|Auto)$/ })
      .click();
    const before = await previewCounts(page);
    expect(before.length, 'по числу на каждое установленное ядро').toBe(
      Number(inst.xray) + Number(inst.mihomo)
    );
    expect(
      before.every((n) => n >= 2),
      'оба узла попали в предпросмотр'
    ).toBe(true);

    // исключение по подстроке без учёта регистра уменьшает состав в каждом ядре
    await modal.getByTestId('group-exclude').fill('dlgbeta');
    await expect
      .poll(async () => (await previewCounts(page)).every((n, i) => n === before[i] - 1), {
        timeout: T.action
      })
      .toBe(true);

    await save.click();
    await expect(modal).toHaveCount(0, { timeout: T.action });
    const card = page.locator('article[data-testid^="group-card-"]').filter({ hasText: name });
    await expect(card).toBeVisible({ timeout: T.action });
    await expect(card.locator('[data-testid^="group-mode-"]')).toHaveText(/Авто|Auto/);

    // в черновике лежит то, что показал диалог
    const saved = (await groupsFull(page)).find((g) => g.name === name);
    expect(saved?.mode).toBe('auto');
    expect(saved?.exclude).toEqual(['dlgbeta']);
    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test('groups: диалог группы — «Вручную» с участниками DIRECT и вложенной «Авто», цикл при вложении группы, уже содержащей текущую', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await resetDraft(page);
  const port = randomPort();
  try {
    await addToDraft(page, nodeLink('vless_ws.txt', port, 'DlgMember'));
    const autoId = await saveGroupApi(page, { name: `RT Вложенная ${port}`, mode: 'auto' });
    await openGroups(page);

    // «Вручную» (режим по умолчанию): участники DIRECT и вложенная группа, порядок кнопками
    const modal = await openNewGroup(page);
    const name = `RT Ручная ${port}`;
    await modal.getByRole('textbox', { name: /^(Имя|Name)$/ }).fill(name);
    const add = modal.getByTestId('group-member-add');
    await add.selectOption('direct');
    await add.selectOption(`group:${autoId}`);
    const members = modal.getByTestId('group-members').locator('li');
    await expect(members).toHaveCount(2);
    await expect(members.first()).toContainText('DIRECT');
    // «Вниз» у первого участника меняет порядок
    await members
      .first()
      .getByRole('button', { name: /Опустить участника|Move member .* down/ })
      .click();
    await expect(members.last()).toContainText('DIRECT');
    await page.getByTestId('group-save').click();
    await expect(modal).toHaveCount(0, { timeout: T.action });
    const manual = (await groupsFull(page)).find((g) => g.name === name);
    expect(
      manual?.members.map((m) => m.kind),
      'порядок участников'
    ).toEqual(['group', 'direct']);
    const manualId = manual?.id ?? '';

    // цикл: вторая ручная группа вкладывает первую; вложить вторую в первую нельзя
    const outerId = await saveGroupApi(page, {
      name: `RT Внешняя ${port}`,
      mode: 'manual',
      members: [{ kind: 'group', id: manualId }]
    });
    await expect(page.getByTestId(`group-card-${outerId}`)).toBeVisible({ timeout: T.action });
    await page.getByTestId(`group-edit-${manualId}`).click();
    const edit = modalOf(page);
    await expect(edit).toBeVisible({ timeout: T.action });
    await expect(edit.getByRole('textbox', { name: /^(Имя|Name)$/ })).toHaveValue(name);
    await edit.getByTestId('group-member-add').selectOption(`group:${outerId}`);
    await page.getByTestId('group-save').click();
    const cycle = edit.getByTestId('group-cycle-error');
    await expect(cycle).toBeVisible({ timeout: T.action });
    // диалог остался открытым с введённым, отмена закрывает без записи
    await expect(edit).toBeVisible();
    await page.getByTestId('group-cancel').click();
    await expect(edit).toHaveCount(0, { timeout: T.action });
    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});

test('groups: диалог группы — «Резерв» без резервного узла: ошибка под полем и неактивное сохранение', async ({
  page
}) => {
  const guard = attachConsoleGuard(page);
  await startPage(page);
  await resetDraft(page);
  const port = randomPort();
  try {
    await addToDraft(page, nodeLink('vless_ws.txt', port, 'DlgReserve'));
    await openGroups(page);
    const modal = await openNewGroup(page);
    const name = `RT Резерв ${port}`;
    await modal.getByRole('textbox', { name: /^(Имя|Name)$/ }).fill(name);
    await modal
      .getByTestId('group-mode')
      .getByRole('button', { name: /^(Резерв|Fallback)$/ })
      .click();

    const error = modal.getByTestId('group-reserve-error');
    await expect(error).toBeVisible({ timeout: T.action });
    await expect(page.getByTestId('group-save')).toBeDisabled();

    // выбор резервного узла снимает ошибку; сохранение пишет в черновик режим и узел
    await modal.getByTestId('group-reserve').selectOption({ index: 1 });
    await expect(error).toHaveCount(0);
    await expect(page.getByTestId('group-save')).toBeEnabled();
    await page.getByTestId('group-save').click();
    await expect(modal).toHaveCount(0, { timeout: T.action });
    const saved = (await groupsFull(page)).find((g) => g.name === name);
    expect(saved?.mode).toBe('fallback');
    expect(saved?.reserve, 'резервный узел записан').not.toBe('');
    await guard.assertClean();
  } finally {
    await resetDraft(page);
  }
});
