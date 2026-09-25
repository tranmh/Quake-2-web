/* Scratch C harness that produced cl_fx_vec.txt: links the ORIGINAL client/cl_fx.c with stubs.
 * Build (from repo root, after oracle sync):
 *   S=oracle/build/src; gcc -m64 -O1 -g -std=gnu89 -fcommon -fno-strict-aliasing -fno-fast-math \
 *     -ffp-contract=off -fexcess-precision=standard -Dstricmp=strcasecmp -w -I$S -I$S/client \
 *     cl_fx_harness.c $S/client/cl_fx.c $S/game/q_shared.c $S/game/m_flash.c -lm -o h && ./h > cl_fx_vec.txt
 */
/* scratch harness: links the real client/cl_fx.c with stubs and dumps particle state */
#include "client/client.h"
client_state_t cl; client_static_t cls; centity_t cl_entities[MAX_EDICTS];
sizebuf_t net_message; cvar_t *cl_footsteps; int vidref_val = VIDREF_GL;
struct sfx_s *cl_sfx_footsteps[4]; struct model_s *cl_mod_smoke, *cl_mod_flash;
vec3_t bytedirs[NUMVERTEXNORMALS] = {
#include "client/anorms.h"
};
float frand(void){ return (rand()&32767)* (1.0/32767); }
float crand(void){ return (rand()&32767)* (2.0/32767) - 1; }
static unsigned ph=2166136261u; static int pn;
static unsigned fb(float f){ unsigned u; memcpy(&u,&f,4); return u; }
static void mix(unsigned v){ int k; for(k=0;k<4;k++){ ph^=(v>>(8*k))&255; ph*=16777619u; } }
static int nsnd;
void S_StartSound (vec3_t origin, int entnum, int entchannel, struct sfx_s *sfx, float fvol,  float attenuation, float timeofs){ nsnd++; printf("S %d %d %s %08x %08x %08x\n", entnum, entchannel, (char*)sfx, fb(fvol), fb(attenuation), fb(timeofs));}
struct sfx_s *S_RegisterSound (char *name){ static char b[64][64]; static int k; k=(k+1)&63; strcpy(b[k],name); return (struct sfx_s*)b[k]; }
void V_AddParticle (vec3_t org, int color, float alpha){ if(pn<2) printf("P %08x %08x %08x %d %08x\n", fb(org[0]),fb(org[1]),fb(org[2]),color,fb(alpha)); pn++; mix(fb(org[0]));mix(fb(org[1]));mix(fb(org[2]));mix(color);mix(fb(alpha)); }
void CL_AddParticles (void);
static void AP(void){ ph=2166136261u; pn=0; CL_AddParticles(); printf("A %d %d %08x\n", cl.time, pn, ph); }
void V_AddLight (vec3_t org, float intensity, float r, float g, float b){ printf("L %08x %08x %08x %08x %08x %08x %08x\n",fb(org[0]),fb(org[1]),fb(org[2]),fb(intensity),fb(r),fb(g),fb(b)); }
void V_AddLightStyle (int style, float r, float g, float b){}
void CL_SmokeAndFlash(vec3_t origin){ printf("SMOKE\n"); }
void Com_Error (int code, char *fmt, ...){ printf("ERR\n"); exit(1); }
static byte *mbuf; static int mpos;
int MSG_ReadShort (sizebuf_t *s){ int v=(short)(mbuf[mpos]|(mbuf[mpos+1]<<8)); mpos+=2; return v; }
int MSG_ReadByte (sizebuf_t *s){ return mbuf[mpos++]; }
void CL_ClearEffects(void);
void CL_ParticleEffect (vec3_t, vec3_t, int, int); void CL_ParticleEffect2 (vec3_t, vec3_t, int, int); void CL_ParticleEffect3 (vec3_t, vec3_t, int, int);
void CL_TeleporterParticles (entity_state_t *); void CL_LogoutEffect (vec3_t, int); void CL_ItemRespawnParticles (vec3_t);
void CL_ExplosionParticles (vec3_t); void CL_BigTeleportParticles (vec3_t); void CL_BlasterParticles (vec3_t, vec3_t);
void CL_BlasterTrail (vec3_t, vec3_t); void CL_QuadTrail (vec3_t, vec3_t); void CL_FlagTrail (vec3_t, vec3_t, float);
void CL_DiminishingTrail (vec3_t, vec3_t, centity_t *, int); void CL_RocketTrail (vec3_t, vec3_t, centity_t *);
void CL_RailTrail (vec3_t, vec3_t); void CL_IonripperTrail (vec3_t, vec3_t); void CL_BubbleTrail (vec3_t, vec3_t);
void CL_FlyEffect (centity_t *, vec3_t); void CL_BfgParticles (entity_t *); void CL_TrapParticles (entity_t *);
void CL_BFGExplosionParticles (vec3_t); void CL_TeleportParticles (vec3_t); void CL_AddParticles (void);
void CL_ParseMuzzleFlash (void); void CL_ParseMuzzleFlash2 (void); void CL_AddDLights(void); void CL_RunDLights(void);
void CL_SetLightstyle(int); void CL_RunLightStyles(void); void CL_EntityEvent (entity_state_t *ent);
int main(void){
  vec3_t a={10.5,-20.25,30}, b={200.75,-50,10.125}, d={0.6,0.8,0}, g={-100,33.3,-7.7};
  centity_t ce; entity_t e; entity_state_t es; byte m[16]; cvar_t fs; int t;
  fs.value=1; cl_footsteps=&fs;
  for(t=0;t<4;t++) cl_sfx_footsteps[t]=S_RegisterSound(t==0?"step1":t==1?"step2":t==2?"step3":"step4");
  srand(1); CL_ClearEffects(); cl.time=1000; cls.frametime=0.016f;
  CL_ParticleEffect(a,d,0xe0,20); CL_ParticleEffect2(b,d,0x10,10); CL_ParticleEffect3(g,d,0x20,10);
  memset(&es,0,sizeof(es)); VectorCopy(a,es.origin); es.number=5; CL_TeleporterParticles(&es);
  CL_LogoutEffect(b,MZ_LOGIN); CL_ItemRespawnParticles(g); CL_ExplosionParticles(a); CL_BlasterParticles(b,d);
  CL_BlasterTrail(a,b); CL_QuadTrail(b,g); CL_FlagTrail(g,a,242);
  memset(&ce,0,sizeof(ce)); ce.trailcount=1024; CL_DiminishingTrail(a,b,&ce,EF_GIB); CL_DiminishingTrail(b,g,&ce,EF_GREENGIB);
  CL_RocketTrail(g,a,&ce); printf("TC %d\n", ce.trailcount);
  CL_RailTrail(a,b); CL_IonripperTrail(b,a); CL_BubbleTrail(a,g);
  ce.fly_stoptime=0; CL_FlyEffect(&ce,a); printf("FS %d\n", ce.fly_stoptime);
  memset(&e,0,sizeof(e)); VectorCopy(b,e.origin); CL_BfgParticles(&e); CL_TrapParticles(&e);
  CL_BFGExplosionParticles(g); CL_TeleportParticles(a);
  cl.time=1250; AP();
  cl.time=1600; AP();
  CL_BigTeleportParticles(b); cl.time=1700; AP();
  cl.time=5000; AP(); cl.time=12000; AP(); /* drain */
  /* muzzle flashes */
  VectorCopy(a,cl_entities[3].current.origin); cl_entities[3].current.angles[1]=37.5; cl_entities[3].current.angles[0]=-12;
  { int w[] = {MZ_BLASTER,MZ_MACHINEGUN,MZ_SHOTGUN,MZ_CHAINGUN2,MZ_CHAINGUN3,MZ_ROCKET,MZ_LOGIN,MZ_RAILGUN|MZ_SILENCED,MZ_TRACKER,MZ_NUKE1};
    for(t=0;t<10;t++){ m[0]=3;m[1]=0;m[2]=w[t]; mbuf=m; mpos=0; CL_ParseMuzzleFlash(); CL_AddDLights(); }}
  { int w[] = {MZ2_INFANTRY_MACHINEGUN_1,MZ2_TANK_MACHINEGUN_5,MZ2_WIDOW2_BEAMER_1,MZ2_GLADIATOR_RAILGUN_1,MZ2_MAKRON_BLASTER_3,200};
    for(t=0;t<6;t++){ m[0]=3;m[1]=0;m[2]=w[t]; mbuf=m; mpos=0; if (w[t]==200) break; CL_ParseMuzzleFlash2(); CL_AddDLights(); }}
  cl.time=1800; CL_RunDLights(); CL_AddDLights(); cl.time=2100; AP();
  es.event=EV_FOOTSTEP; CL_EntityEvent(&es); es.event=EV_PLAYER_TELEPORT; CL_EntityEvent(&es);
  cl.time=2200; AP();
  printf("R %d\n", rand());
  return 0;
}
void Com_Printf (char *fmt, ...){}
