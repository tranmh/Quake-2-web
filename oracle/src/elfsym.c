/* elfsym.c -- exact address -> symbol name for the dlopen'ed game module.
 * dladdr only sees .dynsym and returns the *nearest* exported symbol, which silently mislabels static
 * functions; so we read the full .symtab (the module is built with -g and not stripped) and require an
 * exact address match. */
#define _GNU_SOURCE
#include <dlfcn.h>
#include <elf.h>
#include <link.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

typedef struct { unsigned long addr; const char *name; } esym_t;
static esym_t *syms;
static int nsyms;
static unsigned long base;

static int cmp(const void *a, const void *b)
{
	const esym_t *x = a, *y = b;
	if (x->addr != y->addr) return x->addr < y->addr ? -1 : 1;
	return strcmp(x->name, y->name);
}

/* anchor: any address inside the loaded module */
int elfsym_load(void *anchor)
{
	Dl_info info;
	FILE *f;
	long size;
	unsigned char *img;
	Elf64_Ehdr *eh;
	Elf64_Shdr *sh;
	int i, pass;

	if (!dladdr(anchor, &info) || !info.dli_fname) return 0;
	base = (unsigned long)info.dli_fbase;
	f = fopen(info.dli_fname, "rb");
	if (!f) return 0;
	fseek(f, 0, SEEK_END); size = ftell(f); fseek(f, 0, SEEK_SET);
	img = malloc(size);
	if (fread(img, 1, size, f) != (size_t)size) { fclose(f); return 0; }
	fclose(f);
	eh = (Elf64_Ehdr *)img;
	sh = (Elf64_Shdr *)(img + eh->e_shoff);
	for (pass = 0; pass < 2 && !nsyms; pass++) {
		unsigned want = pass == 0 ? SHT_SYMTAB : SHT_DYNSYM;
		for (i = 0; i < eh->e_shnum; i++) {
			Elf64_Sym *s; int n, j; const char *str;
			if (sh[i].sh_type != want) continue;
			s = (Elf64_Sym *)(img + sh[i].sh_offset);
			n = sh[i].sh_size / sizeof(Elf64_Sym);
			str = (const char *)(img + sh[sh[i].sh_link].sh_offset);
			syms = realloc(syms, sizeof(esym_t) * (nsyms + n));
			for (j = 0; j < n; j++) {
				int t = ELF64_ST_TYPE(s[j].st_info);
				if ((t != STT_FUNC && t != STT_OBJECT) || !s[j].st_value || !str[s[j].st_name]) continue;
				syms[nsyms].addr = s[j].st_value;
				syms[nsyms].name = str + s[j].st_name;
				nsyms++;
			}
		}
	}
	qsort(syms, nsyms, sizeof(esym_t), cmp);
	return nsyms;
}

/* NULL for NULL; exact symbol name, or "?+0x<offset>" when the address is not a symbol start */
const char *elfsym_name(const void *p)
{
	static char buf[8][64];
	static int k;
	unsigned long a;
	int lo = 0, hi = nsyms - 1;
	if (!p) return NULL;
	a = (unsigned long)p - base;
	while (lo <= hi) {
		int mid = (lo + hi) / 2;
		if (syms[mid].addr == a) {
			while (mid > 0 && syms[mid - 1].addr == a) mid--;   /* aliases: alphabetically first */
			return syms[mid].name;
		}
		if (syms[mid].addr < a) lo = mid + 1; else hi = mid - 1;
	}
	k = (k + 1) & 7;
	snprintf(buf[k], sizeof(buf[k]), "?+0x%lx", a);
	return buf[k];
}
