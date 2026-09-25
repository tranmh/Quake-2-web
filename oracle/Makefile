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
all: $(B)/q2ded $(B)/baseq2/game.so $(B)/ctf/game.so

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

clean:
	rm -rf build/obj build/bin
