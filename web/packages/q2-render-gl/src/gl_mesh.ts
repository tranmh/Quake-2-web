// Port of ref_gl/gl_mesh.c: triangle (alias) model drawing.
// GL_LerpVerts and the per-vertex shadedots lighting run on the CPU exactly as in C; the glcmd strips and
// fans go through the immediate-mode emulation (gl_vertex_arrays 0 path, the C default).
import {
  AngleVectors,
  BYTE_DIRS,
  DotProduct,
  PRINT_ALL,
  RDF_IRGOGGLES,
  RF_DEPTHHACK,
  RF_FULLBRIGHT,
  RF_GLOW,
  RF_IR_VISIBLE,
  RF_MINLIGHT,
  RF_SHELL_BLUE,
  RF_SHELL_DOUBLE,
  RF_SHELL_GREEN,
  RF_SHELL_HALF_DAM,
  RF_SHELL_RED,
  RF_TRANSLUCENT,
  RF_WEAPONMODEL,
  VectorAdd,
  VectorCopy,
  VectorNormalize,
  VectorSubtract,
  fr,
} from 'q2-shared';
import { POWERSUIT_SCALE, type RefEntity } from 'q2-ref';
import { GLState, PITCH, YAW, type Image } from './gl_local';
import { MAX_MD2SKINS, type AliasData } from './gl_model_h';
import { GL_Bind, GL_TexEnv } from './gl_image';
import { R_LightPoint } from './gl_light';
import { MYgluPerspective, R_RotateForEntity } from './gl_rmain';
import { R_AVERTEXNORMAL_DOTS } from './generated/tables';
import {
  GL_BACK,
  GL_BLEND,
  GL_FRONT,
  GL_MODELVIEW,
  GL_MODULATE,
  GL_PROJECTION,
  GL_REPLACE,
  GL_TEXTURE_2D,
  GL_TRIANGLE_FAN,
  GL_TRIANGLE_STRIP,
} from './qgl';

export const NUMVERTEXNORMALS = 162;
/** C: gl_mesh.c:34 r_avertexnormals (anorms.h, identical to q_shared bytedirs) */
export const r_avertexnormals = BYTE_DIRS;
export const SHADEDOT_QUANT = 16;

const SHELL_FLAGS = RF_SHELL_RED | RF_SHELL_GREEN | RF_SHELL_BLUE | RF_SHELL_DOUBLE | RF_SHELL_HALF_DAM;

/**
 * C: gl_mesh.c:54 GL_LerpVerts. v / ov are frame vertex arrays (4 bytes per vertex: x y z normalindex),
 * `verts` supplies the light normal index (the current frame), lerp receives 4 floats per vertex.
 */
export function GL_LerpVerts(
  nverts: number,
  v: Uint8Array,
  ov: Uint8Array,
  verts: Uint8Array,
  lerp: Float32Array,
  move: ArrayLike<number>,
  frontv: ArrayLike<number>,
  backv: ArrayLike<number>,
  shell: boolean,
): void {
  const m0 = move[0]!,
    m1 = move[1]!,
    m2 = move[2]!;
  const f0 = frontv[0]!,
    f1 = frontv[1]!,
    f2 = frontv[2]!;
  const b0 = backv[0]!,
    b1 = backv[1]!,
    b2 = backv[2]!;
  if (shell) {
    for (let i = 0; i < nverts; i++) {
      const o = i * 4;
      const n = verts[o + 3]! * 3;
      lerp[o] = fr(
        fr(fr(m0 + fr(ov[o]! * b0)) + fr(v[o]! * f0)) + fr(r_avertexnormals[n]! * POWERSUIT_SCALE),
      );
      lerp[o + 1] = fr(
        fr(fr(m1 + fr(ov[o + 1]! * b1)) + fr(v[o + 1]! * f1)) +
          fr(r_avertexnormals[n + 1]! * POWERSUIT_SCALE),
      );
      lerp[o + 2] = fr(
        fr(fr(m2 + fr(ov[o + 2]! * b2)) + fr(v[o + 2]! * f2)) +
          fr(r_avertexnormals[n + 2]! * POWERSUIT_SCALE),
      );
    }
  } else {
    for (let i = 0; i < nverts; i++) {
      const o = i * 4;
      lerp[o] = fr(fr(m0 + fr(ov[o]! * b0)) + fr(v[o]! * f0));
      lerp[o + 1] = fr(fr(m1 + fr(ov[o + 1]! * b1)) + fr(v[o + 1]! * f1));
      lerp[o + 2] = fr(fr(m2 + fr(ov[o + 2]! * b2)) + fr(v[o + 2]! * f2));
    }
  }
}

/**
 * C: gl_mesh.c:90 GL_DrawAliasFrameLerp, the setup math: move / frontv / backv for GL_LerpVerts.
 */
export function aliasLerpVectors(
  e: RefEntity,
  frameTranslate: ArrayLike<number>,
  frameScale: ArrayLike<number>,
  oldTranslate: ArrayLike<number>,
  oldScale: ArrayLike<number>,
  backlerp: number,
  move: Float32Array,
  frontv: Float32Array,
  backv: Float32Array,
): void {
  backlerp = fr(backlerp);
  const frontlerp = fr(1.0 - backlerp);
  const delta = alDelta;
  // move should be the delta back to the previous frame * backlerp
  VectorSubtract(e.oldorigin, e.origin, delta);
  AngleVectors(e.angles, alVec0, alVec1, alVec2);
  move[0] = DotProduct(delta, alVec0); // forward
  move[1] = -DotProduct(delta, alVec1); // left
  move[2] = DotProduct(delta, alVec2); // up
  VectorAdd(move, oldTranslate, move);
  for (let i = 0; i < 3; i++) move[i] = fr(fr(backlerp * move[i]!) + fr(frontlerp * frameTranslate[i]!));
  for (let i = 0; i < 3; i++) {
    frontv[i] = fr(frontlerp * frameScale[i]!);
    backv[i] = fr(backlerp * oldScale[i]!);
  }
}
const alDelta = new Float32Array(3);
const alVec0 = new Float32Array(3);
const alVec1 = new Float32Array(3);
const alVec2 = new Float32Array(3);
const alMove = new Float32Array(3);
const alFrontv = new Float32Array(3);
const alBackv = new Float32Array(3);

// C: gl_mesh.c:90 GL_DrawAliasFrameLerp -- interpolates between two frames and origins
function GL_DrawAliasFrameLerp(r: GLState, al: AliasData, backlerp: number): void {
  const e = r.currententity;
  const md2 = al.md2;
  const frame = md2.frames[e.frame]!;
  const oldframe = md2.frames[e.oldframe]!;
  const v = frame.verts;
  const qgl = r.qgl;

  const alpha = e.flags & RF_TRANSLUCENT ? fr(e.alpha) : 1.0;
  const shell = !!(e.flags & SHELL_FLAGS);
  if (shell) qgl?.disable(GL_TEXTURE_2D);

  aliasLerpVectors(
    e,
    frame.translate,
    frame.scale,
    oldframe.translate,
    oldframe.scale,
    backlerp,
    alMove,
    alFrontv,
    alBackv,
  );

  const lerp = r.s_lerped;
  GL_LerpVerts(md2.header.num_xyz, v, oldframe.verts, v, lerp, alMove, alFrontv, alBackv, shell);

  if (qgl) {
    const order = al.glcmds;
    const orderF = al.glcmdsF;
    const sl = r.shadelight;
    const dots = R_AVERTEXNORMAL_DOTS;
    const sd = r.shadedots;
    const rgbShell = !!(e.flags & (RF_SHELL_RED | RF_SHELL_GREEN | RF_SHELL_BLUE));
    let p = 0;
    while (1) {
      // get the vertex count and primitive type
      let count = order[p++]!;
      if (!count) break; // done
      if (count < 0) {
        count = -count;
        qgl.begin(GL_TRIANGLE_FAN);
      } else qgl.begin(GL_TRIANGLE_STRIP);

      if (rgbShell) {
        do {
          const index_xyz = order[p + 2]!;
          p += 3;
          qgl.color4f(sl[0]!, sl[1]!, sl[2]!, alpha);
          qgl.vertex3f(lerp[index_xyz * 4]!, lerp[index_xyz * 4 + 1]!, lerp[index_xyz * 4 + 2]!);
        } while (--count);
      } else {
        do {
          // texture coordinates come from the draw list
          qgl.texCoord2f(orderF[p]!, orderF[p + 1]!);
          const index_xyz = order[p + 2]!;
          p += 3;
          // normals and vertexes come from the frame list
          const l = dots[sd + v[index_xyz * 4 + 3]!]!;
          qgl.color4f(fr(l * sl[0]!), fr(l * sl[1]!), fr(l * sl[2]!), alpha);
          qgl.vertex3f(lerp[index_xyz * 4]!, lerp[index_xyz * 4 + 1]!, lerp[index_xyz * 4 + 2]!);
        } while (--count);
      }
      qgl.end();
    }
  }

  if (shell) qgl?.enable(GL_TEXTURE_2D);
}

const shPoint = new Float32Array(3);

// C: gl_mesh.c:306 GL_DrawAliasShadow
function GL_DrawAliasShadow(r: GLState, al: AliasData, _posenum: number): void {
  const qgl = r.qgl;
  if (!qgl) return;
  const e = r.currententity;
  const lheight = fr(e.origin[2]! - r.lightspot[2]!);
  const order = al.glcmds;
  const height = fr(-lheight + 1.0);
  const lerp = r.s_lerped;
  const sv = r.shadevector;
  const point = shPoint;
  let p = 0;
  while (1) {
    // get the vertex count and primitive type
    let count = order[p++]!;
    if (!count) break; // done
    if (count < 0) {
      count = -count;
      qgl.begin(GL_TRIANGLE_FAN);
    } else qgl.begin(GL_TRIANGLE_STRIP);
    do {
      const i = order[p + 2]! * 4;
      point[0] = lerp[i]!;
      point[1] = lerp[i + 1]!;
      point[2] = lerp[i + 2]!;
      point[0] = point[0]! - fr(sv[0]! * fr(point[2]! + lheight));
      point[1] = point[1]! - fr(sv[1]! * fr(point[2]! + lheight));
      point[2] = height;
      qgl.vertex3f(point[0]!, point[1]!, point[2]!);
      p += 3;
    } while (--count);
    qgl.end();
  }
}

const caMins = new Float32Array(3);
const caMaxs = new Float32Array(3);
const caThisMins = new Float32Array(3);
const caThisMaxs = new Float32Array(3);
const caOldMins = new Float32Array(3);
const caOldMaxs = new Float32Array(3);
const caAngles = new Float32Array(3);
const caTmp = new Float32Array(3);
const caBbox: Float32Array[] = [];
for (let i = 0; i < 8; i++) caBbox.push(new Float32Array(3));

// C: gl_mesh.c:373 R_CullAliasModel
export function R_CullAliasModel(r: GLState, bbox: Float32Array[], e: RefEntity): boolean {
  const md2 = r.currentmodel.alias!.md2;
  const num_frames = md2.header.num_frames;
  if (e.frame >= num_frames || e.frame < 0) {
    r.ri.conPrintf(PRINT_ALL, `R_CullAliasModel ${r.currentmodel.name}: no such frame ${e.frame}\n`);
    e.frame = 0;
  }
  if (e.oldframe >= num_frames || e.oldframe < 0) {
    r.ri.conPrintf(PRINT_ALL, `R_CullAliasModel ${r.currentmodel.name}: no such oldframe ${e.oldframe}\n`);
    e.oldframe = 0;
  }
  const pframe = md2.frames[e.frame]!;
  const poldframe = md2.frames[e.oldframe]!;
  const mins = caMins;
  const maxs = caMaxs;

  // compute axially aligned mins and maxs
  if (pframe === poldframe) {
    for (let i = 0; i < 3; i++) {
      mins[i] = pframe.translate[i]!;
      maxs[i] = fr(mins[i]! + fr(pframe.scale[i]! * 255));
    }
  } else {
    for (let i = 0; i < 3; i++) {
      caThisMins[i] = pframe.translate[i]!;
      caThisMaxs[i] = fr(caThisMins[i]! + fr(pframe.scale[i]! * 255));
      caOldMins[i] = poldframe.translate[i]!;
      caOldMaxs[i] = fr(caOldMins[i]! + fr(poldframe.scale[i]! * 255));
      mins[i] = caThisMins[i]! < caOldMins[i]! ? caThisMins[i]! : caOldMins[i]!;
      maxs[i] = caThisMaxs[i]! > caOldMaxs[i]! ? caThisMaxs[i]! : caOldMaxs[i]!;
    }
  }

  // compute a full bounding box
  for (let i = 0; i < 8; i++) {
    const b = bbox[i]!;
    b[0] = i & 1 ? mins[0]! : maxs[0]!;
    b[1] = i & 2 ? mins[1]! : maxs[1]!;
    b[2] = i & 4 ? mins[2]! : maxs[2]!;
  }

  // rotate the bounding box
  VectorCopy(e.angles, caAngles);
  caAngles[YAW] = -caAngles[YAW]!;
  AngleVectors(caAngles, alVec0, alVec1, alVec2);
  for (let i = 0; i < 8; i++) {
    const b = bbox[i]!;
    VectorCopy(b, caTmp);
    b[0] = DotProduct(alVec0, caTmp);
    b[1] = -DotProduct(alVec1, caTmp);
    b[2] = DotProduct(alVec2, caTmp);
    VectorAdd(e.origin, b, b);
  }

  let aggregatemask = ~0;
  for (let p = 0; p < 8; p++) {
    let mask = 0;
    for (let f = 0; f < 4; f++) {
      const dp = DotProduct(r.frustum[f]!.normal, bbox[p]!);
      if (fr(dp - r.frustum[f]!.dist) < 0) mask |= 1 << f;
    }
    aggregatemask &= mask;
  }
  return aggregatemask !== 0;
}

function setLightLevel(r: GLState): void {
  const sl = r.shadelight;
  // pick the greatest component, which should be the same as the mono value returned by software
  if (sl[0]! > sl[1]!) {
    if (sl[0]! > sl[2]!) r.cv.r_lightlevel.value = fr(150 * sl[0]!);
    else r.cv.r_lightlevel.value = fr(150 * sl[2]!);
  } else {
    if (sl[1]! > sl[2]!) r.cv.r_lightlevel.value = fr(150 * sl[1]!);
    else r.cv.r_lightlevel.value = fr(150 * sl[2]!);
  }
}

// C: gl_mesh.c:519 R_DrawAliasModel
export function R_DrawAliasModel(r: GLState, e: RefEntity): void {
  if (!(e.flags & RF_WEAPONMODEL)) {
    if (R_CullAliasModel(r, caBbox, e)) return;
  }
  if (e.flags & RF_WEAPONMODEL) {
    if (r.cv.r_lefthand.value === 2) return;
  }

  const al = r.currentmodel.alias!;
  const ce = r.currententity;
  const sl = r.shadelight;
  const qgl = r.qgl;

  // get lighting information
  if (ce.flags & (RF_SHELL_HALF_DAM | RF_SHELL_GREEN | RF_SHELL_RED | RF_SHELL_BLUE | RF_SHELL_DOUBLE)) {
    // PMM -special case for godmode
    if (ce.flags & RF_SHELL_RED && ce.flags & RF_SHELL_BLUE && ce.flags & RF_SHELL_GREEN) {
      for (let i = 0; i < 3; i++) sl[i] = 1.0;
    } else if (ce.flags & (RF_SHELL_RED | RF_SHELL_BLUE | RF_SHELL_DOUBLE)) {
      sl[0] = sl[1] = sl[2] = 0;
      if (ce.flags & RF_SHELL_RED) {
        sl[0] = 1.0;
        if (ce.flags & (RF_SHELL_BLUE | RF_SHELL_DOUBLE)) sl[2] = 1.0;
      } else if (ce.flags & RF_SHELL_BLUE) {
        if (ce.flags & RF_SHELL_DOUBLE) {
          sl[1] = 1.0;
          sl[2] = 1.0;
        } else sl[2] = 1.0;
      } else if (ce.flags & RF_SHELL_DOUBLE) {
        sl[0] = 0.9;
        sl[1] = 0.7;
      }
    } else if (ce.flags & (RF_SHELL_HALF_DAM | RF_SHELL_GREEN)) {
      sl[0] = sl[1] = sl[2] = 0;
      // PMM - new colors
      if (ce.flags & RF_SHELL_HALF_DAM) {
        sl[0] = 0.56;
        sl[1] = 0.59;
        sl[2] = 0.45;
      }
      if (ce.flags & RF_SHELL_GREEN) sl[1] = 1.0;
    }
  } else if (ce.flags & RF_FULLBRIGHT) {
    for (let i = 0; i < 3; i++) sl[i] = 1.0;
  } else {
    R_LightPoint(r, ce.origin, sl);

    // player lighting hack for communication back to server
    if (ce.flags & RF_WEAPONMODEL) setLightLevel(r);

    if (r.cv.gl_monolightmap.string[0] !== '0') {
      let s = sl[0]!;
      if (s < sl[1]!) s = sl[1]!;
      if (s < sl[2]!) s = sl[2]!;
      sl[0] = s;
      sl[1] = s;
      sl[2] = s;
    }
  }

  if (ce.flags & RF_MINLIGHT) {
    let i;
    for (i = 0; i < 3; i++) if (sl[i]! > 0.1) break;
    if (i === 3) {
      sl[0] = 0.1;
      sl[1] = 0.1;
      sl[2] = 0.1;
    }
  }

  if (ce.flags & RF_GLOW) {
    // bonus items will pulse with time
    const scale = fr(0.1 * Math.sin(fr(fr(r.r_newrefdef.time) * 7)));
    for (let i = 0; i < 3; i++) {
      const min = fr(sl[i]! * 0.8);
      sl[i] = sl[i]! + scale;
      if (sl[i]! < min) sl[i] = min;
    }
  }

  // PGM	ir goggles color override
  if (r.r_newrefdef.rdflags & RDF_IRGOGGLES && ce.flags & RF_IR_VISIBLE) {
    sl[0] = 1.0;
    sl[1] = 0.0;
    sl[2] = 0.0;
  }

  r.shadedots = 256 * (Math.trunc(ce.angles[1]! * (SHADEDOT_QUANT / 360.0)) & (SHADEDOT_QUANT - 1));

  const an = fr(fr(ce.angles[1]! / 180) * Math.PI);
  r.shadevector[0] = Math.cos(-an);
  r.shadevector[1] = Math.sin(-an);
  r.shadevector[2] = 1;
  VectorNormalize(r.shadevector);

  // locate the proper data
  r.c_alias_polys += al.md2.header.num_tris;

  // draw all the triangles
  if (ce.flags & RF_DEPTHHACK) {
    // hack the depth range to prevent view model from poking into walls
    qgl?.depthRange(r.gldepthmin, r.gldepthmin + 0.3 * (r.gldepthmax - r.gldepthmin));
  }

  const lefthanded = !!(ce.flags & RF_WEAPONMODEL) && fr(r.cv.r_lefthand.value) === 1.0;
  if (lefthanded && qgl) {
    qgl.matrixMode(GL_PROJECTION);
    qgl.pushMatrix();
    qgl.loadIdentity();
    qgl.scalef(-1, 1, 1);
    MYgluPerspective(r, r.r_newrefdef.fov_y, fr(r.r_newrefdef.width / r.r_newrefdef.height), 4, 4096);
    qgl.matrixMode(GL_MODELVIEW);
    qgl.cullFace(GL_BACK);
  }

  qgl?.pushMatrix();
  e.angles[PITCH] = -e.angles[PITCH]!; // sigh.
  R_RotateForEntity(r, e);
  e.angles[PITCH] = -e.angles[PITCH]!; // sigh.

  // select skin
  let skin: Image | null | undefined;
  if (ce.skin)
    skin = ce.skin as unknown as Image; // custom player skin
  else {
    if (ce.skinnum >= MAX_MD2SKINS) skin = r.currentmodel.skins[0];
    else {
      skin = r.currentmodel.skins[ce.skinnum];
      if (!skin) skin = r.currentmodel.skins[0];
    }
  }
  if (!skin) skin = r.r_notexture; // fallback...
  GL_Bind(r, skin.texnum);

  // draw it
  GL_TexEnv(r, GL_MODULATE);
  if (ce.flags & RF_TRANSLUCENT) qgl?.enable(GL_BLEND);

  const num_frames = al.md2.header.num_frames;
  if (ce.frame >= num_frames || ce.frame < 0) {
    r.ri.conPrintf(PRINT_ALL, `R_DrawAliasModel ${r.currentmodel.name}: no such frame ${ce.frame}\n`);
    ce.frame = 0;
    ce.oldframe = 0;
  }
  if (ce.oldframe >= num_frames || ce.oldframe < 0) {
    r.ri.conPrintf(PRINT_ALL, `R_DrawAliasModel ${r.currentmodel.name}: no such oldframe ${ce.oldframe}\n`);
    ce.frame = 0;
    ce.oldframe = 0;
  }

  if (!r.cv.r_lerpmodels.value) ce.backlerp = 0;
  GL_DrawAliasFrameLerp(r, al, ce.backlerp);

  GL_TexEnv(r, GL_REPLACE);

  qgl?.popMatrix();

  if (lefthanded && qgl) {
    qgl.matrixMode(GL_PROJECTION);
    qgl.popMatrix();
    qgl.matrixMode(GL_MODELVIEW);
    qgl.cullFace(GL_FRONT);
  }

  if (ce.flags & RF_TRANSLUCENT) qgl?.disable(GL_BLEND);

  if (ce.flags & RF_DEPTHHACK) qgl?.depthRange(r.gldepthmin, r.gldepthmax);

  if (r.cv.gl_shadows.value && !(ce.flags & (RF_TRANSLUCENT | RF_WEAPONMODEL))) {
    qgl?.pushMatrix();
    R_RotateForEntity(r, e);
    qgl?.disable(GL_TEXTURE_2D);
    qgl?.enable(GL_BLEND);
    qgl?.color4f(0, 0, 0, 0.5);
    GL_DrawAliasShadow(r, al, ce.frame);
    qgl?.enable(GL_TEXTURE_2D);
    qgl?.disable(GL_BLEND);
    qgl?.popMatrix();
  }
  qgl?.color4f(1, 1, 1, 1);
}
