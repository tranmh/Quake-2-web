/* ostate.h -- JSON encoders/decoders for shared Quake 2 structs (see docs/FIXTURES.md). */
#ifndef OSTATE_H
#define OSTATE_H
#include <stdio.h>
#include "ojson.h"

/* the caller must include q_shared.h (game/ or ctf/, identical layouts) before this header */
void os_es(FILE *f, const entity_state_t *s);          /* ES */
void os_uc(FILE *f, const usercmd_t *c);               /* UC */
void os_pms(FILE *f, const pmove_state_t *s);          /* pmove_state */
void os_ps(FILE *f, const player_state_t *ps);         /* PS */

void os_read_uc(const ojv_t *v, usercmd_t *c);
void os_read_pms(const ojv_t *v, pmove_state_t *s);
void os_read_vec3(const ojv_t *v, float *out);
void os_read_short3(const ojv_t *v, short *out);
#endif
