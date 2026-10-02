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
  | 'ended' // watch / replay: the bot's run (or the replay) is over
  | 'error';

export type Overlay = null | 'main' | 'options' | 'video' | 'saveload';

export interface LoadingState {
  mapname?: string;
  stage?: string;
  progress?: number;
}

/** Replay of a recorded bot run (GameSession source replay). */
export interface ReplayUiState {
  /** the episode's level attempts in order (one .dm2 each) */
  levels: { name: string; map: string; attempt: number }[];
  /** index into levels (-1 before the first) */
  current: number;
  /** timescale */
  speed: number;
  paused: boolean;
  /** the last level finished playing */
  finished: boolean;
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
  /** watch: the bot's final status once its run ended */
  runStatus: string | null;
  replay: ReplayUiState | null;
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
  runStatus: null,
  replay: null as ReplayUiState | null,
};

export const useGameStore = create<GameUiState>((set) => ({
  ...initial,
  set: (p) => set(p),
  reset: () => set(initial),
}));
