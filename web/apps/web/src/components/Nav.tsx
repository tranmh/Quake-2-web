'use client';
import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import { useEffect } from 'react';
import { useSession } from '@/lib/session';
import { Q2Text } from './Q2Text';
import styles from './Nav.module.css';

const LINKS = [
  { href: '/servers', label: 'Servers' },
  { href: '/bots', label: 'Bots' },
  { href: '/saves', label: 'Saves' },
  { href: '/paks', label: 'Paks' },
  { href: '/settings', label: 'Settings' },
];

export function Nav() {
  const path = usePathname();
  const router = useRouter();
  const { user, loaded, refresh, logout } = useSession();
  useEffect(() => {
    if (!loaded) void refresh();
  }, [loaded, refresh]);
  if (path?.startsWith('/play/') || path?.startsWith('/watch/')) return null; // the game pages are full-window
  return (
    <header className={styles.nav}>
      <Link href="/" className={styles.brand} aria-label="Quake II Web home">
        <Q2Text text="QUAKE II" scale={3} alt />
        <span className={styles.sub}>web</span>
      </Link>
      <nav className={styles.links}>
        {LINKS.map((l) => (
          <Link key={l.href} href={l.href} className={path?.startsWith(l.href) ? styles.active : undefined}>
            {l.label}
          </Link>
        ))}
      </nav>
      <div className={styles.user}>
        {user ? (
          <>
            <span className="muted mono">{user.displayName}</span>
            <button
              className="btn small"
              onClick={async () => {
                await logout();
                router.push('/');
              }}
            >
              Log out
            </button>
          </>
        ) : loaded ? (
          <>
            <Link className="btn small" href="/login">
              Log in
            </Link>
            <Link className="btn small primary" href="/register">
              Register
            </Link>
          </>
        ) : null}
      </div>
    </header>
  );
}
