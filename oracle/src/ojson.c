/* ojson.c -- see ojson.h */
#include "ojson.h"
#include <stdlib.h>
#include <string.h>
#include <math.h>

/* ================= writer ================= */

void oj_float(FILE *f, float v)
{
	char buf[64];
	if (v != v) { fputs("null", f); return; }  /* never expected */
	snprintf(buf, sizeof(buf), "%.9g", (double)v);
	fputs(buf, f);
}

void oj_double(FILE *f, double v)
{
	if (v != v) { fputs("null", f); return; }
	fprintf(f, "%.17g", v);
}

void oj_vec3(FILE *f, const float *v)
{
	fputc('[', f); oj_float(f, v[0]); fputc(',', f); oj_float(f, v[1]); fputc(',', f); oj_float(f, v[2]); fputc(']', f);
}

void oj_short3(FILE *f, const short *v)
{
	fprintf(f, "[%d,%d,%d]", v[0], v[1], v[2]);
}

void oj_str(FILE *f, const char *s)
{
	const unsigned char *p = (const unsigned char *)s;
	fputc('"', f);
	if (p) for (; *p; p++) {
		unsigned c = *p;
		if (c == '"') fputs("\\\"", f);
		else if (c == '\\') fputs("\\\\", f);
		else if (c == '\n') fputs("\\n", f);
		else if (c == '\r') fputs("\\r", f);
		else if (c == '\t') fputs("\\t", f);
		else if (c < 0x20 || c >= 0x7f) fprintf(f, "\\u%04x", c);
		else fputc(c, f);
	}
	fputc('"', f);
}

void oj_hex(FILE *f, const unsigned char *b, int n)
{
	static const char hx[] = "0123456789abcdef";
	int i;
	fputc('"', f);
	for (i = 0; i < n; i++) { fputc(hx[b[i] >> 4], f); fputc(hx[b[i] & 15], f); }
	fputc('"', f);
}

/* ================= reader ================= */

typedef struct { const char *p; char *err; int errlen; int failed; } ojp_t;

static void ws(ojp_t *P) { while (*P->p == ' ' || *P->p == '\t' || *P->p == '\n' || *P->p == '\r') P->p++; }

static void fail(ojp_t *P, const char *msg)
{
	if (!P->failed) snprintf(P->err, P->errlen, "%s near '%.20s'", msg, P->p);
	P->failed = 1;
}

static ojv_t *newv(ojtype_t t) { ojv_t *v = calloc(1, sizeof(*v)); v->type = t; return v; }

static void addkid(ojv_t *v, ojv_t *k, char *key)
{
	v->kids = realloc(v->kids, sizeof(*v->kids) * (v->n + 1));
	if (v->type == OJ_OBJ) v->keys = realloc(v->keys, sizeof(*v->keys) * (v->n + 1)), v->keys[v->n] = key;
	v->kids[v->n++] = k;
}

static void put_utf8(char **o, unsigned c)
{
	/* the oracle only emits/reads bytes; \u00XX maps back to byte XX, larger code points to UTF-8 */
	if (c < 0x100) { *(*o)++ = (char)c; return; }
	if (c < 0x800) { *(*o)++ = 0xC0 | (c >> 6); *(*o)++ = 0x80 | (c & 63); return; }
	*(*o)++ = 0xE0 | (c >> 12); *(*o)++ = 0x80 | ((c >> 6) & 63); *(*o)++ = 0x80 | (c & 63);
}

static char *pstring(ojp_t *P)
{
	const char *s = P->p + 1;
	char *out = malloc(strlen(s) + 1), *o = out;
	while (*s && *s != '"') {
		if (*s == '\\') {
			s++;
			switch (*s) {
			case 'n': *o++ = '\n'; break;
			case 'r': *o++ = '\r'; break;
			case 't': *o++ = '\t'; break;
			case 'b': *o++ = '\b'; break;
			case 'f': *o++ = '\f'; break;
			case 'u': { unsigned c = 0; int i; for (i = 1; i <= 4; i++) { char h = s[i]; c <<= 4;
					if (h >= '0' && h <= '9') c |= h - '0'; else if (h >= 'a' && h <= 'f') c |= h - 'a' + 10;
					else if (h >= 'A' && h <= 'F') c |= h - 'A' + 10; }
				s += 4; put_utf8(&o, c); break; }
			default: *o++ = *s; break;
			}
			s++;
		} else *o++ = *s++;
	}
	*o = 0;
	if (*s != '"') fail(P, "unterminated string");
	else s++;
	P->p = s;
	return out;
}

static ojv_t *pvalue(ojp_t *P)
{
	ojv_t *v;
	ws(P);
	if (P->failed) return newv(OJ_NULL);
	switch (*P->p) {
	case '{':
		v = newv(OJ_OBJ); P->p++; ws(P);
		if (*P->p == '}') { P->p++; return v; }
		for (;;) {
			char *key; ws(P);
			if (*P->p != '"') { fail(P, "expected key"); return v; }
			key = pstring(P); ws(P);
			if (*P->p != ':') { fail(P, "expected ':'"); free(key); return v; }
			P->p++;
			addkid(v, pvalue(P), key);
			if (P->failed) return v;
			ws(P);
			if (*P->p == ',') { P->p++; continue; }
			if (*P->p == '}') { P->p++; return v; }
			fail(P, "expected ',' or '}'"); return v;
		}
	case '[':
		v = newv(OJ_ARR); P->p++; ws(P);
		if (*P->p == ']') { P->p++; return v; }
		for (;;) {
			addkid(v, pvalue(P), NULL);
			if (P->failed) return v;
			ws(P);
			if (*P->p == ',') { P->p++; continue; }
			if (*P->p == ']') { P->p++; return v; }
			fail(P, "expected ',' or ']'"); return v;
		}
	case '"':
		v = newv(OJ_STR); v->str = pstring(P); return v;
	case 't': if (!strncmp(P->p, "true", 4)) { P->p += 4; v = newv(OJ_BOOL); v->num = 1; return v; } break;
	case 'f': if (!strncmp(P->p, "false", 5)) { P->p += 5; v = newv(OJ_BOOL); v->num = 0; return v; } break;
	case 'n': if (!strncmp(P->p, "null", 4)) { P->p += 4; return newv(OJ_NULL); } break;
	default:
		if (*P->p == '-' || (*P->p >= '0' && *P->p <= '9')) {
			char *end; v = newv(OJ_NUM); v->num = strtod(P->p, &end);
			if (end == P->p) fail(P, "bad number");
			P->p = end; return v;
		}
	}
	fail(P, "unexpected character");
	return newv(OJ_NULL);
}

ojv_t *oj_parse(const char *text, char *err, int errlen)
{
	ojp_t P; ojv_t *v;
	P.p = text; P.err = err; P.errlen = errlen; P.failed = 0;
	v = pvalue(&P);
	ws(&P);
	if (!P.failed && *P.p) fail(&P, "trailing garbage");
	if (P.failed) { oj_free(v); return NULL; }
	return v;
}

ojv_t *oj_parse_file(const char *path)
{
	FILE *f = fopen(path, "rb");
	long n; char *buf; char err[256]; ojv_t *v;
	if (!f) { fprintf(stderr, "oracle: cannot open %s\n", path); exit(2); }
	fseek(f, 0, SEEK_END); n = ftell(f); fseek(f, 0, SEEK_SET);
	buf = malloc(n + 1);
	if (fread(buf, 1, n, f) != (size_t)n) { fprintf(stderr, "oracle: read error %s\n", path); exit(2); }
	buf[n] = 0; fclose(f);
	v = oj_parse(buf, err, sizeof(err));
	if (!v) { fprintf(stderr, "oracle: JSON error in %s: %s\n", path, err); exit(2); }
	free(buf);
	return v;
}

ojv_t *oj_get(const ojv_t *o, const char *key)
{
	int i;
	if (!o || o->type != OJ_OBJ) return NULL;
	for (i = 0; i < o->n; i++) if (!strcmp(o->keys[i], key)) return o->kids[i];
	return NULL;
}

double oj_num(const ojv_t *v, double def) { return (v && (v->type == OJ_NUM || v->type == OJ_BOOL)) ? v->num : def; }
int oj_int(const ojv_t *v, int def) { return (v && (v->type == OJ_NUM || v->type == OJ_BOOL)) ? (int)v->num : def; }
const char *oj_string(const ojv_t *v, const char *def) { return (v && v->type == OJ_STR) ? v->str : def; }

void oj_free(ojv_t *v)
{
	int i;
	if (!v) return;
	for (i = 0; i < v->n; i++) { oj_free(v->kids[i]); if (v->keys) free(v->keys[i]); }
	free(v->kids); free(v->keys); free(v->str); free(v);
}

/* ================= SHA-256 ================= */

static const uint32_t K256[64] = {
	0x428a2f98,0x71374491,0xb5c0fbcf,0xe9b5dba5,0x3956c25b,0x59f111f1,0x923f82a4,0xab1c5ed5,
	0xd807aa98,0x12835b01,0x243185be,0x550c7dc3,0x72be5d74,0x80deb1fe,0x9bdc06a7,0xc19bf174,
	0xe49b69c1,0xefbe4786,0x0fc19dc6,0x240ca1cc,0x2de92c6f,0x4a7484aa,0x5cb0a9dc,0x76f988da,
	0x983e5152,0xa831c66d,0xb00327c8,0xbf597fc7,0xc6e00bf3,0xd5a79147,0x06ca6351,0x14292967,
	0x27b70a85,0x2e1b2138,0x4d2c6dfc,0x53380d13,0x650a7354,0x766a0abb,0x81c2c92e,0x92722c85,
	0xa2bfe8a1,0xa81a664b,0xc24b8b70,0xc76c51a3,0xd192e819,0xd6990624,0xf40e3585,0x106aa070,
	0x19a4c116,0x1e376c08,0x2748774c,0x34b0bcb5,0x391c0cb3,0x4ed8aa4a,0x5b9cca4f,0x682e6ff3,
	0x748f82ee,0x78a5636f,0x84c87814,0x8cc70208,0x90befffa,0xa4506ceb,0xbef9a3f7,0xc67178f2 };

#define ROR(x,n) (((x) >> (n)) | ((x) << (32 - (n))))

static void sha_block(uint32_t h[8], const unsigned char *p)
{
	uint32_t w[64], a, b, c, d, e, f, g, hh, t1, t2;
	int i;
	for (i = 0; i < 16; i++) w[i] = ((uint32_t)p[i*4] << 24) | ((uint32_t)p[i*4+1] << 16) | ((uint32_t)p[i*4+2] << 8) | p[i*4+3];
	for (i = 16; i < 64; i++) {
		uint32_t s0 = ROR(w[i-15], 7) ^ ROR(w[i-15], 18) ^ (w[i-15] >> 3);
		uint32_t s1 = ROR(w[i-2], 17) ^ ROR(w[i-2], 19) ^ (w[i-2] >> 10);
		w[i] = w[i-16] + s0 + w[i-7] + s1;
	}
	a = h[0]; b = h[1]; c = h[2]; d = h[3]; e = h[4]; f = h[5]; g = h[6]; hh = h[7];
	for (i = 0; i < 64; i++) {
		t1 = hh + (ROR(e, 6) ^ ROR(e, 11) ^ ROR(e, 25)) + ((e & f) ^ (~e & g)) + K256[i] + w[i];
		t2 = (ROR(a, 2) ^ ROR(a, 13) ^ ROR(a, 22)) + ((a & b) ^ (a & c) ^ (b & c));
		hh = g; g = f; f = e; e = d + t1; d = c; c = b; b = a; a = t1 + t2;
	}
	h[0] += a; h[1] += b; h[2] += c; h[3] += d; h[4] += e; h[5] += f; h[6] += g; h[7] += hh;
}

void sha256(const unsigned char *data, size_t len, unsigned char out[32])
{
	uint32_t h[8] = { 0x6a09e667,0xbb67ae85,0x3c6ef372,0xa54ff53a,0x510e527f,0x9b05688c,0x1f83d9ab,0x5be0cd19 };
	unsigned char tail[128];
	size_t i, rem = len % 64, full = len - rem, tl;
	uint64_t bits = (uint64_t)len * 8;
	for (i = 0; i < full; i += 64) sha_block(h, data + i);
	memset(tail, 0, sizeof(tail));
	memcpy(tail, data + full, rem);
	tail[rem] = 0x80;
	tl = (rem < 56) ? 64 : 128;
	for (i = 0; i < 8; i++) tail[tl - 1 - i] = (unsigned char)(bits >> (8 * i));
	sha_block(h, tail);
	if (tl == 128) sha_block(h, tail + 64);
	for (i = 0; i < 8; i++) { out[i*4] = h[i] >> 24; out[i*4+1] = h[i] >> 16; out[i*4+2] = h[i] >> 8; out[i*4+3] = h[i]; }
}

void sha256_hex(const unsigned char *data, size_t len, char out[65])
{
	unsigned char d[32]; int i;
	sha256(data, len, d);
	for (i = 0; i < 32; i++) sprintf(out + i * 2, "%02x", d[i]);
	out[64] = 0;
}

/* ================= PRNG ================= */

void xr_seed(xr_t *r, uint32_t seed) { r->s = seed ? seed : 0x9E3779B9u; }

uint32_t xr_next(xr_t *r)
{
	uint32_t x = r->s;
	x ^= x << 13; x ^= x >> 17; x ^= x << 5;
	r->s = x;
	return x;
}

uint32_t xr_range(xr_t *r, uint32_t n) { return n ? xr_next(r) % n : 0; }

float xr_float(xr_t *r, float lo, float hi)
{
	float t = (float)(xr_next(r) >> 8) / 16777216.0f;
	return lo + (hi - lo) * t;
}

/* ================= serializer (for echoing parsed input) ================= */
void oj_write(FILE *f, const ojv_t *v)
{
	int i;
	if (!v) { fputs("null", f); return; }
	switch (v->type) {
	case OJ_NULL: fputs("null", f); break;
	case OJ_BOOL: fputs(v->num ? "true" : "false", f); break;
	case OJ_NUM:
		if (v->num == (double)(long long)v->num && fabs(v->num) < 9e15) fprintf(f, "%lld", (long long)v->num);
		else oj_double(f, v->num);
		break;
	case OJ_STR: oj_str(f, v->str); break;
	case OJ_ARR:
		fputc('[', f);
		for (i = 0; i < v->n; i++) { if (i) fputc(',', f); oj_write(f, v->kids[i]); }
		fputc(']', f);
		break;
	case OJ_OBJ:
		fputc('{', f);
		for (i = 0; i < v->n; i++) { if (i) fputc(',', f); oj_str(f, v->keys[i]); fputc(':', f); oj_write(f, v->kids[i]); }
		fputc('}', f);
		break;
	}
}
