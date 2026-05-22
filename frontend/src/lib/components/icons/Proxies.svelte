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
  <circle cx="12" cy="5" r="3" />
  <circle cx="5" cy="19" r="3" />
  <circle cx="19" cy="19" r="3" />
  <path d="M5 16v-3a4 4 0 0 1 4-4h6a4 4 0 0 1 4 4v3" />
  <path d="M12 8v8" />
</svg>
