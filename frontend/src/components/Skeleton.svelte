<script lang="ts">
  let {
    type = 'text-line',
    width,
    height
  } = $props<{
    type?: 'text-line' | 'card' | 'circle' | 'rect';
    width?: string;
    height?: string;
  }>();

  // Compute styles dynamically if custom width/height is passed
  let styleString = $derived.by(() => {
    let styles = [];
    if (width) styles.push(`width: ${width}`);
    if (height) styles.push(`height: ${height}`);
    return styles.join('; ');
  });
</script>

<div class="skeleton skeleton-{type}" style={styleString} aria-hidden="true"></div>

<style>
  .skeleton {
    background-color: var(--color-border-subtle);
    background-image: linear-gradient(
      90deg,
      var(--color-border-subtle) 0px,
      var(--border-light) 50%,
      var(--color-border-subtle) 100%
    );
    background-size: 200px 100%;
    background-repeat: no-repeat;
    animation: shimmer 1.5s infinite linear;
    border-radius: var(--radius-sm);
  }

  .skeleton-text-line {
    height: 1rem;
    width: 100%;
    margin-bottom: var(--spacing-2);
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
      background-position: -200px 0;
    }
    100% {
      background-position: calc(200px + 100%) 0;
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
