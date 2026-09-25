/* sys_oracle.c -- headless system + network layer for the oracle binaries (replaces linux/sys_linux.c and
 * linux/net_udp.c). No sockets are ever opened; the console goes to stderr (stdout is reserved for fixtures). */
#include <stdio.h>
#include <stdlib.h>
#include <stdarg.h>
#include <string.h>
#include <dlfcn.h>
#include "../build/src/qcommon/qcommon.h"

/* set by the game driver before SV_InitGameProgs */
char oracle_game_path[1024];
void *oracle_game_library;
/* called with the engine's import table before GetGameAPI; may replace entries */
void (*oracle_wrap_import)(void *import);

int oracle_quiet = 1;   /* suppress console output unless ORACLE_VERBOSE is set */

void Sys_ConsoleOutput(char *string)
{
	if (!oracle_quiet) fputs(string, stderr);
}

void Sys_Printf(char *fmt, ...)
{
	va_list ap;
	if (oracle_quiet) return;
	va_start(ap, fmt); vfprintf(stderr, fmt, ap); va_end(ap);
}

void Sys_Quit(void) { exit(0); }
void Sys_Init(void) { if (getenv("ORACLE_VERBOSE")) oracle_quiet = 0; }

void Sys_Error(char *error, ...)
{
	va_list ap;
	fprintf(stderr, "oracle: Sys_Error: ");
	va_start(ap, error); vfprintf(stderr, error, ap); va_end(ap);
	fprintf(stderr, "\n");
	exit(3);
}

void Sys_Warn(char *warning, ...)
{
	va_list ap;
	va_start(ap, warning); vfprintf(stderr, warning, ap); va_end(ap);
}

char *Sys_ConsoleInput(void) { return NULL; }
void Sys_AppActivate(void) {}
void Sys_SendKeyEvents(void) {}
char *Sys_GetClipboardData(void) { return NULL; }
void Sys_CopyProtect(void) {}
void Sys_MakeCodeWriteable(unsigned long startaddr, unsigned long length) {}

void Sys_UnloadGame(void)
{
	if (oracle_game_library) dlclose(oracle_game_library);
	oracle_game_library = NULL;
}

void *Sys_GetGameAPI(void *parms)
{
	void *(*GetGameAPI)(void *);
	if (!oracle_game_path[0]) return NULL;
	oracle_game_library = dlopen(oracle_game_path, RTLD_NOW);
	if (!oracle_game_library) { fprintf(stderr, "oracle: dlopen %s: %s\n", oracle_game_path, dlerror()); return NULL; }
	GetGameAPI = (void *(*)(void *))dlsym(oracle_game_library, "GetGameAPI");
	if (!GetGameAPI) { Sys_UnloadGame(); return NULL; }
	if (oracle_wrap_import) oracle_wrap_import(parms);
	return GetGameAPI(parms);
}

/* ---------------- network: inert ---------------- */
netadr_t net_local_adr;

void NET_Init(void) {}
void NET_Shutdown(void) {}
void NET_Config(qboolean multiplayer) {}
void NET_Sleep(int msec) {}
qboolean NET_GetPacket(netsrc_t sock, netadr_t *from, sizebuf_t *msg) { return false; }
void NET_SendPacket(netsrc_t sock, int length, void *data, netadr_t to) {}

qboolean NET_CompareAdr(netadr_t a, netadr_t b)
{
	return a.type == b.type && !memcmp(a.ip, b.ip, 4) && a.port == b.port;
}

qboolean NET_CompareBaseAdr(netadr_t a, netadr_t b)
{
	if (a.type != b.type) return false;
	if (a.type == NA_LOOPBACK) return true;
	return !memcmp(a.ip, b.ip, 4);
}

qboolean NET_IsLocalAddress(netadr_t adr) { return adr.type == NA_LOOPBACK; }

char *NET_AdrToString(netadr_t a)
{
	static char s[64];
	if (a.type == NA_LOOPBACK) return "loopback";
	Com_sprintf(s, sizeof(s), "%i.%i.%i.%i:%i", a.ip[0], a.ip[1], a.ip[2], a.ip[3], ((a.port & 255) << 8) | (a.port >> 8));
	return s;
}

qboolean NET_StringToAdr(char *s, netadr_t *a)
{
	memset(a, 0, sizeof(*a));
	if (!strcmp(s, "localhost") || !strcmp(s, "loopback")) { a->type = NA_LOOPBACK; return true; }
	a->type = NA_IP;   /* never resolved: no DNS / sockets in the oracle */
	return true;
}

/* ---------------- engine bring-up ----------------
 * Runs the real Qcommon_Init with command-line style "+set" early commands so cvars are
 * set exactly as "quake2 +set k v" would. A harmless late command ("+echo") suppresses the default
 * "d1"/"dedicated_start" action. */
void oracle_init(const char *basedir, int nkv, const char **kv)
{
	char *argv[512];
	int argc = 0, i;
	argv[argc++] = "oracle";
	argv[argc++] = "+set"; argv[argc++] = "basedir"; argv[argc++] = (char *)basedir;
	for (i = 0; i < nkv && argc < 500; i++) {
		argv[argc++] = "+set"; argv[argc++] = (char *)kv[2 * i]; argv[argc++] = (char *)kv[2 * i + 1];
	}
	argv[argc++] = "+echo";
	argv[argc] = NULL;
	Qcommon_Init(argc, argv);
	Cbuf_Execute();
}

const char *oracle_basedir(const char *arg)
{
	const char *e;
	if (arg) return arg;
	e = getenv("Q2_BASEDIR");
	return e ? e : "assets/demo";
}
