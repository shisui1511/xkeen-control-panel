<script lang="ts">
  let {
    type = 'text-line',
    width,
    height,
    style = ''
  } = $props<{
    type?: 'text-line' | 'card' | 'circle' | 'rect';
    width?: string;
    height?: string;
    style?: string;
  }>();

  let styleString = $derived.by(() => {
    let s: string[] = [];
    if (width) s.push(`width: ${width}`);
    if (height) s.push(`height: ${height}`);
    if (style) s.push(style);
    return s.join('; ');
  });
</script>

<div class="skeleton skeleton-{type}" style={styleString} aria-hidden="true"></div>

<style>
  .skeleton {
    background:
      linear-gradient(
        90deg,
        rgba(255, 255, 255, 0) 0%,
        rgba(41, 194, 240, 0.08) 50%,
        rgba(255, 255, 255, 0) 100%
      ),
      var(--bg-elevated);
    background-size:
      200px 100%,
      auto;
    background-repeat: no-repeat;
    animation: shimmer 1.4s infinite linear;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border-light);
  }
  .skeleton-text-line {
    height: 14px;
    width: 100%;
    margin-bottom: 8px;
  }
  .skeleton-card {
    height: 100px;
    width: 100%;
    border-radius: var(--radius-md);
  }
  .skeleton-circle {
    width: 40px;
    height: 40px;
    border-radius: 50%;
  }
  .skeleton-rect {
    width: 100%;
    height: 50px;
  }

  @keyframes shimmer {
    0% {
      background-position:
        -200px 0,
        0 0;
    }
    100% {
      background-position:
        calc(200px + 100%) 0,
        0 0;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .skeleton {
      animation: none !important;
      background-image: none !important;
      opacity: 0.7;
    }
  }
</style>
