/* game_dump.c -- per-frame JSON dump of game-private state. Compiled twice: against game/g_local.h
 * (oracle_game) and ctf/g_local.h (-DORACLE_CTF, oracle_game_ctf), so private layouts are exact. */
#include "g_local.h"
#include "ostate.h"

const char *elfsym_name(const void *p);

int gd_edict_size(void) { return sizeof(edict_t); }

#ifdef ORACLE_CTF
const char *gd_module(void) { return "ctf"; }
#else
const char *gd_module(void) { return "baseq2"; }
#endif

static int idx(edict_t *base, edict_t *e) { return e ? (int)(e - base) : -1; }

static void w_name(FILE *f, const void *p)
{
	const char *n = elfsym_name(p);
	if (n) oj_str(f, n); else fputs("null", f);
}

void gd_level(FILE *f, void *levelp)
{
	level_locals_t *l = levelp;
	fprintf(f, "{\"framenum\":%d,\"time\":", l->framenum); oj_float(f, l->time);
	fprintf(f, ",\"killed_monsters\":%d,\"total_monsters\":%d,\"found_secrets\":%d,\"total_secrets\":%d}",
		l->killed_monsters, l->total_monsters, l->found_secrets, l->total_secrets);
}

/* Edict records are rendered per edict and compared with the previous frame's rendering.
 * full=1: list every inuse edict. full=0 (delta): list only edicts whose record changed (or became inuse) and
 * write the numbers of edicts that stopped being inuse into "freed". */
static char *prev[MAX_EDICTS];

static void w_edict(FILE *f, edict_t *base, edict_t *e, int n)
{
	fprintf(f, "{\"n\":%d,\"classname\":", n);
	if (e->classname) oj_str(f, e->classname); else fputs("null", f);
	fputs(",\"s\":", f); os_es(f, &e->s);
	fprintf(f, ",\"solid\":%d,\"svflags\":%d,\"mins\":", (int)e->solid, e->svflags); oj_vec3(f, e->mins);
	fputs(",\"maxs\":", f); oj_vec3(f, e->maxs);
	fprintf(f, ",\"health\":%d,\"movetype\":%d,\"flags\":%d,\"nextthink\":", e->health, e->movetype, e->flags);
	oj_float(f, e->nextthink);
	fputs(",\"velocity\":", f); oj_vec3(f, e->velocity);
	fputs(",\"avelocity\":", f); oj_vec3(f, e->avelocity);
	fprintf(f, ",\"groundentity\":%d,\"enemy\":%d,\"owner\":%d,\"takedamage\":%d,\"deadflag\":%d,\"waterlevel\":%d,"
		"\"spawnflags\":%d,\"think\":", idx(base, e->groundentity), idx(base, e->enemy), idx(base, e->owner),
		e->takedamage, e->deadflag, e->waterlevel, e->spawnflags);
	w_name(f, (void *)e->think);
	if (e->svflags & SVF_MONSTER) {
		fprintf(f, ",\"aiflags\":%d,\"currentmove\":", e->monsterinfo.aiflags);
		w_name(f, e->monsterinfo.currentmove);
		fputc('}', f);
	} else
		fputs(",\"aiflags\":null,\"currentmove\":null}", f);
}

void gd_edicts(FILE *f, void *edictsp, int num_edicts, int full)
{
	edict_t *base = edictsp, *e;
	int n, first = 1;
	fputs("\"edicts\":[", f);
	for (n = 0; n < MAX_EDICTS; n++) {
		char *rec = NULL; size_t len = 0; FILE *m;
		e = base + n;
		if (n >= num_edicts || !e->inuse) continue;
		m = open_memstream(&rec, &len);
		w_edict(m, base, e, n);
		fclose(m);
		if (full || !prev[n] || strcmp(prev[n], rec)) {
			if (!first) fputc(',', f);
			first = 0;
			fputs(rec, f);
		}
		free(prev[n]);
		prev[n] = rec;
	}
	fputs("],\"freed\":[", f);
	first = 1;
	for (n = 0; n < MAX_EDICTS; n++) {
		if (!prev[n]) continue;
		if (n < num_edicts && base[n].inuse) continue;
		free(prev[n]); prev[n] = NULL;
		fprintf(f, first ? "%d" : ",%d", n);
		first = 0;
	}
	fputc(']', f);
}

/* clients: game client slot numbers (0-based) to dump, in order */
void gd_clients(FILE *f, void *edictsp, const int *slots, int nslots)
{
	edict_t *base = edictsp;
	int i, k;
	fputc('[', f);
	for (i = 0; i < nslots; i++) {
		edict_t *e = base + 1 + slots[i];
		gclient_t *cl = e->client;
		if (i) fputc(',', f);
		if (!cl) { fputs("null", f); continue; }
		fputs("{\"ps\":", f); os_ps(f, &cl->ps);
		fputs(",\"inventory\":[", f);
		for (k = 0; k < MAX_ITEMS; k++) fprintf(f, k ? ",%d" : "%d", cl->pers.inventory[k]);
		fprintf(f, "],\"health\":%d,\"score\":%d,\"weapon\":", e->health, cl->resp.score);
		if (cl->pers.weapon && cl->pers.weapon->classname) oj_str(f, cl->pers.weapon->classname); else fputs("null", f);
		fputc('}', f);
	}
	fputc(']', f);
}
