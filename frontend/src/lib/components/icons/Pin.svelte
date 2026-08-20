<script lang="ts">
  let { size = 14, color = 'currentColor' } = $props<{
    size?: number;
    color?: string;
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
>
  <path d="M12 17v5" />
  <path
    d="M9 3h6l-.5 4.5a3 3 0 0 0 1 2.6l1.2 1a1.6 1.6 0 0 1-1 2.9H8.3a1.6 1.6 0 0 1-1-2.9l1.2-1a3 3 0 0 0 1-2.6z"
  />
</svg>
