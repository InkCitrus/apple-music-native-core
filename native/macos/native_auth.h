#pragma once
#include <cstdint>
#include <string>
struct NativeShared { void* object; void* control; };
struct AuthOutcome {
 bool accepted=false, codeRequested=false, codeSubmitted=false, cancelled=false;
 int responseType=-1, errorCode=0, challengeCount=0;
 int64_t externalErrorCode=0;
 std::string reason="unknown";
};
void native_auth_setup(void* library,uintptr_t base,const char* username,const char* password);
AuthOutcome native_auth_run(NativeShared context);
void native_auth_finish();
void native_auth_stage(const char* stage);
void native_auth_result(const AuthOutcome&,bool sessionReady,const char* stage,const char* directory);
