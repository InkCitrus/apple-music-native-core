#include "android_syscall_bridge.h"
#include <mach/mach.h>
#include <mach/mach_vm.h>
#include <libkern/OSCacheControl.h>
#include <pthread.h>
#include <time.h>
#include <cstdio>
#include <cstring>
#include <atomic>
namespace {
// Translate Linux SVC instructions before they can enter the Darwin kernel.
// Only instruction locations recorded while loading ELF executable segments
// are handled. On-disk libraries and native DRM decisions remain intact.
constexpr uint32_t linuxSVC=0xd4000001,compatBRK=0xd4200000|(0x4d47u<<5);
uintptr_t locations[4096]{};size_t locationCount=0;
std::atomic<unsigned> calls{0};
bool registered(uintptr_t pc){for(size_t i=0;i<locationCount;++i)if(locations[i]==pc)return true;return false;}
bool copyOut(uintptr_t target,const void* data,size_t size){return mach_vm_write(mach_task_self(),target,reinterpret_cast<vm_offset_t>(data),mach_msg_type_number_t(size))==KERN_SUCCESS;}
}
bool prepare_android_syscalls(const char* elf,uintptr_t base){
 FILE* file=fopen(elf,"rb");if(!file)return false;unsigned char header[64]{};bool good=fread(header,1,64,file)==64&&memcmp(header,"\177ELF\2\1",6)==0;
 uint64_t ph=0;uint16_t step=0,count=0;if(good){memcpy(&ph,header+32,8);memcpy(&step,header+54,2);memcpy(&count,header+56,2);good=step>=56&&count<256;}
 if(good)for(unsigned i=0;i<count;++i){
  unsigned char p[56]{};if(fseek(file,long(ph+uint64_t(i)*step),SEEK_SET)||fread(p,1,56,file)!=56){good=false;break;}
  uint32_t type,flags;uint64_t address,size;memcpy(&type,p,4);memcpy(&flags,p+4,4);memcpy(&address,p+16,8);memcpy(&size,p+32,8);
  if(type!=1||!(flags&1))continue;
  auto first=base+address;auto end=first+size;
  pthread_jit_write_protect_np(0);
  for(uintptr_t pc=(first+3)&~uintptr_t(3);pc+4<=end;pc+=4){
   auto instruction=reinterpret_cast<uint32_t*>(pc);if(*instruction!=linuxSVC)continue;
   if(locationCount>=4096){good=false;break;}
   locations[locationCount++]=pc;*instruction=compatBRK;
  }
  sys_icache_invalidate(reinterpret_cast<void*>(first),size);pthread_jit_write_protect_np(1);
  if(!good)break;
 }
 fclose(file);return good;
}
int android_syscall_number(ucontext_t* context){auto& s=context->uc_mcontext->__ss;return registered(s.__pc)?int(s.__x[8]):-1;}
bool handle_android_syscall(ucontext_t* context){
 auto& s=context->uc_mcontext->__ss;if(!registered(s.__pc))return false;
 long result=0;switch(s.__x[8]){
 case 169:{
  // Linux AArch64 timeval contains two int64 values. Darwin's tv_usec is int32.
  if(s.__x[1])return false;
  timespec time{};if(clock_gettime(CLOCK_REALTIME,&time)){result=-5;break;}
  AndroidTimeval value{time.tv_sec,time.tv_nsec/1000};
  if(s.__x[0]&&!copyOut(s.__x[0],&value,sizeof(value)))result=-14;
  break;
 }
 default:return false;
 }
 calls.fetch_add(1,std::memory_order_relaxed);s.__x[0]=uint64_t(result);s.__pc+=4;return true;
}
unsigned android_syscall_count(){return calls.load(std::memory_order_relaxed);}
