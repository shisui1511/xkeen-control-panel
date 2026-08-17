import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { trafficStream, formatTrafficSpeed, type TrafficState } from '../src/lib/trafficStream';

class MockWebSocket {
  static instances: MockWebSocket[] = [];
  public readyState: number = 0; // CONNECTING
  public url: string;
  public onopen: (() => void) | null = null;
  public onmessage: ((event: { data: string }) => void) | null = null;
  public onclose: (() => void) | null = null;
  public onerror: (() => void) | null = null;

  constructor(url: string) {
    this.url = url;
    MockWebSocket.instances.push(this);
    setTimeout(() => {
      this.readyState = 1; // OPEN
      if (this.onopen) this.onopen();
    }, 10);
  }

  close() {
    this.readyState = 3; // CLOSED
    if (this.onclose) this.onclose();
  }
}

describe('trafficStream', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    MockWebSocket.instances = [];
    (global as any).WebSocket = MockWebSocket;
    (global as any).window = {
      location: {
        protocol: 'http:',
        host: 'localhost:8080'
      }
    };
    trafficStream.resetForTesting();
  });

  afterEach(() => {
    trafficStream.resetForTesting();
    vi.useRealTimers();
  });

  describe('formatTrafficSpeed', () => {
    it('handles 0 and negative/invalid numbers', () => {
      expect(formatTrafficSpeed(0)).toBe('0 B/s');
      expect(formatTrafficSpeed(-100)).toBe('0 B/s');
      expect(formatTrafficSpeed(NaN)).toBe('0 B/s');
    });

    it('formats bytes correctly', () => {
      expect(formatTrafficSpeed(0.5)).toBe('0.5 B/s');
      expect(formatTrafficSpeed(500)).toBe('500 B/s');
    });

    it('formats kilobytes correctly', () => {
      expect(formatTrafficSpeed(1024)).toBe('1 KB/s');
      expect(formatTrafficSpeed(150 * 1024)).toBe('150 KB/s');
    });

    it('formats megabytes and gigabytes correctly', () => {
      expect(formatTrafficSpeed(2.4 * 1024 * 1024)).toBe('2.4 MB/s');
      expect(formatTrafficSpeed(1.5 * 1024 * 1024 * 1024)).toBe('1.5 GB/s');
    });
  });

  describe('subscription and connection lifecycle', () => {
    it('delivers initial state to new subscriber', () => {
      const states: TrafficState[] = [];
      const unsub = trafficStream.subscribe((st) => states.push(st));

      expect(states.length).toBe(1);
      expect(states[0].connected).toBe(false);
      expect(states[0].up).toBe(0);
      expect(states[0].down).toBe(0);

      unsub();
    });

    it('initiates websocket connection on first subscriber', () => {
      const unsub = trafficStream.subscribe(() => {});
      expect(MockWebSocket.instances.length).toBe(1);
      expect(MockWebSocket.instances[0].url).toBe('ws://localhost:8080/api/traffic/ws');

      vi.advanceTimersByTime(20);
      expect(trafficStream.getState().connected).toBe(true);

      unsub();
    });

    it('broadcasts parsed message to all active subscribers', () => {
      const sub1States: TrafficState[] = [];
      const sub2States: TrafficState[] = [];

      const unsub1 = trafficStream.subscribe((s) => sub1States.push(s));
      const unsub2 = trafficStream.subscribe((s) => sub2States.push(s));

      vi.advanceTimersByTime(20);

      const wsInstance = MockWebSocket.instances[0];
      expect(wsInstance).toBeDefined();

      wsInstance.onmessage?.({
        data: JSON.stringify({
          up: 1048576,
          down: 2097152,
          total_up: 5000000,
          total_down: 10000000,
          connections: 42,
          tcp_connections: 30,
          udp_connections: 12
        })
      });

      const latest1 = sub1States[sub1States.length - 1];
      const latest2 = sub2States[sub2States.length - 1];

      expect(latest1.up).toBe(1048576);
      expect(latest1.down).toBe(2097152);
      expect(latest1.connections).toBe(42);
      expect(latest2.totalUp).toBe(5000000);
      expect(latest2.totalDown).toBe(10000000);

      unsub1();
      unsub2();
    });

    it('gracefully disconnects after grace period when all subscribers unsubscribe', () => {
      const unsub = trafficStream.subscribe(() => {});
      vi.advanceTimersByTime(20);

      const wsInstance = MockWebSocket.instances[0];
      expect(wsInstance.readyState).toBe(1); // OPEN

      unsub();
      // Right after unsub, should still be alive during grace period
      expect(wsInstance.readyState).toBe(1);

      // Advance by grace period (5000ms)
      vi.advanceTimersByTime(5100);
      expect(wsInstance.readyState).toBe(3); // CLOSED
    });

    it('caps session traffic delta to prevent massive spikes after pause or sleep', () => {
      const unsub = trafficStream.subscribe(() => {});
      vi.advanceTimersByTime(20);

      const wsInstance = MockWebSocket.instances[0];
      expect(wsInstance).toBeDefined();

      // First tick
      wsInstance.onmessage?.({
        data: JSON.stringify({ up: 1000, down: 2000 })
      });
      expect(trafficStream.getState().sessionUp).toBe(0);
      expect(trafficStream.getState().sessionDown).toBe(0);

      // Advance time by 60 seconds (simulating tab suspension)
      vi.advanceTimersByTime(60000);

      // Next tick with 1000 B/s up, 2000 B/s down
      wsInstance.onmessage?.({
        data: JSON.stringify({ up: 1000, down: 2000 })
      });

      // Delta should be capped at 2.5 seconds (not 60 seconds)
      expect(trafficStream.getState().sessionUp).toBe(1000 * 2.5);
      expect(trafficStream.getState().sessionDown).toBe(2000 * 2.5);

      unsub();
    });
  });
});
