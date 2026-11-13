import { expect, type Page } from '@playwright/test';
import { T } from './env';
import { addToDraft, api, nodeLink, randomPort, resetDraft } from './nodes-api';

// Подготовка стенда к перехвату DNS для роутерных сценариев (фаза 148): секция DNS ядра, маршрутизация
// панели и группа-цель «Прокси». Без неё включение заблокировано (section, routing, group), и
// сценарии перехвата молча пропускаются. Помощник делает только недостающее и запоминает, что
// сделал сам: уборка возвращает ровно это. Сеть не подменяется.

export const DNS_TARGET_GROUP = 'Прокси';

/** Что помощник поменял на стенде. */
export interface DnsTargetPrep {
  groupId: string;
  routingTaken: boolean;
  sectionKernel: string;
  /** Имя узла из testdata, который помощник положил в «Мои узлы», чтобы группы не были пустыми. */
  nodeName: string;
}

export const emptyDnsTargetPrep = (): DnsTargetPrep => ({
  groupId: '',
  routingTaken: false,
  sectionKernel: '',
  nodeName: ''
});

async function draftRevision(page: Page): Promise<number> {
  const st = await api(page, 'GET', '/api/configlayer/state');
  expect(st.status, 'GET /api/configlayer/state').toBe(200);
  return Number(st.data?.draft_revision ?? 0);
}

/** Запускает применение и ждёт его конца: применение не идёт, черновик пуст. */
async function applyAndWait(page: Page): Promise<void> {
  const start = await api(page, 'POST', '/api/configlayer/apply', {});
  expect(start.status, `запуск применения: ${JSON.stringify(start.data)}`).toBe(200);
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

/**
 * На стенде без узлов дефолтные группы «Авто» и «Прокси» пусты, и «Применить» блокирует
 * group_empty (D-36). Тогда в «Мои узлы» кладётся узел из testdata; уборка его убирает.
 */
async function ensureNodeForGroups(page: Page, prep: DnsTargetPrep): Promise<void> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  const issues = Array.isArray(view.data?.issues) ? (view.data.issues as { code?: string }[]) : [];
  if (!issues.some((i) => i?.code === 'group_empty')) return;
  const name = `RtDnsNode${randomPort()}`;
  await addToDraft(page, nodeLink('vless_ws.txt', randomPort(), name));
  prep.nodeName = name;
}

/** Убирает узел, положенный ensureNodeForGroups. */
async function releaseNode(page: Page, prep: DnsTargetPrep): Promise<void> {
  if (!prep.nodeName) return;
  const view = await api(page, 'GET', '/api/configlayer/sources');
  expect(view.status, 'GET /api/configlayer/sources').toBe(200);
  const sources = (view.data?.sources ?? []) as {
    kind?: string;
    nodes?: { name?: string; entry_id?: string }[];
  }[];
  const entry = sources
    .filter((s) => s.kind === 'my')
    .flatMap((s) => s.nodes ?? [])
    .find((n) => n.name === prep.nodeName);
  if (!entry?.entry_id) return;
  const del = await api(page, 'POST', '/api/configlayer/sources/nodes/delete', {
    revision: Number(view.data?.draft_revision ?? 0),
    entry_id: entry.entry_id
  });
  expect(del.status, 'удаление узла помощника').toBe(200);
}

async function routingInfo(
  page: Page
): Promise<{ revision: number; desired: boolean; groupNames: string[] }> {
  const view = await api(page, 'GET', '/api/configlayer/routing');
  expect(view.status, 'GET /api/configlayer/routing').toBe(200);
  const groups = Array.isArray(view.data?.groups) ? (view.data.groups as { name?: string }[]) : [];
  return {
    revision: Number(view.data?.revision ?? 0),
    desired: view.data?.desired === true,
    groupNames: groups.map((g) => String(g?.name ?? ''))
  };
}

async function dnsBlocks(page: Page): Promise<{ blocks: string[]; kernel: string }> {
  const res = await api(page, 'GET', '/api/configlayer/dns');
  expect(res.status, 'GET /api/configlayer/dns').toBe(200);
  return {
    blocks: (res.data?.blocks ?? []) as string[],
    kernel: String(res.data?.active_kernel ?? '')
  };
}

/**
 * Снимает блокировки включения section, routing и group: берёт секцию DNS (Xray) и
 * маршрутизацию, создаёт группу «Прокси», если её нет, применяет. Секции Mihomo приходят
 * вместе с маршрутизацией (mihomo_with_profile). Прочие блокировки (ядро остановлено,
 * конфликт ядер) не трогает — сценарий сам пропускается с их причиной. Сделанное пишется в
 * prep по ходу дела: при сбое на полпути уборка вернёт уже сделанное.
 */
export async function ensureDnsTarget(
  page: Page,
  prep: DnsTargetPrep = emptyDnsTargetPrep()
): Promise<DnsTargetPrep> {
  let v = await dnsBlocks(page);
  if (v.blocks.includes('draft')) {
    await resetDraft(page);
    v = await dnsBlocks(page);
  }
  const mihomoSection = v.kernel === 'mihomo' && v.blocks.includes('section');
  const needGroup = v.blocks.includes('group');
  const needRouting = v.blocks.includes('routing') || needGroup || mihomoSection;
  let changed = false;

  if (v.blocks.includes('section') && !mihomoSection) {
    const take = await api(page, 'POST', '/api/configlayer/cores/take', {
      revision: await draftRevision(page),
      kernel: v.kernel,
      sections: ['dns'],
      ack_comments: true
    });
    expect(take.status, `взятие секции DNS ${v.kernel}: ${JSON.stringify(take.data)}`).toBe(200);
    prep.sectionKernel = v.kernel;
    changed = true;
  }
  if (needRouting) {
    if (!(await routingInfo(page)).desired) {
      const take = await api(page, 'POST', '/api/configlayer/routing/take', {
        revision: (await routingInfo(page)).revision
      });
      expect(take.status, `взятие маршрутизации: ${JSON.stringify(take.data)}`).toBe(200);
      prep.routingTaken = true;
      changed = true;
    }
    if (!(await routingInfo(page)).groupNames.includes(DNS_TARGET_GROUP)) {
      const saved = await api(page, 'POST', '/api/configlayer/routing/groups/save', {
        revision: (await routingInfo(page)).revision,
        group: {
          id: '',
          name: DNS_TARGET_GROUP,
          mode: 'manual',
          sources: { all: true, ids: [] },
          include: [],
          exclude: [],
          regex: false,
          picks: [],
          members: [{ kind: 'direct' }],
          reserve: ''
        }
      });
      expect(saved.status, `группа-цель перехвата: ${JSON.stringify(saved.data)}`).toBe(200);
      prep.groupId = String((saved.data?.group as { id?: string } | undefined)?.id ?? '');
      changed = true;
    }
  }
  if (changed) {
    await ensureNodeForGroups(page, prep);
    await applyAndWait(page);
  }
  return prep;
}

/** Возвращает стенд к состоянию до ensureDnsTarget: группа, маршрутизация, секция. */
export async function releaseDnsTarget(page: Page, prep: DnsTargetPrep): Promise<void> {
  if (prep.groupId) {
    // Включение перехвата записывает id группы по умолчанию в параметры: без сброса после удаления
    // группы в параметрах остаётся ссылка на неё, и следующее включение блокируется (group).
    const dns = await api(page, 'GET', '/api/configlayer/dns');
    const params = (dns.data?.draft_params ?? null) as { group?: string; zones?: string[] } | null;
    if (params?.group === prep.groupId) {
      const reset = await api(page, 'POST', '/api/configlayer/draft', {
        revision: await draftRevision(page),
        section: 'dns_takeover',
        value: { group: '', zones: params.zones ?? [] }
      });
      expect(reset.status, 'сброс группы в параметрах перехвата').toBe(200);
    }
    const del = await api(page, 'POST', '/api/configlayer/routing/groups/delete', {
      revision: (await routingInfo(page)).revision,
      id: prep.groupId,
      cascade: { mode: '' }
    });
    expect(del.status, 'удаление группы-цели').toBe(200);
  }
  if (prep.routingTaken) {
    const rel = await api(page, 'POST', '/api/configlayer/routing/release', {
      revision: (await routingInfo(page)).revision
    });
    expect(rel.status, 'возврат маршрутизации').toBe(200);
  }
  if (prep.sectionKernel) {
    const rel = await api(page, 'POST', '/api/configlayer/cores/release', {
      revision: await draftRevision(page),
      kernel: prep.sectionKernel,
      section: 'dns'
    });
    expect(rel.status, 'возврат секции DNS').toBe(200);
  }
  await releaseNode(page, prep);
  const st = await api(page, 'GET', '/api/configlayer/state');
  if (Number(st.data?.draft_changes ?? 0) > 0) await applyAndWait(page);
  await resetDraft(page);
  Object.assign(prep, emptyDnsTargetPrep());
}
