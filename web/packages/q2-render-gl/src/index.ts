// q2-render-gl: WebGL2 port of ref_gl (see README in docs/PARITY.md for deviations).
export { createGLRefresh, type GLRefresh, type GLRefreshOptions } from './gl_rmain';
export {
  GLState,
  Image,
  BLOCK_WIDTH,
  BLOCK_HEIGHT,
  it_pic,
  it_skin,
  it_sky,
  it_sprite,
  it_wall,
} from './gl_local';
export * from './gl_model_h';
// CPU-side pure functions (unit-testable without WebGL)
export {
  GL_ResampleTexture,
  GL_LightScaleTexture,
  GL_MipMap,
  GL_Upload32Levels,
  GL_Expand8,
  R_FloodFillSkin,
  Scrap_AllocBlock,
  buildGammaTable,
  buildIntensityTable,
  LoadPCX,
} from './gl_image';
export {
  R_BuildLightMap,
  R_AddDynamicLights,
  R_SetCacheState,
  R_LightPoint,
  RecursiveLightPoint,
  type LightMapContext,
} from './gl_light';
export { GL_LerpVerts, aliasLerpVectors, R_CullAliasModel } from './gl_mesh';
export {
  ClipSkyPolygon,
  MakeSkyVecST,
  SubdividePolygon,
  warpST,
  TURBSCALE,
  skyImageNames,
  type SkyBounds,
} from './gl_warp';
export {
  CalcSurfaceExtents,
  Mod_ForName,
  Mod_PointInLeaf,
  Mod_ClusterPVS,
  modelImageDependencies,
  R_BeginRegistration,
  R_RegisterModel,
  R_EndRegistration,
} from './gl_model';
export { LM_AllocBlock, buildSurfacePolygon, flowingScroll, R_MarkLeaves } from './gl_rsurf';
export { stretchRawResample } from './gl_draw';
export { R_Init, R_Register, R_SetupFrame, R_SetFrustum } from './gl_rmain';
export { R_AVERTEXNORMAL_DOTS, R_TURBSIN_RAW } from './generated/tables';
export { QGL } from './qgl';
