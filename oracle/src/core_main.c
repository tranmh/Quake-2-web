/* core_main.c -- oracle_core: deterministic dumps of qcommon behaviour (cmodel, pmove, msg, crc, md4, rand, pak).
 * Output formats: docs/FIXTURES.md. Usage: oracle_core [--basedir DIR] [-o OUT] <subcommand> args... */
#include "../build/src/qcommon/qcommon.h"
#include "../build/src/qcommon/crc.h"
#include "ojson.h"
#include "ostate.h"

void oracle_init(const char *basedir, int nkv, const char **kv);
const char *oracle_basedir(const char *arg);

/* ---- cmodel.c globals (all non-static in the original) ---- */
typedef struct { int contents; int cluster; int area; unsigned short firstleafbrush; unsigned short numleafbrushes; } o_cleaf_t;
extern int numclusters, numareas, numleafs, numnodes, numplanes, numbrushes, numbrushsides, numleafbrushes,
	numtexinfo, numareaportals, numcmodels, numentitychars;
extern o_cleaf_t map_leafs[];
extern cmodel_t map_cmodels[];
extern char map_entitystring[];
extern float pm_airaccelerate;
extern vec3_t bytedirs[NUMVERTEXNORMALS];

static FILE *out;
static const char *g_basedir;

static void load_map(const char *map, unsigned *checksum)
{
	char name[MAX_QPATH];
	oracle_init(g_basedir, 0, NULL);
	Com_sprintf(name, sizeof(name), "maps/%s.bsp", map);
	CM_LoadMap(name, false, checksum);
}

/* =================================== bsp =================================== */
static void cmd_bsp(const char *map)
{
	unsigned checksum;
	int i, j, rowbytes;
	char sha[65];

	load_map(map, &checksum);
	sha256_hex((unsigned char *)map_entitystring, strlen(map_entitystring), sha);
	fprintf(out, "{\"map\":"); oj_str(out, map);
	fprintf(out, ",\"checksum\":%u,\"numclusters\":%d,\"numareas\":%d,\"numleafs\":%d,\"numnodes\":%d,\"numplanes\":%d,"
		"\"numbrushes\":%d,\"numbrushsides\":%d,\"numleafbrushes\":%d,\"numtexinfo\":%d,\"numareaportals\":%d,"
		"\"numcmodels\":%d,\"entitystring_sha256\":\"%s\",\n\"cmodels\":[",
		checksum, numclusters, numareas, numleafs, numnodes, numplanes, numbrushes, numbrushsides, numleafbrushes,
		numtexinfo, numareaportals, numcmodels, sha);
	for (i = 0; i < numcmodels; i++) {
		cmodel_t *c = &map_cmodels[i];
		fputs(i ? ",\n{\"mins\":" : "\n{\"mins\":", out); oj_vec3(out, c->mins);
		fputs(",\"maxs\":", out); oj_vec3(out, c->maxs);
		fputs(",\"origin\":", out); oj_vec3(out, c->origin);
		fprintf(out, ",\"headnode\":%d}", c->headnode);
	}
	rowbytes = (numclusters + 7) >> 3;
	fputs("],\n\"pvs\":[", out);
	for (i = 0; i < numclusters; i++) { fputs(i ? ",\n" : "\n", out); oj_hex(out, CM_ClusterPVS(i), rowbytes); }
	fputs("],\n\"phs\":[", out);
	for (i = 0; i < numclusters; i++) { fputs(i ? ",\n" : "\n", out); oj_hex(out, CM_ClusterPHS(i), rowbytes); }
	fputs("],\n\"leafs\":[", out);
	for (j = 0; j < numleafs; j++)
		fprintf(out, "%s{\"contents\":%d,\"cluster\":%d,\"area\":%d}", j ? ",\n" : "\n",
			map_leafs[j].contents, map_leafs[j].cluster, map_leafs[j].area);
	fputs("]}\n", out);
}

/* =================================== trace =================================== */
static void w_trace(const trace_t *t)
{
	fprintf(out, "\"r\":{\"allsolid\":%d,\"startsolid\":%d,\"fraction\":", t->allsolid ? 1 : 0, t->startsolid ? 1 : 0);
	oj_float(out, t->fraction);
	fputs(",\"endpos\":", out); oj_vec3(out, t->endpos);
	fputs(",\"plane\":{\"normal\":", out); oj_vec3(out, t->plane.normal);
	fputs(",\"dist\":", out); oj_float(out, t->plane.dist);
	fprintf(out, ",\"type\":%d,\"signbits\":%d},\"surface\":{\"name\":", t->plane.type, t->plane.signbits);
	oj_str(out, t->surface ? t->surface->name : "");
	fprintf(out, ",\"flags\":%d,\"value\":%d},\"contents\":%d}", t->surface ? t->surface->flags : 0,
		t->surface ? t->surface->value : 0, t->contents);
}

typedef struct {
	int kind; /* 0 box 1 transformed 2 point 3 pointt 4 leaf */
	vec3_t start, end, mins, maxs, origin, angles, p;
	int headnode, mask;
} tq_t;

static const char *kindnames[] = { "box", "transformed", "point", "pointt", "leaf" };

static void run_query(const tq_t *q)
{
	trace_t t;
	vec3_t s, e, mi, ma, o, a, p;
	VectorCopy(q->start, s); VectorCopy(q->end, e); VectorCopy(q->mins, mi); VectorCopy(q->maxs, ma);
	VectorCopy(q->origin, o); VectorCopy(q->angles, a); VectorCopy(q->p, p);
	fprintf(out, "{\"q\":{\"kind\":\"%s\"", kindnames[q->kind]);
	if (q->kind <= 1) {
		fputs(",\"start\":", out); oj_vec3(out, q->start);
		fputs(",\"end\":", out); oj_vec3(out, q->end);
		fputs(",\"mins\":", out); oj_vec3(out, q->mins);
		fputs(",\"maxs\":", out); oj_vec3(out, q->maxs);
		fprintf(out, ",\"headnode\":%d,\"mask\":%d", q->headnode, q->mask);
	} else {
		fputs(",\"p\":", out); oj_vec3(out, q->p);
		if (q->kind != 4) fprintf(out, ",\"headnode\":%d", q->headnode);
	}
	if (q->kind == 1 || q->kind == 3) {
		fputs(",\"origin\":", out); oj_vec3(out, q->origin);
		fputs(",\"angles\":", out); oj_vec3(out, q->angles);
	}
	fputs("},", out);
	switch (q->kind) {
	case 0: t = CM_BoxTrace(s, e, mi, ma, q->headnode, q->mask); w_trace(&t); break;
	case 1: t = CM_TransformedBoxTrace(s, e, mi, ma, q->headnode, q->mask, o, a); w_trace(&t); break;
	case 2: fprintf(out, "\"r\":{\"contents\":%d}", CM_PointContents(p, q->headnode)); break;
	case 3: fprintf(out, "\"r\":{\"contents\":%d}", CM_TransformedPointContents(p, q->headnode, o, a)); break;
	case 4: fprintf(out, "\"r\":{\"leaf\":%d}", CM_PointLeafnum(p)); break;
	}
	fputs("}\n", out);
}

static const int masks[] = { MASK_PLAYERSOLID, MASK_SOLID, MASK_SHOT, MASK_OPAQUE, MASK_WATER, MASK_ALL,
	MASK_MONSTERSOLID, MASK_DEADSOLID, MASK_CURRENT, CONTENTS_SOLID, CONTENTS_WINDOW | CONTENTS_SOLID };
#define NMASKS (int)(sizeof(masks) / sizeof(masks[0]))

static void rand_hull(xr_t *r, vec3_t mins, vec3_t maxs)
{
	int h = xr_range(r, 8), i;
	switch (h) {
	case 0: case 1: VectorClear(mins); VectorClear(maxs); break;                         /* point */
	case 2: case 3: VectorSet(mins, -16, -16, -24); VectorSet(maxs, 16, 16, 32); break;  /* player */
	case 4: VectorSet(mins, -16, -16, -24); VectorSet(maxs, 16, 16, 4); break;          /* ducked */
	case 5: VectorSet(mins, -32, -32, -24); VectorSet(maxs, 32, 32, 64); break;         /* tank-ish */
	default:
		for (i = 0; i < 3; i++) {
			mins[i] = -(float)(int)xr_range(r, 48) - (xr_range(r, 4) == 0 ? 0.5f : 0);
			maxs[i] = (float)(int)xr_range(r, 48) + (xr_range(r, 4) == 0 ? 0.25f : 0);
		}
	}
}

static void rand_point(xr_t *r, const cmodel_t *w, vec3_t p, float pad)
{
	int i;
	for (i = 0; i < 3; i++) p[i] = xr_float(r, w->mins[i] - pad, w->maxs[i] + pad);
	if (xr_range(r, 3) == 0) for (i = 0; i < 3; i++) p[i] = (float)(int)p[i];  /* integral coords hit planes exactly */
}

static int empty_point(xr_t *r, const cmodel_t *w, vec3_t p)
{
	int tries;
	for (tries = 0; tries < 200; tries++) {
		rand_point(r, w, p, 0);
		if (!(CM_PointContents(p, 0) & MASK_SOLID)) return 1;
	}
	return 0;
}

static void rand_angles(xr_t *r, vec3_t a)
{
	switch (xr_range(r, 4)) {
	case 0: VectorClear(a); break;
	case 1: VectorSet(a, 0, (float)(int)xr_range(r, 360), 0); break;
	default: a[0] = xr_float(r, -90, 90); a[1] = xr_float(r, -360, 360); a[2] = xr_float(r, -180, 180);
	}
}

static void cmd_trace_random(const char *map, int n, unsigned seed)
{
	unsigned checksum;
	xr_t r;
	tq_t q;
	int i, k, count = 0;
	const cmodel_t *w;
	vec3_t e0;

	load_map(map, &checksum);
	w = &map_cmodels[0];
	xr_seed(&r, seed);

	/* ---- hand-written edge cases ---- */
	for (i = 0; i < 16; i++) {
		if (!empty_point(&r, w, e0)) break;
		memset(&q, 0, sizeof(q)); q.kind = 0; q.mask = MASK_PLAYERSOLID;
		for (k = 0; k < 4; k++) {
			if (k == 1) { VectorSet(q.mins, -16, -16, -24); VectorSet(q.maxs, 16, 16, 32); }
			if (k == 2) { VectorSet(q.mins, -16, -16, -24); VectorSet(q.maxs, 16, 16, 4); }
			if (k == 3) { VectorSet(q.mins, -1, -2, -3); VectorSet(q.maxs, 3, 2, 1); }
			if (k == 0) { VectorClear(q.mins); VectorClear(q.maxs); }
			/* zero length */
			VectorCopy(e0, q.start); VectorCopy(e0, q.end); run_query(&q); count++;
			/* axial in all six directions, long */
			{ int ax, sg; for (ax = 0; ax < 3; ax++) for (sg = -1; sg <= 1; sg += 2) {
				VectorCopy(e0, q.start); VectorCopy(e0, q.end); q.end[ax] += sg * 8192.0f;
				run_query(&q); count++;
				/* and a tiny move */
				VectorCopy(e0, q.end); q.end[ax] += sg * 0.125f; run_query(&q); count++;
			} }
		}
		/* start in solid: from far outside the world to the empty point, and fully inside solid */
		VectorClear(q.mins); VectorClear(q.maxs);
		VectorSet(q.start, w->maxs[0] + 1024, w->maxs[1] + 1024, w->maxs[2] + 1024); VectorCopy(e0, q.end);
		run_query(&q); count++;
		VectorCopy(e0, q.start); VectorCopy(q.start, q.end); q.start[0] = w->mins[0] - 512; q.end[0] = w->mins[0] - 256;
		run_query(&q); count++;
		VectorSet(q.mins, -16, -16, -24); VectorSet(q.maxs, 16, 16, 32);
		VectorSet(q.start, w->mins[0] - 64, w->mins[1] - 64, w->mins[2] - 64); VectorCopy(e0, q.end);
		run_query(&q); count++;
	}
	/* point / leaf queries at every inline model origin and bounds corners */
	for (i = 1; i < numcmodels; i++) {
		memset(&q, 0, sizeof(q));
		q.kind = 2; q.headnode = map_cmodels[i].headnode;
		VectorAdd(map_cmodels[i].mins, map_cmodels[i].maxs, q.p); VectorScale(q.p, 0.5f, q.p);
		run_query(&q); count++;
		q.kind = 4; run_query(&q); count++;
	}

	/* ---- random mix ---- */
	while (count < n) {
		int sel = xr_range(&r, 100);
		memset(&q, 0, sizeof(q));
		q.mask = masks[xr_range(&r, NMASKS)];
		if (sel < 22) {                /* long random box traces */
			q.kind = 0;
			rand_point(&r, w, q.start, 64); rand_point(&r, w, q.end, 64);
			rand_hull(&r, q.mins, q.maxs);
		} else if (sel < 50) {         /* short traces from empty space */
			q.kind = 0;
			if (!empty_point(&r, w, q.start)) rand_point(&r, w, q.start, 0);
			for (k = 0; k < 3; k++) q.end[k] = q.start[k] + xr_float(&r, -384, 384);
			if (xr_range(&r, 4) == 0) q.end[0] = q.start[0], q.end[1] = q.start[1];  /* vertical */
			rand_hull(&r, q.mins, q.maxs);
		} else if (sel < 56) {         /* axial */
			int ax = xr_range(&r, 3);
			q.kind = 0;
			if (!empty_point(&r, w, q.start)) rand_point(&r, w, q.start, 0);
			VectorCopy(q.start, q.end); q.end[ax] += xr_float(&r, -1024, 1024);
			rand_hull(&r, q.mins, q.maxs);
		} else if (sel < 59) {         /* zero length */
			q.kind = 0;
			rand_point(&r, w, q.start, 0); VectorCopy(q.start, q.end);
			rand_hull(&r, q.mins, q.maxs);
		} else if ((sel < 64 || sel >= 94) && numcmodels > 1) { /* inline brush models, untransformed / transformed */
			int m = 1 + xr_range(&r, numcmodels - 1);
			const cmodel_t *c = &map_cmodels[m];
			q.kind = (sel < 64) ? 0 : 1;
			q.headnode = c->headnode;
			if (q.kind == 1) {
				if (xr_range(&r, 4)) for (k = 0; k < 3; k++) q.origin[k] = (float)(int)xr_float(&r, -96, 96);
				rand_angles(&r, q.angles);
			}
			for (k = 0; k < 3; k++) {
				q.start[k] = xr_float(&r, c->mins[k] - 96, c->maxs[k] + 96) + q.origin[k];
				q.end[k] = xr_float(&r, c->mins[k] - 96, c->maxs[k] + 96) + q.origin[k];
			}
			rand_hull(&r, q.mins, q.maxs);
		} else if (sel < 76) {         /* point contents, world or inline */
			q.kind = 2;
			rand_point(&r, w, q.p, 32);
			if (numcmodels > 1 && xr_range(&r, 4) == 0) {
				int m = 1 + xr_range(&r, numcmodels - 1);
				q.headnode = map_cmodels[m].headnode;
				for (k = 0; k < 3; k++) q.p[k] = xr_float(&r, map_cmodels[m].mins[k] - 16, map_cmodels[m].maxs[k] + 16);
			}
		} else if (sel < 82 && numcmodels > 1) { /* transformed point contents */
			int m = 1 + xr_range(&r, numcmodels - 1);
			q.kind = 3;
			q.headnode = map_cmodels[m].headnode;
			for (k = 0; k < 3; k++) q.origin[k] = (float)(int)xr_float(&r, -64, 64);
			rand_angles(&r, q.angles);
			for (k = 0; k < 3; k++) q.p[k] = xr_float(&r, map_cmodels[m].mins[k] - 32, map_cmodels[m].maxs[k] + 32) + q.origin[k];
		} else {                       /* leaf queries */
			q.kind = 4;
			rand_point(&r, w, q.p, 32);
		}
		run_query(&q); count++;
	}
}

static void cmd_trace_file(const char *map, const char *path)
{
	unsigned checksum;
	FILE *f = fopen(path, "r");
	char *line = NULL; size_t cap = 0; char err[256];
	if (!f) { fprintf(stderr, "cannot open %s\n", path); exit(2); }
	load_map(map, &checksum);
	while (getline(&line, &cap, f) > 0) {
		ojv_t *v, *qv; tq_t q; const char *k; int i;
		if (line[0] == '\n' || !line[0]) continue;
		v = oj_parse(line, err, sizeof(err));
		if (!v) { fprintf(stderr, "bad query: %s\n", err); exit(2); }
		qv = oj_get(v, "q"); if (!qv) qv = v;
		memset(&q, 0, sizeof(q));
		k = oj_string(oj_get(qv, "kind"), "box");
		for (i = 0; i < 5; i++) if (!strcmp(k, kindnames[i])) q.kind = i;
		os_read_vec3(oj_get(qv, "start"), q.start); os_read_vec3(oj_get(qv, "end"), q.end);
		os_read_vec3(oj_get(qv, "mins"), q.mins); os_read_vec3(oj_get(qv, "maxs"), q.maxs);
		os_read_vec3(oj_get(qv, "origin"), q.origin); os_read_vec3(oj_get(qv, "angles"), q.angles);
		os_read_vec3(oj_get(qv, "p"), q.p);
		q.headnode = oj_int(oj_get(qv, "headnode"), 0);
		q.mask = oj_int(oj_get(qv, "mask"), MASK_ALL);
		run_query(&q);
		oj_free(v);
	}
	fclose(f);
}

/* =================================== pmove =================================== */
static trace_t pm_trace(vec3_t start, vec3_t mins, vec3_t maxs, vec3_t end)
{
	trace_t t = CM_BoxTrace(start, end, mins, maxs, 0, MASK_PLAYERSOLID);
	if (t.fraction < 1.0)
		t.ent = (struct edict_s *)1;   /* as CL_PMTrace: world hits carry a non-NULL entity */
	return t;
}

static int pm_pointcontents(vec3_t p) { return CM_PointContents(p, 0); }

static void cmd_pmove(const char *map, const char *scenario)
{
	ojv_t *sc = oj_parse_file(scenario), *steps;
	unsigned checksum;
	pmove_state_t st;
	int i, have = 0;
	const char *m = oj_string(oj_get(sc, "map"), map);

	if (strcmp(m, map)) { fprintf(stderr, "scenario map %s != %s\n", m, map); exit(2); }
	load_map(map, &checksum);
	pm_airaccelerate = (float)oj_num(oj_get(sc, "airaccelerate"), 0);
	fprintf(out, "{\"header\":{\"map\":"); oj_str(out, map);
	fputs(",\"airaccelerate\":", out); oj_float(out, pm_airaccelerate); fputs("}}\n", out);

	steps = oj_get(sc, "steps");
	memset(&st, 0, sizeof(st));
	for (i = 0; steps && i < steps->n; i++) {
		ojv_t *s = steps->kids[i], *in = oj_get(s, "in");
		pmove_t pm;
		memset(&pm, 0, sizeof(pm));
		if (in && in->type == OJ_OBJ) { os_read_pms(in, &st); have = 1; }
		if (!have) { fprintf(stderr, "first pmove step needs \"in\"\n"); exit(2); }
		pm.s = st;
		os_read_uc(oj_get(s, "cmd"), &pm.cmd);
		pm.snapinitial = false;
		pm.trace = pm_trace;
		pm.pointcontents = pm_pointcontents;
		Pmove(&pm);

		fputs("{\"cmd\":", out); os_uc(out, &pm.cmd);
		if (in && in->type == OJ_OBJ) { fputs(",\"in\":", out); os_pms(out, &st); }
		fputs(",\"out\":{\"s\":", out); os_pms(out, &pm.s);
		fputs(",\"viewangles\":", out); oj_vec3(out, pm.viewangles);
		fputs(",\"viewheight\":", out); oj_float(out, pm.viewheight);
		fputs(",\"mins\":", out); oj_vec3(out, pm.mins);
		fputs(",\"maxs\":", out); oj_vec3(out, pm.maxs);
		fprintf(out, ",\"groundentity\":%d,\"watertype\":%d,\"waterlevel\":%d,\"numtouch\":%d}}\n",
			pm.groundentity ? 1 : 0, pm.watertype, pm.waterlevel, pm.numtouch);
		st = pm.s;
	}
	oj_free(sc);
}

/* ============================ msg ============================ */
/* body of SV_WritePlayerstateToClient (server/sv_ents.c) on two player_state_t */
static void write_playerstate(player_state_t *from, player_state_t *to, sizebuf_t *msg)
{
	int				i;
	int				pflags;
	player_state_t	*ps, *ops;
	player_state_t	dummy;
	int				statbits;

	ps = to;
	if (!from)
	{
		memset (&dummy, 0, sizeof(dummy));
		ops = &dummy;
	}
	else
		ops = from;

	pflags = 0;
	if (ps->pmove.pm_type != ops->pmove.pm_type)
		pflags |= PS_M_TYPE;
	if (ps->pmove.origin[0] != ops->pmove.origin[0]
		|| ps->pmove.origin[1] != ops->pmove.origin[1]
		|| ps->pmove.origin[2] != ops->pmove.origin[2] )
		pflags |= PS_M_ORIGIN;
	if (ps->pmove.velocity[0] != ops->pmove.velocity[0]
		|| ps->pmove.velocity[1] != ops->pmove.velocity[1]
		|| ps->pmove.velocity[2] != ops->pmove.velocity[2] )
		pflags |= PS_M_VELOCITY;
	if (ps->pmove.pm_time != ops->pmove.pm_time)
		pflags |= PS_M_TIME;
	if (ps->pmove.pm_flags != ops->pmove.pm_flags)
		pflags |= PS_M_FLAGS;
	if (ps->pmove.gravity != ops->pmove.gravity)
		pflags |= PS_M_GRAVITY;
	if (ps->pmove.delta_angles[0] != ops->pmove.delta_angles[0]
		|| ps->pmove.delta_angles[1] != ops->pmove.delta_angles[1]
		|| ps->pmove.delta_angles[2] != ops->pmove.delta_angles[2] )
		pflags |= PS_M_DELTA_ANGLES;
	if (ps->viewoffset[0] != ops->viewoffset[0]
		|| ps->viewoffset[1] != ops->viewoffset[1]
		|| ps->viewoffset[2] != ops->viewoffset[2] )
		pflags |= PS_VIEWOFFSET;
	if (ps->viewangles[0] != ops->viewangles[0]
		|| ps->viewangles[1] != ops->viewangles[1]
		|| ps->viewangles[2] != ops->viewangles[2] )
		pflags |= PS_VIEWANGLES;
	if (ps->kick_angles[0] != ops->kick_angles[0]
		|| ps->kick_angles[1] != ops->kick_angles[1]
		|| ps->kick_angles[2] != ops->kick_angles[2] )
		pflags |= PS_KICKANGLES;
	if (ps->blend[0] != ops->blend[0]
		|| ps->blend[1] != ops->blend[1]
		|| ps->blend[2] != ops->blend[2]
		|| ps->blend[3] != ops->blend[3] )
		pflags |= PS_BLEND;
	if (ps->fov != ops->fov)
		pflags |= PS_FOV;
	if (ps->rdflags != ops->rdflags)
		pflags |= PS_RDFLAGS;
	if (ps->gunframe != ops->gunframe)
		pflags |= PS_WEAPONFRAME;
	pflags |= PS_WEAPONINDEX;

	MSG_WriteByte (msg, svc_playerinfo);
	MSG_WriteShort (msg, pflags);
	if (pflags & PS_M_TYPE)
		MSG_WriteByte (msg, ps->pmove.pm_type);
	if (pflags & PS_M_ORIGIN)
	{
		MSG_WriteShort (msg, ps->pmove.origin[0]);
		MSG_WriteShort (msg, ps->pmove.origin[1]);
		MSG_WriteShort (msg, ps->pmove.origin[2]);
	}
	if (pflags & PS_M_VELOCITY)
	{
		MSG_WriteShort (msg, ps->pmove.velocity[0]);
		MSG_WriteShort (msg, ps->pmove.velocity[1]);
		MSG_WriteShort (msg, ps->pmove.velocity[2]);
	}
	if (pflags & PS_M_TIME)
		MSG_WriteByte (msg, ps->pmove.pm_time);
	if (pflags & PS_M_FLAGS)
		MSG_WriteByte (msg, ps->pmove.pm_flags);
	if (pflags & PS_M_GRAVITY)
		MSG_WriteShort (msg, ps->pmove.gravity);
	if (pflags & PS_M_DELTA_ANGLES)
	{
		MSG_WriteShort (msg, ps->pmove.delta_angles[0]);
		MSG_WriteShort (msg, ps->pmove.delta_angles[1]);
		MSG_WriteShort (msg, ps->pmove.delta_angles[2]);
	}
	if (pflags & PS_VIEWOFFSET)
	{
		MSG_WriteChar (msg, ps->viewoffset[0]*4);
		MSG_WriteChar (msg, ps->viewoffset[1]*4);
		MSG_WriteChar (msg, ps->viewoffset[2]*4);
	}
	if (pflags & PS_VIEWANGLES)
	{
		MSG_WriteAngle16 (msg, ps->viewangles[0]);
		MSG_WriteAngle16 (msg, ps->viewangles[1]);
		MSG_WriteAngle16 (msg, ps->viewangles[2]);
	}
	if (pflags & PS_KICKANGLES)
	{
		MSG_WriteChar (msg, ps->kick_angles[0]*4);
		MSG_WriteChar (msg, ps->kick_angles[1]*4);
		MSG_WriteChar (msg, ps->kick_angles[2]*4);
	}
	if (pflags & PS_WEAPONINDEX)
	{
		MSG_WriteByte (msg, ps->gunindex);
	}
	if (pflags & PS_WEAPONFRAME)
	{
		MSG_WriteByte (msg, ps->gunframe);
		MSG_WriteChar (msg, ps->gunoffset[0]*4);
		MSG_WriteChar (msg, ps->gunoffset[1]*4);
		MSG_WriteChar (msg, ps->gunoffset[2]*4);
		MSG_WriteChar (msg, ps->gunangles[0]*4);
		MSG_WriteChar (msg, ps->gunangles[1]*4);
		MSG_WriteChar (msg, ps->gunangles[2]*4);
	}
	if (pflags & PS_BLEND)
	{
		MSG_WriteByte (msg, ps->blend[0]*255);
		MSG_WriteByte (msg, ps->blend[1]*255);
		MSG_WriteByte (msg, ps->blend[2]*255);
		MSG_WriteByte (msg, ps->blend[3]*255);
	}
	if (pflags & PS_FOV)
		MSG_WriteByte (msg, ps->fov);
	if (pflags & PS_RDFLAGS)
		MSG_WriteByte (msg, ps->rdflags);

	statbits = 0;
	for (i=0 ; i<MAX_STATS ; i++)
		if (ps->stats[i] != ops->stats[i])
			statbits |= 1<<i;
	MSG_WriteLong (msg, statbits);
	for (i=0 ; i<MAX_STATS ; i++)
		if (statbits & (1<<i) )
			MSG_WriteShort (msg, ps->stats[i]);
}

static byte msgbuf[MAX_MSGLEN * 4];
static sizebuf_t sb;
static void sb_reset(void) { SZ_Init(&sb, msgbuf, sizeof(msgbuf)); }

/* random value helpers producing values that the C code handles without UB */
static float r_coord(xr_t *r)
{
	switch (xr_range(r, 5)) {
	case 0: return (float)((int)xr_range(r, 8192) - 4096);
	case 1: return (float)((int)xr_range(r, 65536) - 32768) * 0.125f;
	case 2: return 0;
	default: return xr_float(r, -4095.0f, 4095.0f);
	}
}
static float r_angle(xr_t *r)
{
	switch (xr_range(r, 4)) {
	case 0: return (float)(int)xr_range(r, 360);
	case 1: return 0;
	default: return xr_float(r, -720.0f, 720.0f);
	}
}
static int r_int_pick(xr_t *r, const int *vals, int n) { return vals[xr_range(r, n)]; }

static void rand_es(xr_t *r, entity_state_t *s)
{
	static const int bigs[] = { 0, 1, 255, 256, 257, 32767, 32768, 65535, 65536, 0x7fffffff, -1, -256 };
	int k;
	memset(s, 0, sizeof(*s));
	s->number = 1 + xr_range(r, 1023);
	for (k = 0; k < 3; k++) { s->origin[k] = r_coord(r); s->angles[k] = r_angle(r); s->old_origin[k] = r_coord(r); }
	s->modelindex = xr_range(r, 256); s->modelindex2 = xr_range(r, 256);
	s->modelindex3 = xr_range(r, 256); s->modelindex4 = xr_range(r, 256);
	s->frame = xr_range(r, 3) ? (int)xr_range(r, 256) : (int)xr_range(r, 65536);
	s->skinnum = xr_range(r, 3) ? (int)xr_range(r, 256) : r_int_pick(r, bigs, 12);
	s->effects = xr_range(r, 3) ? xr_range(r, 256) : xr_next(r);
	s->renderfx = xr_range(r, 3) ? (int)xr_range(r, 256) : (int)(xr_next(r) & 0xffff);
	if (xr_range(r, 6) == 0) s->renderfx |= RF_BEAM;
	s->solid = xr_range(r, 2) ? (int)xr_range(r, 65536) : 31;
	s->sound = xr_range(r, 256);
	s->event = xr_range(r, 2) ? 0 : (int)xr_range(r, 12);
}

static void mutate_es(xr_t *r, const entity_state_t *from, entity_state_t *to)
{
	entity_state_t t;
	int f;
	*to = *from;
	rand_es(r, &t);
	for (f = 0; f < 15; f++) {
		if (xr_range(r, 4)) continue;
		switch (f) {
		case 0: to->origin[0] = t.origin[0]; break;
		case 1: to->origin[1] = t.origin[1]; break;
		case 2: to->origin[2] = t.origin[2]; break;
		case 3: to->angles[0] = t.angles[0]; break;
		case 4: to->angles[1] = t.angles[1]; break;
		case 5: to->angles[2] = t.angles[2]; break;
		case 6: VectorCopy(t.old_origin, to->old_origin); break;
		case 7: to->modelindex = t.modelindex; to->modelindex2 = t.modelindex2; break;
		case 8: to->modelindex3 = t.modelindex3; to->modelindex4 = t.modelindex4; break;
		case 9: to->frame = t.frame; break;
		case 10: to->skinnum = t.skinnum; break;
		case 11: to->effects = t.effects; break;
		case 12: to->renderfx = t.renderfx; break;
		case 13: to->solid = t.solid; to->sound = t.sound; break;
		case 14: to->event = t.event; break;
		}
	}
	if (xr_range(r, 3) == 0) to->event = 0;
}

static void msg_entity(xr_t *r, int n)
{
	int i;
	for (i = 0; i < n; i++) {
		entity_state_t from, to;
		int force = xr_range(r, 4) == 0, newent = xr_range(r, 5) == 0;
		int mode = xr_range(r, 10);
		if (mode == 0) { memset(&from, 0, sizeof(from)); rand_es(r, &to); }  /* from null state (baseline) */
		else if (mode == 1) { rand_es(r, &from); to = from; }                   /* unchanged */
		else { rand_es(r, &from); mutate_es(r, &from, &to); }
		if (xr_range(r, 3) == 0) to.number = 256 + xr_range(r, MAX_EDICTS - 256);  /* U_NUMBER16 */
		from.number = to.number;
		sb_reset();
		MSG_WriteDeltaEntity(&from, &to, &sb, force, newent);
		fputs("{\"from\":", out); os_es(out, &from);
		fputs(",\"to\":", out); os_es(out, &to);
		fprintf(out, ",\"force\":%d,\"newentity\":%d,\"bytes\":", force, newent);
		oj_hex(out, sb.data, sb.cursize); fputs("}\n", out);
	}
}

static void rand_uc(xr_t *r, usercmd_t *c)
{
	static const int moves[] = { 0, 0, 200, -200, 400, -400, 100, 32767, -32768, 1 };
	int k;
	c->msec = xr_range(r, 3) ? (byte)(1 + xr_range(r, 250)) : (byte)xr_range(r, 256);
	c->buttons = xr_range(r, 2) ? (byte)(xr_range(r, 4) ? 0 : 1) : (byte)xr_range(r, 256);
	for (k = 0; k < 3; k++) c->angles[k] = xr_range(r, 3) ? (short)(xr_next(r) & 0xffff) : 0;
	c->forwardmove = xr_range(r, 2) ? moves[xr_range(r, 10)] : (short)(xr_next(r) & 0xffff);
	c->sidemove = xr_range(r, 2) ? moves[xr_range(r, 10)] : (short)(xr_next(r) & 0xffff);
	c->upmove = xr_range(r, 2) ? moves[xr_range(r, 10)] : (short)(xr_next(r) & 0xffff);
	c->impulse = xr_range(r, 4) ? 0 : (byte)xr_range(r, 256);
	c->lightlevel = (byte)xr_range(r, 256);
}

static void msg_usercmd(xr_t *r, int n)
{
	int i, k;
	for (i = 0; i < n; i++) {
		usercmd_t from, to;
		memset(&from, 0, sizeof(from));
		if (xr_range(r, 5)) rand_uc(r, &from);
		to = from;
		if (xr_range(r, 6) == 0) rand_uc(r, &to);
		else {
			usercmd_t t; rand_uc(r, &t);
			for (k = 0; k < 9; k++) if (xr_range(r, 3) == 0) switch (k) {
				case 0: to.msec = t.msec; break; case 1: to.buttons = t.buttons; break;
				case 2: to.angles[0] = t.angles[0]; break; case 3: to.angles[1] = t.angles[1]; break;
				case 4: to.angles[2] = t.angles[2]; break; case 5: to.forwardmove = t.forwardmove; break;
				case 6: to.sidemove = t.sidemove; break; case 7: to.upmove = t.upmove; break;
				case 8: to.impulse = t.impulse; to.lightlevel = t.lightlevel; break;
			}
		}
		sb_reset();
		MSG_WriteDeltaUsercmd(&sb, &from, &to);
		fputs("{\"from\":", out); os_uc(out, &from);
		fputs(",\"to\":", out); os_uc(out, &to);
		fputs(",\"bytes\":", out); oj_hex(out, sb.data, sb.cursize); fputs("}\n", out);
	}
}

static float r_small(xr_t *r, float lim)   /* values with *4 inside signed char range */
{
	switch (xr_range(r, 3)) {
	case 0: return 0;
	case 1: return (float)((int)xr_range(r, 256) - 128) * 0.25f;
	default: return xr_float(r, -lim, lim);
	}
}

static void rand_ps(xr_t *r, player_state_t *ps)
{
	int k;
	memset(ps, 0, sizeof(*ps));
	ps->pmove.pm_type = (pmtype_t)xr_range(r, 5);
	for (k = 0; k < 3; k++) {
		ps->pmove.origin[k] = (short)(xr_next(r) & 0xffff);
		ps->pmove.velocity[k] = xr_range(r, 2) ? (short)(xr_next(r) & 0xffff) : 0;
		ps->pmove.delta_angles[k] = xr_range(r, 2) ? (short)(xr_next(r) & 0xffff) : 0;
		ps->viewangles[k] = r_angle(r);
		ps->viewoffset[k] = r_small(r, 31.0f);
		ps->kick_angles[k] = r_small(r, 31.0f);
		ps->gunangles[k] = r_small(r, 31.0f);
		ps->gunoffset[k] = r_small(r, 31.0f);
	}
	ps->pmove.pm_flags = (byte)xr_range(r, 256);
	ps->pmove.pm_time = (byte)xr_range(r, 256);
	ps->pmove.gravity = xr_range(r, 2) ? 800 : (short)(xr_next(r) & 0xffff);
	ps->gunindex = xr_range(r, 256);
	ps->gunframe = xr_range(r, 256);
	for (k = 0; k < 4; k++) ps->blend[k] = xr_range(r, 2) ? 0 : xr_float(r, 0, 1);
	ps->fov = xr_range(r, 2) ? 90 : (float)(1 + xr_range(r, 179));
	ps->rdflags = xr_range(r, 4);
	for (k = 0; k < MAX_STATS; k++) ps->stats[k] = xr_range(r, 2) ? 0 : (short)(xr_next(r) & 0xffff);
}

static void msg_player(xr_t *r, int n)
{
	int i, k;
	for (i = 0; i < n; i++) {
		player_state_t from, to, t;
		int hasfrom = xr_range(r, 6) != 0;
		rand_ps(r, &from);
		to = from;
		rand_ps(r, &t);
		if (xr_range(r, 5) == 0) to = t;
		else for (k = 0; k < 18; k++) if (xr_range(r, 3) == 0) switch (k) {
			case 0: to.pmove.pm_type = t.pmove.pm_type; break;
			case 1: VectorCopy(t.pmove.origin, to.pmove.origin); break;
			case 2: to.pmove.origin[xr_range(r, 3)] = t.pmove.origin[0]; break;
			case 3: VectorCopy(t.pmove.velocity, to.pmove.velocity); break;
			case 4: to.pmove.pm_time = t.pmove.pm_time; break;
			case 5: to.pmove.pm_flags = t.pmove.pm_flags; break;
			case 6: to.pmove.gravity = t.pmove.gravity; break;
			case 7: VectorCopy(t.pmove.delta_angles, to.pmove.delta_angles); break;
			case 8: VectorCopy(t.viewoffset, to.viewoffset); break;
			case 9: VectorCopy(t.viewangles, to.viewangles); break;
			case 10: VectorCopy(t.kick_angles, to.kick_angles); break;
			case 11: to.gunindex = t.gunindex; break;
			case 12: to.gunframe = t.gunframe; VectorCopy(t.gunoffset, to.gunoffset); VectorCopy(t.gunangles, to.gunangles); break;
			case 13: memcpy(to.blend, t.blend, sizeof(to.blend)); break;
			case 14: to.fov = t.fov; break;
			case 15: to.rdflags = t.rdflags; break;
			case 16: to.stats[xr_range(r, MAX_STATS)] = t.stats[0]; break;
			case 17: memcpy(to.stats, t.stats, sizeof(to.stats)); break;
		}
		sb_reset();
		write_playerstate(hasfrom ? &from : NULL, &to, &sb);
		fputs("{\"from\":", out);
		if (hasfrom) os_ps(out, &from); else fputs("null", out);
		fputs(",\"to\":", out); os_ps(out, &to);
		fputs(",\"bytes\":", out); oj_hex(out, sb.data, sb.cursize); fputs("}\n", out);
	}
}

static void sc_begin(const char *op) { sb_reset(); fprintf(out, "{\"op\":\"%s\",\"v\":", op); }
static void sc_end(void) { fputs(",\"bytes\":", out); oj_hex(out, sb.data, sb.cursize); fputs("}\n", out); }

static void msg_scalar(xr_t *r)
{
	static const int ints[] = { 0, 1, -1, 2, 127, 128, -128, -129, 255, 256, 32767, 32768, -32768, -32769, 65535, 65536,
		0x7fffffff, (int)0x80000000, 1000, -1000, 0x12345678, 70000, -70000 };
	static const float fl[] = { 0.0f, -0.0f, 1.0f, -1.0f, 0.5f, 0.125f, 0.1f, 1e-10f, 3.4e38f, -3.4e38f, 1.17549435e-38f,
		4095.875f, -4096.0f, 4095.9f, 359.9f, 360.0f, 180.0f, -180.0f, 45.0f, 90.0f, 1.40129846e-45f };
	static const char *strs[] = { "", "a", "hello world", "\\name\\player\\skin\\male/grunt", "line\nbreak",
		"quote\"q", "tab\tx", "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ !#$%&'()*+,-./:;<=>?@[]^_`{|}~" };
	int i, k, j;
	const char *ops[] = { "coord", "angle", "angle16", "float" };

	for (i = 0; i < (int)(sizeof(ints) / sizeof(ints[0])); i++) {
		sc_begin("long"); fprintf(out, "%d", ints[i]); MSG_WriteLong(&sb, ints[i]); sc_end();
		sc_begin("short"); fprintf(out, "%d", ints[i]); MSG_WriteShort(&sb, ints[i]); sc_end();
		sc_begin("char"); fprintf(out, "%d", ints[i]); MSG_WriteChar(&sb, ints[i]); sc_end();
		sc_begin("byte"); fprintf(out, "%d", ints[i]); MSG_WriteByte(&sb, ints[i]); sc_end();
	}
	for (i = 0; i < 400; i++) {
		int v = (int)xr_next(r);
		if (xr_range(r, 2)) v = (int)xr_range(r, 70000) - 35000;
		sc_begin("long"); fprintf(out, "%d", v); MSG_WriteLong(&sb, v); sc_end();
		sc_begin("short"); fprintf(out, "%d", v); MSG_WriteShort(&sb, v); sc_end();
		sc_begin("char"); fprintf(out, "%d", v); MSG_WriteChar(&sb, v); sc_end();
		sc_begin("byte"); fprintf(out, "%d", v); MSG_WriteByte(&sb, v); sc_end();
	}
	/* float-valued ops: edge values (within each op's defined range) then random */
	for (i = 0; i < (int)(sizeof(fl) / sizeof(fl[0])); i++) {
		float v = fl[i];
		sc_begin("float"); oj_float(out, v); MSG_WriteFloat(&sb, v); sc_end();
		if (fabs(v) < 4096) { sc_begin("coord"); oj_float(out, v); MSG_WriteCoord(&sb, v); sc_end(); }
		if (fabs(v) < 1e6) {
			sc_begin("angle"); oj_float(out, v); MSG_WriteAngle(&sb, v); sc_end();
			sc_begin("angle16"); oj_float(out, v); MSG_WriteAngle16(&sb, v); sc_end();
		}
	}
	for (k = 0; k < 4; k++) for (i = 0; i < 1000; i++) {
		float v;
		if (k == 0) v = r_coord(r);
		else if (k == 3) { unsigned u = xr_next(r); memcpy(&v, &u, 4); if (v != v || fabs(v) > 3.4e38f) v = xr_float(r, -1e6f, 1e6f); }
		else v = (xr_range(r, 2) ? xr_float(r, -1080, 1080) : (float)((int)xr_range(r, 2160) - 1080) * 0.5f);
		sc_begin(ops[k]); oj_float(out, v);
		if (k == 0) MSG_WriteCoord(&sb, v);
		else if (k == 1) MSG_WriteAngle(&sb, v);
		else if (k == 2) MSG_WriteAngle16(&sb, v);
		else MSG_WriteFloat(&sb, v);
		sc_end();
	}
	for (i = 0; i < 1000; i++) {
		vec3_t p; for (j = 0; j < 3; j++) p[j] = r_coord(r);
		sc_begin("pos"); oj_vec3(out, p); MSG_WritePos(&sb, p); sc_end();
	}
	/* dirs: all 162 bytedirs exactly, negated, then random (normalized and not) vectors */
	for (i = 0; i < NUMVERTEXNORMALS; i++) {
		vec3_t d; VectorCopy(bytedirs[i], d);
		sc_begin("dir"); oj_vec3(out, d); MSG_WriteDir(&sb, d); sc_end();
		VectorNegate(bytedirs[i], d);
		sc_begin("dir"); oj_vec3(out, d); MSG_WriteDir(&sb, d); sc_end();
	}
	sc_begin("dir"); fputs("null", out); MSG_WriteDir(&sb, NULL); sc_end();
	for (i = 0; i < 2000; i++) {
		vec3_t d; for (j = 0; j < 3; j++) d[j] = xr_float(r, -1, 1);
		if (xr_range(r, 4)) VectorNormalize(d);
		else if (xr_range(r, 2)) VectorScale(d, xr_float(r, 0, 100), d);
		sc_begin("dir"); oj_vec3(out, d); MSG_WriteDir(&sb, d); sc_end();
	}
	for (i = 0; i < (int)(sizeof(strs) / sizeof(strs[0])); i++) {
		sc_begin("string"); oj_str(out, strs[i]); MSG_WriteString(&sb, (char *)strs[i]); sc_end();
	}
	for (i = 0; i < 200; i++) {
		char s[128]; int len = xr_range(r, 100);
		for (j = 0; j < len; j++) s[j] = (char)(32 + xr_range(r, 95));
		s[len] = 0;
		sc_begin("string"); oj_str(out, s); MSG_WriteString(&sb, s); sc_end();
	}
}

static void rand_block(xr_t *r, byte *b, int n)
{
	int i, mode = xr_range(r, 4);
	for (i = 0; i < n; i++) b[i] = mode == 0 ? 0 : mode == 1 ? (byte)i : (byte)xr_next(r);
}

static void msg_crc(xr_t *r)
{
	byte b[256];
	int i, n;
	static const int seqs[] = { 0, 1, 2, 3, 1019, 1020, 1021, 1022, 1023, 1024, 2040, 65535, 65536, 0x7fffffff };
	for (i = 0; i < 1500; i++) {
		int seq;
		n = (i < 100) ? i : (int)xr_range(r, 100);
		rand_block(r, b, n);
		seq = (i < 14) ? seqs[i] : (xr_range(r, 2) ? (int)xr_range(r, 5000) : (int)(xr_next(r) & 0x7fffffff));
		fputs("{\"base\":", out); oj_hex(out, b, n);
		fprintf(out, ",\"sequence\":%d,\"crc\":%d}\n", seq, COM_BlockSequenceCRCByte(b, n, seq));
	}
	for (i = 0; i < 1000; i++) {
		n = (i < 64) ? i : (int)xr_range(r, 257);
		rand_block(r, b, n);
		fputs("{\"block\":", out); oj_hex(out, b, n);
		fprintf(out, ",\"crc16\":%d}\n", CRC_Block(b, n));
	}
}

static void msg_md4(xr_t *r)
{
	static byte b[4096];
	int i, n;
	for (i = 0; i < 600; i++) {
		n = (i < 130) ? i : (int)xr_range(r, 4097);
		rand_block(r, b, n);
		fputs("{\"block\":", out); oj_hex(out, b, n);
		fprintf(out, ",\"checksum\":%u}\n", (unsigned)Com_BlockChecksum(b, n));
	}
}

static void cmd_msg(const char *kind, unsigned seed)
{
	xr_t r;
	xr_seed(&r, seed);
	oracle_init(g_basedir, 0, NULL);
	if (!strcmp(kind, "entity")) msg_entity(&r, 6000);
	else if (!strcmp(kind, "usercmd")) msg_usercmd(&r, 4000);
	else if (!strcmp(kind, "player")) msg_player(&r, 3000);
	else if (!strcmp(kind, "scalar")) msg_scalar(&r);
	else if (!strcmp(kind, "crc")) msg_crc(&r);
	else if (!strcmp(kind, "md4")) msg_md4(&r);
	else { fprintf(stderr, "unknown msg kind %s\n", kind); exit(2); }
}

/* =================================== rand =================================== */
static void cmd_rand(unsigned seed, int n)
{
	int i;
	srand(seed);
	fprintf(out, "{\"seed\":%u,\"values\":[", seed);
	for (i = 0; i < n; i++) fprintf(out, i ? (i % 20 ? ",%d" : ",\n%d") : "%d", rand());
	fputs("]}\n", out);
}

/* =================================== pak =================================== */
static int le32(const void *p) { const unsigned char *b = p; return (int)(b[0] | (b[1] << 8) | (b[2] << 16) | ((unsigned)b[3] << 24)); }

static void cmd_pak(const char *path)
{
	FILE *f = fopen(path, "rb");
	dpackheader_t h;
	dpackfile_t *d;
	int i, n;
	if (!f) { fprintf(stderr, "cannot open %s\n", path); exit(2); }
	if (fread(&h, sizeof(h), 1, f) != 1 || le32(&h.ident) != IDPAKHEADER) { fprintf(stderr, "not a pak\n"); exit(2); }
	n = le32(&h.dirlen) / sizeof(dpackfile_t);
	d = malloc(n * sizeof(*d));
	fseek(f, le32(&h.dirofs), SEEK_SET);
	if (fread(d, sizeof(*d), n, f) != (size_t)n) { fprintf(stderr, "short pak dir\n"); exit(2); }
	fputs("{\"files\":[", out);
	for (i = 0; i < n; i++) {
		int pos = le32(&d[i].filepos), len = le32(&d[i].filelen);
		unsigned char *buf = malloc(len > 0 ? len : 1);
		char sha[65], name[57];
		fseek(f, pos, SEEK_SET);
		if (len > 0 && fread(buf, 1, len, f) != (size_t)len) { fprintf(stderr, "short read\n"); exit(2); }
		sha256_hex(buf, len, sha);
		memcpy(name, d[i].name, 56); name[56] = 0;
		fputs(i ? ",\n{\"name\":" : "\n{\"name\":", out); oj_str(out, name);
		fprintf(out, ",\"filepos\":%d,\"filelen\":%d,\"sha256\":\"%s\"}", pos, len, sha);
		free(buf);
	}
	fputs("]}\n", out);
	fclose(f);
	free(d);
}

/* =================================== main =================================== */
static void usage(void)
{
	fprintf(stderr,
		"usage: oracle_core [--basedir DIR] [-o OUT] <cmd>\n"
		"  bsp <map>\n"
		"  trace <map> <queries.jsonl> | trace <map> --random N --seed S\n"
		"  pmove <map> <scenario.json>\n"
		"  msg entity|usercmd|player|scalar|crc|md4 [--seed S]\n"
		"  rand [--seed S] [--count N]\n"
		"  pak <pakfile>\n");
	exit(2);
}

int main(int argc, char **argv)
{
	int i, n = 0;
	char *args[16];
	const char *outpath = NULL, *basedir = NULL;
	unsigned seed = 1; int count = 100000, random_n = -1;

	for (i = 1; i < argc; i++) {
		if (!strcmp(argv[i], "--basedir") && i + 1 < argc) basedir = argv[++i];
		else if (!strcmp(argv[i], "-o") && i + 1 < argc) outpath = argv[++i];
		else if (!strcmp(argv[i], "--seed") && i + 1 < argc) seed = (unsigned)strtoul(argv[++i], NULL, 0);
		else if (!strcmp(argv[i], "--count") && i + 1 < argc) count = atoi(argv[++i]);
		else if (!strcmp(argv[i], "--random") && i + 1 < argc) random_n = atoi(argv[++i]);
		else if (n < 16) args[n++] = argv[i];
	}
	if (n < 1) usage();
	g_basedir = oracle_basedir(basedir);
	out = outpath ? fopen(outpath, "w") : stdout;
	if (!out) { perror(outpath); return 2; }
	setvbuf(out, NULL, _IOFBF, 1 << 20);

	if (!strcmp(args[0], "bsp") && n == 2) cmd_bsp(args[1]);
	else if (!strcmp(args[0], "trace") && n == 2 && random_n > 0) cmd_trace_random(args[1], random_n, seed);
	else if (!strcmp(args[0], "trace") && n == 3) cmd_trace_file(args[1], args[2]);
	else if (!strcmp(args[0], "pmove") && n == 3) cmd_pmove(args[1], args[2]);
	else if (!strcmp(args[0], "msg") && n == 2) cmd_msg(args[1], seed);
	else if (!strcmp(args[0], "rand")) cmd_rand(seed, count);
	else if (!strcmp(args[0], "pak") && n == 2) cmd_pak(args[1]);
	else usage();

	if (fflush(out) || (outpath && fclose(out))) { perror("write"); return 2; }
	return 0;
}
