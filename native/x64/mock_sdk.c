/* Test-only ABI fixture. It never contacts Apple or processes real accounts. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include "../../third_party/drm_types.h"
#include "../../third_party/adam-drm/native/mygo_diagnostics.h"
#include <signal.h>
#include <unistd.h>
typedef void(*auth_cb)(const char*,char*,int,void*);
static drm_state_callback_t state_callback;
static void(*diagnostic)(const char*,int);
void mygo_set_diagnostic_callback(void(*callback)(const char*,int)){diagnostic=callback;}
#ifndef MYGO_FIXTURE_LEGACY_DIAGNOSTICS
static mygo_error_detail_callback error_detail;
void mygo_set_error_detail_callback(mygo_error_detail_callback callback){error_detail=callback;}
#endif
int drm_init(const struct drm_config* cfg){state_callback=cfg->state_callback;printf("MOCK_NATIVE_LOG_MUST_BE_DISCARDED\n");return 0;}
int hybris_session_probe(const char* base,const char* device,const char* lib){(void)base;(void)device;(void)lib;if(state_callback)state_callback("PHASE_CONTEXT_CONFIG_CREATE",NULL);if(getenv("MYGO_FIXTURE_CRASH"))raise(SIGSEGV);return 0;}
int hybris_auth_flow_probe(const char* base,const char* device,const char* lib){int r=hybris_session_probe(base,device,lib);if(diagnostic){diagnostic("credentials_cancelled",1);diagnostic("private_data_must_be_filtered",42);}return r;}
int hybris_backend_init(const char* a,const char* b,const char* c){(void)a;(void)b;(void)c;return 0;}
int hybris_fairplay_init(const char* base,const char* device,const char* lib,const char* user,const char* pass,auth_cb callback,void* ud){(void)base;(void)device;(void)lib;if(user&&pass){char code[16]={0};callback("2fa",code,16,ud);return strcmp(code,"001234")? -1:0;}
 if(getenv("MYGO_FIXTURE_CACHED_FAILURE")){
  char code[16]={0};callback("2fa",code,16,ud);if(code[0])return 99;
  if(diagnostic){diagnostic("credentials_cancelled",1);diagnostic("login_response_type",4);diagnostic("login_error_code",9);}
#ifndef MYGO_FIXTURE_LEGACY_DIAGNOSTICS
  if(error_detail){mygo_error_detail d={1,0,1,MYGO_ERROR_CODE|MYGO_ERROR_EXTERNAL|MYGO_ERROR_CATEGORY_VERIFIED|MYGO_ERROR_NESTED,9,0,INT64_MIN};error_detail(&d);d.depth=1;d.condition_kind=2;d.flags=MYGO_ERROR_CODE|MYGO_ERROR_CATEGORY_VERIFIED;d.code=-512;error_detail(&d);d.abi_version=999;error_detail(&d);}
#endif
  return -1;
 }return 0;}
void* hybris_open_kd_ctx_from_uri(const char* id,const char* uri,unsigned* epoch){(void)id;(void)uri;static int key;if(epoch)*epoch=1;return &key;}
int hybris_backend_decrypt(void* key,unsigned epoch,uint8_t* data,uint32_t length){(void)key;if(epoch!=1||length%16)return -1;for(uint32_t i=0;i<length;++i)data[i]^=0xaa;return 0;}
char* drm_lib_get_account(void){if(getenv("MYGO_FIXTURE_NO_TOKENS"))return NULL;return strdup("{\"storefront_id\":\"143465\",\"dev_token\":\"SYNTHETIC_DEVELOPER\",\"music_token\":\"SYNTHETIC_USER_TOKEN\"}");}
const char* hybris_last_error(void){return "test fixture";}
void drm_lib_shutdown(void){if(getenv("MYGO_FIXTURE_SHUTDOWN_HANG"))for(;;)sleep(1);}
__attribute__((destructor)) static void fixture_exit_cleanup(void){if(getenv("MYGO_FIXTURE_EXIT_HANG"))for(;;)sleep(1);}
