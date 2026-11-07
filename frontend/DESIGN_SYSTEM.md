# Дизайн-система панели

Один источник правды для вида панели: токены в `src/styles/global.css`, компоненты в
`src/components`, стражи в `scripts/check-*.cjs`. Стражи запускаются командой
`npm run check:contrast` и работают в CI на каждой сборке.

Правило по умолчанию: **сначала токен и компонент, потом свой CSS.** Если нужного варианта
нет, добавьте его в систему (токен, модификатор, проп), а не локальный класс в компоненте.

## Токены

Все токены объявлены в первом блоке `:root` в `global.css`. Темы (`[data-theme]`,
`[data-scheme='light']`) и плотность (`[data-density]`) только переопределяют значения.
У токена из `:root` запасное значение не пишется: `var(--spacing-2)`, а не
`var(--spacing-2, 8px)`. Оно никогда не срабатывает, и `check-css-tokens` его запрещает.

### Цвет

| Роль             | Токены                                                                                                                                               |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Поверхности      | `--bg` (страница), `--bg-card`, `--bg-elevated` (поле, приподнятый блок), `--bg-deep` (шапка, нижняя панель — тёмные во всех темах), `--bg-terminal` |
| Текст            | `--fg-primary`, `--fg-secondary`, `--fg-dim` (подписи 12px, держит 4.5:1), `--fg-faint` (только декор)                                               |
| Линии            | `--border`, `--border-strong`, `--border-control` (рамка поля, 3:1), `--border-light`                                                                |
| Акцент           | `--accent`, `--accent-hover`, `--accent-text` (акцент как цвет текста), `--accent-soft` (подложка), `--accent-line` (рамка), `--hover`               |
| Статусы          | `--success`, `--warning`, `--danger`, `--idle`, `--purple`; подложки `--*-soft`                                                                      |
| Наложения        | `--surface-tint-faint`, `--surface-tint`, `--surface-tint-strong`                                                                                    |
| Текст на заливке | `--btn-primary-text`, `--on-danger`, `--on-deep*`                                                                                                    |
| Код              | `--cm-*` (CodeMirror), `--code-*` (статичное превью конфигурации)                                                                                    |
| Ряды данных      | `--seq-1…8` — цвет по индексу элемента, не по хешу                                                                                                   |

Полупрозрачность цвета из токена — через `color-mix(in srgb, var(--success) 14%, transparent)`
или `rgba(var(--accent-rgb), 0.2)`. Литеральные hex запрещены (`check-hardcoded-colors`),
пары контраста по всем темам проверяет `check-contrast` (`scripts/contrast-pairs.json`).

Устаревшие имена (`--primary`, `--text`, `--fg-muted`, `--error`, `--spacing-sm` …) запрещены,
`check-css-tokens` подскажет замену.

### Отступы — `--spacing-N`

Шаг 4px: номер × 4px, полушаги — через дефис.

| Токен           | px  |     | Токен           | px  |
| --------------- | --- | --- | --------------- | --- |
| `--spacing-0-5` | 2   |     | `--spacing-3-5` | 14  |
| `--spacing-1`   | 4   |     | `--spacing-4`   | 16  |
| `--spacing-1-5` | 6   |     | `--spacing-5`   | 20  |
| `--spacing-2`   | 8   |     | `--spacing-6`   | 24  |
| `--spacing-2-5` | 10  |     | `--spacing-8`   | 32  |
| `--spacing-3`   | 12  |     | `--spacing-10`  | 40  |

Для `padding`, `margin` и `gap` литералом можно писать только `0` и `±1px` (оптическая
подгонка). Отрицательный отступ — `calc(-1 * var(--spacing-2))`.

### Типографика

- Размер: `--font-size-2xs` (11) · `xs` (12) · `sm` (13) · `base` (14) · `lg` (16) · `xl` (18) ·
  `2xl` (22) · `3xl` (24). Литералы в px и rem запрещены (`check-font-sizes`).
- Шрифт: `--font-family-sans` (Manrope), `--font-family-mono` (JetBrains Mono). Значения
  конфигурации — адреса, теги, ключи — пишутся моноширинным.

### Радиусы

`--radius-xs` 3 · `--radius-sm` 4 · `--radius-md` 6 · `--radius` 8 · `--radius-lg` 10 ·
`--radius-xl` 12 · `--radius-full` (пилюля). Круг — литерал `50%`. Тема Amber сжимает всю шкалу,
поэтому радиус в px в обход токена ломает её острые углы.

### Тени

`--shadow-sm` (кнопка, мелкий блок) · `--shadow` (карточка) · `--shadow-md` (приподнятая
панель) · `--shadow-lg` (выпадающее меню, поповер). Кольцо фокуса и обводка собираются из
цветовых токенов: `0 0 0 3px var(--accent-soft)`. Цвет-литерал в `box-shadow` — нарушение.

### Слои — `--z-*`

| Токен                                        | Значение | Что                                                  |
| -------------------------------------------- | -------- | ---------------------------------------------------- |
| `--z-sticky`                                 | 20       | закреплённая полоса внутри области прокрутки         |
| `--z-dock`                                   | 90       | нижняя панель телефона                               |
| `--z-dropdown` · `--z-header` · `--z-drawer` | 100      | выпадающее меню · мобильная шапка · шторка           |
| `--z-sidebar`                                | 200      | боковое меню                                         |
| `--z-overlay`                                | 1000     | модальное окно, контекстное меню, плавающий прогресс |
| `--z-popover`                                | 1100     | подсказка и поповер поверх окна                      |
| `--z-toast`                                  | 9999     | уведомления                                          |
| `--z-fullscreen`                             | 10000    | полноэкранный редактор                               |

Внутри компонента порядок задаётся литералами от `-1` до `10`. Соседний слой —
`calc(var(--z-drawer) + 1)`.

### Движение

Длительности: `--duration-fast` 140 (наведение, нажатие) · `exit` 160 · `enter` 200 · `move` 260 ·
`slow` 400 · `spin` 800 (оборот спиннера). Кривые: `--ease-standard`, `--ease-out` (появление),
`--ease-in` (уход), `--ease-spring`. Сокращения `--transition-fast` и `--transition-move` уже
содержат кривую. Зеркало для JS — `DURATION` в `src/lib/motion.svelte.ts`. При
`prefers-reduced-motion` анимации гасятся. Сырые длительности ловит `check-motion`.

### Раскладка и плотность

`--sidebar-w`, `--rail-w`, `--dock-h`, `--dock-space`, `--mhdr-h`; плотность — `--card-pad`,
`--input-h`, `--btn-h`, `--table-row-h`, `--grid-gap`. Брейкпоинты: 480 · 768 · 1024
(`min-width` 769 и 2401 — парные); другие значения ловит `check-breakpoints`.

## Компоненты

| Компонент                             | Когда                                           | Главные пропсы                                                                                                                                |
| ------------------------------------- | ----------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------- |
| `Button`                              | любое действие                                  | `variant`: primary · secondary · ghost · danger · danger-soft · warning; `size`: md · sm · xs; `iconOnly` (+`ariaLabel`); `loading`; `testid` |
| `Input`                               | текстовое поле                                  | `id`, `label` или `ariaLabel`, `type`, `size`: md · sm, `error`, `warning`, `hint`, `mono`, `bare` (без обёртки), `bind:value`                |
| `Select`                              | выбор из списка                                 | `options`, `bind:value`, `ariaLabel`                                                                                                          |
| `Checkbox`                            | флажок в форме, применяется кнопкой «Сохранить» | `bind:checked`, `label` или children                                                                                                          |
| `Toggle`                              | мгновенное «вкл/выкл» настройки                 | `bind:checked`, `ariaLabel`, `loading`; это `role="switch"`                                                                                   |
| `SegmentedControl`                    | 2–5 взаимоисключающих режимов                   | `items`, `bind:value`, `ariaLabel`                                                                                                            |
| `Tabs`                                | разделы одной страницы                          | `items`, `value`, `variant`: line · pill                                                                                                      |
| `Badge`                               | метка, тип, состояние                           | `tone`: neutral · emphasis · info · success · warning · danger; `variant`: soft · tag · status; `dot`, `pulse`                                |
| `StatusBadge`                         | состояние сервиса, ядра, файла                  | `variant`: running · stopped · warning · idle · info; `label`, `pulse`; отдаёт `data-status`                                                  |
| `Modal`                               | окно поверх страницы                            | `open`, `title`, `onclose`, `maxWidth`, `testid`                                                                                              |
| `showConfirm()`                       | подтверждение действия                          | заголовок — вопрос о действии, кнопка называет действие                                                                                       |
| `showToast()`                         | итог действия                                   | `type`, `message`, `action`                                                                                                                   |
| `Card` · `Section`                    | группа содержимого                              | `title`, `actions` / `description`, `meta`                                                                                                    |
| `EmptyState`                          | пустой список                                   | `title`, `description`, `ctaText`, `oncta`                                                                                                    |
| `Skeleton` · `Spinner`                | загрузка                                        | `type` / `size`, `label` (тогда `role="status"`), `tone`                                                                                      |
| `Icon` (`lib/components/Icon.svelte`) | значок                                          | `name`, `size`; новая иконка — файл в `lib/components/icons`                                                                                  |

Имена пропсов везде одинаковые: `open` (не `isOpen`, `show`), `onclose` (не `onClose`),
`testid` (не `testId`, `dataTestid`). `onCancel` у форм означает «отменить», а не «закрыть».

### Бейджи

Одна система `.badge`. Тон задаётся `--badge-color`, форма — модификатором:

- **soft** (`.badge`) — метка, фильтр, счётчик;
- **tag** (`.badge--tag`) — тип, версия, канал: моноширинный, прописными;
- **status** (`.badge--status` + `.badge-dot`) — состояние, через `StatusBadge`.

Свои классы `*-badge`, `*-chip`, `*-pill` в компонентах не заводятся. Классы `.chip*`,
`.badge-error`, `.badge-primary`, `.badge-type`, `.status-badge` и `.status-pill` удалены.

### Кнопки и поля

`<Button>` и `<button class="btn …">` выглядят одинаково: у компонента нет своих стилей. Новый
код пишет `<Button>`. Плотные формы конструкторов используют `size="sm"` у `Input`
(`.input.input-sm`, 30px). Класс `.form-input` удалён. Многострочное поле — `textarea.input`.

## Стражи

| Скрипт                                                    | Проверяет                                                                                                                            |
| --------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `check-contrast`                                          | пары текст/фон во всех темах, отсутствие сырых цветов в ключевых селекторах                                                          |
| `check-hardcoded-colors`                                  | hex-цвета в стилях компонентов                                                                                                       |
| `check-css-tokens`                                        | необъявленные и устаревшие токены, запасные значения у токенов `:root`, rgba с цветами токенов                                       |
| `check-font-sizes` · `check-breakpoints` · `check-motion` | размер шрифта, брейкпоинты, длительности — только из шкалы                                                                           |
| `check-inline-styles`                                     | статичный `style="…"` в разметке (храповик)                                                                                          |
| `check-scales`                                            | отступы, радиусы, z-index, тени в обход токенов (храповик)                                                                           |
| `check-badges`                                            | удалённые классы бейджей; свои `*-badge/*-chip/*-pill` (храповик)                                                                    |
| `check-controls`                                          | `.form-input`, переключатель без `role="switch"`; голые `.btn`, поля, флажки, перекраска `.btn/.input`, статичные `<svg>` (храповик) |

**Храповик.** Старый код ещё нарушает правило, поэтому число нарушений по каждому файлу
записано в `scripts/*-baseline.json`. Число может только уменьшаться, у нового файла оно ноль.
Исправили — снизьте базу: `node scripts/check-<имя>.cjs --update`. Команда никогда не поднимает
базу, поэтому новое нарушение так не узаконить.

## Как добавить

- **Токен** — в первый блок `:root`. Если значение зависит от темы — ещё и в блоки тем.
  Цвет текста — пара в `contrast-pairs.json`.
- **Вариант компонента** — модификатор в `global.css` рядом с базовым классом и проп в
  компоненте; строку в таблицу выше.
- **Иконка** — `lib/components/icons/<Имя>.svelte` (24×24, `stroke="currentColor"`,
  `aria-hidden`), ветка в `lib/components/Icon.svelte`.
