/* ostate.c -- JSON encoding of shared structs. Field names are fixed by docs/FIXTURES.md. */
#include "../build/src/game/q_shared.h"
#include "ostate.h"

void os_es(FILE *f, const entity_state_t *s)
{
	fprintf(f, "{\"number\":%d,\"origin\":", s->number); oj_vec3(f, s->origin);
	fputs(",\"angles\":", f); oj_vec3(f, s->angles);
	fputs(",\"old_origin\":", f); oj_vec3(f, s->old_origin);
	fprintf(f, ",\"modelindex\":%d,\"modelindex2\":%d,\"modelindex3\":%d,\"modelindex4\":%d,\"frame\":%d,\"skinnum\":%d,"
		"\"effects\":%u,\"renderfx\":%d,\"solid\":%d,\"sound\":%d,\"event\":%d}",
		s->modelindex, s->modelindex2, s->modelindex3, s->modelindex4, s->frame, s->skinnum,
		s->effects, s->renderfx, s->solid, s->sound, s->event);
}

void os_uc(FILE *f, const usercmd_t *c)
{
	fprintf(f, "{\"msec\":%d,\"buttons\":%d,\"angles\":[%d,%d,%d],\"forwardmove\":%d,\"sidemove\":%d,\"upmove\":%d,"
		"\"impulse\":%d,\"lightlevel\":%d}", c->msec, c->buttons, c->angles[0], c->angles[1], c->angles[2],
		c->forwardmove, c->sidemove, c->upmove, c->impulse, c->lightlevel);
}

void os_pms(FILE *f, const pmove_state_t *s)
{
	fprintf(f, "{\"pm_type\":%d,\"origin\":", (int)s->pm_type); oj_short3(f, s->origin);
	fputs(",\"velocity\":", f); oj_short3(f, s->velocity);
	fprintf(f, ",\"pm_flags\":%d,\"pm_time\":%d,\"gravity\":%d,\"delta_angles\":", s->pm_flags, s->pm_time, s->gravity);
	oj_short3(f, s->delta_angles);
	fputc('}', f);
}

void os_ps(FILE *f, const player_state_t *ps)
{
	int i;
	fputs("{\"pmove\":", f); os_pms(f, &ps->pmove);
	fputs(",\"viewangles\":", f); oj_vec3(f, ps->viewangles);
	fputs(",\"viewoffset\":", f); oj_vec3(f, ps->viewoffset);
	fputs(",\"kick_angles\":", f); oj_vec3(f, ps->kick_angles);
	fputs(",\"gunangles\":", f); oj_vec3(f, ps->gunangles);
	fputs(",\"gunoffset\":", f); oj_vec3(f, ps->gunoffset);
	fprintf(f, ",\"gunindex\":%d,\"gunframe\":%d,\"blend\":[", ps->gunindex, ps->gunframe);
	for (i = 0; i < 4; i++) { if (i) fputc(',', f); oj_float(f, ps->blend[i]); }
	fputs("],\"fov\":", f); oj_float(f, ps->fov);
	fprintf(f, ",\"rdflags\":%d,\"stats\":[", ps->rdflags);
	for (i = 0; i < MAX_STATS; i++) fprintf(f, i ? ",%d" : "%d", ps->stats[i]);
	fputs("]}", f);
}

void os_read_vec3(const ojv_t *v, float *out)
{
	int i;
	for (i = 0; i < 3; i++) out[i] = (v && v->type == OJ_ARR && v->n > i) ? (float)oj_num(v->kids[i], 0) : 0;
}

void os_read_short3(const ojv_t *v, short *out)
{
	int i;
	for (i = 0; i < 3; i++) out[i] = (v && v->type == OJ_ARR && v->n > i) ? (short)oj_int(v->kids[i], 0) : 0;
}

void os_read_uc(const ojv_t *v, usercmd_t *c)
{
	memset(c, 0, sizeof(*c));
	c->msec = (byte)oj_int(oj_get(v, "msec"), 0);
	c->buttons = (byte)oj_int(oj_get(v, "buttons"), 0);
	os_read_short3(oj_get(v, "angles"), c->angles);
	c->forwardmove = (short)oj_int(oj_get(v, "forwardmove"), 0);
	c->sidemove = (short)oj_int(oj_get(v, "sidemove"), 0);
	c->upmove = (short)oj_int(oj_get(v, "upmove"), 0);
	c->impulse = (byte)oj_int(oj_get(v, "impulse"), 0);
	c->lightlevel = (byte)oj_int(oj_get(v, "lightlevel"), 0);
}

void os_read_pms(const ojv_t *v, pmove_state_t *s)
{
	memset(s, 0, sizeof(*s));
	s->pm_type = (pmtype_t)oj_int(oj_get(v, "pm_type"), 0);
	os_read_short3(oj_get(v, "origin"), s->origin);
	os_read_short3(oj_get(v, "velocity"), s->velocity);
	s->pm_flags = (byte)oj_int(oj_get(v, "pm_flags"), 0);
	s->pm_time = (byte)oj_int(oj_get(v, "pm_time"), 0);
	s->gravity = (short)oj_int(oj_get(v, "gravity"), 0);
	os_read_short3(oj_get(v, "delta_angles"), s->delta_angles);
}
