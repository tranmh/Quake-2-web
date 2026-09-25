// Port of ref_gl/gl_rmain.c: refresh main loop, entities, particles, cvars, init, and GetRefAPI
// (createGLRefresh returning the Refresh interface of q2-ref).
import {
  AngleVectors,
  BOX_ON_PLANE_SIDE,
  CONTENTS_SOLID,
  CVAR_ARCHIVE,
  CVAR_USERINFO,
  ERR_DROP,
  PLANE_ANYZ,
  PRINT_ALL,
  PerpendicularVector,
  RDF_NOWORLDMODEL,
  RF_BEAM,
  RF_FULLBRIGHT,
  RF_TRANSLUCENT,
  RotatePointAroundVector,
  VectorAdd,
  VectorCopy,
  VectorMA,
  VectorNormalize,
  VectorScale,
  fr,
} from 'q2-shared';
import type { ImageHandle, ModelHandle, Particle, RefDef, RefEntity, RefImport, Refresh } from 'q2-ref';
import {
  GLState,
  GL_RENDERER_3DLABS,
  GL_RENDERER_MCD,
  GL_RENDERER_OTHER,
  GL_RENDERER_PERMEDIA2,
  GL_RENDERER_POWERVR,
  REF_VERSION,
  type Cvar,
  type GLCvars,
  type Image,
} from './gl_local';
import { Model, mod_alias, mod_brush, mod_sprite } from './gl_model_h';
import {
  GL_Bind,
  GL_ImageList_f,
  GL_InitImages,
  GL_SetTexturePalette,
  GL_ShutdownImages,
  GL_TexEnv,
  GL_TextureAlphaMode,
  GL_TextureMode,
  GL_TextureSolidMode,
  R_RegisterSkin,
  imageFileFor,
  Draw_GetPalette,
} from './gl_image';
import {
  Mod_FreeAll,
  Mod_Init,
  Mod_Modellist_f,
  Mod_PointInLeaf,
  R_BeginRegistration,
  R_EndRegistration,
  R_RegisterModel,
  modelImageDependencies,
} from './gl_model';
import { R_DrawAlphaSurfaces, R_DrawBrushModel, R_DrawWorld, R_MarkLeaves } from './gl_rsurf';
import { R_LightPoint, R_PushDlights, R_RenderDlights } from './gl_light';
import { R_DrawAliasModel } from './gl_mesh';
import { R_SetSky, skyImageNames } from './gl_warp';
import {
  Draw_Char,
  Draw_FadeScreen,
  Draw_Fill,
  Draw_FindPic,
  Draw_GetPicSize,
  Draw_InitLocal,
  Draw_Pic,
  Draw_StretchPic,
  Draw_StretchRaw,
  Draw_TileClear,
  picFileName,
} from './gl_draw';
import {
  GL_ScreenShot_f,
  GL_SetDefaultState,
  GL_Strings_f,
  GL_UpdateSwapInterval,
  R_InitParticleTexture,
} from './gl_rmisc';
import {
  GL_ALPHA_TEST,
  GL_BLEND,
  GL_COLOR_BUFFER_BIT,
  GL_CULL_FACE,
  GL_DEPTH_BUFFER_BIT,
  GL_DEPTH_TEST,
  GL_FRONT,
  GL_GEQUAL,
  GL_LEQUAL,
  GL_MODELVIEW,
  GL_MODULATE,
  GL_PROJECTION,
  GL_QUADS,
  GL_REPLACE,
  GL_SCISSOR_TEST,
  GL_TEXTURE_2D,
  GL_TRIANGLES,
  GL_TRIANGLE_FAN,
  GL_TRIANGLE_STRIP,
  QGL,
  type QGLOptions,
} from './qgl';

// C: gl_rmain.c:143 R_CullBox -- returns true if the box is completely outside the frustom
export function R_CullBox(r: GLState, mins: ArrayLike<number>, maxs: ArrayLike<number>): boolean {
  if (r.cv.r_nocull.value) return false;
  for (let i = 0; i < 4; i++) if (BOX_ON_PLANE_SIDE(mins, maxs, r.frustum[i]!) === 2) return true;
  return false;
}

// C: gl_rmain.c:157 R_RotateForEntity
export function R_RotateForEntity(r: GLState, e: RefEntity): void {
  const qgl = r.qgl;
  if (!qgl) return;
  qgl.translatef(e.origin[0]!, e.origin[1]!, e.origin[2]!);
  qgl.rotatef(e.angles[1]!, 0, 0, 1);
  qgl.rotatef(-e.angles[0]!, 0, 1, 0);
  qgl.rotatef(-e.angles[2]!, 1, 0, 0);
}

// =============================================================
// SPRITE MODELS

const spPoint = new Float32Array(3);

// C: gl_rmain.c:181 R_DrawSpriteModel
export function R_DrawSpriteModel(r: GLState, e: RefEntity): void {
  let alpha = 1.0;
  // don't even bother culling, because it's just a single polygon without a surface cache
  const psprite = r.currentmodel.sprite!;
  e.frame %= psprite.frames.length;
  const frame = psprite.frames[e.frame];
  if (!frame) return; // port: negative frame numbers index before the array in C
  const qgl = r.qgl;

  // normal sprite
  const up = r.vup;
  const right = r.vright;

  if (e.flags & RF_TRANSLUCENT) alpha = fr(e.alpha);
  if (alpha !== 1.0) qgl?.enable(GL_BLEND);
  qgl?.color4f(1, 1, 1, alpha);

  GL_Bind(r, r.currentmodel.skins[e.frame]?.texnum ?? r.r_notexture.texnum);
  GL_TexEnv(r, GL_MODULATE);

  if (alpha === 1.0) qgl?.enable(GL_ALPHA_TEST);
  else qgl?.disable(GL_ALPHA_TEST);

  if (qgl) {
    const point = spPoint;
    qgl.begin(GL_QUADS);
    qgl.texCoord2f(0, 1);
    VectorMA(e.origin, -frame.origin_y, up, point);
    VectorMA(point, -frame.origin_x, right, point);
    qgl.vertex3fv(point);

    qgl.texCoord2f(0, 0);
    VectorMA(e.origin, frame.height - frame.origin_y, up, point);
    VectorMA(point, -frame.origin_x, right, point);
    qgl.vertex3fv(point);

    qgl.texCoord2f(1, 0);
    VectorMA(e.origin, frame.height - frame.origin_y, up, point);
    VectorMA(point, frame.width - frame.origin_x, right, point);
    qgl.vertex3fv(point);

    qgl.texCoord2f(1, 1);
    VectorMA(e.origin, -frame.origin_y, up, point);
    VectorMA(point, frame.width - frame.origin_x, right, point);
    qgl.vertex3fv(point);
    qgl.end();
  }

  qgl?.disable(GL_ALPHA_TEST);
  GL_TexEnv(r, GL_REPLACE);
  if (alpha !== 1.0) qgl?.disable(GL_BLEND);
  qgl?.color4f(1, 1, 1, 1);
}

// ==================================================================================

const nmShadelight = new Float32Array(3);

// C: gl_rmain.c:278 R_DrawNullModel
export function R_DrawNullModel(r: GLState): void {
  const sl = nmShadelight;
  const ce = r.currententity;
  if (ce.flags & RF_FULLBRIGHT) sl[0] = sl[1] = sl[2] = 1.0;
  else R_LightPoint(r, ce.origin, sl);

  const qgl = r.qgl;
  if (!qgl) return;
  qgl.pushMatrix();
  R_RotateForEntity(r, ce);

  qgl.disable(GL_TEXTURE_2D);
  qgl.color3f(sl[0]!, sl[1]!, sl[2]!);

  qgl.begin(GL_TRIANGLE_FAN);
  qgl.vertex3f(0, 0, -16);
  for (let i = 0; i <= 4; i++)
    qgl.vertex3f(fr(16 * Math.cos((i * Math.PI) / 2)), fr(16 * Math.sin((i * Math.PI) / 2)), 0);
  qgl.end();

  qgl.begin(GL_TRIANGLE_FAN);
  qgl.vertex3f(0, 0, 16);
  for (let i = 4; i >= 0; i--)
    qgl.vertex3f(fr(16 * Math.cos((i * Math.PI) / 2)), fr(16 * Math.sin((i * Math.PI) / 2)), 0);
  qgl.end();

  qgl.color3f(1, 1, 1);
  qgl.popMatrix();
  qgl.enable(GL_TEXTURE_2D);
}

function drawEntity(r: GLState, ce: RefEntity): void {
  if (ce.flags & RF_BEAM) {
    R_DrawBeam(r, ce);
    return;
  }
  r.currentmodel = ce.model as unknown as Model;
  if (!r.currentmodel) {
    R_DrawNullModel(r);
    return;
  }
  switch (r.currentmodel.type) {
    case mod_alias:
      R_DrawAliasModel(r, ce);
      break;
    case mod_brush:
      R_DrawBrushModel(r, ce);
      break;
    case mod_sprite:
      R_DrawSpriteModel(r, ce);
      break;
    default:
      r.ri.sysError(ERR_DROP, 'Bad modeltype');
  }
}

// C: gl_rmain.c:316 R_DrawEntitiesOnList
export function R_DrawEntitiesOnList(r: GLState): void {
  if (!r.cv.r_drawentities.value) return;
  const rd = r.r_newrefdef;

  // draw non-transparent first
  for (let i = 0; i < rd.num_entities; i++) {
    r.currententity = rd.entities[i]!;
    if (r.currententity.flags & RF_TRANSLUCENT) continue; // solid
    drawEntity(r, r.currententity);
  }

  // draw transparent entities; we could sort these if it ever becomes a problem...
  r.qgl?.depthMask(0); // no z writes
  for (let i = 0; i < rd.num_entities; i++) {
    r.currententity = rd.entities[i]!;
    if (!(r.currententity.flags & RF_TRANSLUCENT)) continue; // solid
    drawEntity(r, r.currententity);
  }
  r.qgl?.depthMask(1); // back to writing
}

const pUp = new Float32Array(3);
const pRight = new Float32Array(3);

// C: gl_rmain.c:407 GL_DrawParticles
export function GL_DrawParticles(
  r: GLState,
  num_particles: number,
  particles: Particle[],
  colortable: Uint32Array,
): void {
  const qgl = r.qgl;
  if (!qgl) return;
  GL_Bind(r, r.r_particletexture.texnum);
  qgl.depthMask(false); // no z buffering
  qgl.enable(GL_BLEND);
  GL_TexEnv(r, GL_MODULATE);
  qgl.begin(GL_TRIANGLES);

  VectorScale(r.vup, 1.5, pUp);
  VectorScale(r.vright, 1.5, pRight);
  const o = r.r_origin;
  const vpn = r.vpn;

  for (let i = 0; i < num_particles; i++) {
    const p = particles[i]!;
    const po = p.origin;
    // hack a scale up to keep particles from disapearing
    let scale = fr(
      fr(fr(fr(po[0]! - o[0]!) * vpn[0]!) + fr(fr(po[1]! - o[1]!) * vpn[1]!)) +
        fr(fr(po[2]! - o[2]!) * vpn[2]!),
    );
    if (scale < 20) scale = 1;
    else scale = fr(1 + scale * 0.004);

    const c = colortable[p.color & 255]!;
    qgl.color4ub(c & 255, (c >>> 8) & 255, (c >>> 16) & 255, Math.trunc(fr(p.alpha * 255)) & 255);

    qgl.texCoord2f(0.0625, 0.0625);
    qgl.vertex3f(po[0]!, po[1]!, po[2]!);

    qgl.texCoord2f(1.0625, 0.0625);
    qgl.vertex3f(
      fr(po[0]! + fr(pUp[0]! * scale)),
      fr(po[1]! + fr(pUp[1]! * scale)),
      fr(po[2]! + fr(pUp[2]! * scale)),
    );

    qgl.texCoord2f(0.0625, 1.0625);
    qgl.vertex3f(
      fr(po[0]! + fr(pRight[0]! * scale)),
      fr(po[1]! + fr(pRight[1]! * scale)),
      fr(po[2]! + fr(pRight[2]! * scale)),
    );
  }

  qgl.end();
  qgl.disable(GL_BLEND);
  qgl.color4f(1, 1, 1, 1);
  qgl.depthMask(1); // back to normal Z buffering
  GL_TexEnv(r, GL_REPLACE);
}

// C: gl_rmain.c:467 R_DrawParticles -- qglPointParameterfEXT is NULL in WebGL: always GL_DrawParticles
export function R_DrawParticles(r: GLState): void {
  GL_DrawParticles(r, r.r_newrefdef.num_particles, r.r_newrefdef.particles, r.d_8to24table);
}

// C: gl_rmain.c:510 R_PolyBlend
export function R_PolyBlend(r: GLState): void {
  if (!r.cv.gl_polyblend.value) return;
  if (!r.v_blend[3]) return;
  const qgl = r.qgl;
  if (!qgl) return;

  qgl.disable(GL_ALPHA_TEST);
  qgl.enable(GL_BLEND);
  qgl.disable(GL_DEPTH_TEST);
  qgl.disable(GL_TEXTURE_2D);

  qgl.loadIdentity();
  // FIXME: get rid of these
  qgl.rotatef(-90, 1, 0, 0); // put Z going up
  qgl.rotatef(90, 0, 0, 1); // put Z going up

  qgl.color4f(r.v_blend[0]!, r.v_blend[1]!, r.v_blend[2]!, r.v_blend[3]!);
  qgl.begin(GL_QUADS);
  qgl.vertex3f(10, 100, 100);
  qgl.vertex3f(10, -100, 100);
  qgl.vertex3f(10, -100, -100);
  qgl.vertex3f(10, 100, -100);
  qgl.end();

  qgl.disable(GL_BLEND);
  qgl.enable(GL_TEXTURE_2D);
  qgl.enable(GL_ALPHA_TEST);
  qgl.color4f(1, 1, 1, 1);
}

// =======================================================================

// C: gl_rmain.c:547 SignbitsForPlane
export function SignbitsForPlane(normal: ArrayLike<number>): number {
  // for fast box on planeside test
  let bits = 0;
  for (let j = 0; j < 3; j++) if (normal[j]! < 0) bits |= 1 << j;
  return bits;
}

// C: gl_rmain.c:563 R_SetFrustum
export function R_SetFrustum(r: GLState): void {
  const f = r.frustum;
  const rd = r.r_newrefdef;
  // rotate VPN right by FOV_X/2 degrees
  RotatePointAroundVector(f[0]!.normal, r.vup, r.vpn, -(90 - fr(rd.fov_x / 2)));
  // rotate VPN left by FOV_X/2 degrees
  RotatePointAroundVector(f[1]!.normal, r.vup, r.vpn, 90 - fr(rd.fov_x / 2));
  // rotate VPN up by FOV_X/2 degrees
  RotatePointAroundVector(f[2]!.normal, r.vright, r.vpn, 90 - fr(rd.fov_y / 2));
  // rotate VPN down by FOV_X/2 degrees
  RotatePointAroundVector(f[3]!.normal, r.vright, r.vpn, -(90 - fr(rd.fov_y / 2)));

  for (let i = 0; i < 4; i++) {
    const p = f[i]!;
    p.type = PLANE_ANYZ;
    p.dist = fr(
      fr(fr(r.r_origin[0]! * p.normal[0]!) + fr(r.r_origin[1]! * p.normal[1]!)) +
        fr(r.r_origin[2]! * p.normal[2]!),
    );
    p.signbits = SignbitsForPlane(p.normal);
  }
}

// =======================================================================

const sfTemp = new Float32Array(3);

// C: gl_rmain.c:610 R_SetupFrame
export function R_SetupFrame(r: GLState): void {
  r.r_framecount++;
  const rd = r.r_newrefdef;

  // build the transformation matrix for the given view angles
  VectorCopy(rd.vieworg, r.r_origin);
  AngleVectors(rd.viewangles, r.vpn, r.vright, r.vup);

  // current viewcluster
  if (!(rd.rdflags & RDF_NOWORLDMODEL)) {
    r.r_oldviewcluster = r.r_viewcluster;
    r.r_oldviewcluster2 = r.r_viewcluster2;
    let leaf = Mod_PointInLeaf(r, r.r_origin, r.r_worldmodel);
    r.r_viewcluster = r.r_viewcluster2 = leaf.cluster;

    // check above and below so crossing solid water doesn't draw wrong
    VectorCopy(r.r_origin, sfTemp);
    if (!leaf.contents) {
      // look down a bit
      sfTemp[2] = sfTemp[2]! - 16;
    } else {
      // look up a bit
      sfTemp[2] = sfTemp[2]! + 16;
    }
    leaf = Mod_PointInLeaf(r, sfTemp, r.r_worldmodel);
    if (!(leaf.contents & CONTENTS_SOLID) && leaf.cluster !== r.r_viewcluster2)
      r.r_viewcluster2 = leaf.cluster;
  }

  for (let i = 0; i < 4; i++) r.v_blend[i] = rd.blend[i]!;

  r.c_brush_polys = 0;
  r.c_alias_polys = 0;

  // clear out the portion of the screen that the NOWORLDMODEL defines
  if (rd.rdflags & RDF_NOWORLDMODEL) {
    const qgl = r.qgl;
    qgl?.enable(GL_SCISSOR_TEST);
    qgl?.clearColor(0.3, 0.3, 0.3, 1);
    qgl?.scissor(rd.x, r.vid.height - rd.height - rd.y, rd.width, rd.height);
    qgl?.clear(GL_COLOR_BUFFER_BIT | GL_DEPTH_BUFFER_BIT);
    qgl?.clearColor(1, 0, 0.5, 0.5);
    qgl?.disable(GL_SCISSOR_TEST);
  }
}

// C: gl_rmain.c:674 MYgluPerspective
export function MYgluPerspective(
  r: GLState,
  fovy: number,
  aspect: number,
  zNear: number,
  zFar: number,
): void {
  const ymax = zNear * Math.tan((fovy * Math.PI) / 360.0);
  const ymin = -ymax;
  let xmin = ymin * aspect;
  let xmax = ymax * aspect;
  xmin += -(2 * r.gl_state.camera_separation) / zNear;
  xmax += -(2 * r.gl_state.camera_separation) / zNear;
  r.qgl?.frustum(xmin, xmax, ymin, ymax, zNear, zFar);
}

// C: gl_rmain.c:697 R_SetupGL
export function R_SetupGL(r: GLState): void {
  const rd = r.r_newrefdef;
  const vid = r.vid;
  const qgl = r.qgl;
  if (!qgl) return;

  // set up viewport (integer arithmetic like C)
  const x = Math.floor(Math.trunc((rd.x * vid.width) / vid.width));
  const x2 = Math.ceil(Math.trunc(((rd.x + rd.width) * vid.width) / vid.width));
  const y = Math.floor(vid.height - Math.trunc((rd.y * vid.height) / vid.height));
  const y2 = Math.ceil(vid.height - Math.trunc(((rd.y + rd.height) * vid.height) / vid.height));
  const w = x2 - x;
  const h = y - y2;
  qgl.viewport(x, y2, w, h);

  // set up projection matrix
  const screenaspect = fr(rd.width / rd.height);
  qgl.matrixMode(GL_PROJECTION);
  qgl.loadIdentity();
  MYgluPerspective(r, rd.fov_y, screenaspect, 4, 4096);

  qgl.cullFace(GL_FRONT);

  qgl.matrixMode(GL_MODELVIEW);
  qgl.loadIdentity();

  qgl.rotatef(-90, 1, 0, 0); // put Z going up
  qgl.rotatef(90, 0, 0, 1); // put Z going up
  qgl.rotatef(-rd.viewangles[2]!, 1, 0, 0);
  qgl.rotatef(-rd.viewangles[0]!, 0, 1, 0);
  qgl.rotatef(-rd.viewangles[1]!, 0, 0, 1);
  qgl.translatef(-rd.vieworg[0]!, -rd.vieworg[1]!, -rd.vieworg[2]!);

  qgl.getModelview(r.r_world_matrix);

  // set drawing parms
  if (r.cv.gl_cull.value) qgl.enable(GL_CULL_FACE);
  else qgl.disable(GL_CULL_FACE);

  qgl.disable(GL_BLEND);
  qgl.disable(GL_ALPHA_TEST);
  qgl.enable(GL_DEPTH_TEST);
}

// C: gl_rmain.c:760 R_Clear
export function R_Clear(r: GLState): void {
  const qgl = r.qgl;
  if (r.cv.gl_ztrick.value) {
    if (r.cv.gl_clear.value) qgl?.clear(GL_COLOR_BUFFER_BIT);
    r.trickframe++;
    if (r.trickframe & 1) {
      r.gldepthmin = 0;
      r.gldepthmax = fr(0.49999);
      qgl?.depthFunc(GL_LEQUAL);
    } else {
      r.gldepthmin = 1;
      r.gldepthmax = 0.5;
      qgl?.depthFunc(GL_GEQUAL);
    }
  } else {
    if (r.cv.gl_clear.value) qgl?.clear(GL_COLOR_BUFFER_BIT | GL_DEPTH_BUFFER_BIT);
    else qgl?.clear(GL_DEPTH_BUFFER_BIT);
    r.gldepthmin = 0;
    r.gldepthmax = 1;
    qgl?.depthFunc(GL_LEQUAL);
  }
  qgl?.depthRange(r.gldepthmin, r.gldepthmax);
}

// C: gl_rmain.c:798 R_Flash
export function R_Flash(r: GLState): void {
  R_PolyBlend(r);
}

/** C: r_newrefdef = *fd (arrays are referenced, like the pointers inside refdef_t). */
function copyRefDef(dst: RefDef, fd: RefDef): void {
  dst.x = fd.x;
  dst.y = fd.y;
  dst.width = fd.width;
  dst.height = fd.height;
  dst.fov_x = fr(fd.fov_x);
  dst.fov_y = fr(fd.fov_y);
  VectorCopy(fd.vieworg, dst.vieworg);
  VectorCopy(fd.viewangles, dst.viewangles);
  for (let i = 0; i < 4; i++) dst.blend[i] = fd.blend[i]!;
  dst.time = fr(fd.time);
  dst.rdflags = fd.rdflags;
  dst.areabits = fd.areabits;
  dst.lightstyles = fd.lightstyles;
  dst.num_entities = fd.num_entities;
  dst.entities = fd.entities;
  dst.num_dlights = fd.num_dlights;
  dst.dlights = fd.dlights;
  dst.num_particles = fd.num_particles;
  dst.particles = fd.particles;
}

// C: gl_rmain.c:810 R_RenderView -- r_newrefdef must be set before the first call
export function R_RenderView(r: GLState, fd: RefDef): void {
  if (r.cv.r_norefresh.value) return;

  copyRefDef(r.r_newrefdef, fd);

  if (!r.r_worldmodel && !(r.r_newrefdef.rdflags & RDF_NOWORLDMODEL))
    r.ri.sysError(ERR_DROP, 'R_RenderView: NULL worldmodel');

  if (r.cv.r_speeds.value) {
    r.c_brush_polys = 0;
    r.c_alias_polys = 0;
  }

  R_PushDlights(r);

  if (r.cv.gl_finish.value) r.qgl?.finish();

  R_SetupFrame(r);
  R_SetFrustum(r);
  R_SetupGL(r);
  R_MarkLeaves(r); // done here so we know if we're in water
  R_DrawWorld(r);
  R_DrawEntitiesOnList(r);
  R_RenderDlights(r);
  R_DrawParticles(r);
  R_DrawAlphaSurfaces(r);
  R_Flash(r);

  if (r.cv.r_speeds.value) {
    r.ri.conPrintf(
      PRINT_ALL,
      `${String(r.c_brush_polys).padStart(4)} wpoly ${String(r.c_alias_polys).padStart(4)} epoly ${r.c_visible_textures} tex ${r.c_visible_lightmaps} lmaps\n`,
    );
  }
}

// C: gl_rmain.c:862 R_SetGL2D
export function R_SetGL2D(r: GLState): void {
  const qgl = r.qgl;
  if (!qgl) return;
  // set 2D virtual screen size
  qgl.viewport(0, 0, r.vid.width, r.vid.height);
  qgl.matrixMode(GL_PROJECTION);
  qgl.loadIdentity();
  qgl.ortho(0, r.vid.width, r.vid.height, 0, -99999, 99999);
  qgl.matrixMode(GL_MODELVIEW);
  qgl.loadIdentity();
  qgl.disable(GL_DEPTH_TEST);
  qgl.disable(GL_CULL_FACE);
  qgl.disable(GL_BLEND);
  qgl.enable(GL_ALPHA_TEST);
  qgl.color4f(1, 1, 1, 1);
}

const llShadelight = new Float32Array(3);

// C: gl_rmain.c:926 R_SetLightLevel
export function R_SetLightLevel(r: GLState): void {
  if (r.r_newrefdef.rdflags & RDF_NOWORLDMODEL) return;
  // save off light value for server to look at (BIG HACK!)
  const sl = llShadelight;
  R_LightPoint(r, r.r_newrefdef.vieworg, sl);
  // pick the greatest component, which should be the same as the mono value returned by software
  if (sl[0]! > sl[1]!) {
    if (sl[0]! > sl[2]!) r.cv.r_lightlevel.value = fr(150 * sl[0]!);
    else r.cv.r_lightlevel.value = fr(150 * sl[2]!);
  } else {
    if (sl[1]! > sl[2]!) r.cv.r_lightlevel.value = fr(150 * sl[1]!);
    else r.cv.r_lightlevel.value = fr(150 * sl[2]!);
  }
}

// C: gl_rmain.c:962 R_RenderFrame
export function R_RenderFrame(r: GLState, fd: RefDef): void {
  R_RenderView(r, fd);
  R_SetLightLevel(r);
  R_SetGL2D(r);
}

// C: gl_rmain.c:970 R_Register
export function R_Register(r: GLState): void {
  const g = (name: string, value: string, flags: number): Cvar => r.ri.cvarGet(name, value, flags);
  const cv: Partial<GLCvars> = {
    r_lefthand: g('hand', '0', CVAR_USERINFO | CVAR_ARCHIVE),
    r_norefresh: g('r_norefresh', '0', 0),
    r_fullbright: g('r_fullbright', '0', 0),
    r_drawentities: g('r_drawentities', '1', 0),
    r_drawworld: g('r_drawworld', '1', 0),
    r_novis: g('r_novis', '0', 0),
    r_nocull: g('r_nocull', '0', 0),
    r_lerpmodels: g('r_lerpmodels', '1', 0),
    r_speeds: g('r_speeds', '0', 0),
    r_lightlevel: g('r_lightlevel', '0', 0),
    gl_nosubimage: g('gl_nosubimage', '0', 0),
    gl_allow_software: g('gl_allow_software', '0', 0),
    gl_particle_min_size: g('gl_particle_min_size', '2', CVAR_ARCHIVE),
    gl_particle_max_size: g('gl_particle_max_size', '40', CVAR_ARCHIVE),
    gl_particle_size: g('gl_particle_size', '40', CVAR_ARCHIVE),
    gl_particle_att_a: g('gl_particle_att_a', '0.01', CVAR_ARCHIVE),
    gl_particle_att_b: g('gl_particle_att_b', '0.0', CVAR_ARCHIVE),
    gl_particle_att_c: g('gl_particle_att_c', '0.01', CVAR_ARCHIVE),
    gl_modulate: g('gl_modulate', '1', CVAR_ARCHIVE),
    gl_log: g('gl_log', '0', 0),
    gl_bitdepth: g('gl_bitdepth', '0', 0),
    gl_mode: g('gl_mode', '3', CVAR_ARCHIVE),
    gl_lightmap: g('gl_lightmap', '0', 0),
    gl_shadows: g('gl_shadows', '0', CVAR_ARCHIVE),
    gl_dynamic: g('gl_dynamic', '1', 0),
    gl_nobind: g('gl_nobind', '0', 0),
    gl_round_down: g('gl_round_down', '1', 0),
    gl_picmip: g('gl_picmip', '0', 0),
    gl_skymip: g('gl_skymip', '0', 0),
    gl_showtris: g('gl_showtris', '0', 0),
    gl_ztrick: g('gl_ztrick', '0', 0),
    gl_finish: g('gl_finish', '0', CVAR_ARCHIVE),
    gl_clear: g('gl_clear', '0', 0),
    gl_cull: g('gl_cull', '1', 0),
    gl_polyblend: g('gl_polyblend', '1', 0),
    gl_flashblend: g('gl_flashblend', '0', 0),
    gl_playermip: g('gl_playermip', '0', 0),
    gl_monolightmap: g('gl_monolightmap', '0', 0),
    gl_driver: g('gl_driver', 'opengl32', CVAR_ARCHIVE),
    gl_texturemode: g('gl_texturemode', 'GL_LINEAR_MIPMAP_NEAREST', CVAR_ARCHIVE),
    gl_texturealphamode: g('gl_texturealphamode', 'default', CVAR_ARCHIVE),
    gl_texturesolidmode: g('gl_texturesolidmode', 'default', CVAR_ARCHIVE),
    gl_lockpvs: g('gl_lockpvs', '0', 0),
    gl_vertex_arrays: g('gl_vertex_arrays', '0', CVAR_ARCHIVE),
    gl_ext_swapinterval: g('gl_ext_swapinterval', '1', CVAR_ARCHIVE),
    gl_ext_palettedtexture: g('gl_ext_palettedtexture', '1', CVAR_ARCHIVE),
    gl_ext_multitexture: g('gl_ext_multitexture', '1', CVAR_ARCHIVE),
    gl_ext_pointparameters: g('gl_ext_pointparameters', '1', CVAR_ARCHIVE),
    gl_ext_compiled_vertex_array: g('gl_ext_compiled_vertex_array', '1', CVAR_ARCHIVE),
    gl_drawbuffer: g('gl_drawbuffer', 'GL_BACK', 0),
    gl_swapinterval: g('gl_swapinterval', '1', CVAR_ARCHIVE),
    gl_saturatelighting: g('gl_saturatelighting', '0', 0),
    gl_3dlabs_broken: g('gl_3dlabs_broken', '1', CVAR_ARCHIVE),
    vid_fullscreen: g('vid_fullscreen', '0', CVAR_ARCHIVE),
    vid_gamma: g('vid_gamma', '1.0', CVAR_ARCHIVE),
    vid_ref: g('vid_ref', 'soft', CVAR_ARCHIVE),
  };
  cv.intensity = r.cv?.intensity ?? g('intensity', '2', 0);
  r.cv = cv as GLCvars;

  r.ri.addCommand('imagelist', () => GL_ImageList_f(r));
  r.ri.addCommand('screenshot', () => GL_ScreenShot_f(r, r.screenshotHook ?? undefined));
  r.ri.addCommand('modellist', () => Mod_Modellist_f(r));
  r.ri.addCommand('gl_strings', () => GL_Strings_f(r));
}

/**
 * C: gl_rmain.c:1050 R_SetMode. There are no video modes in the browser: the drawing buffer size of the
 * canvas (set by the embedding page) is the video size. The modified flags are cleared like C.
 */
export function R_SetMode(r: GLState): boolean {
  r.cv.vid_fullscreen.modified = false;
  r.cv.gl_mode.modified = false;
  if (r.qgl) {
    r.vid.width = r.qgl.drawingBufferWidth;
    r.vid.height = r.qgl.drawingBufferHeight;
  }
  r.gl_state.prev_mode = r.cv.gl_mode.value;
  return true;
}

// C: gl_rmain.c:1103 R_Init (after the async prefetch of pics/colormap.pcx and pics/conchars.pcx)
export function R_Init(r: GLState): boolean {
  for (let j = 0; j < 256; j++) r.r_turbsin[j] = r.r_turbsin[j]! * 0.5;

  r.ri.conPrintf(PRINT_ALL, `ref_gl version: ${REF_VERSION}\n`);

  Draw_GetPalette(r);
  R_Register(r);

  // set our "safe" modes
  r.gl_state.prev_mode = 3;
  if (!R_SetMode(r)) {
    r.ri.conPrintf(PRINT_ALL, 'ref_gl::R_Init() - could not R_SetMode()\n');
    return false;
  }

  // get our various GL strings
  const gl = r.qgl?.gl;
  r.gl_config.vendor_string = gl ? String(gl.getParameter(gl.VENDOR)) : 'none';
  r.ri.conPrintf(PRINT_ALL, `GL_VENDOR: ${r.gl_config.vendor_string}\n`);
  r.gl_config.renderer_string = gl ? String(gl.getParameter(gl.RENDERER)) : 'none';
  r.ri.conPrintf(PRINT_ALL, `GL_RENDERER: ${r.gl_config.renderer_string}\n`);
  r.gl_config.version_string = gl ? String(gl.getParameter(gl.VERSION)) : 'none';
  r.ri.conPrintf(PRINT_ALL, `GL_VERSION: ${r.gl_config.version_string}\n`);
  r.gl_config.extensions_string = gl ? (gl.getSupportedExtensions() ?? []).join(' ') : '';
  r.ri.conPrintf(PRINT_ALL, `GL_EXTENSIONS: ${r.gl_config.extensions_string}\n`);

  const renderer_buffer = r.gl_config.renderer_string.toLowerCase();
  const vendor_buffer = r.gl_config.vendor_string.toLowerCase();
  if (renderer_buffer.includes('voodoo')) r.gl_config.renderer = renderer_buffer.includes('rush') ? 0x4 : 0x1;
  else if (vendor_buffer.includes('sgi')) r.gl_config.renderer = 0x00f00000;
  else if (renderer_buffer.includes('permedia')) r.gl_config.renderer = GL_RENDERER_PERMEDIA2;
  else if (renderer_buffer.includes('glint')) r.gl_config.renderer = 0x00000200;
  else if (renderer_buffer.includes('glzicd')) r.gl_config.renderer = 0x00001000;
  else if (renderer_buffer.includes('gdi')) r.gl_config.renderer = GL_RENDERER_MCD;
  else if (renderer_buffer.includes('pcx2')) r.gl_config.renderer = 0x00000020;
  else if (renderer_buffer.includes('verite')) r.gl_config.renderer = 0x001c0000;
  else r.gl_config.renderer = GL_RENDERER_OTHER;

  if ((r.cv.gl_monolightmap.string[1] ?? '').toUpperCase() !== 'F') {
    if (r.gl_config.renderer === GL_RENDERER_PERMEDIA2) {
      r.ri.cvarSet('gl_monolightmap', 'A');
      r.ri.conPrintf(PRINT_ALL, "...using gl_monolightmap 'a'\n");
    } else r.ri.cvarSet('gl_monolightmap', '0');
  }

  // power vr can't have anything stay in the framebuffer, so the screen needs to redraw the tiled background every frame
  if (r.gl_config.renderer & GL_RENDERER_POWERVR) r.ri.cvarSet('scr_drawall', '1');
  else r.ri.cvarSet('scr_drawall', '0');

  // MCD has buffering issues
  if (r.gl_config.renderer === GL_RENDERER_MCD) r.ri.cvarSet('gl_finish', '1');

  if (r.gl_config.renderer & GL_RENDERER_3DLABS) r.gl_config.allow_cds = !r.cv.gl_3dlabs_broken.value;
  else r.gl_config.allow_cds = true;
  r.ri.conPrintf(PRINT_ALL, r.gl_config.allow_cds ? '...allowing CDS\n' : '...disabling CDS\n');

  // grab extensions: the WIN32 extension block finds none of them in WebGL
  r.ri.conPrintf(PRINT_ALL, '...GL_EXT_compiled_vertex_array not found\n');
  r.ri.conPrintf(PRINT_ALL, '...GL_EXT_point_parameters not found\n');
  r.ri.conPrintf(PRINT_ALL, '...GL_EXT_shared_texture_palette not found\n');
  r.ri.conPrintf(PRINT_ALL, '...GL_SGIS_multitexture not found\n');

  GL_SetDefaultState(r);

  GL_InitImages(r);
  Mod_Init(r);
  R_InitParticleTexture(r);
  Draw_InitLocal(r);

  const err = r.qgl?.getError() ?? 0;
  if (err !== 0) r.ri.conPrintf(PRINT_ALL, `glGetError() = 0x${err.toString(16)}\n`);
  return true;
}

// C: gl_rmain.c:1348 R_Shutdown
export function R_Shutdown(r: GLState): void {
  r.ri.removeCommand('modellist');
  r.ri.removeCommand('screenshot');
  r.ri.removeCommand('imagelist');
  r.ri.removeCommand('gl_strings');
  Mod_FreeAll(r);
  GL_ShutdownImages(r);
}

// C: gl_rmain.c:1377 R_BeginFrame
export function R_BeginFrame(r: GLState, camera_separation: number): void {
  r.gl_state.camera_separation = fr(camera_separation);

  // change modes if necessary
  if (r.cv.gl_mode.modified || r.cv.vid_fullscreen.modified) {
    // FIXME: only restart if CDS is required
    const ref = r.ri.cvarGet('vid_ref', 'gl', 0);
    ref.modified = true;
  }

  if (r.cv.gl_log.modified) r.cv.gl_log.modified = false; // GLimp_EnableLogging: no GL logging in the port

  // update 3Dfx gamma -- it is expected that a user will do a vid_restart after tweaking this value
  if (r.cv.vid_gamma.modified) r.cv.vid_gamma.modified = false;

  // GLimp_BeginFrame: the canvas drawing buffer is the video size
  const qgl = r.qgl;
  if (qgl) {
    r.vid.width = qgl.drawingBufferWidth;
    r.vid.height = qgl.drawingBufferHeight;
    qgl.resetStats();
  }

  // go into 2D mode
  if (qgl) {
    qgl.viewport(0, 0, r.vid.width, r.vid.height);
    qgl.matrixMode(GL_PROJECTION);
    qgl.loadIdentity();
    qgl.ortho(0, r.vid.width, r.vid.height, 0, -99999, 99999);
    qgl.matrixMode(GL_MODELVIEW);
    qgl.loadIdentity();
    qgl.disable(GL_DEPTH_TEST);
    qgl.disable(GL_CULL_FACE);
    qgl.disable(GL_BLEND);
    qgl.enable(GL_ALPHA_TEST);
    qgl.color4f(1, 1, 1, 1);
  }

  // draw buffer stuff (GL_FRONT / GL_BACK selection does not exist in WebGL)
  if (r.cv.gl_drawbuffer.modified) r.cv.gl_drawbuffer.modified = false;

  // texturemode stuff
  if (r.cv.gl_texturemode.modified) {
    GL_TextureMode(r, r.cv.gl_texturemode.string);
    r.cv.gl_texturemode.modified = false;
  }
  if (r.cv.gl_texturealphamode.modified) {
    GL_TextureAlphaMode(r, r.cv.gl_texturealphamode.string);
    r.cv.gl_texturealphamode.modified = false;
  }
  if (r.cv.gl_texturesolidmode.modified) {
    GL_TextureSolidMode(r, r.cv.gl_texturesolidmode.string);
    r.cv.gl_texturesolidmode.modified = false;
  }

  // swapinterval stuff
  GL_UpdateSwapInterval(r);

  // clear screen if desired
  R_Clear(r);
}

// C: gl_rmain.c:1497 R_SetPalette
export function R_SetPalette(r: GLState, palette: Uint8Array | null): void {
  const rp = r.r_rawpalette;
  if (palette) {
    for (let i = 0; i < 256; i++) {
      rp[i] =
        (palette[i * 3]! | (palette[i * 3 + 1]! << 8) | (palette[i * 3 + 2]! << 16) | (0xff << 24)) >>> 0;
    }
  } else {
    for (let i = 0; i < 256; i++) {
      const c = r.d_8to24table[i]!;
      rp[i] = ((c & 0xff) | (((c >>> 8) & 0xff) << 8) | (((c >>> 16) & 0xff) << 16) | (0xff << 24)) >>> 0;
    }
  }
  GL_SetTexturePalette(r, rp);
  const qgl = r.qgl;
  qgl?.rawPalette(rp);
  qgl?.clearColor(0, 0, 0, 0);
  qgl?.clear(GL_COLOR_BUFFER_BIT);
  qgl?.clearColor(1, 0, 0.5, 0.5);
}

const NUM_BEAM_SEGS = 6;
const bmPerpvec = new Float32Array(3);
const bmDirection = new Float32Array(3);
const bmNormDir = new Float32Array(3);
const bmStart: Float32Array[] = [];
const bmEnd: Float32Array[] = [];
for (let i = 0; i < NUM_BEAM_SEGS; i++) {
  bmStart.push(new Float32Array(3));
  bmEnd.push(new Float32Array(3));
}
const bmOld = new Float32Array(3);
const bmOrg = new Float32Array(3);

// C: gl_rmain.c:1533 R_DrawBeam
export function R_DrawBeam(r: GLState, e: RefEntity): void {
  VectorCopy(e.oldorigin, bmOld);
  VectorCopy(e.origin, bmOrg);
  bmNormDir[0] = bmDirection[0] = bmOld[0]! - bmOrg[0]!;
  bmNormDir[1] = bmDirection[1] = bmOld[1]! - bmOrg[1]!;
  bmNormDir[2] = bmDirection[2] = bmOld[2]! - bmOrg[2]!;

  if (VectorNormalize(bmNormDir) === 0) return;

  PerpendicularVector(bmPerpvec, bmNormDir);
  VectorScale(bmPerpvec, Math.trunc(e.frame / 2), bmPerpvec);

  for (let i = 0; i < 6; i++) {
    RotatePointAroundVector(bmStart[i]!, bmNormDir, bmPerpvec, (360.0 / NUM_BEAM_SEGS) * i);
    VectorAdd(bmStart[i]!, bmOrg, bmStart[i]!);
    VectorAdd(bmStart[i]!, bmDirection, bmEnd[i]!);
  }

  const qgl = r.qgl;
  if (!qgl) return;
  qgl.disable(GL_TEXTURE_2D);
  qgl.enable(GL_BLEND);
  qgl.depthMask(false);

  const c = r.d_8to24table[e.skinnum & 0xff]!;
  let cr = c & 0xff;
  let cg = (c >>> 8) & 0xff;
  let cb = (c >>> 16) & 0xff;
  cr = fr(cr * fr(1 / 255.0));
  cg = fr(cg * fr(1 / 255.0));
  cb = fr(cb * fr(1 / 255.0));

  qgl.color4f(cr, cg, cb, fr(e.alpha));

  qgl.begin(GL_TRIANGLE_STRIP);
  for (let i = 0; i < NUM_BEAM_SEGS; i++) {
    qgl.vertex3fv(bmStart[i]!);
    qgl.vertex3fv(bmEnd[i]!);
    qgl.vertex3fv(bmStart[(i + 1) % NUM_BEAM_SEGS]!);
    qgl.vertex3fv(bmEnd[(i + 1) % NUM_BEAM_SEGS]!);
  }
  qgl.end();

  qgl.enable(GL_TEXTURE_2D);
  qgl.disable(GL_BLEND);
  qgl.depthMask(true);
}

// ===================================================================
// GetRefAPI

export interface GLRefreshOptions extends QGLOptions {
  /** Called with (name, png) by the "screenshot" command instead of triggering a download. */
  onScreenshot?: (name: string, blob: Blob) => void;
}

/** Refresh (refexport_t) plus debugging hooks of the WebGL renderer. */
export interface GLRefresh extends Refresh {
  /** The renderer state (C globals); for tests and tools. */
  readonly state: GLState;
  /** WebGL draw calls issued since the last beginFrame. */
  drawCalls(): number;
  /** Encode the current drawing buffer as PNG (call right after endFrame). */
  screenshot(): Promise<Blob | null>;
}

class WebGLRefresh implements GLRefresh {
  readonly state: GLState;
  private readonly canvas: HTMLCanvasElement;
  private readonly opts: GLRefreshOptions;
  private readonly files = new Map<string, Uint8Array | null>();
  private readonly inflight = new Map<string, Promise<void>>();

  constructor(canvas: HTMLCanvasElement, imports: RefImport, opts: GLRefreshOptions) {
    this.canvas = canvas;
    this.opts = opts;
    const r = new GLState(imports);
    this.state = r;
    r.files = {
      get: (name: string) => {
        const v = this.files.get(name);
        return v === undefined ? (this.files.has(name) ? null : undefined) : v;
      },
    };
    r.requestFile = (name: string) => void this.fetch(name);
  }

  private fetch(name: string): Promise<void> {
    if (this.files.has(name)) return Promise.resolve();
    let p = this.inflight.get(name);
    if (!p) {
      p = this.state.ri
        .loadFile(name)
        .catch(() => null)
        .then((d) => {
          this.files.set(name, d);
          this.inflight.delete(name);
        });
      this.inflight.set(name, p);
    }
    return p;
  }

  private async prefetch(names: (string | null)[]): Promise<void> {
    const r = this.state;
    const todo: string[] = [];
    for (const n of names) {
      if (!n || this.files.has(n)) continue;
      let loaded = false;
      for (let i = 0; i < r.numgltextures; i++) {
        if (r.gltextures[i]!.name === n) {
          loaded = true;
          break;
        }
      }
      if (!loaded) todo.push(n);
    }
    await Promise.all(todo.map((n) => this.fetch(n)));
  }

  private async prefetchModel(name: string): Promise<void> {
    if (!name || name[0] === '*') return;
    const r = this.state;
    let known = false;
    for (let i = 0; i < r.mod_numknown; i++) if (r.mod_known[i]!.name === name) known = true;
    if (!known) await this.fetch(name);
    const buf = this.files.get(name);
    if (buf) await this.prefetch(modelImageDependencies(buf).map(imageFileFor));
    else if (known) {
      const mod = r.mod_known.find((m) => m.name === name);
      if (mod?.alias) await this.prefetch(mod.alias.md2.skins.map(imageFileFor));
      else if (mod?.sprite) await this.prefetch(mod.sprite.frames.map((f) => imageFileFor(f.name)));
    }
  }

  async init(): Promise<boolean> {
    const r = this.state;
    try {
      r.qgl = new QGL(this.canvas, this.opts);
    } catch (e) {
      r.ri.conPrintf(PRINT_ALL, `ref_gl::R_Init() - could not create a WebGL2 context: ${String(e)}\n`);
      return false;
    }
    await this.prefetch(['pics/colormap.pcx', 'pics/conchars.pcx']);
    return R_Init(r);
  }

  shutdown(): void {
    R_Shutdown(this.state);
    this.state.qgl = null;
  }

  async beginRegistration(map: string): Promise<void> {
    // negative lookups are retried for every map
    for (const [k, v] of this.files) if (v === null) this.files.delete(k);
    const fullname = `maps/${map}.bsp`.slice(0, 63);
    await this.prefetchModel(fullname);
    R_BeginRegistration(this.state, map);
  }

  async registerModel(name: string): Promise<ModelHandle | null> {
    await this.prefetchModel(name);
    return R_RegisterModel(this.state, name) as unknown as ModelHandle | null;
  }

  async registerSkin(name: string): Promise<ImageHandle | null> {
    await this.prefetch([imageFileFor(name)]);
    return R_RegisterSkin(this.state, name) as unknown as ImageHandle | null;
  }

  async registerPic(name: string): Promise<ImageHandle | null> {
    await this.prefetch([picFileName(name)]);
    return Draw_FindPic(this.state, name) as unknown as ImageHandle | null;
  }

  async setSky(name: string, rotate: number, axis: Float32Array): Promise<void> {
    await this.prefetch(skyImageNames(name));
    R_SetSky(this.state, name, rotate, axis);
  }

  endRegistration(): void {
    R_EndRegistration(this.state);
    // the files have been turned into models / images; keep only the negative lookups
    for (const [k, v] of this.files) if (v !== null) this.files.delete(k);
  }

  renderFrame(fd: RefDef): void {
    R_RenderFrame(this.state, fd);
  }

  drawGetPicSize(name: string): [number, number] {
    return Draw_GetPicSize(this.state, name);
  }
  drawPic(x: number, y: number, name: string): void {
    Draw_Pic(this.state, x, y, name);
  }
  drawStretchPic(x: number, y: number, w: number, h: number, name: string): void {
    Draw_StretchPic(this.state, x, y, w, h, name);
  }
  drawChar(x: number, y: number, c: number): void {
    Draw_Char(this.state, x, y, c);
  }
  drawTileClear(x: number, y: number, w: number, h: number, name: string): void {
    Draw_TileClear(this.state, x, y, w, h, name);
  }
  drawFill(x: number, y: number, w: number, h: number, c: number): void {
    Draw_Fill(this.state, x, y, w, h, c);
  }
  drawFadeScreen(): void {
    Draw_FadeScreen(this.state);
  }
  drawStretchRaw(
    x: number,
    y: number,
    w: number,
    h: number,
    cols: number,
    rows: number,
    data: Uint8Array,
  ): void {
    Draw_StretchRaw(this.state, x, y, w, h, cols, rows, data);
  }
  cinematicSetPalette(palette: Uint8Array | null): void {
    R_SetPalette(this.state, palette);
  }
  beginFrame(cameraSeparation: number): void {
    R_BeginFrame(this.state, cameraSeparation);
  }
  endFrame(): void {
    // GLimp_EndFrame: the browser presents the drawing buffer when the task ends
    this.state.qgl?.flush();
  }
  appActivate(_activate: boolean): void {}

  drawCalls(): number {
    return this.state.qgl?.drawCalls ?? 0;
  }

  screenshot(): Promise<Blob | null> {
    this.state.qgl?.flush();
    return new Promise((resolve) => {
      if (typeof this.canvas.toBlob !== 'function') resolve(null);
      else this.canvas.toBlob((b) => resolve(b), 'image/png');
    });
  }
}

/**
 * C: gl_rmain.c:1624 GetRefAPI. Creates the WebGL2 refresh on `canvas`; call init() before use.
 */
export function createGLRefresh(
  canvas: HTMLCanvasElement,
  imports: RefImport,
  opts: GLRefreshOptions = {},
): GLRefresh {
  const ref = new WebGLRefresh(canvas, imports, opts);
  ref.state.screenshotHook = opts.onScreenshot ?? null;
  return ref;
}

export type { Image };
