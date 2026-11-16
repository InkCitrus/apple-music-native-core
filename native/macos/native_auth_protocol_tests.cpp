#include <cstdio>
#include <cstring>
#include <cstddef>
extern "C" void* __loader_dlsym(void*,const char*,const void*){return nullptr;}
void crash_probe_stage(const char*){}
extern "C" int native_auth_code_test(int,char*,size_t);
int main(){char value[16]{};int ok=native_auth_code_test(2,value,sizeof(value));bool valid=ok&&strcmp(value,"001234")==0;memset(value,0,sizeof(value));printf("{\"testCodeReceivedCorrectly\":%s,\"accepted\":%s}\n",valid?"true":"false",ok?"true":"false");return ok?0:2;}
