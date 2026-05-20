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
  <path d="M4.5 16.5c-1.5 1.25-2.5 3.5-2.5 3.5s2.25-1 3.5-2.5" />
  <path d="M12 12l9-9-3 12-3 1-3-3-1-3z" />
  <path d="M9 15l-3-3" />
</svg>
