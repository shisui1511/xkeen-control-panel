<script lang="ts">
  import NodeImporter from '../subscriptions/NodeImporter.svelte';

  interface Subscription {
    id: string;
    name: string;
  }

  interface ParseReport {
    timestamp: string;
    parsed_count: number;
    skipped_count: number;
    skipped: { line: number; reason: string; snippet: string }[];
  }

  interface RawResponse {
    headers: Record<string, string[]>;
    body: string;
  }

  interface Props {
    diagnosticSub: Subscription | null;
    diagnosticTab?: 'report' | 'headers' | 'raw';
    diagnosticLoading?: boolean;
    parseReportData?: ParseReport | null;
    rawResponseData?: RawResponse | null;
    onClose: () => void;
    onTabChange?: (tab: 'report' | 'headers' | 'raw') => void;
  }

  let {
    diagnosticSub,
    diagnosticTab = 'report',
    diagnosticLoading = false,
    parseReportData = null,
    rawResponseData = null,
    onClose,
    onTabChange = () => {}
  }: Props = $props();
</script>

{#if diagnosticSub}
  <NodeImporter
    {diagnosticSub}
    {diagnosticTab}
    {diagnosticLoading}
    {parseReportData}
    {rawResponseData}
    {onClose}
    {onTabChange}
  />
{/if}
