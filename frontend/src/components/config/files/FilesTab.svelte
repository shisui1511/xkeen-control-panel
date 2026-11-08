<script lang="ts">
  import Editor from '../../../Editor.svelte';

  // Вкладка «Файлы» раздела «Конфигурация» (D-01, CFG-03): нынешний Редактор во встроенном
  // режиме, без копии вёрстки. Раздел держит вкладку смонтированной после первого открытия
  // и скрывает её, пока открыта другая вкладка: открытые файлы и несохранённые правки живут.
  let { active = true }: { active?: boolean } = $props();
</script>

<section class="files-tab" data-testid="files-tab" hidden={!active}>
  <Editor embedded {active} />
</section>
