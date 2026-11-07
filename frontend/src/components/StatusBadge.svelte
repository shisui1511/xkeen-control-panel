<script lang="ts">
  import Badge, { type BadgeTone } from './Badge.svelte';

  // Состояние сервиса, ядра, файла. Обёртка над Badge variant="status": переводит
  // смысловой вариант в тон. data-status хранит вариант для тестов и стилей страниц.
  interface Props {
    variant: 'running' | 'stopped' | 'warning' | 'idle' | 'info';
    label: string;
    pulse?: boolean;
  }

  let { variant, label, pulse = false }: Props = $props();

  const TONE: Record<Props['variant'], BadgeTone> = {
    running: 'success',
    stopped: 'danger',
    warning: 'warning',
    idle: 'emphasis',
    info: 'info'
  };
</script>

<Badge variant="status" tone={TONE[variant]} {pulse} data-status={variant}>{label}</Badge>
