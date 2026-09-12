<script lang="ts">
  interface Props {
    live: boolean;
    label: string;
  }

  let { live, label }: Props = $props();
</script>

<!-- Пульсация обеспечивается global.css .status-indicator.connected::before (keyframe ledPulse) -->
<span class="status-indicator" class:connected={live}>{label}</span>
