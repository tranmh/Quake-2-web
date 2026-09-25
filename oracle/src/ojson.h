/* ojson.h -- tiny JSON reader/writer, SHA-256, hex and a deterministic PRNG for the oracle drivers.
 * Self-contained (libc only) so it can be linked into every oracle binary. */
#ifndef OJSON_H
#define OJSON_H

#include <stdio.h>
#include <stdint.h>

/* ---------------- writer ---------------- */
void oj_float(FILE *f, float v);           /* %.9g, C float */
void oj_double(FILE *f, double v);         /* %.17g */
void oj_vec3(FILE *f, const float *v);     /* [a,b,c] with %.9g */
void oj_short3(FILE *f, const short *v);   /* [a,b,c] */
void oj_str(FILE *f, const char *s);       /* JSON string (bytes >= 0x80 escaped as \u00XX) */
void oj_hex(FILE *f, const unsigned char *b, int n); /* "hex" */

/* ---------------- reader ---------------- */
typedef enum { OJ_NULL, OJ_BOOL, OJ_NUM, OJ_STR, OJ_ARR, OJ_OBJ } ojtype_t;
typedef struct ojv_s {
	ojtype_t type;
	double num;
	char *str;             /* OJ_STR (NUL terminated, may contain decoded bytes) */
	int n;                 /* number of children (ARR/OBJ) */
	struct ojv_s **kids;
	char **keys;           /* OBJ keys */
} ojv_t;

ojv_t *oj_parse(const char *text, char *err, int errlen);
ojv_t *oj_parse_file(const char *path);        /* exits on error */
ojv_t *oj_get(const ojv_t *obj, const char *key); /* NULL if missing or obj not an object */
double oj_num(const ojv_t *v, double def);
int oj_int(const ojv_t *v, int def);
const char *oj_string(const ojv_t *v, const char *def); /* def if missing/null */
void oj_free(ojv_t *v);
void oj_write(FILE *f, const ojv_t *v);    /* compact re-serialization */

/* ---------------- misc ---------------- */
void sha256(const unsigned char *data, size_t len, unsigned char out[32]);
void sha256_hex(const unsigned char *data, size_t len, char out[65]);

/* xorshift32 (Marsaglia 13/17/5). state must be nonzero; seed 0 is mapped to 0x9E3779B9. */
typedef struct { uint32_t s; } xr_t;
void xr_seed(xr_t *r, uint32_t seed);
uint32_t xr_next(xr_t *r);
uint32_t xr_range(xr_t *r, uint32_t n);           /* xr_next % n */
float xr_float(xr_t *r, float lo, float hi);      /* lo + (hi-lo) * (next>>8)/2^24, in float */

#endif
