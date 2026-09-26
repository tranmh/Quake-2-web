// Logged-in account state shared by the shell pages.
//
// The session cookie is HttpOnly, so the page cannot see it. A localStorage hint remembers that this
// browser logged in; without it anonymous page views skip GET /auth/me (which would answer 401 and
// log a resource error in the console on every page).
import { create } from 'zustand';
import { api, type User } from './api';
import { clearLocalConfig } from './config';

const HINT = 'q2.session';

function hint(): boolean {
  try {
    return localStorage.getItem(HINT) === '1';
  } catch {
    return true; // storage unavailable: always ask the server
  }
}

function setHint(on: boolean): void {
  try {
    if (on) localStorage.setItem(HINT, '1');
    else localStorage.removeItem(HINT);
  } catch {
    // ignore
  }
}

interface SessionState {
  user: User | null;
  loaded: boolean;
  /** GET /auth/me (skipped when this browser never logged in) */
  refresh(): Promise<User | null>;
  setUser(u: User | null): void;
  logout(): Promise<void>;
}

let inflight: Promise<User | null> | null = null;

export const useSession = create<SessionState>((set) => ({
  user: null,
  loaded: false,
  refresh() {
    if (!hint()) {
      set({ user: null, loaded: true });
      return Promise.resolve(null);
    }
    inflight ??= api
      .me()
      .then(
        (u) => {
          setHint(!!u);
          set({ user: u, loaded: true });
          return u;
        },
        () => {
          set({ user: null, loaded: true });
          return null;
        },
      )
      .finally(() => {
        inflight = null;
      });
    return inflight;
  },
  setUser: (u) => {
    setHint(!!u);
    set({ user: u, loaded: true });
  },
  async logout() {
    try {
      await api.logout();
    } finally {
      setHint(false);
      // the local config.cfg mirror belongs to this account (shared computers)
      clearLocalConfig();
      set({ user: null, loaded: true });
    }
  },
}));
