#pragma once
#include "native_auth.h"
struct NativeTokens {std::string storefront,developer,user;};
bool native_account_tokens(void* library,uintptr_t base,NativeShared context,NativeTokens& result);
bool save_native_tokens(const char* directory,const NativeTokens& tokens);
