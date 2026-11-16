#pragma once
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
typedef void (*mygo_diagnostic_callback)(const char *kind, int code);
void mygo_set_diagnostic_callback(mygo_diagnostic_callback callback);
/* Kinds are fixed literals. Codes never contain pointers or account values. */
void mygo_native_diagnostic(const char *kind, int code);
int drm_lib_probe_auth(const void *config);
typedef struct {
    uint32_t abi_version, depth, condition_kind, flags;
    int32_t code, reserved;
    int64_t external_code;
} mygo_error_detail;
enum { MYGO_ERROR_CODE=1, MYGO_ERROR_EXTERNAL=2, MYGO_ERROR_CATEGORY_VERIFIED=4,
       MYGO_ERROR_NESTED=8, MYGO_ERROR_TRUNCATED=16, MYGO_ERROR_GETTER_MISSING=32 };
typedef void (*mygo_error_detail_callback)(const mygo_error_detail *detail);
void mygo_set_error_detail_callback(mygo_error_detail_callback callback);
void mygo_publish_error_detail(const mygo_error_detail *detail);
void mygo_capture_error_details(void *condition);
#ifdef __cplusplus
}
#endif
