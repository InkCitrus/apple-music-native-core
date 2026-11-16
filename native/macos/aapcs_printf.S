.text
.p2align 2
.macro GUEST_PRINTF name, kind
.globl _\name
_\name:
 sub sp, sp, #224
 stp x0, x1, [sp, #0]
 stp x2, x3, [sp, #16]
 stp x4, x5, [sp, #32]
 stp x6, x7, [sp, #48]
 stp q0, q1, [sp, #64]
 stp q2, q3, [sp, #96]
 stp q4, q5, [sp, #128]
 stp q6, q7, [sp, #160]
 add x8, sp, #224
 str x8, [sp, #192]
 stp x29, x30, [sp, #208]
 mov x0, sp
 mov x1, #\kind
 .if \kind < 4
 bl _guest_printf_frame
 .elseif \kind < 9
 bl _guest_more_printf_frame
 .else
 bl _guest_scan_frame
 .endif
 ldp x29, x30, [sp, #208]
 add sp, sp, #224
 ret
.endm
GUEST_PRINTF guest_snprintf, 0
GUEST_PRINTF guest_sprintf, 1
GUEST_PRINTF guest_printf, 2
GUEST_PRINTF guest___snprintf_chk, 3

GUEST_PRINTF guest_sscanf, 4
GUEST_PRINTF guest_fprintf, 5
GUEST_PRINTF guest_asprintf, 6
GUEST_PRINTF guest_dprintf, 7

GUEST_PRINTF guest_swprintf, 8

GUEST_PRINTF guest_fscanf, 9
GUEST_PRINTF guest_scanf, 10
