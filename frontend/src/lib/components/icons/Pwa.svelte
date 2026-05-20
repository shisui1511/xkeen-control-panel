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
  <rect x="5" y="2" width="14" height="20" rx="2" ry="2" />
  <line x1="12" y1="18" x2="12.01" y2="18" />
  <polyline points="9 10 12 13 15 10" />
  <line x1="12" y1="6" x2="12" y2="13" />
</svg>
