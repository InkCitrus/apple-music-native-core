#pragma once
#include <cstdint>
#include <sys/ucontext.h>
struct AndroidTimeval {int64_t seconds,microseconds;};
bool prepare_android_syscalls(const char* elf,uintptr_t base);
bool handle_android_syscall(ucontext_t* context);
unsigned android_syscall_count();
int android_syscall_number(ucontext_t* context);
