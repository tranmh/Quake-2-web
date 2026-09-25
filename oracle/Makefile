# C oracle harness. Builds from build/src (a patched copy of ../Quake-2 made by sync.sh).
CC      ?= gcc
S       := build/src
O       := build/obj
B       := build/bin
CFLAGS  := -m64 -O1 -g -std=gnu89 -fcommon -fno-strict-aliasing -fno-fast-math \
           -ffp-contract=off -fexcess-precision=standard -Dstricmp=strcasecmp \
           -DORACLE -w -Isrc
PICFLAGS := $(CFLAGS) -fPIC -DGAME_HARD_LINKED_NO
LDLIBS  := -lm -ldl

CORE := qcommon/cmd qcommon/cmodel qcommon/common qcommon/crc qcommon/cvar \
        qcommon/files qcommon/md4 qcommon/net_chan qcommon/pmove game/q_shared \
        linux/q_shlinux linux/glob linux/net_udp
SERVER := server/sv_ccmds server/sv_ents server/sv_game server/sv_init server/sv_main \
          server/sv_send server/sv_user server/sv_world
GAME := $(basename $(notdir $(wildcard $(S)/game/*.c)))
CTF  := $(basename $(notdir $(wildcard $(S)/ctf/*.c)))

core_o   = $(addprefix $(O)/,$(addsuffix .o,$(CORE)))
server_o = $(addprefix $(O)/,$(addsuffix .o,$(SERVER)))
game_po  = $(addprefix $(O)/pic/game/,$(addsuffix .o,$(GAME)))
ctf_po   = $(addprefix $(O)/pic/ctf/,$(addsuffix .o,$(CTF)))

.PHONY: all sync clean
all: $(B)/q2ded $(B)/baseq2/game.so $(B)/ctf/game.so $(B)/oracle_core $(B)/oracle_game $(B)/oracle_game_ctf

sync:
	./sync.sh

$(O)/%.o: $(S)/%.c
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -c $< -o $@

$(O)/pic/%.o: $(S)/%.c
	@mkdir -p $(dir $@)
	$(CC) $(PICFLAGS) -c $< -o $@

$(O)/oracle/%.o: src/%.c
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -I$(S) -c $< -o $@

$(O)/linux/sys_linux.o: CFLAGS += -DDEDICATED_ONLY

$(B)/q2ded: $(core_o) $(server_o) $(O)/linux/sys_linux.o $(O)/null/cl_null.o $(O)/null/cd_null.o
	@mkdir -p $(dir $@)
	$(CC) -o $@ $^ $(LDLIBS)

$(B)/baseq2/game.so: $(game_po)
	@mkdir -p $(dir $@)
	$(CC) -shared -o $@ $^ -lm

$(B)/ctf/game.so: $(ctf_po)
	@mkdir -p $(dir $@)
	$(CC) -shared -o $@ $^ -lm

# ---- oracle drivers (sources in src/, see docs/ORACLE.md) ----
# The oracle binaries use sys_oracle.c (headless Sys_*, inert NET_*) instead of sys_linux.c/net_udp.c.
ORACLE_ENGINE := $(filter-out $(O)/linux/net_udp.o,$(core_o)) $(server_o) $(O)/null/cl_null.o $(O)/null/cd_null.o \
                 $(O)/oracle/sys_oracle.o $(O)/oracle/ojson.o $(O)/oracle/ostate.o
ORACLE_H := $(wildcard src/*.h)

$(O)/oracle/%.o: $(ORACLE_H)

$(O)/oracle/game_dump_baseq2.o: src/game_dump.c $(ORACLE_H)
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -I$(S)/game -c $< -o $@

$(O)/oracle/game_dump_ctf.o: src/game_dump.c $(ORACLE_H)
	@mkdir -p $(dir $@)
	$(CC) $(CFLAGS) -I$(S)/ctf -DORACLE_CTF -c $< -o $@

$(B)/oracle_core: $(ORACLE_ENGINE) $(O)/oracle/core_main.o
	@mkdir -p $(dir $@)
	$(CC) -o $@ $^ $(LDLIBS)

# rand is exported from the executable so game.so's rand() calls resolve to the counting wrapper
$(B)/oracle_game: $(ORACLE_ENGINE) $(O)/oracle/game_main.o $(O)/oracle/elfsym.o $(O)/oracle/game_dump_baseq2.o
	@mkdir -p $(dir $@)
	$(CC) -Wl,--export-dynamic-symbol=rand -o $@ $^ $(LDLIBS)

$(B)/oracle_game_ctf: $(ORACLE_ENGINE) $(O)/oracle/game_main.o $(O)/oracle/elfsym.o $(O)/oracle/game_dump_ctf.o
	@mkdir -p $(dir $@)
	$(CC) -Wl,--export-dynamic-symbol=rand -o $@ $^ $(LDLIBS)

clean:
	rm -rf build/obj build/bin
