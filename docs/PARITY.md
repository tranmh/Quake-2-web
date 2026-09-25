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
| `client/anorms.h` | 181 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/asm_i386.h` | 81 | D | x86 asm / software-renderer helpers | - | n/a |
| `client/block16.h` | 123 | D | x86 asm / software-renderer helpers | - | n/a |
| `client/block8.h` | 124 | D | x86 asm / software-renderer helpers | - | n/a |
| `client/cdaudio.h` | 26 | R | optional user OGG tracks keyed by CS_CDTRACK | - | pending |
| `client/cl_cin.c` | 650 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_ents.c` | 1500 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_fx.c` | 2298 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_input.c` | 542 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_inv.c` | 142 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_main.c` | 1844 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_newfx.c` | 1323 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_parse.c` | 806 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_pred.c` | 278 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_scrn.c` | 1401 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_tent.c` | 1745 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/cl_view.c` | 584 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/client.h` | 584 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/console.c` | 682 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/console.h` | 62 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/input.h` | 34 | R | browser input / canvas | - | pending |
| `client/keys.c` | 943 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/keys.h` | 146 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/menu.c` | 4016 | R | React shell (Q2 aesthetic) + in-canvas where server-driven | manual/Playwright | pending |
| `client/qmenu.c` | 674 | R | React shell (Q2 aesthetic) + in-canvas where server-driven | manual/Playwright | pending |
| `client/qmenu.h` | 140 | R | React shell (Q2 aesthetic) + in-canvas where server-driven | manual/Playwright | pending |
| `client/ref.h` | 224 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/screen.h` | 62 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/snd_dma.c` | 1214 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/snd_loc.h` | 164 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/snd_mem.c` | 359 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/snd_mix.c` | 497 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/sound.h` | 45 | P | TS q2-client / q2-snd-worklet | oracle_client | pending |
| `client/vid.h` | 42 | R | browser input / canvas | - | pending |
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
| `ctf/g_ai.c` | 1117 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_chase.c` | 157 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_cmds.c` | 1066 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_combat.c` | 596 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_ctf.c` | 4016 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_ctf.h` | 185 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_func.c` | 2047 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_items.c` | 2446 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_local.h` | 1145 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_main.c` | 427 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_misc.c` | 1909 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_monster.c` | 740 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_phys.c` | 959 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_save.c` | 743 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_spawn.c` | 998 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_svcmds.c` | 48 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_target.c` | 809 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_trigger.c` | 598 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_utils.c` | 570 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/g_weapon.c` | 919 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/game.h` | 242 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/layout.txt` | 12 | N/A | build/doc file | - | n/a |
| `ctf/m_move.c` | 556 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/m_player.h` | 225 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/p_client.c` | 1726 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/p_hud.c` | 544 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/p_menu.c` | 256 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/p_menu.h` | 49 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/p_trail.c` | 146 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/p_view.c` | 1135 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/p_weapon.c` | 1469 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/q_shared.c` | 1419 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `ctf/q_shared.h` | 1200 | P | Go internal/game (CTF mode, //ZOID hunks inline) | oracle_game_ctf | pending |
| `game/g_ai.c` | 1117 | P | Go internal/game | oracle_game | pending |
| `game/g_chase.c` | 175 | P | Go internal/game | oracle_game | pending |
| `game/g_cmds.c` | 992 | P | Go internal/game | oracle_game | pending |
| `game/g_combat.c` | 576 | P | Go internal/game | oracle_game | pending |
| `game/g_func.c` | 2048 | P | Go internal/game | oracle_game | pending |
| `game/g_items.c` | 2216 | P | Go internal/game | oracle_game | pending |
| `game/g_local.h` | 1113 | P | Go internal/game | oracle_game | pending |
| `game/g_main.c` | 411 | P | Go internal/game | oracle_game | pending |
| `game/g_misc.c` | 1876 | P | Go internal/game | oracle_game | pending |
| `game/g_monster.c` | 740 | P | Go internal/game | oracle_game | pending |
| `game/g_phys.c` | 961 | P | Go internal/game | oracle_game | pending |
| `game/g_save.c` | 769 | R | named-function registry + versioned saves (ADR-0004) | save idempotence | pending |
| `game/g_spawn.c` | 984 | P | Go internal/game | oracle_game | pending |
| `game/g_svcmds.c` | 300 | P | Go internal/game | oracle_game | pending |
| `game/g_target.c` | 809 | P | Go internal/game | oracle_game | pending |
| `game/g_trigger.c` | 598 | P | Go internal/game | oracle_game | pending |
| `game/g_turret.c` | 432 | P | Go internal/game | oracle_game | pending |
| `game/g_utils.c` | 568 | P | Go internal/game | oracle_game | pending |
| `game/g_weapon.c` | 916 | P | Go internal/game | oracle_game | pending |
| `game/game.001` | 1619 | N/A | build file | - | n/a |
| `game/game.def` | 2 | N/A | build file | - | n/a |
| `game/game.dsp` | 1618 | N/A | build file | - | n/a |
| `game/game.h` | 254 | P | Go internal/game | oracle_game | pending |
| `game/game.plg` | 75 | N/A | build file | - | n/a |
| `game/m_actor.c` | 609 | P | Go internal/game | oracle_game | pending |
| `game/m_actor.h` | 506 | P | Go internal/game | oracle_game | pending |
| `game/m_berserk.c` | 457 | P | Go internal/game | oracle_game | pending |
| `game/m_berserk.h` | 269 | P | Go internal/game | oracle_game | pending |
| `game/m_boss2.c` | 679 | P | Go internal/game | oracle_game | pending |
| `game/m_boss2.h` | 206 | P | Go internal/game | oracle_game | pending |
| `game/m_boss3.c` | 76 | P | Go internal/game | oracle_game | pending |
| `game/m_boss31.c` | 749 | P | Go internal/game | oracle_game | pending |
| `game/m_boss31.h` | 213 | P | Go internal/game | oracle_game | pending |
| `game/m_boss32.c` | 913 | P | Go internal/game | oracle_game | pending |
| `game/m_boss32.h` | 516 | P | Go internal/game | oracle_game | pending |
| `game/m_brain.c` | 676 | P | Go internal/game | oracle_game | pending |
| `game/m_brain.h` | 247 | P | Go internal/game | oracle_game | pending |
| `game/m_chick.c` | 677 | P | Go internal/game | oracle_game | pending |
| `game/m_chick.h` | 313 | P | Go internal/game | oracle_game | pending |
| `game/m_flash.c` | 488 | P | generated tables (genconst) + Go game | genconst | pending |
| `game/m_flipper.c` | 403 | P | Go internal/game | oracle_game | pending |
| `game/m_flipper.h` | 185 | P | Go internal/game | oracle_game | pending |
| `game/m_float.c` | 663 | P | Go internal/game | oracle_game | pending |
| `game/m_float.h` | 273 | P | Go internal/game | oracle_game | pending |
| `game/m_flyer.c` | 626 | P | Go internal/game | oracle_game | pending |
| `game/m_flyer.h` | 182 | P | Go internal/game | oracle_game | pending |
| `game/m_gladiator.c` | 387 | P | Go internal/game | oracle_game | pending |
| `game/m_gladiator.h` | 115 | P | Go internal/game | oracle_game | pending |
| `game/m_gunner.c` | 628 | P | Go internal/game | oracle_game | pending |
| `game/m_gunner.h` | 234 | P | Go internal/game | oracle_game | pending |
| `game/m_hover.c` | 620 | P | Go internal/game | oracle_game | pending |
| `game/m_hover.h` | 230 | P | Go internal/game | oracle_game | pending |
| `game/m_infantry.c` | 607 | P | Go internal/game | oracle_game | pending |
| `game/m_infantry.h` | 232 | P | Go internal/game | oracle_game | pending |
| `game/m_insane.c` | 693 | P | Go internal/game | oracle_game | pending |
| `game/m_insane.h` | 307 | P | Go internal/game | oracle_game | pending |
| `game/m_medic.c` | 769 | P | Go internal/game | oracle_game | pending |
| `game/m_medic.h` | 262 | P | Go internal/game | oracle_game | pending |
| `game/m_move.c` | 556 | P | Go internal/game | oracle_game | pending |
| `game/m_mutant.c` | 663 | P | Go internal/game | oracle_game | pending |
| `game/m_mutant.h` | 174 | P | Go internal/game | oracle_game | pending |
| `game/m_parasite.c` | 552 | P | Go internal/game | oracle_game | pending |
| `game/m_parasite.h` | 143 | P | Go internal/game | oracle_game | pending |
| `game/m_player.h` | 224 | P | Go internal/game | oracle_game | pending |
| `game/m_rider.h` | 66 | P | Go internal/game | oracle_game | pending |
| `game/m_soldier.c` | 1299 | P | Go internal/game | oracle_game | pending |
| `game/m_soldier.h` | 500 | P | Go internal/game | oracle_game | pending |
| `game/m_supertank.c` | 717 | P | Go internal/game | oracle_game | pending |
| `game/m_supertank.h` | 279 | P | Go internal/game | oracle_game | pending |
| `game/m_tank.c` | 856 | P | Go internal/game | oracle_game | pending |
| `game/m_tank.h` | 319 | P | Go internal/game | oracle_game | pending |
| `game/p_client.c` | 1805 | P | Go internal/game | oracle_game | pending |
| `game/p_hud.c` | 571 | P | Go internal/game | oracle_game | pending |
| `game/p_trail.c` | 146 | P | Go internal/game | oracle_game | pending |
| `game/p_view.c` | 1087 | P | Go internal/game | oracle_game | pending |
| `game/p_weapon.c` | 1434 | P | Go internal/game | oracle_game | pending |
| `game/q_shared.c` | 1418 | P | Go internal/qcommon/shared; TS q2-shared | core | pending |
| `game/q_shared.h` | 1200 | P | Go internal/qcommon/shared; TS q2-shared | core | pending |
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
| `qcommon/cmd.c` | 892 | P | Go internal/qcommon/cmd; TS q2-client | core | pending |
| `qcommon/cmodel.c` | 1770 | P | Go internal/cmodel; TS q2-pmove | core | pending |
| `qcommon/common.c` | 1588 | P | Go internal/qcommon/msg; TS q2-protocol (Z_* -> GC, Qcommon_Frame -> loops) | core | pending |
| `qcommon/crc.c` | 92 | P | Go internal/qcommon/crc; TS q2-protocol | core | pending |
| `qcommon/crc.h` | 6 | P | shared header (types ported alongside users) | core | pending |
| `qcommon/cvar.c` | 527 | P | Go internal/qcommon/cvar; TS q2-client | core | pending |
| `qcommon/files.c` | 877 | P | Go internal/assets/pak; TS q2-formats | core | pending |
| `qcommon/md4.c` | 278 | P | Go internal/qcommon/md4; TS q2-protocol | core | pending |
| `qcommon/net_chan.c` | 387 | P | Go internal/net; TS q2-protocol | core | pending |
| `qcommon/pmove.c` | 1360 | P | Go internal/pmove; TS q2-pmove | core | pending |
| `qcommon/qcommon.h` | 826 | P | shared header (types ported alongside users) | core | pending |
| `qcommon/qfiles.h` | 482 | P | shared header (types ported alongside users) | core | pending |
| `quake2.001` | 2061 | N/A | build/docs | - | n/a |
| `quake2.bce` | 81 | N/A | build/docs | - | n/a |
| `quake2.bcp` | 6 | N/A | build/docs | - | n/a |
| `quake2.dsp` | 2050 | N/A | build/docs | - | n/a |
| `quake2.dsw` | 77 | N/A | build/docs | - | n/a |
| `quake2.mak` | 4593 | N/A | build/docs | - | n/a |
| `quake2.opt` | 1296 | N/A | build/docs | - | n/a |
| `quake2.plg` | 715 | N/A | build/docs | - | n/a |
| `readme.txt` | 29 | N/A | build/docs | - | n/a |
| `ref_gl/anorms.h` | 181 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/anormtab.h` | 37 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_draw.c` | 415 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_image.c` | 1571 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_light.c` | 729 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_local.h` | 460 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_mesh.c` | 839 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_model.c` | 1223 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_model.h` | 261 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_rmain.c` | 1692 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_rmisc.c` | 246 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_rsurf.c` | 1660 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/gl_warp.c` | 662 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
| `ref_gl/qgl.h` | 442 | R | WebGL2 context (no qgl function table) | - | pending |
| `ref_gl/ref_gl.001` | 752 | N/A | build file | - | n/a |
| `ref_gl/ref_gl.def` | 2 | N/A | build file | - | n/a |
| `ref_gl/ref_gl.dsp` | 773 | N/A | build file | - | n/a |
| `ref_gl/ref_gl.plg` | 17 | N/A | build file | - | n/a |
| `ref_gl/warpsin.h` | 51 | P | TS q2-render-gl (WebGL2) | ref fixtures | pending |
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
| `server/server.h` | 341 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_ccmds.c` | 1050 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_ents.c` | 727 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_game.c` | 396 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_init.c` | 465 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_main.c` | 1055 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_null.c` | 15 | D | stub for client-only builds | - | n/a |
| `server/sv_send.c` | 567 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_user.c` | 664 | P | Go internal/sv | lockstep/.dm2 | pending |
| `server/sv_world.c` | 659 | P | Go internal/sv | lockstep/.dm2 | pending |
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
- q2-client (TS, Phase 3): downloads replaced by async asset loading (map checksum still verified vs CS_MAPCHECKSUM);
  async registration with generation guards; async `exec`; `seta/setu/sets/toggle` added; demos recorded in memory;
  pingservers unavailable, rcon over current connection only, no CD audio / VID_CheckChanges; menus delegated to React host;
  ProtocolError/CMError → ERR_DROP; V_RenderView entity sort groups by model then skin (C qsort order is implementation-defined).
- game (Go, Phase 4): save format stores all edicts below num_edicts (incl. freed-slot freetimes), restores linkcount,
  and saves precache indices / player trail / static animation counters so save→load→continue equals an uninterrupted run;
  coop `item->drop = NULL` global mutation kept per game instance; `writeip` keeps filters in memory (no file);
  atan2/sin from Go math (may differ from glibc by 1 ulp before narrowing; not observed in goldens).
