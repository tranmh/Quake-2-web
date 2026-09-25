// Package md4 ports qcommon/md4.c (RSA MD4) and Com_BlockChecksum. UINT4 is
// uint32 (the oracle is built with the LP64 patch UINT4 -> unsigned int).
package md4

import "encoding/binary"

// Constants for MD4Transform routine.
const (
	s11 = 3
	s12 = 7
	s13 = 11
	s14 = 19
	s21 = 3
	s22 = 5
	s23 = 9
	s24 = 13
	s31 = 3
	s32 = 9
	s33 = 11
	s34 = 15
)

// padding is C PADDING.
var padding = [64]byte{0x80}

// Ctx is C MD4_CTX.
type Ctx struct {
	state  [4]uint32 // state (ABCD)
	count  [2]uint32 // number of bits, modulo 2^64 (lsb first)
	buffer [64]byte  // input buffer
}

func rotl(x uint32, n uint) uint32 { return (x << n) | (x >> (32 - n)) }

func ff(a, b, c, d, x uint32, s uint) uint32 {
	a += ((b & c) | (^b & d)) + x
	return rotl(a, s)
}

func gg(a, b, c, d, x uint32, s uint) uint32 {
	a += ((b & c) | (b & d) | (c & d)) + x + 0x5a827999
	return rotl(a, s)
}

func hh(a, b, c, d, x uint32, s uint) uint32 {
	a += (b ^ c ^ d) + x + 0x6ed9eba1
	return rotl(a, s)
}

// Init begins an MD4 operation.
// C: qcommon/md4.c:98 MD4Init
func (ctx *Ctx) Init() {
	ctx.count[0], ctx.count[1] = 0, 0
	ctx.state[0] = 0x67452301
	ctx.state[1] = 0xefcdab89
	ctx.state[2] = 0x98badcfe
	ctx.state[3] = 0x10325476
}

// Update continues an MD4 message-digest operation.
// C: qcommon/md4.c:110 MD4Update
func (ctx *Ctx) Update(input []byte) {
	inputLen := uint32(len(input))
	var i uint32

	// Compute number of bytes mod 64
	index := (ctx.count[0] >> 3) & 0x3F

	// Update number of bits
	ctx.count[0] += inputLen << 3
	if ctx.count[0] < inputLen<<3 {
		ctx.count[1]++
	}
	ctx.count[1] += inputLen >> 29

	partLen := 64 - index

	// Transform as many times as possible.
	if inputLen >= partLen {
		copy(ctx.buffer[index:], input[:partLen])
		transform(&ctx.state, ctx.buffer[:])
		for i = partLen; i+63 < inputLen; i += 64 {
			transform(&ctx.state, input[i:i+64])
		}
		index = 0
	} else {
		i = 0
	}

	// Buffer remaining input
	copy(ctx.buffer[index:], input[i:inputLen])
}

// Final ends an MD4 operation and returns the digest.
// C: qcommon/md4.c:145 MD4Final
func (ctx *Ctx) Final() [16]byte {
	var bits [8]byte
	binary.LittleEndian.PutUint32(bits[0:], ctx.count[0])
	binary.LittleEndian.PutUint32(bits[4:], ctx.count[1])

	// Pad out to 56 mod 64.
	index := (ctx.count[0] >> 3) & 0x3f
	var padLen uint32
	if index < 56 {
		padLen = 56 - index
	} else {
		padLen = 120 - index
	}
	ctx.Update(padding[:padLen])

	// Append length (before padding)
	ctx.Update(bits[:])

	var digest [16]byte
	for i := 0; i < 4; i++ {
		binary.LittleEndian.PutUint32(digest[i*4:], ctx.state[i])
	}
	*ctx = Ctx{}
	return digest
}

// transform is the MD4 basic transformation.
// C: qcommon/md4.c:170 MD4Transform
func transform(state *[4]uint32, block []byte) {
	a, b, c, d := state[0], state[1], state[2], state[3]
	var x [16]uint32
	for i := 0; i < 16; i++ {
		x[i] = binary.LittleEndian.Uint32(block[i*4:])
	}

	// Round 1
	a = ff(a, b, c, d, x[0], s11)
	d = ff(d, a, b, c, x[1], s12)
	c = ff(c, d, a, b, x[2], s13)
	b = ff(b, c, d, a, x[3], s14)
	a = ff(a, b, c, d, x[4], s11)
	d = ff(d, a, b, c, x[5], s12)
	c = ff(c, d, a, b, x[6], s13)
	b = ff(b, c, d, a, x[7], s14)
	a = ff(a, b, c, d, x[8], s11)
	d = ff(d, a, b, c, x[9], s12)
	c = ff(c, d, a, b, x[10], s13)
	b = ff(b, c, d, a, x[11], s14)
	a = ff(a, b, c, d, x[12], s11)
	d = ff(d, a, b, c, x[13], s12)
	c = ff(c, d, a, b, x[14], s13)
	b = ff(b, c, d, a, x[15], s14)

	// Round 2
	a = gg(a, b, c, d, x[0], s21)
	d = gg(d, a, b, c, x[4], s22)
	c = gg(c, d, a, b, x[8], s23)
	b = gg(b, c, d, a, x[12], s24)
	a = gg(a, b, c, d, x[1], s21)
	d = gg(d, a, b, c, x[5], s22)
	c = gg(c, d, a, b, x[9], s23)
	b = gg(b, c, d, a, x[13], s24)
	a = gg(a, b, c, d, x[2], s21)
	d = gg(d, a, b, c, x[6], s22)
	c = gg(c, d, a, b, x[10], s23)
	b = gg(b, c, d, a, x[14], s24)
	a = gg(a, b, c, d, x[3], s21)
	d = gg(d, a, b, c, x[7], s22)
	c = gg(c, d, a, b, x[11], s23)
	b = gg(b, c, d, a, x[15], s24)

	// Round 3
	a = hh(a, b, c, d, x[0], s31)
	d = hh(d, a, b, c, x[8], s32)
	c = hh(c, d, a, b, x[4], s33)
	b = hh(b, c, d, a, x[12], s34)
	a = hh(a, b, c, d, x[2], s31)
	d = hh(d, a, b, c, x[10], s32)
	c = hh(c, d, a, b, x[6], s33)
	b = hh(b, c, d, a, x[14], s34)
	a = hh(a, b, c, d, x[1], s31)
	d = hh(d, a, b, c, x[9], s32)
	c = hh(c, d, a, b, x[5], s33)
	b = hh(b, c, d, a, x[13], s34)
	a = hh(a, b, c, d, x[3], s31)
	d = hh(d, a, b, c, x[11], s32)
	c = hh(c, d, a, b, x[7], s33)
	b = hh(b, c, d, a, x[15], s34)

	state[0] += a
	state[1] += b
	state[2] += c
	state[3] += d
}

// Sum returns the MD4 digest of data.
func Sum(data []byte) [16]byte {
	var ctx Ctx
	ctx.Init()
	ctx.Update(data)
	return ctx.Final()
}

// Com_BlockChecksum XORs the four little-endian digest words.
// C: qcommon/md4.c:265 Com_BlockChecksum
func Com_BlockChecksum(buffer []byte) uint32 {
	d := Sum(buffer)
	return binary.LittleEndian.Uint32(d[0:]) ^ binary.LittleEndian.Uint32(d[4:]) ^
		binary.LittleEndian.Uint32(d[8:]) ^ binary.LittleEndian.Uint32(d[12:])
}
