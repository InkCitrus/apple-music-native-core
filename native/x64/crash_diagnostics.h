#pragma once
#include <csignal>
#include <atomic>
#ifndef _WIN32
#include <sys/resource.h>
#endif

// Keep only a fixed phase and exception metadata. Never inspect guest memory.
namespace crash_diagnostics {
static int report_fd = -1;
static std::atomic<const char*> phase{"runtime_load"};
#ifdef _WIN32
using guest_dladdr = int(*)(const void*,void*);
static guest_dladdr guest_location = nullptr;
static volatile LONG recording = 0, faults_recorded = 0;
static void record(EXCEPTION_POINTERS* info, bool unhandled) {
    if (InterlockedCompareExchange(&recording,1,0)) return;
    char line[512];
    char path[MAX_PATH] = {}, module[96] = "unknown_guest";
    HMODULE owner = nullptr;
    unsigned long long offset = 0;
    auto address = info->ExceptionRecord->ExceptionAddress;
    if (GetModuleHandleExA(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS |
                          GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT,
                          reinterpret_cast<LPCSTR>(address), &owner) &&
        GetModuleFileNameA(owner, path, MAX_PATH)) {
        offset = reinterpret_cast<uintptr_t>(address) - reinterpret_cast<uintptr_t>(owner);
    } else if (guest_location) {
        struct { const char* name; void* base; const char* symbol; void* symbol_address; } location{};
        if (guest_location(address,&location) && location.name && location.base) {
            snprintf(path,sizeof(path),"%s",location.name);
            offset = reinterpret_cast<uintptr_t>(address) - reinterpret_cast<uintptr_t>(location.base);
        }
    }
    if (path[0]) {
        const char* name = path;
        for (const char* p = path; *p; ++p) if (*p == '/' || *p == '\\') name = p + 1;
        size_t n = 0;
        for (; name[n] && n < sizeof(module)-1; ++n) {
            char c = name[n];
            module[n] = (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
                        (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '_' ? c : '_';
        }
        module[n] = 0;
    }
    bool near_null = info->ExceptionRecord->ExceptionCode == EXCEPTION_ACCESS_VIOLATION &&
        info->ExceptionRecord->NumberParameters >= 2 && info->ExceptionRecord->ExceptionInformation[1] < 4096;
    int n = snprintf(line, sizeof(line),
        "{\"event\":\"%s\",\"stage\":\"%s\",\"exceptionCode\":%lu,"
        "\"module\":\"%s\",\"moduleOffset\":%llu,\"faultNearNull\":%s,\"memoryDumpSaved\":false}\n",
        unhandled ? "nativeCrash" : "nativeFault", phase.load(std::memory_order_relaxed),
        static_cast<unsigned long>(info->ExceptionRecord->ExceptionCode), module, offset, near_null ? "true" : "false");
    if (n > 0 && n < static_cast<int>(sizeof(line))) _write(report_fd, line, n);
    InterlockedExchange(&recording,0);
}
static LONG WINAPI observe(EXCEPTION_POINTERS* info) {
    DWORD code = info->ExceptionRecord->ExceptionCode;
    if ((code == EXCEPTION_ACCESS_VIOLATION || code == EXCEPTION_ILLEGAL_INSTRUCTION ||
         code == EXCEPTION_PRIV_INSTRUCTION || code == EXCEPTION_STACK_OVERFLOW) &&
        InterlockedIncrement(&faults_recorded) <= 8) record(info,false);
    // A first-chance fault may be handled by the runtime. Observe it only.
    return EXCEPTION_CONTINUE_SEARCH;
}
static LONG WINAPI fatal(EXCEPTION_POINTERS* info) {
    record(info,true);
    return EXCEPTION_EXECUTE_HANDLER;
}
static void install(int fd) {
    report_fd = fd;
    AddVectoredExceptionHandler(1,observe);
    SetUnhandledExceptionFilter(fatal);
}
#else
static void fatal(int signal) {
    char line[256];
    int n = snprintf(line, sizeof(line),
        "{\"event\":\"nativeCrash\",\"stage\":\"%s\",\"signal\":%d,\"memoryDumpSaved\":false}\n",
        phase.load(std::memory_order_relaxed), signal);
    if (n > 0 && n < static_cast<int>(sizeof(line))) (void)write(report_fd, line, size_t(n));
    _exit(128 + signal);
}
static void install(int fd) {
    report_fd = fd;
    struct rlimit limit = {0, 0};
    setrlimit(RLIMIT_CORE, &limit);
    for (int signal : {SIGSEGV, SIGBUS, SIGILL, SIGFPE}) std::signal(signal, fatal);
}
#endif
}
