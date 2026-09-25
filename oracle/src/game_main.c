/* game_main.c -- oracle_game / oracle_game_ctf: headless, deterministic game runs (docs/FIXTURES.md "game/").
 *
 * Engine path: real Qcommon_Init, real SV_InitGame (-> SV_InitGameProgs -> ge->Init), srand(seed), real
 * SV_SpawnServer (SpawnEntities + 2 RunFrame), scripted clients through the real connect path
 * (ge->ClientConnect, SV_UserinfoChanged, "new"/"begin" via SV_ExecuteUserCommand). Each frame then does what
 * SV_Frame does for game state, minus networking and minus its rand() call:
 *     inputs (SV_ClientThink / SV_ExecuteUserCommand) -> sv.framenum++, sv.time, ge->RunFrame()
 *     -> dump -> SV_PrepWorldFrame (clears s.event)
 * The game_import_t table is wrapped (after SV_InitGameProgs filled it) to capture events; rand() is interposed
 * (exported from the executable) to count calls made by both engine and game module.
 * Usage: oracle_game [--basedir DIR] [--game-so PATH] [-o OUT] scenario.json */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <unistd.h>
#include "server/server.h"
#include "ojson.h"
#include "ostate.h"

void oracle_init(const char *basedir, int nkv, const char **kv);
const char *oracle_basedir(const char *arg);
extern char oracle_game_path[1024];
extern void *oracle_game_library;
extern void (*oracle_wrap_import)(void *import);
int elfsym_load(void *anchor);
void SV_ClientThink(client_t *cl, usercmd_t *cmd);

int gd_edict_size(void);
void gd_level(FILE *f, void *levelp);
void gd_edicts(FILE *f, void *edicts, int num_edicts, int full);
static int full_edicts;
void gd_clients(FILE *f, void *edicts, const int *slots, int nslots);

const char *gd_module(void);

static void o_strncpyz(char *d, char *s, int n) { strncpy(d, s, n - 1); d[n - 1] = 0; }

/* ================= rand interposition ================= */
static unsigned long rand_calls;
static int (*libc_rand)(void);

int rand(void)
{
	if (!libc_rand) libc_rand = (int (*)(void))dlsym(RTLD_NEXT, "rand");
	rand_calls++;
	return libc_rand();
}

/* ================= event capture ================= */
static FILE *ev;           /* memstream for the current frame's events */
static char *evbuf;
static size_t evlen;
static int nev;

static void ev_open(void) { ev = open_memstream(&evbuf, &evlen); nev = 0; }
static void ev_sep(void) { if (nev++) fputc(',', ev); }

static game_import_t real;

static int ent_num(edict_t *e) { return e ? NUM_FOR_EDICT(e) : -1; }

static void w_sound(edict_t *ent, int channel, int soundindex, float volume, float attenuation, float timeofs)
{
	ev_sep();
	fprintf(ev, "{\"t\":\"sound\",\"ent\":%d,\"channel\":%d,\"sound\":", ent_num(ent), channel);
	oj_str(ev, (soundindex >= 0 && soundindex < MAX_SOUNDS) ? sv.configstrings[CS_SOUNDS + soundindex] : "");
	fputs(",\"volume\":", ev); oj_float(ev, volume);
	fputs(",\"attenuation\":", ev); oj_float(ev, attenuation);
	fputs(",\"timeofs\":", ev); oj_float(ev, timeofs);
	fputc('}', ev);
	real.sound(ent, channel, soundindex, volume, attenuation, timeofs);
}

static void w_positioned_sound(vec3_t origin, edict_t *ent, int channel, int soundindex, float volume,
	float attenuation, float timeofs)
{
	ev_sep();
	fprintf(ev, "{\"t\":\"positioned_sound\",\"ent\":%d,\"channel\":%d,\"sound\":", ent_num(ent), channel);
	oj_str(ev, (soundindex >= 0 && soundindex < MAX_SOUNDS) ? sv.configstrings[CS_SOUNDS + soundindex] : "");
	fputs(",\"volume\":", ev); oj_float(ev, volume);
	fputs(",\"attenuation\":", ev); oj_float(ev, attenuation);
	fputs(",\"timeofs\":", ev); oj_float(ev, timeofs);
	fputs(",\"origin\":", ev);
	if (origin) oj_vec3(ev, origin); else fputs("null", ev);
	fputc('}', ev);
	real.positioned_sound(origin, ent, channel, soundindex, volume, attenuation, timeofs);
}

static void w_configstring(int index, char *val)
{
	ev_sep();
	fprintf(ev, "{\"t\":\"configstring\",\"index\":%d,\"value\":", index);
	oj_str(ev, val ? val : "");
	fputc('}', ev);
	real.configstring(index, val);
}

static void ev_print(const char *kind, int ent, int level, const char *text)
{
	ev_sep();
	fprintf(ev, "{\"t\":\"print\",\"kind\":\"%s\",\"ent\":%d,\"level\":%d,\"text\":", kind, ent, level);
	oj_str(ev, text);
	fputc('}', ev);
}

static void w_bprintf(int printlevel, char *fmt, ...)
{
	char msg[4096]; va_list ap;
	va_start(ap, fmt); vsnprintf(msg, sizeof(msg), fmt, ap); va_end(ap);
	ev_print("bprintf", -1, printlevel, msg);
	real.bprintf(printlevel, "%s", msg);
}

static void w_dprintf(char *fmt, ...)
{
	char msg[4096]; va_list ap;
	va_start(ap, fmt); vsnprintf(msg, sizeof(msg), fmt, ap); va_end(ap);
	ev_print("dprintf", -1, 0, msg);
	real.dprintf("%s", msg);
}

static void w_cprintf(edict_t *ent, int printlevel, char *fmt, ...)
{
	char msg[4096]; va_list ap;
	va_start(ap, fmt); vsnprintf(msg, sizeof(msg), fmt, ap); va_end(ap);
	ev_print("cprintf", ent_num(ent), printlevel, msg);
	real.cprintf(ent, printlevel, "%s", msg);
}

static void w_centerprintf(edict_t *ent, char *fmt, ...)
{
	char msg[4096]; va_list ap;
	va_start(ap, fmt); vsnprintf(msg, sizeof(msg), fmt, ap); va_end(ap);
	ev_print("centerprintf", ent_num(ent), 0, msg);
	real.centerprintf(ent, "%s", msg);
}

static void w_error(char *fmt, ...)
{
	char msg[4096]; va_list ap;
	va_start(ap, fmt); vsnprintf(msg, sizeof(msg), fmt, ap); va_end(ap);
	fprintf(stderr, "oracle_game: game error: %s\n", msg);
	exit(4);
}

static void w_multicast(vec3_t origin, multicast_t to)
{
	ev_sep();
	fputs("{\"t\":\"multicast\",\"origin\":", ev);
	if (origin) oj_vec3(ev, origin); else fputs("null", ev);
	fprintf(ev, ",\"to\":%d,\"bytes\":", (int)to);
	oj_hex(ev, sv.multicast.data, sv.multicast.cursize);
	fputc('}', ev);
	real.multicast(origin, to);
}

static void w_unicast(edict_t *ent, qboolean reliable)
{
	ev_sep();
	fprintf(ev, "{\"t\":\"unicast\",\"ent\":%d,\"reliable\":%d,\"bytes\":", ent_num(ent), reliable ? 1 : 0);
	oj_hex(ev, sv.multicast.data, sv.multicast.cursize);
	fputc('}', ev);
	real.unicast(ent, reliable);
}

/* recorded, never executed: the oracle does not run the command buffer (no map changes etc.) */
static void w_addcommandstring(char *text)
{
	ev_sep();
	fputs("{\"t\":\"cmd\",\"text\":", ev);
	oj_str(ev, text);
	fputc('}', ev);
}

static void wrap_import(void *p)
{
	game_import_t *gi = p;
	real = *gi;
	gi->sound = w_sound;
	gi->positioned_sound = w_positioned_sound;
	gi->configstring = w_configstring;
	gi->bprintf = w_bprintf;
	gi->dprintf = w_dprintf;
	gi->cprintf = w_cprintf;
	gi->centerprintf = w_centerprintf;
	gi->error = w_error;
	gi->multicast = w_multicast;
	gi->unicast = w_unicast;
	gi->AddCommandString = w_addcommandstring;
}

/* entity string override: substitute the string handed to SpawnEntities */
static const char *entstring_override;
static void (*real_spawnentities)(char *mapname, char *entstring, char *spawnpoint);
static void w_spawnentities(char *mapname, char *entstring, char *spawnpoint)
{
	real_spawnentities(mapname, entstring_override ? (char *)entstring_override : entstring, spawnpoint);
}

/* ================= scripted input ================= */
typedef struct {
	int active, client, start, until;
	xr_t r;
	short yaw, pitch;
} rwalk_t;

#define MAX_WALKS 64
static rwalk_t walks[MAX_WALKS];
static int nwalks;

/* Deterministic random walk (docs/FIXTURES.md). One usercmd per frame. */
static void rwalk_cmd(rwalk_t *w, usercmd_t *c)
{
	static const short fwd[8] = { 400, 400, 400, 200, 0, -200, 400, 400 };
	static const short side[8] = { 0, 0, 0, 0, 200, -200, 400, -400 };
	uint32_t a = xr_next(&w->r), b = xr_next(&w->r), up;
	w->yaw = (short)(w->yaw + ((int)(a & 0x3ff) - 512));
	if (((a >> 10) & 31) == 0) w->yaw = (short)(w->yaw + 8192);
	w->pitch = (short)((int)((a >> 16) & 0xfff) - 2048);
	memset(c, 0, sizeof(*c));
	c->msec = 100;
	c->lightlevel = 128;
	c->angles[0] = w->pitch;
	c->angles[1] = w->yaw;
	c->forwardmove = fwd[b & 7];
	c->sidemove = side[(b >> 3) & 7];
	up = (b >> 6) & 31;
	c->upmove = up < 2 ? 200 : (up == 2 ? -200 : 0);
	if (((b >> 11) & 7) == 0) c->buttons = BUTTON_ATTACK | BUTTON_ANY;
}

static FILE *out;
static int ninputs;
static FILE *inp; static char *inpbuf; static size_t inplen;

static void input_cmd(int client, usercmd_t *c)
{
	if (ninputs++) fputc(',', inp);
	fprintf(inp, "{\"client\":%d,\"cmd\":", client); os_uc(inp, c); fputc('}', inp);
	if (svs.clients[client].state == cs_spawned)
		SV_ClientThink(&svs.clients[client], c);
}

static void input_command(int client, const char *text)
{
	char buf[1024];
	if (ninputs++) fputc(',', inp);
	fprintf(inp, "{\"client\":%d,\"command\":", client); oj_str(inp, text); fputc('}', inp);
	o_strncpyz(buf, (char *)text, sizeof(buf));
	sv_client = &svs.clients[client];
	sv_player = sv_client->edict;
	SV_ExecuteUserCommand(buf);
}

/* ================= main ================= */
static void exe_dir(char *buf, size_t n)
{
	ssize_t k = readlink("/proc/self/exe", buf, n - 1);
	char *s;
	if (k <= 0) { strcpy(buf, "."); return; }
	buf[k] = 0;
	s = strrchr(buf, '/');
	if (s) *s = 0;
}

static void clear_client_buffers(void)
{
	int i;
	for (i = 0; i < maxclients->value; i++) {
		SZ_Clear(&svs.clients[i].netchan.message);
		SZ_Clear(&svs.clients[i].datagram);
	}
}

static void begin_frame(void)
{
	ev_open();
	inp = open_memstream(&inpbuf, &inplen);
	ninputs = 0;
	rand_calls = 0;
}

static void dump_frame(int frame, void *levelp, const int *slots, int nslots)
{
	fclose(ev);
	fclose(inp);
	fprintf(out, "{\"frame\":%d,\"level\":", frame);
	gd_level(out, levelp);
	fprintf(out, ",\"rand_calls\":%lu,\"inputs\":[%s],", rand_calls, inpbuf);
	gd_edicts(out, ge->edicts, ge->num_edicts, full_edicts || frame == 0);
	fputs(",\"clients\":", out);
	gd_clients(out, ge->edicts, slots, nslots);
	fprintf(out, ",\"events\":[%s]}\n", evbuf);
	free(evbuf); evbuf = NULL;
	free(inpbuf); inpbuf = NULL;
}

int main(int argc, char **argv)
{
	const char *basedir = NULL, *outpath = NULL, *scpath = NULL, *gameso = NULL;
	ojv_t *sc, *cv, *clients, *sched;
	const char *kv[256];
	int nkv = 0, i, f, frames, nclients, slots[MAX_CLIENTS];
	const char *map, *module;
	unsigned seed;
	void *levelp;
	char dir[1024];

	for (i = 1; i < argc; i++) {
		if (!strcmp(argv[i], "--basedir") && i + 1 < argc) basedir = argv[++i];
		else if (!strcmp(argv[i], "-o") && i + 1 < argc) outpath = argv[++i];
		else if (!strcmp(argv[i], "--game-so") && i + 1 < argc) gameso = argv[++i];
		else if (!strcmp(argv[i], "--full")) full_edicts = 1;
		else scpath = argv[i];
	}
	if (!scpath) { fprintf(stderr, "usage: oracle_game [--basedir DIR] [--game-so PATH] [-o OUT] scenario.json\n"); return 2; }
	sc = oj_parse_file(scpath);
	map = oj_string(oj_get(sc, "map"), "demo1");
	module = oj_string(oj_get(sc, "module"), "baseq2");
	seed = (unsigned)oj_num(oj_get(sc, "seed"), 1);
	frames = oj_int(oj_get(sc, "frames"), 100);
	entstring_override = oj_string(oj_get(sc, "entstring_override"), NULL);
	clients = oj_get(sc, "clients");
	sched = oj_get(sc, "schedule");
	nclients = clients && clients->type == OJ_ARR ? clients->n : 0;
	if (strcmp(module, gd_module())) { fprintf(stderr, "this binary runs module \"%s\", scenario wants \"%s\"\n", gd_module(), module); return 2; }

	exe_dir(dir, sizeof(dir));
	if (gameso) o_strncpyz(oracle_game_path, (char *)gameso, sizeof(oracle_game_path));
	else Com_sprintf(oracle_game_path, sizeof(oracle_game_path), "%s/%s/game.so", dir, module);

	/* cvars as "+set k v" early commands */
	if (!strcmp(module, "ctf")) { kv[nkv++] = "game"; kv[nkv++] = "ctf"; }
	cv = oj_get(sc, "cvars");
	for (i = 0; cv && cv->type == OJ_OBJ && i < cv->n && nkv < 250; i++) {
		static char numbuf[64][32];
		kv[nkv++] = cv->keys[i];
		if (cv->kids[i]->type == OJ_STR) kv[nkv++] = cv->kids[i]->str;
		else { snprintf(numbuf[i & 63], 32, "%g", cv->kids[i]->num); kv[nkv++] = numbuf[i & 63]; }
	}
	oracle_init(oracle_basedir(basedir), nkv / 2, kv);

	out = outpath ? fopen(outpath, "w") : stdout;
	if (!out) { perror(outpath); return 2; }
	setvbuf(out, NULL, _IOFBF, 1 << 20);

	oracle_wrap_import = wrap_import;
	begin_frame();            /* events/rand of init+spawn+connect are reported in frame 0 */

	SV_InitGame();            /* real; its svs.spawncount = rand() happens before the srand below */
	if (!ge) { fprintf(stderr, "failed to load %s\n", oracle_game_path); return 3; }
	if (!elfsym_load((void *)ge->Init)) { fprintf(stderr, "cannot read symbols of %s\n", oracle_game_path); return 3; }
	levelp = dlsym(oracle_game_library, "level");
	if (!levelp) { fprintf(stderr, "no 'level' symbol in game module\n"); return 3; }
	if (nclients > maxclients->value) { fprintf(stderr, "%d clients > maxclients %g\n", nclients, maxclients->value); return 2; }
	real_spawnentities = ge->SpawnEntities;
	ge->SpawnEntities = w_spawnentities;

	srand(seed);
	rand_calls = 0;
	fclose(ev); free(evbuf); ev_open();    /* drop Init-time events; frame 0 starts at the map spawn */

	SV_SpawnServer((char *)map, "", ss_game, false, false);

	/* connect scripted clients (SVC_DirectConnect "gotnewcl" path, then "new" and "begin") */
	for (i = 0; i < nclients; i++) {
		client_t *cl = &svs.clients[i];
		char userinfo[MAX_INFO_STRING];
		netadr_t adr;
		edict_t *ent;
		o_strncpyz(userinfo, (char *)oj_string(oj_get(clients->kids[i], "userinfo"), ""), sizeof(userinfo));
		Info_SetValueForKey(userinfo, "ip", "loopback");
		memset(cl, 0, sizeof(*cl));
		sv_client = cl;
		ent = EDICT_NUM(i + 1);
		cl->edict = ent;
		slots[i] = i;
		if (!ge->ClientConnect(ent, userinfo)) {
			fprintf(stderr, "client %d rejected: %s\n", i, Info_ValueForKey(userinfo, "rejmsg"));
			continue;
		}
		strncpy(cl->userinfo, userinfo, sizeof(cl->userinfo) - 1);
		SV_UserinfoChanged(cl);
		memset(&adr, 0, sizeof(adr));
		adr.type = NA_LOOPBACK;
		Netchan_Setup(NS_SERVER, &cl->netchan, adr, i);
		cl->state = cs_connected;
		SZ_Init(&cl->datagram, cl->datagram_buf, sizeof(cl->datagram_buf));
		cl->datagram.allowoverflow = true;
		SV_ExecuteUserCommand("new");
		SV_ExecuteUserCommand(va("begin %i", svs.spawncount));
	}

	fputs("{\"header\":{", out);
	for (i = 0; i < sc->n; i++) {
		oj_str(out, sc->keys[i]); fputc(':', out); oj_write(out, sc->kids[i]); fputc(',', out);
	}
	fprintf(out, "\"edict_size\":%d,\"edicts_encoding\":\"%s\"}}\n", ge->edict_size, full_edicts ? "full" : "delta");
	if (ge->edict_size != gd_edict_size()) { fprintf(stderr, "edict_size mismatch %d vs %d\n", ge->edict_size, gd_edict_size()); return 3; }

	dump_frame(0, levelp, slots, nclients);
	SV_PrepWorldFrame();

	for (f = 1; f <= frames; f++) {
		int k;
		begin_frame();
		clear_client_buffers();
		/* scheduled inputs in file order */
		for (k = 0; sched && k < sched->n; k++) {
			ojv_t *e = sched->kids[k], *x;
			int cn;
			if (oj_int(oj_get(e, "frame"), -1) != f) continue;
			cn = oj_int(oj_get(e, "client"), 0);
			if (cn < 0 || cn >= nclients) continue;
			if ((x = oj_get(e, "cmd"))) { usercmd_t c; os_read_uc(x, &c); input_cmd(cn, &c); }
			else if ((x = oj_get(e, "command"))) input_command(cn, oj_string(x, ""));
			else if ((x = oj_get(e, "random_walk")) && nwalks < MAX_WALKS) {
				rwalk_t *w = &walks[nwalks++];
				memset(w, 0, sizeof(*w));
				w->active = 1; w->client = cn; w->start = f;
				w->until = oj_int(oj_get(x, "until"), frames + 1);
				xr_seed(&w->r, (uint32_t)oj_num(oj_get(x, "seed"), 1));
			}
		}
		/* active random walks, in schedule order */
		for (k = 0; k < nwalks; k++) {
			usercmd_t c;
			if (!walks[k].active || f >= walks[k].until) continue;
			rwalk_cmd(&walks[k], &c);
			input_cmd(walks[k].client, &c);
		}
		/* SV_RunGameFrame */
		sv.framenum++;
		sv.time = sv.framenum * 100;
		ge->RunFrame();
		dump_frame(f, levelp, slots, nclients);
		SV_PrepWorldFrame();
	}
	if (fflush(out) || (outpath && fclose(out))) { perror("write"); return 2; }
	return 0;
}
