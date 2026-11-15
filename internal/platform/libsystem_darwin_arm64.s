// Trampolines to libSystem functions (E11), as in golang.org/x/sys/unix.

#include "textflag.h"

TEXT libc_proc_pidinfo_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_proc_pidinfo(SB)
GLOBL	·libc_proc_pidinfo_trampoline_addr(SB), RODATA, $8
DATA	·libc_proc_pidinfo_trampoline_addr(SB)/8, $libc_proc_pidinfo_trampoline<>(SB)

TEXT libc_grantpt_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_grantpt(SB)
GLOBL	·libc_grantpt_trampoline_addr(SB), RODATA, $8
DATA	·libc_grantpt_trampoline_addr(SB)/8, $libc_grantpt_trampoline<>(SB)

TEXT libc_unlockpt_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_unlockpt(SB)
GLOBL	·libc_unlockpt_trampoline_addr(SB), RODATA, $8
DATA	·libc_unlockpt_trampoline_addr(SB)/8, $libc_unlockpt_trampoline<>(SB)

TEXT libc_ptsname_r_trampoline<>(SB),NOSPLIT,$0-0
	JMP	libc_ptsname_r(SB)
GLOBL	·libc_ptsname_r_trampoline_addr(SB), RODATA, $8
DATA	·libc_ptsname_r_trampoline_addr(SB)/8, $libc_ptsname_r_trampoline<>(SB)
