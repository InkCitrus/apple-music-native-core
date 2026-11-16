/* Loads only a synthetic ELF through Q. Never loads Apple or account data. */
#include <windows.h>
#include <io.h>
#include <cstdio>
#include <cstring>
using Open=void*(*)(const char*,int);
using Symbol=void*(*)(void*,const char*);
using Init=void(*)(int,void*(*)(const char*,const char*),int);
typedef int(__attribute__((sysv_abi)) *Probe)();
static Probe probe=nullptr,control=nullptr;static bool worker_passed=false;
static DWORD WINAPI worker(void*){worker_passed=control()==42&&probe()==42;return 0;}
int main(int argc,char**argv){
 FILE* output=_fdopen(_dup(1),"w");if(!output)return 2;
 SetErrorMode(SEM_FAILCRITICALERRORS|SEM_NOGPFAULTERRORBOX);
 freopen("NUL","w",stdout);freopen("NUL","w",stderr);
 int status=1;bool main_passed=false,loaded=false;
 if(argc==4){
  SetDllDirectoryA(argv[1]);HMODULE q=LoadLibraryA(argv[2]);
  if(q){
   auto init=reinterpret_cast<Init>(GetProcAddress(q,"android_linker_init"));
   auto open=reinterpret_cast<Open>(GetProcAddress(q,"android_dlopen"));
   auto symbol=reinterpret_cast<Symbol>(GetProcAddress(q,"android_dlsym"));
   if(init&&open&&symbol){init(29,nullptr,0);void* guest=open(argv[3],2);loaded=guest!=nullptr;
    if(guest){probe=reinterpret_cast<Probe>(symbol(guest,"mygo_guest_tls_probe"));control=reinterpret_cast<Probe>(symbol(guest,"mygo_guest_tls_control"));}
    if(probe&&control&&control()==42){
     fprintf(output,"{\"event\":\"guestTLSControlReady\",\"passed\":true,\"nativeSDKUsed\":false,\"credentialsUsed\":false}\n");fflush(output);
     main_passed=probe()==42;HANDLE thread=CreateThread(nullptr,0,worker,nullptr,0,nullptr);
     if(thread){if(WaitForSingleObject(thread,10000)==WAIT_OBJECT_0&&main_passed&&worker_passed)status=0;CloseHandle(thread);}
    }
   }
  }
 }
 fprintf(output,"{\"event\":\"guestTLSFixtureCompleted\",\"guestLoaded\":%s,\"mainThreadPassed\":%s,\"workerThreadPassed\":%s,\"passed\":%s,\"nativeSDKUsed\":false,\"credentialsUsed\":false}\n",loaded?"true":"false",main_passed?"true":"false",worker_passed?"true":"false",status==0?"true":"false");fflush(output);
 // No SDK/session state; bound exit of this owned synthetic test process.
 TerminateProcess(GetCurrentProcess(),status);return status;
}
