// The ticketed WebSocket transport must report a WebSocket that cannot even be constructed (mixed content
// ws:// from an https page, blocked port, bad URL) instead of leaving the client "connecting" forever.
import { afterEach, describe, expect, it } from 'vitest';
import { createJoinTransportFactory } from '@/game/transport';

const g = globalThis as Record<string, unknown>;
const savedWs = g['WebSocket'];
afterEach(() => {
  g['WebSocket'] = savedWs;
});

describe('createJoinTransportFactory', () => {
  it('closes with a reason when new WebSocket() throws', async () => {
    g['WebSocket'] = class {
      constructor() {
        throw new DOMException("An insecure WebSocket connection may not be initiated", 'SecurityError');
      }
    };
    const t = createJoinTransportFactory('g1', { ticket: 't', wsUrl: 'ws://insecure.example/ws/g1' }, 'https://q2.example')('game-g1');
    const reason = await new Promise<string>((resolve, reject) => {
      t.onClose = resolve;
      setTimeout(() => reject(new Error('transport never reported the failure')), 500);
    });
    expect(reason).toMatch(/insecure|websocket/i);
  });
});
