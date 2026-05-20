<script lang="ts">
  let { size = 16, color = 'currentColor', class: className = '' } = $props<{
    size?: number;
    color?: string;
    class?: string;
  }>();
</script>

<svg
  xmlns="http://www.w3.org/2000/svg"
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke={color}
  stroke-width="2"
  stroke-linecap="round"
  stroke-linejoin="round"
  class={className}
>
  <line x1="8" y1="6" x2="21" y2="6" />
  <line x1="8" y1="12" x2="21" y2="12" />
  <line x1="8" y1="18" x2="21" y2="18" />
  <line x1="3" y1="6" x2="3.01" y2="6" />
  <line x1="3" y1="12" x2="3.01" y2="12" />
  <line x1="3" y1="18" x2="3.01" y2="18" />
</svg>
