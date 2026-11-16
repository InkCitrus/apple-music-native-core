.text
.p2align 2
// AAPCS64 returns non-trivial C++ objects through x8, independently of x0.
.globl _native_call_sret1
_native_call_sret1:
 mov x16, x0
 mov x8, x1
 mov x0, x2
 br x16
