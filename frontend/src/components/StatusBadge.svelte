<script lang="ts">
  interface Props {
    variant: 'running' | 'stopped' | 'warning' | 'idle' | 'info';
    label: string;
  }

  let { variant, label }: Props = $props();
</script>

<span class="status-badge">{label}</span>
