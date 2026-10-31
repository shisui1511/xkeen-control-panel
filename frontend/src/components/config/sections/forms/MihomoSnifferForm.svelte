<script lang="ts">
  import { t } from '../../../../i18n';
  import type {
    CoreKernel,
    ForceProxyView,
    KeyIssue,
    SectionView
  } from '../../../../lib/configCores';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import MihomoToggle from '../MihomoToggle.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import { KERNEL_DEFAULTS, commitSection, fieldTestId, getPath, setPath } from '../sectionForm';

  // Форма Sniffer Mihomo: четыре переключателя. Порты sniff и прочие подключи остаются в
  // «Дополнительно» (YAML). Чужая секция (readonly) показывает действующие значения.
  let {
    kernel,
    section,
    readonly
  }: {
    kernel: CoreKernel;
    section: SectionView;
    readonly: boolean;
    forceProxy?: ForceProxyView | undefined;
    routingTaken?: boolean;
  } = $props();

  const ID = 'sniffer';
  const uid = $props.id();
  const defaults = KERNEL_DEFAULTS['mihomo.sniffer'] ?? {};

  const TOGGLES = [
    { path: 'enable', label: 'cfg.sec.f.sniffer_enable' },
    { path: 'force-dns-mapping', label: 'cfg.sec.f.force_dns_mapping' },
    { path: 'parse-pure-ip', label: 'cfg.sec.f.parse_pure_ip' },
    { path: 'override-destination', label: 'cfg.sec.f.override_destination' }
  ] as const;

  // Значения формы: производные от вида секции, правка их переопределяет, новый вид
  // секции (после записи в черновик) сбрасывает к нему
  let local = $derived<Record<string, unknown>>(
    section.form !== null && typeof section.form === 'object' && !Array.isArray(section.form)
      ? (section.form as Record<string, unknown>)
      : {}
  );
  let issues = $state<KeyIssue[]>([]);

  function hintOf(path: string): string {
    if (getPath(local, path) !== undefined) return '';
    const value = defaults[path];
    return value === undefined ? '' : $t('cfg.sec.default_hint', { value });
  }

  async function save(next: Record<string, unknown>, text: string): Promise<void> {
    issues = await commitSection(kernel, ID, next, text);
  }

  function setField(path: string, value: boolean): void {
    const next = setPath(local, path, value);
    local = next;
    void save(next, section.advanced);
  }

  async function saveAdvanced(text: string): Promise<void> {
    await save(local, text);
  }
</script>

<div class="msf" data-testid="section-form-mihomo-{ID}">
  <div class="msf-fields">
    {#each TOGGLES as item (item.path)}
      <MihomoToggle
        id="{uid}-{item.path}"
        label={$t(item.label)}
        checked={getPath(local, item.path) === true}
        {readonly}
        hint={hintOf(item.path)}
        testid={fieldTestId(kernel, ID, item.path)}
        onchange={(v) => setField(item.path, v)}
      />
    {/each}
  </div>

  <PinnedValues {kernel} id={ID} items={section.pinned} />
  <AdvancedBlock
    {kernel}
    id={ID}
    text={section.advanced}
    {readonly}
    {issues}
    oncommit={saveAdvanced}
  />
</div>

<style>
  .msf {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }

  .msf-fields {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
</style>
