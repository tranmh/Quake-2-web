// Small UI state for the game page. The engine lives outside React (GameSession); it pushes coarse
// state changes here and React components subscribe with selectors. Nothing per-frame goes through it.
import { create } from 'zustand';

export type GamePhase =
  | 'idle'
  | 'index' // fetching the asset index
  | 'engine' // creating renderer + client engine
  | 'joining' // POST /games/{id}/join
  | 'connecting' // netchan handshake
  | 'loading' // CL_PrepRefresh / precache
  | 'active'
  | 'disconnected'
  | 'error';

export type Overlay = null | 'main' | 'options' | 'video' | 'saveload';

export interface LoadingState {
  mapname?: string;
  stage?: string;
  progress?: number;
}

export interface GameUiState {
  phase: GamePhase;
  /** connstate_t (0 uninitialized … 4 active) */
  connState: number;
  /** keydest_t (0 game, 1 console, 2 message, 3 menu) */
  keyDest: number;
  loading: LoadingState | null;
  levelName: string;
  mapName: string;
  pointerLocked: boolean;
  overlay: Overlay;
  /** fatal problem: the session cannot continue */
  fatal: string | null;
  /** last Com_Error(ERR_DROP) text or other non-fatal notice */
  notice: string | null;
  soundEnabled: boolean;
  downloadedBytes: number;
  set(p: Partial<GameUiState>): void;
  reset(): void;
}

const initial = {
  phase: 'idle' as GamePhase,
  connState: 0,
  keyDest: 0,
  loading: null,
  levelName: '',
  mapName: '',
  pointerLocked: false,
  overlay: null as Overlay,
  fatal: null,
  notice: null,
  soundEnabled: false,
  downloadedBytes: 0,
};

export const useGameStore = create<GameUiState>((set) => ({
  ...initial,
  set: (p) => set(p),
  reset: () => set(initial),
}));
