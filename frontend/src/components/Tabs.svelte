<script module lang="ts">
  export interface TabItem {
    value: string;
    label: string;
    disabled?: boolean;
  }

  // RED stub: намеренно не реализовано — GREEN добавит логику выбора.
  export function resolveTabValue(_item: TabItem, currentValue: string): string {
    return currentValue;
  }
</script>

<script lang="ts">
  interface Props {
    items: TabItem[];
    value: string;
    ariaLabel?: string;
    onchange?: (value: string) => void;
  }

  let { value = $bindable() }: Props = $props();
</script>

<!-- RED stub: контейнер не рендерится вовсе, вкладки ещё не реализованы -->
