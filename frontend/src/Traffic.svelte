<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { t } from './i18n'

  interface TrafficPoint {
    up: number
    down: number
    time: number
  }

  let canvas: HTMLCanvasElement
  let ctx: CanvasRenderingContext2D
  let trafficData: TrafficPoint[] = []
  let maxPoints = 60
  let es: EventSource | null = null
  let connected = false
  let totalUp = 0
  let totalDown = 0
  let sessionUp = 0
  let sessionDown = 0

  function formatSpeed(bytesPerSecond: number): string {
    if (bytesPerSecond === 0) return '0 B/s'
    const k = 1024
    const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s']
    const i = Math.floor(Math.log(bytesPerSecond) / Math.log(k))
    return parseFloat((bytesPerSecond / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  }

  function drawChart() {
    if (!ctx || !canvas) return
    
    const width = canvas.width
    const height = canvas.height
    const padding = 40
    const chartWidth = width - padding * 2
    const chartHeight = height - padding * 2

    // Clear
    ctx.clearRect(0, 0, width, height)

    // Background
    ctx.fillStyle = getComputedStyle(canvas).getPropertyValue('--card-bg') || '#161B22'
    ctx.fillRect(0, 0, width, height)

    if (trafficData.length < 2) return

    // Find max for scaling
    let maxValue = 0
    for (const point of trafficData) {
      maxValue = Math.max(maxValue, point.up, point.down)
    }
    if (maxValue === 0) maxValue = 1

    // Grid lines
    ctx.strokeStyle = 'rgba(128,128,128,0.1)'
    ctx.lineWidth = 1
    for (let i = 0; i <= 4; i++) {
      const y = padding + (chartHeight / 4) * i
      ctx.beginPath()
      ctx.moveTo(padding, y)
      ctx.lineTo(width - padding, y)
      ctx.stroke()
      
      // Y-axis labels
      ctx.fillStyle = 'var(--text-secondary)'
      ctx.font = '10px monospace'
      ctx.textAlign = 'right'
      const value = maxValue * (1 - i / 4)
      ctx.fillText(formatSpeed(value), padding - 5, y + 3)
    }

    // Draw upload line
    ctx.strokeStyle = '#3fb950'
    ctx.lineWidth = 2
    ctx.beginPath()
    for (let i = 0; i < trafficData.length; i++) {
      const x = padding + (chartWidth / (maxPoints - 1)) * i
      const y = padding + chartHeight - (trafficData[i].up / maxValue) * chartHeight
      if (i === 0) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    }
    ctx.stroke()

    // Draw download line
    ctx.strokeStyle = '#58a6ff'
    ctx.lineWidth = 2
    ctx.beginPath()
    for (let i = 0; i < trafficData.length; i++) {
      const x = padding + (chartWidth / (maxPoints - 1)) * i
      const y = padding + chartHeight - (trafficData[i].down / maxValue) * chartHeight
      if (i === 0) ctx.moveTo(x, y)
      else ctx.lineTo(x, y)
    }
    ctx.stroke()

    // Legend
    ctx.textAlign = 'left'
    ctx.font = '12px sans-serif'
    
    ctx.fillStyle = '#58a6ff'
    ctx.fillRect(width - 120, 10, 12, 12)
    ctx.fillStyle = 'var(--text)'
    ctx.fillText($t('traffic.download'), width - 105, 21)
    
    ctx.fillStyle = '#3fb950'
    ctx.fillRect(width - 120, 28, 12, 12)
    ctx.fillStyle = 'var(--text)'
    ctx.fillText($t('traffic.upload'), width - 105, 39)
  }

  function connect() {
    const protocol = window.location.protocol === 'https:' ? 'https:' : 'http:'
    const url = `${protocol}//${window.location.host}/api/mihomo/proxy/traffic`
    
    es = new EventSource(url)
    
    es.onopen = () => {
      connected = true
    }
    
    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        trafficData.push({
          up: data.up || 0,
          down: data.down || 0,
          time: Date.now()
        })
        
        if (trafficData.length > maxPoints) {
          trafficData = trafficData.slice(-maxPoints)
        }
        
        totalUp = data.up || 0
        totalDown = data.down || 0
        sessionUp += totalUp
        sessionDown += totalDown
        
        drawChart()
      } catch (e) {
        // Ignore parse errors
      }
    }
    
    es.onerror = () => {
      connected = false
    }
  }

  function disconnect() {
    if (es) {
      es.close()
      es = null
    }
    connected = false
  }

  function resizeCanvas() {
    if (!canvas) return
    const dpr = window.devicePixelRatio || 1
    const rect = canvas.getBoundingClientRect()
    canvas.width = rect.width * dpr
    canvas.height = rect.height * dpr
    ctx.scale(dpr, dpr)
    drawChart()
  }

  onMount(() => {
    ctx = canvas.getContext('2d')!
    resizeCanvas()
    connect()
    window.addEventListener('resize', resizeCanvas)
  })

  onDestroy(() => {
    disconnect()
    window.removeEventListener('resize', resizeCanvas)
  })
</script>

<div class="traffic-page">
  <div class="container">
  <h1>{$t('traffic.title')}</h1>
  <p class="text-secondary mb-3">{$t('traffic.realtime')}</p>

    <div class="stats-grid mb-2">
      <div class="card stat-card">
        <div class="stat-label">{$t('traffic.upload')}</div>
        <div class="stat-value" style="color: #3fb950">{formatSpeed(totalUp)}</div>
        <div class="stat-session">Σ {formatSpeed(sessionUp).replace('/s', '')}</div>
      </div>
      <div class="card stat-card">
        <div class="stat-label">{$t('traffic.download')}</div>
        <div class="stat-value" style="color: #58a6ff">{formatSpeed(totalDown)}</div>
        <div class="stat-session">Σ {formatSpeed(sessionDown).replace('/s', '')}</div>
      </div>
      <div class="card stat-card">
        <div class="stat-label">{$t('app.status')}</div>
        <div class="stat-value" style="color: {connected ? 'var(--success)' : 'var(--danger)'}">
          {connected ? '● Live' : '○ Offline'}
        </div>
      </div>
    </div>

    <div class="card chart-card">
      <canvas bind:this={canvas} class="traffic-canvas"></canvas>
    </div>
  </div>
</div>

<style>
  .traffic-page {
    background: var(--bg);
    min-height: 100vh;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(150px, 1fr));
    gap: 1rem;
  }

  .stat-card {
    padding: 1.5rem;
    text-align: center;
  }

  .stat-label {
    font-size: 0.875rem;
    color: var(--text-secondary);
    margin-bottom: 0.5rem;
  }

  .stat-value {
    font-size: 1.5rem;
    font-weight: 600;
    font-family: monospace;
  }

  .stat-session {
    font-size: 0.75rem;
    color: var(--text-secondary);
    margin-top: 0.25rem;
  }

  .chart-card {
    padding: 1rem;
    height: 400px;
  }

  .traffic-canvas {
    width: 100%;
    height: 100%;
    border-radius: 4px;
  }
</style>
