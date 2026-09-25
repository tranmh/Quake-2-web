/*
 * ref_gl oracle driver: links the real Quake-2/ref_gl/*.c (no-op qgl, see gen_qgl_stub.py) and dumps
 * test vectors as JSON on stdout for web/packages/q2-render-gl/test/oracle.test.ts.
 * Build and run with scripts/oracle/build.sh.
 */
#include "gl_local.h"
#include <stdarg.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>

void ora_qgl_init(void);

/* ---------------------------------------------------------------- hashing / json helpers */
static unsigned fnv(const void *p, int n)
{
	const unsigned char *b = p;
	unsigned h = 2166136261u;
	int i;
	for (i = 0; i < n; i++) {
		h ^= b[i];
		h *= 16777619u;
	}
	return h;
}
static unsigned fnv_add(unsigned h, const void *p, int n)
{
	const unsigned char *b = p;
	int i;
	for (i = 0; i < n; i++) {
		h ^= b[i];
		h *= 16777619u;
	}
	return h;
}
static unsigned lcg_seed;
static int lcg(void)
{
	lcg_seed = lcg_seed * 1103515245u + 12345u;
	return (lcg_seed >> 16) & 0x7fff;
}
static void pf(float f) { printf("%.9g", f); }
static int first_item;
#define SEP() (first_item ? (first_item = 0, "") : ",")

/* ---------------------------------------------------------------- stubs */
static char *pakdata;
static int pakdirofs, pakdirlen;

static void Ora_Sys_Error(int err_level, char *fmt, ...)
{
	va_list ap;
	va_start(ap, fmt);
	fprintf(stderr, "Sys_Error(%d): ", err_level);
	vfprintf(stderr, fmt, ap);
	fprintf(stderr, "\n");
	va_end(ap);
	exit(3);
}
static void Ora_Con_Printf(int lvl, char *fmt, ...)
{
	va_list ap;
	if (!getenv("ORACLE_VERBOSE"))
		return;
	va_start(ap, fmt);
	vfprintf(stderr, fmt, ap);
	va_end(ap);
}
static int Ora_FS_LoadFile(char *name, void **buf)
{
	int i;
	for (i = 0; i < pakdirlen / 64; i++) {
		char *e = pakdata + pakdirofs + i * 64;
		if (!strcmp(e, name)) {
			int ofs = *(int *)(e + 56), len = *(int *)(e + 60);
			if (buf) {
				*buf = malloc(len + 1);
				memcpy(*buf, pakdata + ofs, len);
			}
			return len;
		}
	}
	if (buf)
		*buf = NULL;
	return -1;
}
static void Ora_FS_FreeFile(void *buf) { free(buf); }
static char *Ora_FS_Gamedir(void) { return "/tmp"; }

#define MAX_CV 256
static cvar_t cvars[MAX_CV];
static int numcvars;
static cvar_t *Ora_Cvar_Set(char *name, char *value);
static cvar_t *Ora_Cvar_Get(char *name, char *value, int flags)
{
	int i;
	for (i = 0; i < numcvars; i++)
		if (!strcmp(cvars[i].name, name))
			return &cvars[i];
	cvars[numcvars].name = strdup(name);
	cvars[numcvars].string = strdup(value);
	cvars[numcvars].value = atof(value);
	cvars[numcvars].flags = flags;
	cvars[numcvars].modified = true;
	return &cvars[numcvars++];
}
static cvar_t *Ora_Cvar_Set(char *name, char *value)
{
	cvar_t *v = Ora_Cvar_Get(name, value, 0);
	v->string = strdup(value);
	v->value = atof(value);
	v->modified = true;
	return v;
}
static void Ora_Cvar_SetValue(char *name, float value)
{
	char buf[64];
	sprintf(buf, "%g", value);
	Ora_Cvar_Set(name, buf);
}
static void Ora_Cmd_AddCommand(char *name, void (*cmd)(void)) {}
static void Ora_Cmd_RemoveCommand(char *name) {}
static void Ora_Vid_MenuInit(void) {}

void GLimp_BeginFrame(float camera_separation) {}
void GLimp_EndFrame(void) {}
int GLimp_Init(void *hinstance, void *hWnd) { return 1; }
void GLimp_Shutdown(void) {}
int GLimp_SetMode(int *pwidth, int *pheight, int mode, qboolean fullscreen)
{
	*pwidth = 640;
	*pheight = 480;
	return rserr_ok;
}
void GLimp_AppActivate(qboolean active) {}
void GLimp_EnableLogging(qboolean enable) {}
void GLimp_LogNewFrame(void) {}
qboolean QGL_Init(const char *dllname) { return true; }
void QGL_Shutdown(void) {}
void Sys_Mkdir(char *path) {}
char *strlwr(char *s)
{
	char *p;
	for (p = s; *p; p++)
		*p = tolower(*p);
	return s;
}

static byte *hunkbase;
static int hunkmax, hunkcur;
void *Hunk_Begin(int maxsize)
{
	hunkbase = calloc(1, maxsize);
	hunkmax = maxsize;
	hunkcur = 0;
	return hunkbase;
}
void *Hunk_Alloc(int size)
{
	byte *p;
	size = (size + 31) & ~31;
	if (hunkcur + size > hunkmax)
		Ora_Sys_Error(ERR_FATAL, "Hunk_Alloc overflow");
	p = hunkbase + hunkcur;
	hunkcur += size;
	return p;
}
int Hunk_End(void) { return hunkcur; }
void Hunk_Free(void *base)
{
	if (base)
		free(base);
}

/* ---------------------------------------------------------------- GL capture */
typedef struct {
	int texnum, level, comp, w, h;
	unsigned hash;
} upload_t;
static upload_t uploads[8192];
static int numuploads;
static int boundtex;
static byte lastraw[256 * 256 * 4];

static void APIENTRY c_BindTexture(GLenum target, GLuint tex) { boundtex = tex; }
static void APIENTRY c_TexImage2D(GLenum target, GLint level, GLint comp, GLsizei w, GLsizei h, GLint border,
								  GLenum format, GLenum type, const GLvoid *pixels)
{
	upload_t *u = &uploads[numuploads++];
	u->texnum = boundtex;
	u->level = level;
	u->comp = comp;
	u->w = w;
	u->h = h;
	u->hash = fnv(pixels, w * h * 4);
	if (w == 256 && h == 256)
		memcpy(lastraw, pixels, sizeof(lastraw));
}
static const GLubyte *APIENTRY c_GetString(GLenum name) { return (const GLubyte *)"oracle"; }
static GLenum APIENTRY c_GetError(void) { return 0; }

/* immediate-mode stream recorder */
static int rec_on, rec_count, rec_prims;
static unsigned rec_hash;
static float cur_st[2], cur_col[4] = {1, 1, 1, 1};
static float rec_first[64][9];
static void APIENTRY c_Begin(GLenum mode)
{
	if (rec_on) {
		int m = mode;
		rec_prims++;
		rec_hash = fnv_add(rec_hash, &m, 4);
	}
}
static void APIENTRY c_TexCoord2f(GLfloat s, GLfloat t)
{
	cur_st[0] = s;
	cur_st[1] = t;
}
static void APIENTRY c_Color4f(GLfloat r, GLfloat g, GLfloat b, GLfloat a)
{
	cur_col[0] = r;
	cur_col[1] = g;
	cur_col[2] = b;
	cur_col[3] = a;
}
static void APIENTRY c_Color3f(GLfloat r, GLfloat g, GLfloat b) { c_Color4f(r, g, b, 1); }
static void rec_vertex(float x, float y, float z)
{
	float v[9];
	if (!rec_on)
		return;
	v[0] = x;
	v[1] = y;
	v[2] = z;
	v[3] = cur_st[0];
	v[4] = cur_st[1];
	memcpy(v + 5, cur_col, 16);
	rec_hash = fnv_add(rec_hash, v, sizeof(v));
	if (rec_count < 64)
		memcpy(rec_first[rec_count], v, sizeof(v));
	rec_count++;
}
static void APIENTRY c_Vertex3f(GLfloat x, GLfloat y, GLfloat z) { rec_vertex(x, y, z); }
static void APIENTRY c_Vertex3fv(const GLfloat *v) { rec_vertex(v[0], v[1], v[2]); }
static void APIENTRY c_Vertex2f(GLfloat x, GLfloat y) { rec_vertex(x, y, 0); }

static void rec_start(void)
{
	rec_on = 1;
	rec_count = rec_prims = 0;
	cur_st[0] = cur_st[1] = 0;
	cur_col[0] = cur_col[1] = cur_col[2] = cur_col[3] = 1;
	rec_hash = 2166136261u;
}
static void rec_dump(const char *key, int nfirst)
{
	int i, j;
	rec_on = 0;
	printf("\"%s\":{\"count\":%d,\"prims\":%d,\"hash\":%u,\"first\":[", key, rec_count, rec_prims, rec_hash);
	for (i = 0; i < nfirst && i < rec_count && i < 64; i++) {
		printf(i ? ",[" : "[");
		for (j = 0; j < 9; j++) {
			if (j)
				printf(",");
			pf(rec_first[i][j]);
		}
		printf("]");
	}
	printf("]}");
}

/* ---------------------------------------------------------------- ref_gl symbols used directly */
extern model_t *r_worldmodel;
extern refdef_t r_newrefdef;
extern int r_framecount;
extern entity_t *currententity;
extern model_t *currentmodel;
extern vec3_t r_origin;
extern float skymins[2][6], skymaxs[2][6];
extern image_t *sky_images[6];
extern unsigned d_8to24table[256];
extern image_t gltextures[MAX_GLTEXTURES];
extern int numgltextures;
extern vec3_t pointcolor;
extern vec3_t lightspot;
void R_BuildLightMap(msurface_t *surf, byte *dest, int stride);
void R_SetCacheState(msurface_t *surf);
void ClipSkyPolygon(int nump, vec3_t vecs, int stage);
void R_ClearSkyBox(void);
void R_DrawSkyBox(void);
void R_AddSkySurface(msurface_t *fa);
void EmitWaterPolys(msurface_t *fa);
void GL_ResampleTexture(unsigned *in, int inwidth, int inheight, unsigned *out, int outwidth, int outheight);
void GL_LightScaleTexture(unsigned *in, int inwidth, int inheight, qboolean only_gamma);
void GL_MipMap(byte *in, int width, int height);
void R_FloodFillSkin(byte *skin, int skinwidth, int skinheight);
void GL_InitImages(void);
void Draw_StretchRaw(int x, int y, int w, int h, int cols, int rows, byte *data);
void R_SetPalette(const unsigned char *palette);
void R_DrawAliasModel(entity_t *e);
void R_LightPoint(vec3_t p, vec3_t color);
refexport_t GetRefAPI(refimport_t rimp);

static unsigned hash_floats(const float *f, int n) { return fnv(f, n * 4); }

static unsigned poly_hash(unsigned h, glpoly_t *p)
{
	for (; p; p = p->next) {
		h = fnv_add(h, &p->numverts, 4);
		h = fnv_add(h, p->verts, p->numverts * VERTEXSIZE * 4);
	}
	return h;
}

static lightstyle_t styles[MAX_LIGHTSTYLES];
static dlight_t dlights[4];

int main(int argc, char **argv)
{
	refimport_t ri_;
	refexport_t re;
	FILE *f;
	long len;
	int i, j, k;
	model_t *w;

	f = fopen(argv[1], "rb");
	if (!f)
		Ora_Sys_Error(ERR_FATAL, "no pak %s", argv[1]);
	fseek(f, 0, SEEK_END);
	len = ftell(f);
	fseek(f, 0, SEEK_SET);
	pakdata = malloc(len);
	fread(pakdata, 1, len, f);
	fclose(f);
	pakdirofs = *(int *)(pakdata + 4);
	pakdirlen = *(int *)(pakdata + 8);

	ora_qgl_init();
	qglBindTexture = c_BindTexture;
	qglTexImage2D = c_TexImage2D;
	qglGetString = c_GetString;
	qglGetError = c_GetError;
	qglBegin = c_Begin;
	qglTexCoord2f = c_TexCoord2f;
	qglColor4f = c_Color4f;
	qglColor3f = c_Color3f;
	qglVertex3f = c_Vertex3f;
	qglVertex3fv = c_Vertex3fv;
	qglVertex2f = c_Vertex2f;

	memset(&ri_, 0, sizeof(ri_));
	ri_.Sys_Error = Ora_Sys_Error;
	ri_.Cmd_AddCommand = Ora_Cmd_AddCommand;
	ri_.Cmd_RemoveCommand = Ora_Cmd_RemoveCommand;
	ri_.Con_Printf = Ora_Con_Printf;
	ri_.FS_LoadFile = Ora_FS_LoadFile;
	ri_.FS_FreeFile = Ora_FS_FreeFile;
	ri_.FS_Gamedir = Ora_FS_Gamedir;
	ri_.Cvar_Get = Ora_Cvar_Get;
	ri_.Cvar_Set = Ora_Cvar_Set;
	ri_.Cvar_SetValue = Ora_Cvar_SetValue;
	ri_.Vid_MenuInit = Ora_Vid_MenuInit;
	re = GetRefAPI(ri_);
	re.Init(NULL, NULL);
	re.BeginRegistration("demo1");
	re.RegisterModel("maps/demo1.bsp");
	w = r_worldmodel;

	printf("{");

	/* ---- world counts */
	printf("\"world\":{\"numsurfaces\":%d,\"numnodes\":%d,\"numleafs\":%d,\"numtexinfo\":%d,\"numsubmodels\":%d",
		   w->numsurfaces, w->numnodes, w->numleafs, w->numtexinfo, w->numsubmodels);
	{
		unsigned h = 2166136261u;
		for (i = 0; i < w->numsurfaces; i++)
			h = poly_hash(h, w->surfaces[i].polys);
		printf(",\"allPolysHash\":%u", h);
		h = 2166136261u;
		for (i = 0; i < w->numsurfaces; i++) {
			msurface_t *s = &w->surfaces[i];
			int v[8];
			v[0] = s->texturemins[0];
			v[1] = s->texturemins[1];
			v[2] = s->extents[0];
			v[3] = s->extents[1];
			v[4] = s->light_s;
			v[5] = s->light_t;
			v[6] = s->lightmaptexturenum;
			v[7] = s->flags;
			h = fnv_add(h, v, sizeof(v));
		}
		printf(",\"allSurfHash\":%u}", h);
	}

	/* ---- sampled surfaces */
	printf(",\"surfaces\":[");
	first_item = 1;
	for (i = 0; i < w->numsurfaces; i += w->numsurfaces / 50) {
		msurface_t *s = &w->surfaces[i];
		glpoly_t *p;
		int np = 0;
		for (p = s->polys; p; p = p->next)
			np++;
		printf("%s{\"i\":%d,\"flags\":%d,\"texturemins\":[%d,%d],\"extents\":[%d,%d],\"light\":[%d,%d],\"lmtex\":%d,"
			   "\"image\":\"%s\",\"npolys\":%d,\"polyHash\":%u,\"verts\":[",
			   SEP(), i, s->flags, s->texturemins[0], s->texturemins[1], s->extents[0], s->extents[1], s->light_s,
			   s->light_t, s->lightmaptexturenum, s->texinfo->image->name, np, poly_hash(2166136261u, s->polys));
		if (s->polys)
			for (j = 0; j < s->polys->numverts * VERTEXSIZE; j++) {
				if (j)
					printf(",");
				pf(s->polys->verts[0][j]);
			}
		printf("]}");
	}
	printf("]");

	/* ---- uploads: lightmap pages and images */
	printf(",\"lightmaps\":[");
	first_item = 1;
	for (i = 0; i < numuploads; i++)
		if (uploads[i].texnum > TEXNUM_LIGHTMAPS && uploads[i].texnum < TEXNUM_SCRAPS)
			printf("%s{\"page\":%d,\"hash\":%u}", SEP(), uploads[i].texnum - TEXNUM_LIGHTMAPS, uploads[i].hash);
	printf("]");

	printf(",\"images\":[");
	first_item = 1;
	for (i = 0; i < numgltextures; i++) {
		image_t *im = &gltextures[i];
		if (!im->texnum || im->scrap)
			continue;
		printf("%s{\"name\":\"%s\",\"type\":%d,\"w\":%d,\"h\":%d,\"uw\":%d,\"uh\":%d,\"alpha\":%d,\"levels\":[", SEP(),
			   im->name, im->type, im->width, im->height, im->upload_width, im->upload_height, im->has_alpha);
		k = 0;
		for (j = 0; j < numuploads; j++)
			if (uploads[j].texnum == im->texnum) {
				printf("%s[%d,%d,%d,%d,%u]", k++ ? "," : "", uploads[j].level, uploads[j].comp, uploads[j].w,
					   uploads[j].h, uploads[j].hash);
			}
		printf("]}");
	}
	printf("]");

	/* ---- R_BuildLightMap with non-trivial styles, dlights and mono modes */
	for (i = 0; i < MAX_LIGHTSTYLES; i++) {
		styles[i].rgb[0] = 0.5f + (i % 7) * 0.2f;
		styles[i].rgb[1] = 1.7f - (i % 5) * 0.3f;
		styles[i].rgb[2] = 1.0f;
		styles[i].white = styles[i].rgb[0] + styles[i].rgb[1] + styles[i].rgb[2];
	}
	r_newrefdef.lightstyles = styles;
	printf(",\"buildLightmap\":[");
	first_item = 1;
	{
		static byte buf[34 * 34 * 4];
		const char *modes[] = {"0", "C", "A", "L"};
		int m;
		cvar_t *mono = Ora_Cvar_Get("gl_monolightmap", "0", 0);
		cvar_t *modulate = Ora_Cvar_Get("gl_modulate", "1", 0);
		for (i = 0; i < w->numsurfaces; i += 37) {
			msurface_t *s = &w->surfaces[i];
			int smax, tmax;
			if (s->texinfo->flags & (SURF_SKY | SURF_TRANS33 | SURF_TRANS66 | SURF_WARP))
				continue;
			smax = (s->extents[0] >> 4) + 1;
			tmax = (s->extents[1] >> 4) + 1;
			printf("%s{\"i\":%d,\"size\":[%d,%d],\"h\":[", SEP(), i, smax, tmax);
			for (m = 0; m < 4; m++) {
				mono->string = (char *)modes[m];
				r_framecount = 100;
				s->dlightframe = 0;
				memset(buf, 0, sizeof(buf));
				R_BuildLightMap(s, buf, smax * 4);
				printf("%s%u", m ? "," : "", fnv(buf, smax * tmax * 4));
			}
			mono->string = "0";
			/* dlights: two lights in front of the first poly vertex */
			if (s->polys) {
				float *v = s->polys->verts[0];
				float sign = (s->flags & SURF_PLANEBACK) ? -1 : 1;
				for (k = 0; k < 2; k++) {
					dlights[k].origin[0] = v[0] + s->plane->normal[0] * sign * (20 + 30 * k) + 8 * k;
					dlights[k].origin[1] = v[1] + s->plane->normal[1] * sign * (20 + 30 * k) - 5 * k;
					dlights[k].origin[2] = v[2] + s->plane->normal[2] * sign * (20 + 30 * k);
					dlights[k].intensity = 200 + 100 * k;
					dlights[k].color[0] = 1;
					dlights[k].color[1] = 0.5f + 0.25f * k;
					dlights[k].color[2] = 0.25f;
				}
				r_newrefdef.dlights = dlights;
				r_newrefdef.num_dlights = 2;
				s->dlightframe = r_framecount;
				s->dlightbits = 3;
				memset(buf, 0, sizeof(buf));
				R_BuildLightMap(s, buf, smax * 4);
				printf(",%u", fnv(buf, smax * tmax * 4));
				modulate->value = 1.5f;
				R_BuildLightMap(s, buf, smax * 4);
				printf(",%u", fnv(buf, smax * tmax * 4));
				modulate->value = 1;
				s->dlightframe = 0;
				r_newrefdef.num_dlights = 0;
			}
			printf("]}");
		}
	}
	printf("]");

	/* ---- R_LightPoint */
	{
		static entity_t ent;
		vec3_t p, c;
		currententity = &ent;
		r_newrefdef.dlights = dlights;
		printf(",\"lightPoint\":[");
		first_item = 1;
		for (i = 0; i < w->numsurfaces; i += 97) {
			msurface_t *s = &w->surfaces[i];
			float sign = (s->flags & SURF_PLANEBACK) ? -1 : 1;
			if (!s->polys)
				continue;
			for (k = 0; k < 3; k++) {
				p[k] = s->polys->verts[0][k] + s->plane->normal[k] * sign * 24;
				ent.origin[k] = p[k];
			}
			r_newrefdef.num_dlights = (i / 97) & 1;
			dlights[0].origin[0] = p[0] + 10;
			dlights[0].origin[1] = p[1];
			dlights[0].origin[2] = p[2] + 5;
			R_LightPoint(p, c);
			printf("%s{\"p\":[", SEP());
			pf(p[0]);
			printf(",");
			pf(p[1]);
			printf(",");
			pf(p[2]);
			printf("],\"dl\":%d,\"c\":[", r_newrefdef.num_dlights);
			pf(c[0]);
			printf(",");
			pf(c[1]);
			printf(",");
			pf(c[2]);
			printf("],\"spot\":[");
			pf(lightspot[0]);
			printf(",");
			pf(lightspot[1]);
			printf(",");
			pf(lightspot[2]);
			printf("]}");
		}
		printf("]");
		r_newrefdef.num_dlights = 0;
	}

	/* ---- sky */
	{
		vec3_t axis = {0, 0, 1};
		const float origins[3][3] = {{0, 0, 0}, {512, -300, 128}, {-1024, 700, 40}};
		re.SetSky("unit1_", 0, axis);
		printf(",\"skyImages\":[");
		for (i = 0; i < 6; i++) {
			image_t *im = sky_images[i];
			printf("%s{\"name\":\"%s\",\"levels\":[", i ? "," : "", im->name);
			k = 0;
			for (j = 0; j < numuploads; j++)
				if (uploads[j].texnum == im->texnum)
					printf("%s[%d,%d,%d,%d,%u]", k++ ? "," : "", uploads[j].level, uploads[j].comp, uploads[j].w,
						   uploads[j].h, uploads[j].hash);
			printf("]}");
		}
		printf("]");
		printf(",\"sky\":[");
		for (k = 0; k < 3; k++) {
			VectorCopy(origins[k], r_origin);
			R_ClearSkyBox();
			for (i = 0; i < w->numsurfaces; i++)
				if (w->surfaces[i].texinfo->flags & SURF_SKY)
					R_AddSkySurface(&w->surfaces[i]);
			printf("%s{\"origin\":[%g,%g,%g],\"mins\":[", k ? "," : "", origins[k][0], origins[k][1], origins[k][2]);
			for (i = 0; i < 12; i++) {
				if (i)
					printf(",");
				pf(skymins[i / 6][i % 6]);
			}
			printf("],\"maxs\":[");
			for (i = 0; i < 12; i++) {
				if (i)
					printf(",");
				pf(skymaxs[i / 6][i % 6]);
			}
			printf("],");
			rec_start();
			R_DrawSkyBox();
			rec_dump("draw", 24);
			printf("}");
		}
		printf("]");
	}

	/* ---- water warp */
	{
		const float times[3] = {1.5f, 13.37f, 97.25f};
		msurface_t *ws = NULL, *wf = NULL;
		for (i = 0; i < w->numsurfaces; i++) {
			if (w->surfaces[i].flags & SURF_DRAWTURB) {
				if (!ws)
					ws = &w->surfaces[i];
				if (!wf && (w->surfaces[i].texinfo->flags & SURF_FLOWING))
					wf = &w->surfaces[i];
			}
		}
		printf(",\"warp\":[");
		first_item = 1;
		for (k = 0; k < 3; k++) {
			r_newrefdef.time = times[k];
			if (ws) {
				printf("%s{\"surf\":%d,\"time\":%.9g,", SEP(), (int)(ws - w->surfaces), times[k]);
				rec_start();
				EmitWaterPolys(ws);
				rec_dump("draw", 12);
				printf("}");
			}
			if (wf) {
				printf("%s{\"surf\":%d,\"time\":%.9g,", SEP(), (int)(wf - w->surfaces), times[k]);
				rec_start();
				EmitWaterPolys(wf);
				rec_dump("draw", 12);
				printf("}");
			}
		}
		printf("]");
	}

	/* ---- alias models */
	{
		static entity_t ent;
		struct model_s *mod = re.RegisterModel("models/monsters/soldier/tris.md2");
		const int flags[4] = {0, RF_SHELL_RED, RF_TRANSLUCENT | RF_GLOW, RF_SHELL_HALF_DAM | RF_MINLIGHT};
		r_newrefdef.time = 3.25f;
		r_newrefdef.num_dlights = 1;
		dlights[0].origin[0] = 100;
		dlights[0].origin[1] = 20;
		dlights[0].origin[2] = 40;
		dlights[0].intensity = 300;
		printf(",\"alias\":[");
		for (k = 0; k < 4; k++) {
			memset(&ent, 0, sizeof(ent));
			ent.model = mod;
			ent.frame = 5 + k;
			ent.oldframe = 4 + k;
			ent.backlerp = 0.3f;
			ent.origin[0] = 120.5f;
			ent.origin[1] = -40.25f;
			ent.origin[2] = 30;
			ent.oldorigin[0] = 118;
			ent.oldorigin[1] = -41;
			ent.oldorigin[2] = 29.5f;
			ent.angles[0] = 10;
			ent.angles[1] = 37.5f + 90 * k;
			ent.angles[2] = -5;
			ent.alpha = 0.6f;
			ent.flags = flags[k];
			currententity = &ent;
			currentmodel = mod;
			printf("%s{\"flags\":%d,\"frame\":%d,", k ? "," : "", ent.flags, ent.frame);
			rec_start();
			R_DrawAliasModel(&ent);
			rec_dump("draw", 6);
			printf("}");
		}
		printf("]");
		r_newrefdef.num_dlights = 0;
	}

	/* ---- scrap / pics */
	{
		int n0 = numuploads;
		re.DrawPic(0, 0, "i_health");
		re.DrawPic(0, 0, "a_bullets");
		re.DrawPic(10, 0, "i_health");
		printf(",\"scrap\":[");
		first_item = 1;
		for (i = n0; i < numuploads; i++)
			printf("%s[%d,%d,%d,%d,%u]", SEP(), uploads[i].texnum, uploads[i].comp, uploads[i].w, uploads[i].h,
				   uploads[i].hash);
		printf("]");
	}

	/* ---- synthetic image functions */
	{
		static unsigned in[64 * 64], out[64 * 64];
		static byte skin[16 * 16];
		printf(",\"resample\":[");
		{
			const int sz[4][4] = {{13, 7, 16, 8}, {5, 9, 4, 8}, {64, 32, 16, 16}, {3, 3, 8, 8}};
			for (k = 0; k < 4; k++) {
				lcg_seed = 1234 + k;
				for (i = 0; i < sz[k][0] * sz[k][1]; i++)
					{ unsigned a = lcg(), b = lcg(); in[i] = a | (b << 16); }
				GL_ResampleTexture(in, sz[k][0], sz[k][1], out, sz[k][2], sz[k][3]);
				printf("%s{\"seed\":%d,\"size\":[%d,%d,%d,%d],\"hash\":%u}", k ? "," : "", 1234 + k, sz[k][0],
					   sz[k][1], sz[k][2], sz[k][3], fnv(out, sz[k][2] * sz[k][3] * 4));
			}
		}
		printf("],\"mipmap\":[");
		{
			const int sz[3][2] = {{8, 8}, {16, 4}, {2, 1}};
			for (k = 0; k < 3; k++) {
				lcg_seed = 99 + k;
				for (i = 0; i < sz[k][0] * sz[k][1]; i++)
					{ unsigned a = lcg(), b = lcg(); in[i] = a | (b << 16); }
				GL_MipMap((byte *)in, sz[k][0], sz[k][1]);
				printf("%s{\"seed\":%d,\"size\":[%d,%d],\"hash\":%u}", k ? "," : "", 99 + k, sz[k][0], sz[k][1],
					   fnv(in, sz[k][0] * sz[k][1] * 4));
			}
		}
		printf("],\"floodfill\":[");
		for (k = 0; k < 3; k++) {
			lcg_seed = 7 + k;
			for (i = 0; i < 256; i++)
				skin[i] = (lcg() % 5 == 0) ? (byte)(lcg() & 255) : (byte)(k == 2 ? 255 : 17);
			skin[0] = k == 2 ? 255 : 17;
			R_FloodFillSkin(skin, 16, 16);
			printf("%s{\"seed\":%d,\"hash\":%u}", k ? "," : "", 7 + k, fnv(skin, 256));
		}
		printf("]");

		/* Draw_StretchRaw with the game palette */
		printf(",\"stretchRaw\":[");
		{
			static byte data[320 * 300];
			const int dims[2][2] = {{320, 240}, {200, 300}};
			R_SetPalette(NULL);
			for (k = 0; k < 2; k++) {
				int cols = dims[k][0], rows = dims[k][1], trows = rows > 256 ? 256 : rows;
				lcg_seed = 555 + k;
				for (i = 0; i < cols * rows; i++)
					data[i] = lcg() & 255;
				Draw_StretchRaw(0, 0, 320, 240, cols, rows, data);
				printf("%s{\"seed\":%d,\"cols\":%d,\"rows\":%d,\"hash\":%u}", k ? "," : "", 555 + k, cols, rows,
					   fnv(lastraw, 256 * trows * 4));
			}
		}
		printf("]");

		/* gamma / intensity tables through GL_LightScaleTexture */
		printf(",\"gamma\":[");
		{
			const char *g[3] = {"0.7", "1.3", "1"};
			const char *in_[3] = {"2", "1.5", "3"};
			for (k = 0; k < 3; k++) {
				byte ramp[256 * 4];
				Ora_Cvar_Set("vid_gamma", (char *)g[k]);
				Ora_Cvar_Set("intensity", (char *)in_[k]);
				GL_InitImages();
				printf("%s{\"gamma\":%s,\"intensity\":%s,\"g\":[", k ? "," : "", g[k], in_[k]);
				for (i = 0; i < 256; i++)
					ramp[i * 4] = ramp[i * 4 + 1] = ramp[i * 4 + 2] = i, ramp[i * 4 + 3] = 255;
				GL_LightScaleTexture((unsigned *)ramp, 256, 1, true);
				for (i = 0; i < 256; i++)
					printf("%s%d", i ? "," : "", ramp[i * 4]);
				printf("],\"gi\":[");
				for (i = 0; i < 256; i++)
					ramp[i * 4] = ramp[i * 4 + 1] = ramp[i * 4 + 2] = i, ramp[i * 4 + 3] = 255;
				GL_LightScaleTexture((unsigned *)ramp, 256, 1, false);
				for (i = 0; i < 256; i++)
					printf("%s%d", i ? "," : "", ramp[i * 4]);
				printf("]}");
			}
		}
		printf("]");
	}

	printf("}\n");
	return 0;
}
