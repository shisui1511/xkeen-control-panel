<script module lang="ts">
  import type { Component } from 'svelte';

  export interface SegmentItem {
    value: string;
    label: string;
    icon?: Component<{ size?: number }>;
  }

  // RED stub: намеренно не реализовано — GREEN добавит логику выбора.
  export function resolveSegmentValue(_item: SegmentItem): string {
    return '';
  }
</script>

<script lang="ts">
  interface Props {
    items: SegmentItem[];
    value: string;
    ariaLabel?: string;
    onchange?: (value: string) => void;
  }

  let { value = $bindable() }: Props = $props();
</script>

<!-- RED stub: сегменты ещё не реализованы -->
