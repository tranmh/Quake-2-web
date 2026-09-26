import { existsSync, readFileSync, rmSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const file = resolve(dirname(fileURLToPath(import.meta.url)), '../.e2e/e2e-pids.json');

export default async function globalTeardown(): Promise<void> {
  if (!existsSync(file)) return;
  const pids = JSON.parse(readFileSync(file, 'utf8')) as number[];
  for (const pid of pids) {
    try {
      process.kill(-pid, 'SIGTERM'); // detached: kill the whole process group
    } catch {
      try {
        process.kill(pid, 'SIGTERM');
      } catch {
        // already gone
      }
    }
  }
  rmSync(file, { force: true });
}
