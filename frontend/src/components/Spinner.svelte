<script lang="ts">
  // Индикатор загрузки. Стили — .spinner в global.css (оборот — --duration-spin).
  //   size  — диаметр в px (по умолчанию 16);
  //   label — текст для экранного диктора: спиннер становится role="status".
  //           Без label он декоративный (рядом уже есть текст «Загрузка…»).
  //   tone  — accent (по умолчанию) или current — цвета текста вокруг (в кнопке, бейдже).
  interface Props {
    size?: number;
    label?: string;
    tone?: 'accent' | 'current';
    class?: string;
  }

  let { size = 16, label, tone = 'accent', class: className = '' }: Props = $props();
</script>

<span
  class="spinner {className}"
  class:spinner-current={tone === 'current'}
  style:--spinner-size="{size}px"
  role={label ? 'status' : undefined}
  aria-label={label}
  aria-hidden={label ? undefined : 'true'}
></span>
