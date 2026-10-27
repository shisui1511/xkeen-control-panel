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

<!-- Ручка перетаскивания: два столбца по три точки (точка — отрезок нулевой длины с круглым концом) -->
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
  <line x1="9" y1="6" x2="9" y2="6.01" />
  <line x1="9" y1="12" x2="9" y2="12.01" />
  <line x1="9" y1="18" x2="9" y2="18.01" />
  <line x1="15" y1="6" x2="15" y2="6.01" />
  <line x1="15" y1="12" x2="15" y2="12.01" />
  <line x1="15" y1="18" x2="15" y2="18.01" />
</svg>
