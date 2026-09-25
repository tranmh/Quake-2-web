// Package crand is an exact port of glibc rand()/srand() (random_r.c, TYPE_3
// additive feedback generator, degree 31, separation 3) plus the Quake 2
// helpers built on it (common.c frand/crand, g_local.h random/crandom).
// Each game instance owns its own Rand; there is no global state.
package crand

const (
	randDeg = 31 // glibc DEG_3
	randSep = 3  // glibc SEP_3
)

// Rand is one glibc TYPE_3 generator state (struct random_data).
type Rand struct {
	state [randDeg]int32
	fptr  int
	rptr  int
}

// New returns a generator seeded like a fresh process (srand(1)).
func New(seed uint32) *Rand {
	r := &Rand{}
	r.Srand(seed)
	return r
}

// Srand seeds the generator exactly like glibc __srandom_r.
func (r *Rand) Srand(seed uint32) {
	// We must make sure the seed is not 0.  Take arbitrarily 1 in this case.
	if seed == 0 {
		seed = 1
	}
	r.state[0] = int32(seed)
	word := int32(seed)
	for i := 1; i < randDeg; i++ {
		// Schrage's method for 16807 * word mod (2^31 - 1), in long int.
		hi := int64(word) / 127773
		lo := int64(word) % 127773
		word = int32(16807*lo - 2836*hi)
		if word < 0 {
			word += 2147483647
		}
		r.state[i] = word
	}
	r.fptr = randSep
	r.rptr = 0
	for kc := randDeg * 10; kc > 0; kc-- {
		r.Rand()
	}
}

// Rand returns the next value in [0, RAND_MAX] like glibc rand().
func (r *Rand) Rand() int32 {
	val := uint32(r.state[r.fptr]) + uint32(r.state[r.rptr])
	r.state[r.fptr] = int32(val)
	result := int32(val >> 1)
	r.fptr++
	if r.fptr >= randDeg {
		r.fptr = 0
		r.rptr++
	} else {
		r.rptr++
		if r.rptr >= randDeg {
			r.rptr = 0
		}
	}
	return result
}

// Random returns rand() & 0x7fff, the integer core of the game's random().
func (r *Rand) Random() int32 {
	return r.Rand() & 0x7fff
}

// Frand returns (rand()&32767)* (1.0/32767), computed in double, as float.
// C: qcommon/common.c:1366 frand
func (r *Rand) Frand() float32 {
	return float32(float64(r.Rand()&32767) * (1.0 / 32767))
}

// Crand returns (rand()&32767)* (2.0/32767) - 1, computed in double, as float.
// C: qcommon/common.c:1371 crand
func (r *Rand) Crand() float32 {
	return float32(float64(r.Rand()&32767)*(2.0/32767) - 1)
}

// GRandom is the game macro random(): ((rand () & 0x7fff) / ((float)0x7fff)),
// an int divided by a float, so a float division.
// C: game/g_local.h:513 random
func (r *Rand) GRandom() float32 {
	return float32(r.Rand()&0x7fff) / float32(0x7fff)
}

// GCrandom is the game macro crandom(): (2.0 * (random() - 0.5)). The result
// is a double; callers narrow it where C stores it.
// C: game/g_local.h:514 crandom
func (r *Rand) GCrandom() float64 {
	return 2.0 * (float64(r.GRandom()) - 0.5)
}
