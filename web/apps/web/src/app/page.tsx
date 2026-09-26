import Link from 'next/link';
import { Q2Pic, Q2Text } from '@/components/Q2Text';
import styles from './home.module.css';

export default function Home() {
  return (
    <main className="page">
      <section className={styles.hero}>
        <Q2Pic name="m_main_logo" fallback="QUAKE II" scale={2} />
        <div>
          <h1 className="visually-hidden">Quake II Web</h1>
          <Q2Text text="QUAKE II - IN YOUR BROWSER" scale={3} alt />
          <p className={styles.lead}>
            A faithful port of id Software&apos;s Quake II (3.19 source release): the original client runs in the
            browser with WebGL 2 and Web Audio, the game logic runs on a Go server, and you connect over WebSockets.
          </p>
          <div className="row">
            <Link className="btn primary" href="/servers">
              Play the demo
            </Link>
            <Link className="btn" href="/register">
              Create an account
            </Link>
            <Link className="btn" href="/paks">
              Upload your paks
            </Link>
          </div>
        </div>
      </section>

      <div className="grid2">
        <section className="panel">
          <h2>Demo available</h2>
          <p>
            The freely distributable Quake II demo (<span className="mono">baseq2/pak0.pak</span>, map{' '}
            <span className="mono">demo1</span> &ldquo;Outer Base&rdquo; and friends) is installed on this server. Register,
            open the server browser and start a single-player game.
          </p>
        </section>
        <section className="panel">
          <h2>Game data not included</h2>
          <p>
            Quake II is a trademark of id Software / ZeniMax. This project contains no commercial game data. To play
            the full game, upload the <span className="mono">pak*.pak</span> files from your own legally obtained copy;
            they are stored privately and only served to your account.
          </p>
        </section>
        <section className="panel">
          <h2>Controls</h2>
          <p>
            Click the game to capture the mouse. <span className="mono">W A S D</span> / arrows move,{' '}
            <span className="mono">mouse1</span> fires, <span className="mono">~</span> opens the console,{' '}
            <span className="mono">Esc</span> releases the mouse and opens the menu. Rebind everything under Settings.
          </p>
        </section>
        <section className="panel">
          <h2>Open source</h2>
          <p>
            Based on the Quake II GPL source release. Rendering, prediction and the network protocol are ported line by
            line; see <span className="mono">docs/PARITY.md</span> in the repository.
          </p>
        </section>
      </div>
    </main>
  );
}
