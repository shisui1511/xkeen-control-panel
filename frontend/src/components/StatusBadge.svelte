<script lang="ts">
  interface Props {
    variant: 'running' | 'stopped' | 'warning' | 'idle' | 'info';
    label: string;
  }

  let { variant, label }: Props = $props();
</script>

<!-- Стили модификаторов живут в global.css под .status-badge / .status-badge.{variant} -->
<span class="status-badge {variant}">
  <span class="status-badge-dot" aria-hidden="true"></span>
  {label}
</span>
