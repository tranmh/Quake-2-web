'use client';
import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { useEffect, type ReactNode } from 'react';
import { useSession } from '@/lib/session';

/** Renders children only for logged-in users; otherwise a login prompt. */
export function RequireLogin({ children }: { children: ReactNode }) {
  const { user, loaded, refresh } = useSession();
  const path = usePathname();
  useEffect(() => {
    if (!loaded) void refresh();
  }, [loaded, refresh]);
  if (!loaded) return <p className="muted">Loading…</p>;
  if (!user)
    return (
      <div className="panel">
        <p>You need an account for this page.</p>
        <div className="row">
          <Link className="btn primary" href={`/login?next=${encodeURIComponent(path ?? '/')}`}>
            Log in
          </Link>
          <Link className="btn" href={`/register?next=${encodeURIComponent(path ?? '/')}`}>
            Register
          </Link>
        </div>
      </div>
    );
  return <>{children}</>;
}
