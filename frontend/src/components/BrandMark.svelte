<script lang="ts">
  // Знак XKeen: четыре конечные точки, скрещённые маршруты и узел в центре.
  // Читается и как «X» из названия, и как схема переключения трафика.
  // Единственный источник логотипа в интерфейсе: боковая панель, экран входа,
  // мобильная шапка. Та же геометрия лежит в public/favicon.svg и в PNG-иконках
  // (scripts/gen-icons.cjs). Цвет — currentColor; отверстие в узле вырезано
  // маской, поэтому сквозь него виден фон плитки при любом акценте и теме.
  let { size = 24 }: { size?: number } = $props();
  const uid = $props.id();
  const maskId = `xk-hub-${uid}`;
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 32 32"
  fill="none"
  aria-hidden="true"
  focusable="false"
>
  <defs>
    <mask id={maskId} maskUnits="userSpaceOnUse" x="0" y="0" width="32" height="32">
      <rect width="32" height="32" fill="white" />
      <circle cx="16" cy="16" r="2.1" fill="black" />
    </mask>
  </defs>
  <g mask="url(#{maskId})" fill="currentColor">
    <path
      d="M7.5 7.5 24.5 24.5M24.5 7.5 7.5 24.5"
      stroke="currentColor"
      stroke-width="3.2"
      stroke-linecap="round"
    />
    <circle cx="6" cy="6" r="3.2" />
    <circle cx="26" cy="6" r="3.2" />
    <circle cx="6" cy="26" r="3.2" />
    <circle cx="26" cy="26" r="3.2" />
    <circle cx="16" cy="16" r="5" />
  </g>
</svg>
