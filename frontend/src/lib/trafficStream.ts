import { writable, type Readable } from 'svelte/store';

export interface TrafficDataPoint {
  up: number;
  down: number;
  time: number;
}

export interface TrafficPeaks {
  peak_hour_up: number;
  peak_hour_down: number;
  peak_day_up: number;
  peak_day_down: number;
  peak_week_up: number;
  peak_week_down: number;
  hour_start: number;
  day_start: number;
  week_start: number;
}

export interface TrafficState {
  up: number;
  down: number;
  connected: boolean;
  totalUp: number;
  totalDown: number;
  sessionUp: number;
  sessionDown: number;
  connections: number;
  tcp_connections: number;
  udp_connections: number;
  peaks?: TrafficPeaks;
}

export type TrafficSubscriber = (state: TrafficState) => void;

const initialTrafficState: TrafficState = {
  up: 0,
  down: 0,
  connected: false,
  totalUp: 0,
  totalDown: 0,
  sessionUp: 0,
  sessionDown: 0,
  connections: 0,
  tcp_connections: 0,
  udp_connections: 0
};

export function formatTrafficSpeed(bytesPerSec: number): string {
  if (!bytesPerSec || bytesPerSec <= 0 || isNaN(bytesPerSec)) return '0 B/s';
  const k = 1024;
  const sizes = ['B/s', 'KB/s', 'MB/s', 'GB/s', 'TB/s'];
  const i = Math.floor(Math.log(bytesPerSec) / Math.log(k));
  const clampedIdx = Math.min(i, sizes.length - 1);
  const val = parseFloat((bytesPerSec / Math.pow(k, clampedIdx)).toFixed(1));
  return `${val} ${sizes[clampedIdx]}`;
}

export class TrafficStreamManager {
  private ws: WebSocket | null = null;
  private subscribers = new Set<TrafficSubscriber>();
  private reconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  private reconnectDelay = 1000;
  private readonly MAX_RECONNECT_DELAY = 16000;
  private disconnectTimeout: ReturnType<typeof setTimeout> | null = null;
  private readonly DISCONNECT_GRACE_MS = 5000;

  private state: TrafficState = { ...initialTrafficState };
  private lastTickTime = 0;

  private stateStore = writable<TrafficState>(this.state);

  public readonly store: Readable<TrafficState> = {
    subscribe: this.stateStore.subscribe
  };

  public getState(): TrafficState {
    return { ...this.state };
  }

  public subscribe(fn: TrafficSubscriber): () => void {
    this.subscribers.add(fn);
    // Cancel pending graceful disconnection if a new subscriber joined
    if (this.disconnectTimeout) {
      clearTimeout(this.disconnectTimeout);
      this.disconnectTimeout = null;
    }

    // Immediately deliver current state to new subscriber
    fn({ ...this.state });

    // Connect if this is the first subscriber
    if (this.subscribers.size === 1 && (!this.ws || this.ws.readyState === WebSocket.CLOSED)) {
      this.connect();
    }

    return () => {
      this.subscribers.delete(fn);
      if (this.subscribers.size === 0) {
        this.scheduleGracefulDisconnect();
      }
    };
  }

  private connect(): void {
    if (typeof window === 'undefined') return;

    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }

    if (
      this.ws &&
      (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)
    ) {
      return;
    }

    try {
      const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
      const host = window.location.host || 'localhost';
      const url = `${protocol}//${host}/api/traffic/ws`;

      this.ws = new WebSocket(url);

      this.ws.onopen = () => {
        this.reconnectDelay = 1000;
        this.updateState({ connected: true });
      };

      this.ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data);
          const upSpeed = typeof data.up === 'number' ? data.up : 0;
          const downSpeed = typeof data.down === 'number' ? data.down : 0;
          const now = Date.now();

          let sessionUp = this.state.sessionUp;
          let sessionDown = this.state.sessionDown;

          if (this.lastTickTime > 0) {
            const elapsedSec = (now - this.lastTickTime) / 1000;
            sessionUp += upSpeed * elapsedSec;
            sessionDown += downSpeed * elapsedSec;
          }
          this.lastTickTime = now;

          this.updateState({
            up: upSpeed,
            down: downSpeed,
            connected: true,
            totalUp: typeof data.total_up === 'number' ? data.total_up : this.state.totalUp,
            totalDown: typeof data.total_down === 'number' ? data.total_down : this.state.totalDown,
            sessionUp,
            sessionDown,
            connections: data.connections || 0,
            tcp_connections: data.tcp_connections || 0,
            udp_connections: data.udp_connections || 0,
            peaks: data.peaks || this.state.peaks
          });
        } catch {
          // Ignore invalid packet
        }
      };

      this.ws.onclose = () => {
        this.updateState({ connected: false });
        if (this.subscribers.size > 0) {
          this.scheduleReconnect();
        }
      };

      this.ws.onerror = () => {
        this.updateState({ connected: false });
      };
    } catch {
      this.updateState({ connected: false });
      if (this.subscribers.size > 0) {
        this.scheduleReconnect();
      }
    }
  }

  private scheduleReconnect(): void {
    if (this.reconnectTimeout || this.subscribers.size === 0) return;

    this.reconnectTimeout = setTimeout(() => {
      this.reconnectTimeout = null;
      this.reconnectDelay = Math.min(this.reconnectDelay * 2, this.MAX_RECONNECT_DELAY);
      this.connect();
    }, this.reconnectDelay);
  }

  private scheduleGracefulDisconnect(): void {
    if (this.disconnectTimeout) return;

    this.disconnectTimeout = setTimeout(() => {
      this.disconnectTimeout = null;
      if (this.subscribers.size === 0) {
        this.disconnect();
      }
    }, this.DISCONNECT_GRACE_MS);
  }

  public disconnect(): void {
    if (this.reconnectTimeout) {
      clearTimeout(this.reconnectTimeout);
      this.reconnectTimeout = null;
    }
    if (this.disconnectTimeout) {
      clearTimeout(this.disconnectTimeout);
      this.disconnectTimeout = null;
    }
    if (this.ws) {
      this.ws.onopen = null;
      this.ws.onmessage = null;
      this.ws.onclose = null;
      this.ws.onerror = null;
      try {
        this.ws.close();
      } catch {
        // ignore
      }
      this.ws = null;
    }
    this.updateState({ connected: false, up: 0, down: 0 });
    this.lastTickTime = 0;
  }

  private updateState(partial: Partial<TrafficState>): void {
    this.state = { ...this.state, ...partial };
    this.stateStore.set(this.state);
    for (const sub of this.subscribers) {
      try {
        sub(this.state);
      } catch {
        // subscriber error handler
      }
    }
  }

  public resetForTesting(): void {
    this.disconnect();
    this.subscribers.clear();
    this.state = { ...initialTrafficState };
    this.stateStore.set(this.state);
    this.reconnectDelay = 1000;
  }
}

export const trafficStream = new TrafficStreamManager();
export const trafficSpeedStore = trafficStream.store;
