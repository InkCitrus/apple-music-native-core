/* SPDX-License-Identifier: BSD-3-Clause */
#pragma once
#include <stddef.h>

// Shared by the Windows guest rewriter and account-free decoder tests.
namespace hybris_x86_64 {
using Byte = unsigned char;
inline size_t instruction_length(const Byte* p) {
  size_t i = 0;
  bool opsize16 = false;
  while (p[i] == 0x64 || p[i] == 0x65 || p[i] == 0x66 || p[i] == 0x67 ||
         p[i] == 0xf0 || p[i] == 0xf2 || p[i] == 0xf3 ||
         p[i] == 0x2e || p[i] == 0x36 || p[i] == 0x3e || p[i] == 0x26) {
    if (p[i] == 0x66) opsize16 = true;
    ++i;
  }
  // Immediate width for "iz" operands: imm16 under a 0x66 operand-size prefix,
  // otherwise imm32. (REX.W makes mov-imm 64-bit; handled per-opcode below.)
  const size_t immz = opsize16 ? 2 : 4;
  const bool rex_w = (p[i] & 0xf0) == 0x40 && (p[i] & 8) != 0;
  if ((p[i] & 0xf0) == 0x40) ++i;
  const Byte opcode = p[i++];
  if (opcode == 0x0f) {
    const Byte extended = p[i++];
    if (extended >= 0x80 && extended <= 0x8f) return i + 4;  // jcc rel32
    // 3-byte opcode groups (SSSE3/SSE4/PCLMUL): 0F 38 xx and 0F 3A xx. The
    // Android x86-64 ABI baseline is SSE4.2, so media code (libmediaplatform)
    // is full of these; returning 0 here desynced the linear sweep and left
    // later fs: stack-guard accesses unrewritten. Decode the third opcode byte,
    // the ModRM/SIB/displacement, and the imm8 that every 0F 3A form carries.
    if (extended == 0x38 || extended == 0x3a) {
      ++i;  // third opcode byte
      const Byte modrm3 = p[i++];
      const unsigned mod3 = modrm3 >> 6;
      if (mod3 != 3 && (modrm3 & 7) == 4) {
        const Byte sib3 = p[i++];
        if (mod3 == 0 && (sib3 & 7) == 5) i += 4;
      }
      if (mod3 == 0 && (modrm3 & 7) == 5) i += 4;
      if (mod3 == 1) ++i;
      if (mod3 == 2) i += 4;
      if (extended == 0x3a) ++i;  // 0F 3A opcodes carry an imm8
      return i;
    }
    // setcc, cmovcc and most SSE opcodes all have a ModRM byte.
    const Byte modrm2 = p[i++];
    const unsigned mod2 = modrm2 >> 6;
    if (mod2 != 3 && (modrm2 & 7) == 4) {
      const Byte sib2 = p[i++];
      if (mod2 == 0 && (sib2 & 7) == 5) i += 4;
    }
    if (mod2 == 0 && (modrm2 & 7) == 5) i += 4;
    if (mod2 == 1) ++i;
    if (mod2 == 2) i += 4;
    // Opcodes that carry an imm8 after ModRM.
    switch (extended) {
      case 0x70: case 0x71: case 0x72: case 0x73:
      case 0xc2: case 0xc4: case 0xc5: case 0xc6:
      case 0xa4: case 0xac:
        ++i;
        break;
    }
    return i;
  }
  if (opcode == 0xe8 || opcode == 0xe9 || opcode == 0x68) return i + immz;
  if (opcode == 0xeb || (opcode >= 0x70 && opcode <= 0x7f) ||
      (opcode >= 0xe0 && opcode <= 0xe3) || opcode == 0x6a) return i + 1;
  if (opcode >= 0xb0 && opcode <= 0xb7) return i + 1;
  if (opcode >= 0xb8 && opcode <= 0xbf) return i + (rex_w ? 8 : immz);
  bool has_modrm = false;
  size_t immediate = 0;
  switch (opcode) {
    // Accumulator-register short forms with imm32
    case 0x05: case 0x0d: case 0x15: case 0x1d: case 0x2d: case 0x35: case 0x3d:
      return i + immz;
    // Accumulator-register short forms with imm8
    case 0x04: case 0x0c: case 0x14: case 0x1c: case 0x24: case 0x2c: case 0x34: case 0x3c:
      return i + 1;
    case 0x00: case 0x01: case 0x02: case 0x03:
    case 0x08: case 0x09: case 0x0a: case 0x0b:
    case 0x10: case 0x11: case 0x12: case 0x13:
    case 0x18: case 0x19: case 0x1a: case 0x1b:
    case 0x20: case 0x21: case 0x22: case 0x23:
    case 0x28: case 0x29: case 0x2a: case 0x2b:
    case 0x30: case 0x31: case 0x32: case 0x33:
    case 0x38: case 0x39: case 0x3a: case 0x3b:
    case 0x63: case 0x8b: case 0x89: case 0x8d: case 0x85: case 0xfe: case 0xff:
    // 8-bit and other ModRM-only forms (no immediate). 0x88 (mov r/m8,r8) sits
    // between a function's two stack-guard fs: accesses; without it the sweep
    // desynced and left the second fs:0x28 load unrewritten (faults at 0x28 on
    // a thread whose fs base is 0). 0x84 test, 0x86/0x87 xchg, 0x8a mov r8,r/m8,
    // 0x8c/0x8e mov seg.
    case 0x84: case 0x86: case 0x87: case 0x88: case 0x8a: case 0x8c: case 0x8e:
    // Group 2 shifts/rotates with count 1 or CL have ModRM and no immediate.
    // Missing D1 made "48 d1 e8" look like an unknown REX/D1 followed by
    // CALL rel32. That swallowed the next TEST/JNE and its FS stack guard.
    case 0xd0: case 0xd1: case 0xd2: case 0xd3:
       has_modrm = true;
       break;
    case 0x80: case 0x81: case 0xc6: case 0xc7:
      has_modrm = true;
      // 0x80 (group1 r/m8,imm8) and 0xc6 (mov r/m8,imm8) carry an imm8; only
      // 0x81 (r/m32,imm32) and 0xc7 (mov r/m32,imm32) carry an imm32. Treating
      // 0xc6 as imm32 oversized "mov byte ptr,imm8" by 3 bytes, making the sweep
      // step over the following fs: access (e.g. a stack-guard load).
      immediate = (opcode == 0x80 || opcode == 0xc6) ? 1 : immz;
      break;
    case 0x69:  // imul r, r/m, imm32 (imm16 under 0x66)
      has_modrm = true;
      immediate = immz;
      break;
    case 0x6b:  // imul r, r/m, imm8
      has_modrm = true;
      immediate = 1;
      break;
    case 0xa9:
      return i + immz;
    case 0x25:
      return i + immz;
    case 0x83: case 0xc1: case 0xf6:
      has_modrm = true;
      immediate = 1;
      break;
    case 0x6c: case 0x6d: case 0x6e: case 0x6f: case 0x99: case 0xc9: case 0xec: case 0xf1:
    case 0x90: case 0xa4: case 0xa5: case 0xa6: case 0xa7:
    case 0xaa: case 0xab: case 0xac: case 0xad: case 0xae: case 0xaf:
    case 0xc3: case 0xcc: case 0x5d:
      return i;
    case 0xd4: case 0xd5:
      return i + 1;
    case 0x9a:
      return i + 6;
    default:
      if (opcode >= 0x50 && opcode <= 0x5f) return i;
      return 0;
  }
  const Byte modrm = p[i++];
  const unsigned mod = modrm >> 6;
  if (mod != 3 && (modrm & 7) == 4) {
    const Byte sib = p[i++];
    if (mod == 0 && (sib & 7) == 5) i += 4;
  }
  if (mod == 0 && (modrm & 7) == 5) i += 4;
  if (mod == 1) ++i;
  if (mod == 2) i += 4;
  return i + immediate;
}

}  // namespace hybris_x86_64
