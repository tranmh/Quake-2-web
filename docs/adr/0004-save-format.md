# ADR 0004-save-format

Status: accepted

Raw-struct saves (g_save.c) are unportable. Saves are versioned JSON DTOs compressed with zstd; function pointers are stored by C function name via a registry; edict pointers as indices; items as classnames.
