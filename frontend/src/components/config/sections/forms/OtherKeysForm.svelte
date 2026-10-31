<script lang="ts">
  import type {
    CoreKernel,
    ForceProxyView,
    KeyIssue,
    SectionView
  } from '../../../../lib/configCores';
  import AdvancedBlock from '../AdvancedBlock.svelte';
  import PinnedValues from '../PinnedValues.svelte';
  import { commitSection } from '../sectionForm';

  // «Прочие ключи» обоих ядер (D-08, MIHO-01): форм у секции нет, весь остаток правится в
  // «Дополнительно» — JSON у Xray (stats, metrics, fakedns, reverse), YAML у Mihomo (hosts,
  // ntp, sub-rules, experimental). Props — общий контракт форм секций (SectionCard).
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

  let issues = $state<KeyIssue[]>([]);

  async function save(text: string): Promise<void> {
    issues = await commitSection(kernel, 'other', {}, text);
  }
</script>

<div class="okf">
  <PinnedValues {kernel} id="other" items={section.pinned} />
  <AdvancedBlock {kernel} id="other" text={section.advanced} {readonly} {issues} oncommit={save} />
</div>

<style>
  .okf {
    display: flex;
    flex-direction: column;
    gap: var(--spacing-4);
    min-width: 0;
  }
</style>
