'use client';
import Link from 'next/link';
import { useRouter, useSearchParams } from 'next/navigation';
import { useState, type FormEvent } from 'react';
import { api, errorMessage } from '@/lib/api';
import { useSession } from '@/lib/session';
import { Q2Text } from './Q2Text';

export function AuthForm({ mode }: { mode: 'login' | 'register' }) {
  const router = useRouter();
  const params = useSearchParams();
  const setUser = useSession((s) => s.setUser);
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const next = params.get('next') || '/servers';

  const submit = async (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const u =
        mode === 'login' ? await api.login(email, password) : await api.register(email, password, displayName || email.split('@')[0]!);
      setUser(u);
      router.push(next.startsWith('/') ? next : '/servers');
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="page" style={{ maxWidth: 440 }}>
      <form className="panel" onSubmit={submit} style={{ display: 'grid', gap: 14 }}>
        <Q2Text text={mode === 'login' ? 'LOG IN' : 'NEW ACCOUNT'} scale={3} alt />
        <label className="field">
          Email
          <input type="email" name="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
        </label>
        {mode === 'register' && (
          <label className="field">
            Player name
            <input
              type="text"
              name="displayName"
              maxLength={32}
              placeholder="unnamed"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
            />
          </label>
        )}
        <label className="field">
          Password
          <input
            type="password"
            name="password"
            autoComplete={mode === 'login' ? 'current-password' : 'new-password'}
            required
            minLength={mode === 'register' ? 8 : undefined}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </label>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        <button className="btn primary" type="submit" disabled={busy}>
          {mode === 'login' ? 'Log in' : 'Register'}
        </button>
        <p className="muted">
          {mode === 'login' ? (
            <>
              No account? <Link href="/register">Register</Link>
            </>
          ) : (
            <>
              Already registered? <Link href="/login">Log in</Link>
            </>
          )}
        </p>
      </form>
    </main>
  );
}
