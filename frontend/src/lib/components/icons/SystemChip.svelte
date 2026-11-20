<script lang="ts">
  let {
    size = 16,
    color = 'currentColor',
    class: className = ''
  }: {
    size?: number;
    color?: string;
    class?: string;
  } = $props();
</script>

<svg
  aria-hidden="true"
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
  <rect x="7" y="7" width="10" height="10" rx="1.5" />
  <path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.5 4.5l2 2M17.5 17.5l2 2M4.5 19.5l2-2M17.5 6.5l2-2" />
</svg>
