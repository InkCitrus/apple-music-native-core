#include "crash_probe.h"
#include "android_syscall_bridge.h"
#include <mach/mach.h>
#include <mach/mach_vm.h>
#include <sys/ucontext.h>
#include <signal.h>
#include <unistd.h>
#include <fcntl.h>
#include <dlfcn.h>
#include <dirent.h>
#include <cstdio>
#include <cstring>
#include <cstdint>
#include <string>
#include <atomic>
namespace linker {size_t get_library_base(void*);}
extern "C" void* __loader_dlopen(const char*,int,const void*);
namespace {
struct Module{uintptr_t base,end;char name[96];};
Module modules[40]{};size_t count=0;int output=-1;
std::atomic<const char*> stage{"initialization"};
static_assert(std::atomic<const char*>::is_always_lock_free);
void append(char* data,size_t& length,const char* text){size_t n=strlen(text);if(length+n<16000){memcpy(data+length,text,n);length+=n;}}
void address(char* data,size_t& length,uintptr_t pc){
 char buffer[512];
 for(size_t i=0;i<count;++i)if(pc>=modules[i].base&&pc<modules[i].end){snprintf(buffer,sizeof(buffer),"{\"module\":\"%s\",\"offset\":\"0x%llx\"}",modules[i].name,(unsigned long long)(pc-modules[i].base));append(data,length,buffer);return;}
 Dl_info info{};if(dladdr(reinterpret_cast<void*>(pc),&info)&&info.dli_fname&&info.dli_fbase){const char* name=strrchr(info.dli_fname,'/');name=name?name+1:info.dli_fname;bool safe=strlen(name)<128&&strspn(name,"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.+-")==strlen(name);snprintf(buffer,sizeof(buffer),"{\"module\":\"%s\",\"offset\":\"0x%llx\"}",safe?name:"host",(unsigned long long)(pc-reinterpret_cast<uintptr_t>(info.dli_fbase)));append(data,length,buffer);}else append(data,length,"{\"module\":\"unmapped\"}");
}
void handler(int signal,siginfo_t*,void* context){
 auto state=static_cast<ucontext_t*>(context);uintptr_t pc=state->uc_mcontext->__ss.__pc,frame=state->uc_mcontext->__ss.__fp;
 if(signal==SIGTRAP&&handle_android_syscall(state))return;
 char data[16384]{};size_t length=0;char head[240];snprintf(head,sizeof(head),"{\"event\":\"nativeCrash\",\"signal\":%d,\"stage\":\"%s\",\"androidSyscall\":%d,\"translatedSyscalls\":%u,\"frames\":[",signal,stage.load(std::memory_order_relaxed),android_syscall_number(state),android_syscall_count());append(data,length,head);address(data,length,pc);
 for(int i=0;i<30&&frame&&!(frame&7);++i){uintptr_t pair[2]{};mach_vm_size_t copied=0;if(mach_vm_read_overwrite(mach_task_self(),frame,sizeof(pair),reinterpret_cast<mach_vm_address_t>(pair),&copied)!=KERN_SUCCESS||copied!=sizeof(pair))break;
  append(data,length,",");address(data,length,pair[1]);if(pair[0]<=frame||pair[0]-frame>4*1024*1024)break;frame=pair[0];}
 append(data,length,"],\"credentialsLogged\":false}\n");if(output>=0)write(output,data,length);write(STDOUT_FILENO,data,length);_exit(128+signal);
}
uint64_t extent(const std::string& path){FILE* f=fopen(path.c_str(),"rb");if(!f)return 0;unsigned char h[64]{};uint64_t size=0;if(fread(h,1,64,f)==64&&memcmp(h,"\177ELF\2\1",6)==0){uint64_t offset;uint16_t step,n;memcpy(&offset,h+32,8);memcpy(&step,h+54,2);memcpy(&n,h+56,2);if(step>=56&&n<256)for(unsigned i=0;i<n;++i){unsigned char p[56]{};if(fseek(f,long(offset+uint64_t(i)*step),SEEK_SET)||fread(p,1,56,f)!=56)break;uint32_t type;uint64_t addr,bytes;memcpy(&type,p,4);memcpy(&addr,p+16,8);memcpy(&bytes,p+40,8);if(type==1&&addr+bytes>size)size=addr+bytes;}}fclose(f);return size;}
}
void crash_probe_stage(const char* value){stage.store(value,std::memory_order_relaxed);}
void register_crash_probe_module(const char* path,void* handle){
 if(count>=40||!handle)return;const char* name=strrchr(path,'/');name=name?name+1:path;if(strlen(name)>=sizeof(modules[0].name))return;uintptr_t base=linker::get_library_base(handle);uint64_t size=extent(path);if(!base||!size)return;modules[count].base=base;modules[count].end=base+size;strcpy(modules[count].name,name);++count;if(!prepare_android_syscalls(path,base))_exit(2);
}
void install_crash_probe(const char* directory,const char* runtime){
 std::string path=std::string(directory)+"/native-crash.json";output=open(path.c_str(),O_WRONLY|O_CREAT|O_TRUNC|O_CLOEXEC,0600);
 count=0;if(DIR* d=opendir(runtime)){while(auto entry=readdir(d)){std::string name=entry->d_name;if(name!="wrapper-main"&&(name.size()<3||name.substr(name.size()-3)!=".so"))continue;std::string file=std::string(runtime)+"/"+name;void* handle=__loader_dlopen(file.c_str(),RTLD_NOW|RTLD_NOLOAD,nullptr);register_crash_probe_module(file.c_str(),handle);}closedir(d);}
 struct sigaction action{};action.sa_sigaction=handler;sigemptyset(&action.sa_mask);for(int signal:{SIGSEGV,SIGBUS,SIGABRT,SIGSYS,SIGILL,SIGFPE,SIGTRAP}){action.sa_flags=SA_SIGINFO|(signal==SIGTRAP?0:SA_RESETHAND);sigaction(signal,&action,nullptr);}
}
