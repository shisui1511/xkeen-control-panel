<script lang="ts">
  let {
    size = 16,
    color = 'currentColor',
    class: className = ''
  } = $props<{
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
  <rect x="3" y="3" width="7" height="9" rx="1" />
  <rect x="14" y="3" width="7" height="5" rx="1" />
  <rect x="14" y="12" width="7" height="9" rx="1" />
  <rect x="3" y="16" width="7" height="5" rx="1" />
</svg>
