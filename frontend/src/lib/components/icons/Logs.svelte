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
  <path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2" />
  <rect x="8" y="2" width="8" height="4" rx="1" ry="1" />
  <line x1="9" y1="10" x2="15" y2="10" />
  <line x1="9" y1="14" x2="15" y2="14" />
  <line x1="9" y1="18" x2="13" y2="18" />
</svg>
