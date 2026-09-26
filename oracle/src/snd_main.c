/*
 * snd_main.c -- oracle harness for the sound mixer (client/snd_dma.c + snd_mix.c + snd_mem.c).
 *
 * Links the original snd_*.c with stubs for the rest of the client, a fake 16 bit stereo DMA buffer
 * (0x10000 bytes, submission_chunk 1, like snd_win.c DirectSound) whose play position is scripted,
 * and synthetic WAV files generated here. Writes JSONL to stdout: the generated WAVs, a table of
 * S_SpatializeOrigin results and a scripted sequence of frames (S_StartSound / S_RawSamples /
 * S_StopAllSounds / S_Update) with the mixer state and an FNV-1a hash of the DMA buffer after every
 * S_Update. Consumed by web/packages/q2-sound/test/oracle.test.ts.
 *
 * Build/run: web/packages/q2-sound/scripts/gen-vectors.sh
 * usage: snd_oracle <loadas8bit 0|1>
 *        snd_oracle wav <speed> <loadas8bit 0|1> <name=path>...   (S_LoadSound of real files: sizes + hashes)
 */
#include "../../Quake-2/client/client.h"
#include "../../Quake-2/client/snd_loc.h"

client_state_t cl;
client_static_t cls;
centity_t cl_entities[MAX_EDICTS];
entity_state_t cl_parse_entities[MAX_PARSE_ENTITIES];
cvar_t *cl_paused;

extern int paintedtime, soundtime, s_beginofs, num_sfx;
extern sfx_t known_sfx[];
void S_SpatializeOrigin(vec3_t origin, float master_vol, float dist_mult, int *left_vol, int *right_vol);

/* ---------------------------------------------------------------- stubs */
static cvar_t cvars[64];
static int ncvars;

cvar_t *Cvar_Get(char *name, char *value, int flags)
{
	int i;
	for (i = 0; i < ncvars; i++)
		if (!strcmp(cvars[i].name, name))
			return &cvars[i];
	cvars[ncvars].name = strdup(name);
	cvars[ncvars].string = strdup(value);
	cvars[ncvars].value = (float)atof(value);
	cvars[ncvars].flags = flags;
	cvars[ncvars].modified = true;
	return &cvars[ncvars++];
}
void Cvar_SetValueF(char *name, float v)
{
	cvar_t *c = Cvar_Get(name, "0", 0);
	c->value = v;
	c->modified = true;
}
void Cmd_AddCommand(char *n, xcommand_t f) {}
void Cmd_RemoveCommand(char *n) {}
int Cmd_Argc(void) { return 0; }
char *Cmd_Argv(int i) { return ""; }
void Com_Printf(char *fmt, ...) {}
void Com_DPrintf(char *fmt, ...) {}
void Com_Error(int code, char *fmt, ...)
{
	va_list ap;
	va_start(ap, fmt);
	vfprintf(stderr, fmt, ap);
	va_end(ap);
	exit(1);
}
void Sys_Error(char *fmt, ...) { exit(2); }
void *Z_Malloc(int size) { return calloc(1, size); }
void Z_Free(void *p) { free(p); }

/* in-memory files */
#define MAXFILES 16
static char *fnames[MAXFILES];
static byte *fdata[MAXFILES];
static int flen[MAXFILES], nfiles;

int FS_LoadFile(char *path, void **buffer)
{
	int i;
	for (i = 0; i < nfiles; i++)
		if (!strcmp(fnames[i], path)) {
			byte *b = malloc(flen[i]);
			memcpy(b, fdata[i], flen[i]);
			*buffer = b;
			return flen[i];
		}
	*buffer = NULL;
	return -1;
}
void FS_FreeFile(void *b) { free(b); }
int FS_FOpenFile(char *filename, FILE **file) { *file = NULL; return -1; }
void FS_FCloseFile(FILE *f) {}

/* DMA */
static int dmapos; /* mono samples */
static short dmabuf[0x10000 / 2];
static int dmaspeed = 22050;
qboolean SNDDMA_Init(void)
{
	dma.channels = 2;
	dma.samplebits = 16;
	dma.speed = dmaspeed;
	dma.samples = 0x10000 / 2;
	dma.submission_chunk = 1;
	dma.samplepos = 0;
	dma.buffer = (byte *)dmabuf;
	return true;
}
int SNDDMA_GetDMAPos(void) { return dma.samplepos = dmapos & (dma.samples - 1); }
void SNDDMA_Shutdown(void) {}
void SNDDMA_BeginPainting(void) {}
void SNDDMA_Submit(void) {}

void CL_GetEntitySoundOrigin(int ent, vec3_t org)
{
	VectorCopy(cl_entities[ent].lerp_origin, org);
}

/* ---------------------------------------------------------------- helpers */
static unsigned lcg_state = 12345;
static unsigned lcg(void)
{
	lcg_state = lcg_state * 1103515245u + 12345u;
	return (lcg_state >> 8) & 0xffffff;
}
static float frnd(float lo, float hi) /* float in [lo,hi) on a 1/8 grid */
{
	return lo + (float)(lcg() % (unsigned)((hi - lo) * 8)) * 0.125f;
}

static void hexout(const byte *b, int n)
{
	int i;
	for (i = 0; i < n; i++)
		printf("%02x", b[i]);
}

static unsigned fnv(const byte *b, int n)
{
	unsigned h = 2166136261u;
	int i;
	for (i = 0; i < n; i++) {
		h ^= b[i];
		h *= 16777619u;
	}
	return h;
}

static void put16(byte *p, int v) { p[0] = v & 255; p[1] = (v >> 8) & 255; }
static void put32(byte *p, int v) { put16(p, v); put16(p + 2, v >> 16); }

/* RIFF WAVE, mono PCM; optional cue chunk with a loop start */
static void makewav(char *name, int rate, int width, int samples, int loopstart)
{
	int datalen = samples * width;
	int cuelen = loopstart >= 0 ? 8 + 28 : 0;
	int total = 12 + 24 + cuelen + 8 + datalen + (datalen & 1);
	byte *b = calloc(1, total);
	byte *p = b;
	int i;

	memcpy(p, "RIFF", 4); put32(p + 4, total - 8); memcpy(p + 8, "WAVE", 4); p += 12;
	memcpy(p, "fmt ", 4); put32(p + 4, 16); put16(p + 8, 1); put16(p + 10, 1); put32(p + 12, rate);
	put32(p + 16, rate * width); put16(p + 20, width); put16(p + 22, width * 8); p += 24;
	if (loopstart >= 0) {
		memcpy(p, "cue ", 4); put32(p + 4, 28); put32(p + 8, 1); put32(p + 32, loopstart); p += 36;
	}
	memcpy(p, "data", 4); put32(p + 4, datalen); p += 8;
	for (i = 0; i < samples; i++) {
		int v = (int)(lcg() % 20000) - 10000 + (int)(8000 * ((i % 97) - 48) / 48);
		if (width == 1)
			p[i] = (byte)((v >> 8) + 128);
		else
			put16(p + i * 2, v);
	}
	fnames[nfiles] = malloc(strlen(name) + 8);
	sprintf(fnames[nfiles], "sound/%s", name);
	fdata[nfiles] = b;
	flen[nfiles] = total;
	nfiles++;
	printf("{\"op\":\"wav\",\"name\":\"%s\",\"hex\":\"", name);
	hexout(b, total);
	printf("\"}\n");
}

static void pvec(const char *k, vec3_t v)
{
	printf("\"%s\":[%.9g,%.9g,%.9g]", k, v[0], v[1], v[2]);
}

static void dumpstate(void)
{
	int i;
	printf("{\"op\":\"state\",\"paintedtime\":%d,\"soundtime\":%d,\"beginofs\":%d,\"rawend\":%d,\"hash\":%u,\"ch\":[",
	       paintedtime, soundtime, s_beginofs, s_rawend, fnv((byte *)dmabuf, sizeof(dmabuf)));
	for (i = 0; i < MAX_CHANNELS; i++) {
		channel_t *ch = &channels[i];
		if (i)
			printf(",");
		printf("[%d,%d,%d,%d,%d,%d,%d,%d,%d]", ch->sfx ? (int)(ch->sfx - known_sfx) : -1, ch->leftvol,
		       ch->rightvol, ch->end, ch->pos, ch->entnum, ch->entchannel, ch->master_vol, ch->autosound);
	}
	printf("]}\n");
}

static void angvecs(float yaw, float pitch, vec3_t f, vec3_t r, vec3_t u)
{
	vec3_t a;
	a[0] = pitch;
	a[1] = yaw;
	a[2] = 0;
	AngleVectors(a, f, r, u);
}

static int wavmode(int argc, char **argv)
{
	int i;
	dmaspeed = atoi(argv[2]);
	Swap_Init();
	cl_paused = Cvar_Get("paused", "0", 0);
	Cvar_Get("s_loadas8bit", argv[3], 0);
	S_Init();
	for (i = 4; i < argc; i++) {
		char *eq = strchr(argv[i], '=');
		FILE *fp;
		sfx_t *s;
		*eq = 0;
		fp = fopen(eq + 1, "rb");
		fseek(fp, 0, SEEK_END);
		flen[nfiles] = ftell(fp);
		fseek(fp, 0, SEEK_SET);
		fdata[nfiles] = malloc(flen[nfiles]);
		fread(fdata[nfiles], 1, flen[nfiles], fp);
		fclose(fp);
		fnames[nfiles] = malloc(strlen(argv[i]) + 8);
		sprintf(fnames[nfiles], "sound/%s", argv[i]);
		nfiles++;
		s = S_RegisterSound(argv[i]);
		if (s->cache)
			printf("{\"name\":\"%s\",\"speed\":%d,\"loadas8bit\":%s,\"length\":%d,\"loopstart\":%d,\"width\":%d,\"hash\":%u}\n",
			       argv[i], dma.speed, argv[3], s->cache->length, s->cache->loopstart, s->cache->width,
			       fnv(s->cache->data, s->cache->length * s->cache->width));
		else
			printf("{\"name\":\"%s\",\"speed\":%d,\"loadas8bit\":%s,\"length\":null}\n", argv[i], dma.speed, argv[3]);
		if (nfiles == MAXFILES) { /* recycle the in-memory file table */
			nfiles = 0;
		}
	}
	return 0;
}

int main(int argc, char **argv)
{
	int i, j, frame, loadas8bit = argc > 1 ? atoi(argv[1]) : 0;
	sfx_t *sfx[8];
	int nsfx = 0;
	vec3_t org, f, r, u;
	int servertime = 1000;

	if (argc > 1 && !strcmp(argv[1], "wav"))
		return wavmode(argc, argv);

	Swap_Init();
	cl_paused = Cvar_Get("paused", "0", 0);
	Cvar_Get("s_loadas8bit", loadas8bit ? "1" : "0", 0);
	Cvar_Get("s_khz", "22", 0);
	S_Init();
	cls.state = ca_active;
	cl.playernum = 0;
	cl.sound_prepped = true;

	printf("{\"op\":\"init\",\"speed\":%d,\"samples\":%d,\"loadas8bit\":%d,\"volume\":%.9g,\"mixahead\":%.9g}\n",
	       dma.speed, dma.samples, loadas8bit, Cvar_Get("s_volume", "0", 0)->value,
	       Cvar_Get("s_mixahead", "0", 0)->value);

	/* ---- synthetic sounds */
	makewav("t/a8.wav", 11025, 1, 3000, -1);
	makewav("t/b16.wav", 22050, 2, 4000, -1);
	makewav("t/c16loop.wav", 44100, 2, 6000, 1000);
	makewav("t/d8loop.wav", 22050, 1, 1500, 500);
	makewav("t/e16.wav", 11025, 2, 2500, -1);
	sfx[nsfx++] = S_RegisterSound("t/a8.wav");
	sfx[nsfx++] = S_RegisterSound("t/b16.wav");
	sfx[nsfx++] = S_RegisterSound("t/c16loop.wav");
	sfx[nsfx++] = S_RegisterSound("t/d8loop.wav");
	sfx[nsfx++] = S_RegisterSound("t/e16.wav");
	for (i = 0; i < nsfx; i++) {
		sfxcache_t *sc = sfx[i]->cache;
		printf("{\"op\":\"sfx\",\"index\":%d,\"name\":\"%s\",\"length\":%d,\"loopstart\":%d,\"width\":%d,\"hash\":%u}\n",
		       (int)(sfx[i] - known_sfx), sfx[i]->name, sc->length, sc->loopstart, sc->width,
		       fnv(sc->data, sc->length * sc->width));
	}

	/* ---- spatialization table */
	for (i = 0; i < 300; i++) {
		int lv, rv;
		float mv = (float)(lcg() % 256);
		float dm = i % 5 == 0 ? 0 : (i % 5 == 1 ? 0.0005f : (i % 5 == 2 ? 0.001f : (i % 5 == 3 ? 0.0015f : 0.003f)));
		cls.state = i % 37 == 5 ? ca_connected : ca_active;
		listener_origin[0] = frnd(-2000, 2000);
		listener_origin[1] = frnd(-2000, 2000);
		listener_origin[2] = frnd(-500, 500);
		angvecs(frnd(0, 360), frnd(-80, 80), listener_forward, listener_right, listener_up);
		org[0] = listener_origin[0] + frnd(-700, 700) * (i % 3 == 0 ? 0.05f : 1.0f);
		org[1] = listener_origin[1] + frnd(-700, 700);
		org[2] = listener_origin[2] + frnd(-200, 200);
		S_SpatializeOrigin(org, mv, dm, &lv, &rv);
		printf("{\"op\":\"spat\",\"state\":%d,", cls.state);
		pvec("lo", listener_origin);
		printf(",");
		pvec("lr", listener_right);
		printf(",");
		pvec("o", org);
		printf(",\"mv\":%.9g,\"dm\":%.9g,\"l\":%d,\"r\":%d}\n", mv, dm, lv, rv);
	}
	cls.state = ca_active;

	/* ---- scripted frames */
	for (i = 1; i < 12; i++)
		for (j = 0; j < 3; j++)
			cl_entities[i].lerp_origin[j] = frnd(-800, 800);
	for (frame = 0; frame < 90; frame++) {
		int nstarts = (frame % 7 == 0) ? 4 : (int)(lcg() % 3);
		int adv;
		float yaw = frnd(0, 360);

		printf("{\"op\":\"frame\",\"n\":%d", frame);
		/* entity motion */
		printf(",\"ents\":[");
		for (i = 1; i < 12; i++) {
			for (j = 0; j < 3; j++)
				cl_entities[i].lerp_origin[j] += frnd(-20, 20);
			printf("%s[%d,%.9g,%.9g,%.9g]", i > 1 ? "," : "", i, cl_entities[i].lerp_origin[0],
			       cl_entities[i].lerp_origin[1], cl_entities[i].lerp_origin[2]);
		}
		printf("]");
		/* sounds */
		printf(",\"starts\":[");
		for (i = 0; i < nstarts; i++) {
			int fixed = lcg() % 2;
			int ent = (int)(lcg() % 12);
			int chan = (int)(lcg() % 5);
			int s = (int)(lcg() % nsfx);
			float vol = (float)(lcg() % 256) / 255.0f;
			float att = (float)(lcg() % 4);
			float ofs = (lcg() % 3 == 0) ? (float)(lcg() % 100) * 0.001f : 0;
			org[0] = frnd(-800, 800);
			org[1] = frnd(-800, 800);
			org[2] = frnd(-100, 100);
			S_StartSound(fixed ? org : NULL, ent, chan, sfx[s], vol, att, ofs);
			printf("%s{\"fixed\":%d,", i ? "," : "", fixed);
			pvec("o", org);
			printf(",\"ent\":%d,\"chan\":%d,\"sfx\":%d,\"vol\":%.9g,\"att\":%.9g,\"ofs\":%.9g,\"st\":%d}", ent, chan,
			       (int)(sfx[s] - known_sfx), vol, att, ofs, cl.frame.servertime);
		}
		printf("]");
		/* raw samples (cinematic audio) */
		if (frame == 20 || frame == 21 || frame == 50) {
			static byte raw[8192];
			int n = frame == 50 ? 700 : 1575, w = frame == 50 ? 1 : 2, c = frame == 50 ? 1 : 2;
			int rate = frame == 50 ? 11025 : 22050;
			for (i = 0; i < n * w * c; i++)
				raw[i] = (byte)lcg();
			S_RawSamples(n, rate, w, c, raw);
			printf(",\"raw\":{\"n\":%d,\"rate\":%d,\"w\":%d,\"c\":%d,\"hex\":\"", n, rate, w, c);
			hexout(raw, n * w * c);
			printf("\"}");
		}
		if (frame == 60) {
			S_StopAllSounds();
			printf(",\"stopall\":1");
		}
		if (frame == 40) {
			Cvar_SetValueF("s_volume", 0.45f);
			printf(",\"volume\":%.9g", 0.45f);
		}
		/* loop sounds: frame entities */
		cl.frame.parse_entities = frame * 5;
		cl.frame.num_entities = 5;
		cl.sound_precache[1] = sfx[3];
		cl.sound_precache[2] = sfx[2];
		printf(",\"loops\":[");
		for (i = 0; i < 5; i++) {
			entity_state_t *e = &cl_parse_entities[(cl.frame.parse_entities + i) & (MAX_PARSE_ENTITIES - 1)];
			e->sound = (frame > 10 && i < 3) ? (i == 1 ? 2 : 1) : 0;
			for (j = 0; j < 3; j++)
				e->origin[j] = frnd(-600, 600);
			printf("%s[%d,%d,%.9g,%.9g,%.9g]", i ? "," : "", e->sound,
			       e->sound ? (int)(cl.sound_precache[e->sound] - known_sfx) : -1, e->origin[0], e->origin[1],
			       e->origin[2]);
		}
		printf("]");
		/* listener + dma clock */
		listener_origin[0] = 0;
		angvecs(yaw, 0, f, r, u);
		org[0] = frnd(-100, 100);
		org[1] = frnd(-100, 100);
		org[2] = 0;
		adv = frame == 30 ? 20000 : (int)(lcg() % 1500) + 500; /* frames of audio played */
		if (frame == 70)
			adv = 17000; /* wrap the dma buffer more than half */
		dmapos += adv * 2;
		cls.disable_screen = frame == 45 ? 1 : 0;
		printf(",\"dmapos\":%d,\"disable\":%d,", dmapos & (dma.samples - 1), frame == 45);
		pvec("lo", org);
		printf(",");
		pvec("lf", f);
		printf(",");
		pvec("lr", r);
		printf(",");
		pvec("lu", u);
		printf("}\n");
		S_Update(org, f, r, u);
		dumpstate();
		servertime += 100;
		cl.frame.servertime = servertime;
	}
	return 0;
}
