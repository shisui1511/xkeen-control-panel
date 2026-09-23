/**
 * Mihomo's GET /group/{name}/delay answers with a map of member name to delay
 * in ms; members that timed out are either missing or reported as 0.
 */
export interface GroupDelaySummary {
  total: number;
  alive: number;
  best: number | null;
  current: number | null;
}

const BUILTIN = new Set(['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'COMPATIBLE']);

export function summarizeGroupDelay(
  delays: Record<string, unknown> | null | undefined,
  members: string[],
  now?: string
): GroupDelaySummary {
  const map = delays ?? {};
  const tested = members.filter((m) => !BUILTIN.has(m));
  const valid = (v: unknown): v is number => typeof v === 'number' && v > 0;

  let alive = 0;
  let best: number | null = null;
  for (const name of tested) {
    const v = map[name];
    if (!valid(v)) continue;
    alive++;
    if (best === null || v < best) best = v;
  }

  const cur = now ? map[now] : undefined;
  return { total: tested.length, alive, best, current: valid(cur) ? cur : null };
}
