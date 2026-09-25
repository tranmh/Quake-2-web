// glibc rand()/srand() (random_r.c, TYPE_3: x**31 + x**3 + 1, degree 31, separation 3).
// ADR-0002: reproduced exactly so rand() call sequences match the C oracle.

export const RAND_MAX = 2147483647;

export class QRand {
  private readonly state = new Int32Array(31);
  private f = 3;
  private r = 0;

  constructor(seed = 1) {
    this.srand(seed);
  }

  // C: glibc __srandom_r
  srand(seed: number): void {
    seed = seed >>> 0;
    if (seed === 0) seed = 1;
    const state = this.state;
    state[0] = seed | 0;
    let word = seed | 0;
    for (let i = 1; i < 31; i++) {
      const hi = Math.trunc(word / 127773);
      const lo = word % 127773;
      word = (16807 * lo - 2836 * hi) | 0;
      if (word < 0) word = (word + 2147483647) | 0;
      state[i] = word;
    }
    this.f = 3;
    this.r = 0;
    for (let k = 0; k < 310; k++) this.rand();
  }

  // C: glibc __random_r
  rand(): number {
    const state = this.state;
    const val = (state[this.f]! + state[this.r]!) | 0;
    state[this.f] = val;
    const result = (val >>> 0) >>> 1;
    if (++this.f >= 31) {
      this.f = 0;
      ++this.r;
    } else if (++this.r >= 31) {
      this.r = 0;
    }
    return result;
  }

  /** Snapshot of the generator state (for save games / determinism checks). */
  save(): { state: number[]; f: number; r: number } {
    return { state: Array.from(this.state), f: this.f, r: this.r };
  }

  restore(s: { state: number[]; f: number; r: number }): void {
    this.state.set(s.state);
    this.f = s.f;
    this.r = s.r;
  }
}
