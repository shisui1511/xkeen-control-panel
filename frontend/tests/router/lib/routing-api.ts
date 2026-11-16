import { expect, type Page } from '@playwright/test';
import { T } from './env';
import { api, resetDraft } from './nodes-api';

// Помощники роутерных сценариев групп и выбора узла: вид маршрутизации панели, записи
// «Моих узлов» в черновике, «Применить» с ожиданием конца, взятие маршрутизации и возврат
// стенда. Общие для config-groups и proxies-runtime: подготовка и уборка у них одна.
// Сеть не подменяется. В вывод не попадают имена узлов и групп.

export interface GroupInfo {
  id: string;
  name: string;
  mode: string;
  current: string;
  applied: boolean;
}

export interface RoutingInfo {
  revision: number;
  desired: boolean;
  hasSources: boolean;
  defaultsDone: boolean;
  groups: GroupInfo[];
}

export async function routingInfo(page: Page): Promise<RoutingInfo> {
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
      current: g.current ?? '',
      applied: g.applied === true
    }))
  };
}

/** Число изменений черновика из снимка слоя. */
export async function draftChanges(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_changes ?? 0);
}

/** Идентификаторы записей «Моих узлов» в черновике. */
export async function myEntryIds(page: Page): Promise<string[]> {
  const view = await api(page, 'GET', '/api/configlayer/sources');
  const sources = (view.data?.sources ?? []) as { id: string; nodes?: { entry_id?: string }[] }[];
  const my = sources.find((s) => s.id === 'my');
  return (my?.nodes ?? []).map((n) => n.entry_id ?? '').filter((id) => id !== '');
}

/** Убирает из черновика записи «Моих узлов», которых не было в списке `keep`. */
export async function removeMyEntries(page: Page, keep: string[]): Promise<void> {
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
export async function applyAndWait(page: Page): Promise<void> {
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

interface ApplyView {
  running?: boolean;
  result?: { ok?: boolean; code?: string } | null;
}

async function applyView(page: Page): Promise<ApplyView> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return (st.data?.apply ?? {}) as ApplyView;
}

/**
 * Возвращает панели её файлы в состоянии released («Принять правку» другого сценария или
 * пользователя): «Пересобрать» по их ключам и итог применения. Зовётся после взятия
 * маршрутизации: released-профиль Mihomo слой не пишет, и группы панели в Mihomo не попадают
 * (выбор отвечает failed). В вывод — только число файлов.
 */
export async function rebuildReleased(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  const files = (st.data?.files ?? []) as { key?: string; state?: string }[];
  const keys = files.filter((f) => f.key && f.state === 'released').map((f) => f.key as string);
  if (keys.length === 0) return 0;
  const before = JSON.stringify(await applyView(page));
  const started = Date.now();
  const rb = await api(page, 'POST', '/api/configlayer/files/rebuild', { keys });
  expect(rb.status, `files/rebuild (${keys.length} файлов released)`).toBe(200);
  // пересборка идёт в фоне: ждём, что применение началось или его итог сменился, затем конец
  let seen = false;
  let last: ApplyView = {};
  await expect
    .poll(
      async () => {
        last = await applyView(page);
        if (last.running === true) {
          seen = true;
          return 'идёт';
        }
        if (!last.result) return 'итога нет';
        const changed = JSON.stringify(last) !== before;
        return seen || changed || Date.now() - started > 5000 ? 'готово' : 'ожидание запуска';
      },
      { message: 'итог пересборки released-файлов', timeout: T.save * 3, intervals: [1000] }
    )
    .toBe('готово');
  expect(last.result?.ok, `итог пересборки: ${last.result?.code ?? ''}`).toBe(true);
  return keys.length;
}

/**
 * Берёт маршрутизацию, применяет черновик и возвращает панели released-файлы: после этого
 * группы черновика применены на обоих ядрах.
 */
export async function takeAndApply(page: Page): Promise<void> {
  const take = await api(page, 'POST', '/api/configlayer/routing/take', {
    revision: (await routingInfo(page)).revision
  });
  expect(take.status, 'взятие маршрутизации').toBe(200);
  await applyAndWait(page);
  await rebuildReleased(page);
}

/** Что вернуть на стенде после сценария с группами и взятием маршрутизации. */
export interface RoutingStand {
  /** Группы сценария в порядке удаления: внешние раньше вложенных. */
  groupIds: string[];
  /** Записи «Моих узлов», которые были до сценария. */
  myBefore: string[];
  /** Маршрутизация была взята до сценария. */
  wasManaged: boolean;
}

/**
 * Возврат стенда: свои группы удаляются, записи «Моих узлов» сценария убираются,
 * маршрутизация отпускается, если не была взята; изменения применяются, черновик сбрасывается.
 */
export async function restoreRouting(page: Page, stand: RoutingStand): Promise<void> {
  const rev = async () => (await routingInfo(page)).revision;
  for (const id of stand.groupIds) {
    if (!id) continue;
    await api(page, 'POST', '/api/configlayer/routing/groups/delete', {
      revision: await rev(),
      id,
      cascade: { mode: '' }
    });
  }
  await removeMyEntries(page, stand.myBefore);
  if (!stand.wasManaged) {
    await api(page, 'POST', '/api/configlayer/routing/release', { revision: await rev() });
  }
  if ((await draftChanges(page)) > 0) await applyAndWait(page);
  await resetDraft(page);
}
