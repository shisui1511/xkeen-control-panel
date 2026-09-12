<script lang="ts">
  interface Props {
    live: boolean;
    label: string;
  }

  let { live, label }: Props = $props();
</script>

<span class="status-indicator">{label}</span>
