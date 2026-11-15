/*
 * Prints the width tmux gives every code point when it is built with utf8proc:
 * utf8proc_charwidth, with private use characters (category Co) 1, as tmux's
 * compat/utf8proc.c does. Lines are "first last width" for runs of code points
 * whose width is not 1. Used by scripts/gen-widths.sh.
 */
#include <stdio.h>
#include "utf8proc.h"

static int width(utf8proc_int32_t c) {
	if (utf8proc_category(c) == UTF8PROC_CATEGORY_CO)
		return 1;
	return utf8proc_charwidth(c);
}

int main(void) {
	utf8proc_int32_t c, start = -1;
	int w, cur = 1;

	printf("# utf8proc %s, Unicode %s\n", utf8proc_version(), utf8proc_unicode_version());
	for (c = 0x80; c <= 0x110000; c++) {
		if (c >= 0xd800 && c <= 0xdfff)
			w = -1; /* not a code point: ends any run */
		else if (c == 0x110000)
			w = -1;
		else
			w = width(c);
		if (w != cur) {
			if (cur != 1 && cur != -1 && start != -1)
				printf("%x %x %d\n", start, c - 1, cur);
			start = c;
			cur = w;
		}
	}
	return 0;
}
