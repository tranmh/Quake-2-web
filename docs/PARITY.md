# Parity inventory

Every file in the `Quake-2/` oracle with its disposition. **P** ported, **R** replaced by a modern equivalent,
**D** dropped (reason given), **N/A** non-code. Status: pending / in-progress / ported / verified (oracle gate passing).
`tools/parity-check.sh` fails if a tracked oracle file is missing from this table.

| File | Lines | Disp. | Target / reason | Oracle gate | Status |
|---|---|---|---|---|---|
| `3.15_Changes.txt` | 107 | N/A | build/docs | - | n/a |
| `3.16_Changes.txt` | 127 | N/A | build/docs | - | n/a |
| `3.17_Changes.txt` | 158 | N/A | build/docs | - | n/a |
| `3.18_changes.txt` | 53 | N/A | build/docs | - | n/a |
| `baseq2/config.cfg` | 150 | N/A | build/docs | - | n/a |
| `baseq2/save/save0/game.ssv` | 1 | N/A | build/docs | - | n/a |
| `baseq2/save/save0/server.ssv` | 1 | N/A | build/docs | - | n/a |
| `changes.txt` | 166 | N/A | build/docs | - | n/a |
| `client/adivtab.h` | 1058 | D | x86 asm / software-renderer helpers | - | n/a |
| `client/anorms.h` | 181 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/asm_i386.h` | 81 | D | x86 asm / software-renderer helpers | - | n/a |
| `client/block16.h` | 123 | D | x86 asm / software-renderer helpers | - | n/a |
| `client/block8.h` | 124 | D | x86 asm / software-renderer helpers | - | n/a |
| `client/cdaudio.h` | 26 | R | optional user OGG tracks keyed by CS_CDTRACK | - | ported |
| `client/cl_cin.c` | 650 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/cl_ents.c` | 1500 | P | TS q2-client / q2-snd-worklet + Go fakeclient (headless: frame / delta entity parsing) | oracle_client | ported |
| `client/cl_fx.c` | 2298 | P | TS q2-client / q2-snd-worklet | oracle_client | verified |
| `client/cl_input.c` | 542 | P | TS q2-client / q2-snd-worklet + Go fakeclient (headless: CL_SendCmd) | oracle_client | ported |
| `client/cl_inv.c` | 142 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/cl_main.c` | 1844 | P | TS q2-client / q2-snd-worklet + Go internal/demo (CL_Record_f, CL_WriteDemoMessage, CL_Stop_f) + Go fakeclient (headless: connection handshake) | oracle_client | ported |
| `client/cl_newfx.c` | 1323 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/cl_parse.c` | 806 | P | TS q2-client / q2-snd-worklet + Go fakeclient (headless) | oracle_client | ported |
| `client/cl_pred.c` | 278 | P | TS q2-client / q2-snd-worklet | oracle_client | verified |
| `client/cl_scrn.c` | 1401 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/cl_tent.c` | 1745 | P | TS q2-client / q2-snd-worklet + Go fakeclient (headless: CL_ParseTEnt reads) | oracle_client | ported |
| `client/cl_view.c` | 584 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/client.h` | 584 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/console.c` | 682 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/console.h` | 62 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/input.h` | 34 | R | browser input / canvas | - | ported |
| `client/keys.c` | 943 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/keys.h` | 146 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/menu.c` | 4016 | R | React shell (Q2 aesthetic) + in-canvas where server-driven | manual/Playwright | ported |
| `client/qmenu.c` | 674 | R | React shell (Q2 aesthetic) + in-canvas where server-driven | manual/Playwright | ported |
| `client/qmenu.h` | 140 | R | React shell (Q2 aesthetic) + in-canvas where server-driven | manual/Playwright | ported |
| `client/ref.h` | 224 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/screen.h` | 62 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/snd_dma.c` | 1214 | P | TS q2-client / q2-snd-worklet | oracle_client | verified |
| `client/snd_loc.h` | 164 | P | TS q2-client / q2-snd-worklet | oracle_client | verified |
| `client/snd_mem.c` | 359 | P | TS q2-client / q2-snd-worklet | oracle_client | verified |
| `client/snd_mix.c` | 497 | P | TS q2-client / q2-snd-worklet | oracle_client | verified |
| `client/sound.h` | 45 | P | TS q2-client / q2-snd-worklet | oracle_client | ported |
| `client/vid.h` | 42 | R | browser input / canvas | - | ported |
| `client/x86.c` | 95 | D | x86 asm / software-renderer helpers | - | n/a |
| `ctf/2do.txt` | 16 | N/A | build/doc file | - | n/a |
| `ctf/Makefile.Linux.i386` | 159 | N/A | build/doc file | - | n/a |
| `ctf/ctf.001` | 1009 | N/A | build/doc file | - | n/a |
| `ctf/ctf.def` | 2 | N/A | build/doc file | - | n/a |
| `ctf/ctf.dsp` | 1007 | N/A | build/doc file | - | n/a |
| `ctf/ctf.plg` | 17 | N/A | build/doc file | - | n/a |
| `ctf/docs/admin.gif` | 78 | N/A | build/doc file | - | n/a |
| `ctf/docs/adminset.gif` | 80 | N/A | build/doc file | - | n/a |
| `ctf/docs/automac.gif` | 93 | N/A | build/doc file | - | n/a |
| `ctf/docs/ghost.jpg` | 39 | N/A | build/doc file | - | n/a |
| `ctf/docs/grapple.jpg` | 56 | N/A | build/doc file | - | n/a |
| `ctf/docs/layout.jpg` | 189 | N/A | build/doc file | - | n/a |
| `ctf/docs/mainctf_back.jpg` | 23 | N/A | build/doc file | - | n/a |
| `ctf/docs/menu.gif` | 75 | N/A | build/doc file | - | n/a |
| `ctf/docs/q2ctf.html` | 1243 | N/A | build/doc file | - | n/a |
| `ctf/docs/say_team.gif` | 21 | N/A | build/doc file | - | n/a |
| `ctf/docs/stats.jpg` | 25 | N/A | build/doc file | - | n/a |
| `ctf/docs/tech1.gif` | 2 | N/A | build/doc file | - | n/a |
| `ctf/docs/tech2.gif` | 2 | N/A | build/doc file | - | n/a |
| `ctf/docs/tech3.gif` | 1 | N/A | build/doc file | - | n/a |
| `ctf/docs/tech4.gif` | 4 | N/A | build/doc file | - | n/a |
| `ctf/g_ai.c` | 1117 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_chase.c` | 157 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_cmds.c` | 1066 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_combat.c` | 596 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_ctf.c` | 4016 | P | Go internal/game g_ctf.go / p_menu.go (docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_ctf.h` | 185 | P | Go internal/game g_ctf.go / p_menu.go (docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_func.c` | 2047 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_items.c` | 2446 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_local.h` | 1145 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_main.c` | 427 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_misc.c` | 1909 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_monster.c` | 740 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_phys.c` | 959 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_save.c` | 743 | P | Go internal/game (InitGame hunks inline; the Go save format is shared by both modes, docs/CTF.md) | oracle_game_ctf ctf_* (InitGame) | ported |
| `ctf/g_spawn.c` | 998 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_svcmds.c` | 48 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_target.c` | 809 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_trigger.c` | 598 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_utils.c` | 570 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/g_weapon.c` | 919 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/game.h` | 242 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/layout.txt` | 12 | N/A | build/doc file | - | n/a |
| `ctf/m_move.c` | 556 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/m_player.h` | 225 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/p_client.c` | 1726 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/p_hud.c` | 544 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/p_menu.c` | 256 | P | Go internal/game g_ctf.go / p_menu.go (docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/p_menu.h` | 49 | P | Go internal/game g_ctf.go / p_menu.go (docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/p_trail.c` | 146 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/p_view.c` | 1135 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/p_weapon.c` | 1469 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/q_shared.c` | 1419 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `ctf/q_shared.h` | 1200 | P | Go internal/game (CTF mode, //ZOID hunks inline, docs/CTF.md) | oracle_game_ctf ctf_* | verified |
| `game/g_ai.c` | 1117 | P | Go internal/game | oracle_game | verified |
| `game/g_chase.c` | 175 | P | Go internal/game | oracle_game | verified |
| `game/g_cmds.c` | 992 | P | Go internal/game | oracle_game | verified |
| `game/g_combat.c` | 576 | P | Go internal/game | oracle_game | verified |
| `game/g_func.c` | 2048 | P | Go internal/game | oracle_game | verified |
| `game/g_items.c` | 2216 | P | Go internal/game | oracle_game | verified |
| `game/g_local.h` | 1113 | P | Go internal/game | oracle_game | verified |
| `game/g_main.c` | 411 | P | Go internal/game | oracle_game | verified |
| `game/g_misc.c` | 1876 | P | Go internal/game | oracle_game | verified |
| `game/g_monster.c` | 740 | P | Go internal/game | oracle_game | verified |
| `game/g_phys.c` | 961 | P | Go internal/game | oracle_game | verified |
| `game/g_save.c` | 769 | R | named-function registry + versioned saves (ADR-0004) | save idempotence | verified |
| `game/g_spawn.c` | 984 | P | Go internal/game | oracle_game | verified |
| `game/g_svcmds.c` | 300 | P | Go internal/game | oracle_game | verified |
| `game/g_target.c` | 809 | P | Go internal/game | oracle_game | verified |
| `game/g_trigger.c` | 598 | P | Go internal/game | oracle_game | verified |
| `game/g_turret.c` | 432 | P | Go internal/game | oracle_game | verified |
| `game/g_utils.c` | 568 | P | Go internal/game | oracle_game | verified |
| `game/g_weapon.c` | 916 | P | Go internal/game | oracle_game | verified |
| `game/game.001` | 1619 | N/A | build file | - | n/a |
| `game/game.def` | 2 | N/A | build file | - | n/a |
| `game/game.dsp` | 1618 | N/A | build file | - | n/a |
| `game/game.h` | 254 | P | Go internal/game | oracle_game | verified |
| `game/game.plg` | 75 | N/A | build file | - | n/a |
| `game/m_actor.c` | 609 | P | Go internal/game | oracle_game | verified |
| `game/m_actor.h` | 506 | P | Go internal/game | oracle_game | verified |
| `game/m_berserk.c` | 457 | P | Go internal/game | oracle_game | verified |
| `game/m_berserk.h` | 269 | P | Go internal/game | oracle_game | verified |
| `game/m_boss2.c` | 679 | P | Go internal/game | oracle_game | verified |
| `game/m_boss2.h` | 206 | P | Go internal/game | oracle_game | verified |
| `game/m_boss3.c` | 76 | P | Go internal/game | oracle_game | verified |
| `game/m_boss31.c` | 749 | P | Go internal/game | oracle_game | verified |
| `game/m_boss31.h` | 213 | P | Go internal/game | oracle_game | verified |
| `game/m_boss32.c` | 913 | P | Go internal/game | oracle_game | verified |
| `game/m_boss32.h` | 516 | P | Go internal/game | oracle_game | verified |
| `game/m_brain.c` | 676 | P | Go internal/game | oracle_game | verified |
| `game/m_brain.h` | 247 | P | Go internal/game | oracle_game | verified |
| `game/m_chick.c` | 677 | P | Go internal/game | oracle_game | verified |
| `game/m_chick.h` | 313 | P | Go internal/game | oracle_game | verified |
| `game/m_flash.c` | 488 | P | generated tables (genconst) + Go game | genconst | verified |
| `game/m_flipper.c` | 403 | P | Go internal/game | oracle_game | verified |
| `game/m_flipper.h` | 185 | P | Go internal/game | oracle_game | verified |
| `game/m_float.c` | 663 | P | Go internal/game | oracle_game | verified |
| `game/m_float.h` | 273 | P | Go internal/game | oracle_game | verified |
| `game/m_flyer.c` | 626 | P | Go internal/game | oracle_game | verified |
| `game/m_flyer.h` | 182 | P | Go internal/game | oracle_game | verified |
| `game/m_gladiator.c` | 387 | P | Go internal/game | oracle_game | verified |
| `game/m_gladiator.h` | 115 | P | Go internal/game | oracle_game | verified |
| `game/m_gunner.c` | 628 | P | Go internal/game | oracle_game | verified |
| `game/m_gunner.h` | 234 | P | Go internal/game | oracle_game | verified |
| `game/m_hover.c` | 620 | P | Go internal/game | oracle_game | verified |
| `game/m_hover.h` | 230 | P | Go internal/game | oracle_game | verified |
| `game/m_infantry.c` | 607 | P | Go internal/game | oracle_game | verified |
| `game/m_infantry.h` | 232 | P | Go internal/game | oracle_game | verified |
| `game/m_insane.c` | 693 | P | Go internal/game | oracle_game | verified |
| `game/m_insane.h` | 307 | P | Go internal/game | oracle_game | verified |
| `game/m_medic.c` | 769 | P | Go internal/game | oracle_game | verified |
| `game/m_medic.h` | 262 | P | Go internal/game | oracle_game | verified |
| `game/m_move.c` | 556 | P | Go internal/game | oracle_game | verified |
| `game/m_mutant.c` | 663 | P | Go internal/game | oracle_game | verified |
| `game/m_mutant.h` | 174 | P | Go internal/game | oracle_game | verified |
| `game/m_parasite.c` | 552 | P | Go internal/game | oracle_game | verified |
| `game/m_parasite.h` | 143 | P | Go internal/game | oracle_game | verified |
| `game/m_player.h` | 224 | P | Go internal/game | oracle_game | verified |
| `game/m_rider.h` | 66 | P | Go internal/game | oracle_game | verified |
| `game/m_soldier.c` | 1299 | P | Go internal/game | oracle_game | verified |
| `game/m_soldier.h` | 500 | P | Go internal/game | oracle_game | verified |
| `game/m_supertank.c` | 717 | P | Go internal/game | oracle_game | verified |
| `game/m_supertank.h` | 279 | P | Go internal/game | oracle_game | verified |
| `game/m_tank.c` | 856 | P | Go internal/game | oracle_game | verified |
| `game/m_tank.h` | 319 | P | Go internal/game | oracle_game | verified |
| `game/p_client.c` | 1805 | P | Go internal/game | oracle_game | verified |
| `game/p_hud.c` | 571 | P | Go internal/game | oracle_game | verified |
| `game/p_trail.c` | 146 | P | Go internal/game | oracle_game | verified |
| `game/p_view.c` | 1087 | P | Go internal/game | oracle_game | verified |
| `game/p_weapon.c` | 1434 | P | Go internal/game | oracle_game | verified |
| `game/q_shared.c` | 1418 | P | Go internal/qcommon/shared; TS q2-shared | core | verified |
| `game/q_shared.h` | 1200 | P | Go internal/qcommon/shared; TS q2-shared | core | verified |
| `gnu.txt` | 87 | N/A | build/docs | - | n/a |
| `irix/cd_irix.c` | 40 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `irix/glw_imp.c` | 927 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `irix/q_shirix.c` | 200 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `irix/qgl_irix.c` | 3997 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `irix/snd_irix.c` | 222 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `irix/sys_irix.c` | 383 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `irix/vid_menu.c` | 426 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `irix/vid_so.c` | 492 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `joystick.txt` | 226 | N/A | build/docs | - | n/a |
| `linux/Makefile.AXP` | 716 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/Makefile.i386` | 1085 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/block16.h` | 123 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/block8.h` | 124 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/cd_linux.c` | 420 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/d_copy.s` | 147 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/d_ifacea.h` | 79 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/d_polysa.s` | 1250 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/gl_fxmesa.c` | 206 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/glob.c` | 164 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/glob.h` | 1 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/in_linux.c` | 29 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/math.s` | 403 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/net_udp.c` | 537 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/q_shlinux.c` | 205 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/qasm.h` | 459 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/qgl_linux.c` | 3994 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_aclipa.s` | 195 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_draw16.s` | 1227 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_drawa.s` | 817 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_edgea.s` | 729 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_scana.s` | 68 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_spr8.s` | 879 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_surf8.s` | 762 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/r_varsa.s` | 223 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/rw_in_svgalib.c` | 374 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/rw_linux.h` | 16 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/rw_svgalib.c` | 311 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/rw_x11.c` | 1093 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/snd_linux.c` | 266 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/snd_mixa.s` | 193 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/sys_dosa.s` | 94 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/sys_linux.c` | 379 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/vid_menu.c` | 437 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `linux/vid_so.c` | 489 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `makefile` | 259 | N/A | build/docs | - | n/a |
| `makezip` | 2 | N/A | build/docs | - | n/a |
| `makezip.bat` | 1 | N/A | build/docs | - | n/a |
| `null/cd_null.c` | 31 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `null/cl_null.c` | 55 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `null/glimp_null.c` | 34 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `null/in_null.c` | 36 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `null/snddma_null.c` | 28 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `null/swimp_null.c` | 30 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `null/sys_null.c` | 127 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `null/vid_null.c` | 145 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `qcommon/cmd.c` | 892 | P | Go internal/qcommon/cmd; TS q2-client | core | verified |
| `qcommon/cmodel.c` | 1770 | P | Go internal/cmodel; TS q2-pmove | core | verified |
| `qcommon/common.c` | 1588 | P | Go internal/qcommon/msg; TS q2-protocol (Z_* -> GC, Qcommon_Frame -> loops) | core | verified |
| `qcommon/crc.c` | 92 | P | Go internal/qcommon/crc; TS q2-protocol | core | verified |
| `qcommon/crc.h` | 6 | P | shared header (types ported alongside users) | core | ported |
| `qcommon/cvar.c` | 527 | P | Go internal/qcommon/cvar; TS q2-client | core | verified |
| `qcommon/files.c` | 877 | P | Go internal/assets/pak; TS q2-formats | core | verified |
| `qcommon/md4.c` | 278 | P | Go internal/qcommon/md4; TS q2-protocol | core | verified |
| `qcommon/net_chan.c` | 387 | P | Go internal/net; TS q2-protocol | core | verified |
| `qcommon/pmove.c` | 1360 | P | Go internal/pmove; TS q2-pmove | core | verified |
| `qcommon/qcommon.h` | 826 | P | shared header (types ported alongside users) | core | ported |
| `qcommon/qfiles.h` | 482 | P | shared header (types ported alongside users) | core | ported |
| `quake2.001` | 2061 | N/A | build/docs | - | n/a |
| `quake2.bce` | 81 | N/A | build/docs | - | n/a |
| `quake2.bcp` | 6 | N/A | build/docs | - | n/a |
| `quake2.dsp` | 2050 | N/A | build/docs | - | n/a |
| `quake2.dsw` | 77 | N/A | build/docs | - | n/a |
| `quake2.mak` | 4593 | N/A | build/docs | - | n/a |
| `quake2.opt` | 1296 | N/A | build/docs | - | n/a |
| `quake2.plg` | 715 | N/A | build/docs | - | n/a |
| `readme.txt` | 29 | N/A | build/docs | - | n/a |
| `ref_gl/anorms.h` | 181 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/anormtab.h` | 37 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_draw.c` | 415 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_image.c` | 1571 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_light.c` | 729 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_local.h` | 460 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_mesh.c` | 839 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_model.c` | 1223 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_model.h` | 261 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_rmain.c` | 1692 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_rmisc.c` | 246 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_rsurf.c` | 1660 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/gl_warp.c` | 662 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_gl/qgl.h` | 442 | R | q2-render-gl qgl.ts: fixed-function emulation on WebGL2 | Playwright render smoke | ported |
| `ref_gl/ref_gl.001` | 752 | N/A | build file | - | n/a |
| `ref_gl/ref_gl.def` | 2 | N/A | build file | - | n/a |
| `ref_gl/ref_gl.dsp` | 773 | N/A | build file | - | n/a |
| `ref_gl/ref_gl.plg` | 17 | N/A | build file | - | n/a |
| `ref_gl/warpsin.h` | 51 | P | TS q2-render-gl (WebGL2) | q2-render-gl oracle.test.ts (scripts/oracle) | ported |
| `ref_soft/adivtab.h` | 1077 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/anorms.h` | 181 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/asm_draw.h` | 121 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/block16.inc` | 116 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/block8.inc` | 116 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/d_if.inc` | 81 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/d_ifacea.h` | 76 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/qasm.inc` | 435 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_aclip.c` | 323 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_aclipa.asm` | 200 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_alias.c` | 1198 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_bsp.c` | 637 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_draw.c` | 445 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_draw16.asm` | 1234 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_drawa.asm` | 822 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_edge.c` | 1125 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_edgea.asm` | 733 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_image.c` | 617 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_light.c` | 442 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_local.h` | 849 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_main.c` | 1422 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_misc.c` | 670 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_model.c` | 1241 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_model.h` | 256 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_part.c` | 638 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_poly.c` | 1244 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_polysa.asm` | 812 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_polyse.c` | 1539 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_rast.c` | 852 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_scan.c` | 591 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_scana.asm` | 73 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_spr8.asm` | 884 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_sprite.c` | 123 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_surf.c` | 651 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_surf8.asm` | 771 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/r_varsa.asm` | 220 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/rand1k.h` | 123 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/ref_soft.001` | 1498 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/ref_soft.def` | 2 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/ref_soft.dsp` | 1496 | D | software renderer dropped (user decision) | - | n/a |
| `ref_soft/ref_soft.plg` | 17 | D | software renderer dropped (user decision) | - | n/a |
| `rhapsody/in_next.m` | 332 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/makefile.bak` | 63 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/notes.txt` | 34 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/pb.project` | 17 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/quake2.iconheader` | 2 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/quake2.tiff` | 215 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/r_next.m` | 735 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/rhapqw.txt` | 36 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/snd_next.m` | 2151 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/swimp_rhap.m` | 580 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/sys_rhap.m` | 338 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `rhapsody/vid_next.m` | 1789 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `server/server.h` | 341 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_ccmds.c` | 1050 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_ents.c` | 727 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_game.c` | 396 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_init.c` | 465 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_main.c` | 1055 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_null.c` | 15 | D | stub for client-only builds | - | n/a |
| `server/sv_send.c` | 567 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_user.c` | 664 | P | Go internal/sv | lockstep/.dm2 | ported |
| `server/sv_world.c` | 659 | P | Go internal/sv | lockstep/.dm2 | ported |
| `solaris/Makefile.OLD` | 478 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `solaris/Makefile.Solaris` | 719 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `solaris/g_so.c` | 3 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `solaris/glob.c` | 164 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `solaris/glob.h` | 1 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `solaris/net_udp.c` | 537 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `solaris/q_shsolaris.c` | 196 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `solaris/sys_solaris.c` | 337 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `unix/makefile` | 308 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `unix/makefile_old` | 317 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `unix/next/sv_ccmds.o` | 162 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/cd_win.c` | 510 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/conproc.c` | 431 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/conproc.h` | 24 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/glw_imp.c` | 616 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/glw_win.h` | 47 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/in_win.c` | 889 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/net_wins.c` | 842 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/q2.aps` | 5 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/q2.ico` | 1 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/q2.rc` | 72 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/q_shwin.c` | 215 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/qe3.ico` | 1 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/qgl_win.c` | 4133 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/resource.h` | 16 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/rw_ddraw.c` | 556 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/rw_dib.c` | 375 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/rw_imp.c` | 471 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/rw_win.h` | 68 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/snd_win.c` | 861 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/sys_win.c` | 663 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/vid_dll.c` | 760 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/vid_menu.c` | 473 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/winquake.aps` | 6 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/winquake.h` | 44 | D | platform layer (replaced by browser/Go runtime) | - | n/a |
| `win32/winquake.rc` | 98 | D | platform layer (replaced by browser/Go runtime) | - | n/a |

## Feature-level dispositions

| Feature | Disp. | Replacement |
|---|---|---|
| `svc_download` / allow_download_* | R | HTTP content-addressed assets; cvars kept as no-ops |
| rcon / rcon_password | R | admin REST API |
| master server heartbeat, setmaster, pingservers | R | server-browser API |
| IP filters (g_svcmds addip/listip) | R | account/IP bans in Postgres |
| config.cfg / autoexec.cfg | R | per-account settings (+ exec of user cfg text) |
| screenshots | R | canvas.toBlob |
| CD audio | R | optional user OGG per CS_CDTRACK |
| joystick | R | Gamepad API |
| demo record/playback (.dm2), serverrecord, timedemo | P | client + server |
| Sys_CopyProtect | D | CD check, dead code on Linux |
| IPX | D | obsolete |
| vid_ref switching | D | single WebGL2 renderer |

## TODO-IMPROVE log

(deliberate deviations from the oracle; each behind a flag defaulting to original behavior)

- cmodel/msg/cmd/cvar (Go, Phase 1): memory-safety guards where C has undefined behaviour — negative brush-side
  texinfo resolves to a zero surface (matches the 64-bit oracle), load-time range checks on all BSP cross references
  return errors, PVS decompression past the lump zero-fills, out-of-range area/portal indices raise ERR_DROP,
  MSG_ReadDir with no data raises ERR_DROP, stale-memory reads return zeros. No behavior change on valid input.
- sv (Go, Phase 3): `rand()` and `SV_CheckTimeouts` run exactly once per game frame just before it (C: every host-loop
  iteration, timing-dependent); packets processed on arrival advancing `svs.realtime`; `Cbuf_Execute` before each frame and packet.
- sv: PVS/PHS lookups for cluster -1 count as "not visible" (C reads out of bounds); configstring writes clamp at the array
  end; PF_*printf 1024-byte buffers truncate; clc_move checksum range bounded by buffer size.
- sv: NET_Config, master heartbeats (inert), localtime, gamedir filesystem replaced by host, SaveStore, DemoCreate, FileSystem interfaces.
- sv/cmodel (review 02, docs/review/02-go-engine.md; invalid/hostile input only): clc_move checksum start clamped too;
  `configstrings`/`baselines` with a negative start begin at 0; `begin` is ignored unless sv.state == ss_game;
  client stringcmd `$name` expansion only reads CVAR_SERVERINFO cvars (others expand to "", like unknown cvars);
  client edict pointers re-pointed after ge->ReadGame; a Go runtime panic under HandlePacket/Frame/ExecuteText
  shuts only that instance down (InternalError); maps whose node graph is not a forest (cycles, shared subtrees) are
  rejected at load; leaf clusters < -1 count as not visible in CM_HeadnodeVisible; opt-in `DropClientOnPanic` drops
  only the client whose message panicked; the shared map cache is keyed by SHA-256 and LRU-bounded.
- q2-client (TS, Phase 3): downloads replaced by async asset loading (map checksum still verified vs CS_MAPCHECKSUM);
  async registration with generation guards; async `exec`; `seta/setu/sets/toggle` added; demos recorded in memory;
  pingservers unavailable, rcon over current connection only, no CD audio / VID_CheckChanges; menus delegated to React host;
  ProtocolError/CMError → ERR_DROP; V_RenderView entity sort groups by model then skin (C qsort order is implementation-defined).
- q2-client / q2-formats / q2-pmove (TS, review 04, docs/review/04-ts-engine.md): `svc_stufftext` text (and aliases /
  exec files it defines or runs) executes restricted: `bind unbind unbindall seta rcon screenshot` are refused, as are
  writes to CVAR_ARCHIVE cvars and to `rcon_password`, and `$rcon_password` expands to "" (bindings and archived cvars
  are saved to the account immediately and replayed at every start). Saved config text: key ';' written as SEMICOLON,
  key '"' not written, '"' inside a binding/cvar value written as `'`, archived cvars with quote/';'/space in the name
  skipped. Hostile/invalid input only: negative svc_download size, svc_sound entity < 0 or == MAX_EDICTS, TE_LIGHTNING
  entity out of range, spawnbaseline number out of range and more than MAX_PARSE_ENTITIES entities in one frame raise
  ERR_DROP; netgraph drop marks beyond the 1024-entry ring are skipped (same graph); receive queue capped at 1024
  datagrams; maps whose node graph is not a forest (cycle / shared subtree) are rejected; MD2 frames must not overlap/overrun, TGA/WAV sizes are
  checked against the data before allocating (resampled sounds capped at 2^25 samples), the CIN Huffman decoder stops
  one byte past its block.
- game (Go, Phase 4): save format stores all edicts below num_edicts (incl. freed-slot freetimes), restores linkcount,
  and saves precache indices / player trail / static animation counters so save→load→continue equals an uninterrupted run;
  coop `item->drop = NULL` global mutation kept per game instance; `writeip` keeps filters in memory (no file);
  atan2/sin from Go math (may differ from glibc by 1 ulp before narrowing; not observed in goldens).
- q2-render-gl (TS, Phase 2; see `web/packages/q2-render-gl/src/*.ts` headers): WebGL2 has no GL_SGIS_multitexture,
  GL_EXT_paletted_texture or GL_EXT_point_parameters, so exactly the C code paths of a modern driver run
  (non-multitexture world, RGBA uploads, textured-triangle particles). Immediate mode is emulated by a batching layer
  (qgl.ts); the world/bmodel surfaces are drawn from a static VBO in (texture, lightmap page, SURF_FLOWING) batches and
  the two-pass lightmap blend (GL_ZERO,GL_SRC_COLOR / ONE,ONE / SRC_ALPHA / gl_lightmap) is one shader pass (identical up
  to one framebuffer rounding). Lightmap pages live in a TEXTURE_2D_ARRAY with canonical + displayed CPU mirrors: surfaces
  C draws from the dynamic block 0 get their per-frame lightmap at their own page location and are restored afterwards
  (same texels sampled). Water warp is computed on the CPU exactly as EmitWaterPolys. Draw_StretchRaw uses an R8UI
  index texture + palette texture with bilinear filtering after the lookup (== the RGBA upload C uses without paletted
  textures). Pics and model/texture files are prefetched asynchronously before the synchronous loaders run; a pic drawn
  before its file arrived is skipped (Draw_GetPicSize returns 0,0 while pending) and failed lookups are cached until the
  next BeginRegistration. No video modes (canvas size = vid size), no GL_FRONT drawbuffer / swapinterval / gl_log /
  3dfx gamma; screenshot = canvas.toBlob PNG. Memory-safety guards on BSP cross references (planes, marksurfaces,
  node children, sprite frame < 0) raise ERR_DROP where C would read out of bounds.
- q2-sound: portable C 8-bit mixer (`snd_mix.c` `vol>>11`) makes 8-bit sounds silent; default follows the retail x86
  asm (`vol>>3`); `portableC8bit: true` reproduces the C. Sounds started before data loads play once loaded with the
  original start time; worklet resamples linearly if the browser refuses the s_khz rate; commands dropped before first gesture.
- cl_cin: async file load (C synchronous); short read → ERR_DROP. cl_fx: out-of-range flash index reads 0.
- Review 05 (renderer/sound, memory safety on crafted assets only): texinfo animation chains capped at texinfo count;
  PVS decompression bounded (out-of-range cluster = all visible); MD2 glcmd counts validated (ERR_DROP); warp subdivision
  capped (2^18 polys/model, depth 64); BSP node cycles/shared subtrees rejected; sound channels with invalid loop points
  stopped; resampled sounds capped at 16 MiB. Valid assets unchanged (oracle tests bit-exact).
- Review 03 (game, invalid/hostile input only): every game entry point converts Go panics to gi.error (instance
  drop, not process crash); target/train recursion capped at 4096 nesting (stack overflow in C); SelectPrevItem skips
  negative index; target_string with negative count draws blank; saves validated (array sizes/indices) and
  decompression capped at 64 MB; dead noclip coop body gets a corpse movetype (C: bad movetype shutdown);
  map/nextmap names cut at the first quote or line break (C allowed console command injection); flood_msgs
  outside 1..11 reads the same neighbouring fields C does.
- fakeclient (Go, headless client for tests and the AI agent; no PARITY row of its own, see the `client/*.c` rows):
  opt-in hooks, all off with a zero `Options` (which behaves and sends byte for byte as before; `sv_test.go` and
  `host/oracle_diff_test.go` unchanged): `Feed` (the post-`Recv` body of readPacket) / `Tick` (checkForResend +
  sendConnected) / `BeginConnect` for a driver that owns the datagram loop; `Options.Clock` replaces only `curtime`
  (Sys_Milliseconds); `Options.OnServerMessage` is called at the `CL_WriteDemoMessage` point (after
  CL_ParseServerMessage, before stuffed commands run) with the payload (`datagram[8:]`) and a `Span` per svc command;
  `Options.Passive` + `NewPassive`/`FeedPayload` parse demo blocks, recording stufftext without executing it and
  answering no download (`Feed` refuses datagrams: `ErrPassive`); `Options.MaxHistory` keeps each event history
  between MaxHistory and 2*MaxHistory entries (amortized), with `Client.Counts` (`HistoryCounts`, including
  `Inventory`: svc_inventory messages parsed) and `NewSince` returning exact new-event slices; `TempEntEvents` keeps
  the positions/directions CL_ParseTEnt already reads; `MuzzleFlashes` records the flashes C discards after the
  effect; `RequestFullFrame` sets `cls.demowaiting` (default off, never used in lockstep determinism runs);
  `LevelGen` counts svc_serverdata; `MapName`. Memory safety (hostile input only): a negative 16-bit entity number
  in packetentities raises ERR_DROP (C indexes cl_entities out of bounds).
- demo (Go, `internal/demo` = `CL_Record_f`/`CL_WriteDemoMessage`/`CL_Stop_f`, PORTING rules apply): opt-in
  `Writer.AllBaselines` also writes the received baselines that have no model, which CL_Record_f skips
  (`if (!ent->modelindex) continue`), so sound-only entities such as looping target_speakers replay from their own
  baseline instead of a null one at the world origin; the default is the C behavior (the TS header-parity test uses
  it). `Recorder` (the agent's per-level recording, sets AllBaselines) writes one file per level generation and ends
  each file before the message that leaves the level (svc_disconnect, svc_reconnect, a stuffed `changing` or
  `reconnect`), so every file plays to its end; a C recording keeps going across level changes.
- game (Go, port bug fix, not a deviation): `cfmt` translated only a bare C `%i`, so HelpComputer's `"%3i/%3i"`
  kill counters printed `%!i(int32=  0)`; every `%[flags][width][.prec]i` now formats like `%d` (`%%` untouched),
  as printf does (TestCfmtIntVerbs, TestHelpComputerKillsField). Found by the agent's help-computer parser.
- spectate (Go, not a port; its handshake mirrors SV_New_f/SV_Configstrings_f/SV_Baselines_f/SV_Begin_f and the
  keyframe SV_WriteFrameToClient with deltaframe -1): `writeKeyPlayerstate` also sends PS_WEAPONFRAME when gunframe
  is 0 but gunoffset/gunangles are nonzero (a delta-coding residue the C `from == NULL` path would drop), so a
  viewer's keyframe equals the bot's own playerstate.
