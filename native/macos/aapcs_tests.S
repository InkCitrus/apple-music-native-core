.text
.p2align 2
.globl _test_guest_format
_test_guest_format:
 stp x29, x30, [sp, #-16]!
 adrp x2, Lfmt@PAGE
 add x2, x2, Lfmt@PAGEOFF
 mov x3, #42
 adrp x4, Ltext@PAGE
 add x4, x4, Ltext@PAGEOFF
 fmov d0, #1.25
 mov x5, #3
 mov x6, #7
 bl _guest_snprintf
 ldp x29, x30, [sp], #16
 ret
.globl _test_guest_spill
_test_guest_spill:
 stp x29, x30, [sp, #-48]!
 mov x8, #6
 mov x9, #7
 stp x8, x9, [sp, #0]
 mov x8, #8
 mov x9, #9
 stp x8, x9, [sp, #16]
 // Save link registers after outgoing arguments, outside the callee's stack.
 stp x29, x30, [sp, #32]
 adrp x2, Lspill@PAGE
 add x2, x2, Lspill@PAGEOFF
 mov x3, #1
 mov x4, #2
 mov x5, #3
 mov x6, #4
 mov x7, #5
 bl _guest_snprintf
 ldp x29, x30, [sp, #32]
 add sp, sp, #48
 ret
.globl _test_guest_scan
_test_guest_scan:
 stp x29, x30, [sp, #-16]!
 mov x4, x2
 mov x3, x1
 mov x2, x0
 adrp x0, Lscantext@PAGE
 add x0, x0, Lscantext@PAGEOFF
 adrp x1, Lscanfmt@PAGE
 add x1, x1, Lscanfmt@PAGEOFF
 bl _guest_sscanf
 ldp x29, x30, [sp], #16
 ret
.section __TEXT,__cstring,cstring_literals
Lfmt: .asciz "integer=%d text=%s decimal=%.2f padded=%*d"
Ltext: .asciz "ok"
Lspill: .asciz "%d,%d,%d,%d,%d,%d,%d,%d,%d"
Lscantext: .asciz "42 good 1.25"
Lscanfmt: .asciz "%d %4s %lf"
