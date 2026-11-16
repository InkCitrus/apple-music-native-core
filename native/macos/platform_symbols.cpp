#include <unordered_map>
#include <string>
#include <cstring>
#include <cstdlib>
#include <cstdio>
#include <cerrno>
#include <cstdint>
#include <unistd.h>
#include <pthread.h>
#include <poll.h>
#include <signal.h>
#include <sys/sysctl.h>
#include <sys/time.h>
#include <getopt.h>
#include <vector>
#include <dlfcn.h>
#include <mach/mach.h>
#include "../android-libc-shim/src/errno.h"
#include "file_lock_compat.h"
struct LinuxSysinfo {int64_t uptime;uint64_t loads[3],totalram,freeram,sharedram,bufferram,totalswap,freeswap;uint16_t procs,pad;uint64_t totalhigh,freehigh;uint32_t mem_unit;};
static int guest_sysinfo(LinuxSysinfo* p){if(!p){shim::bionic::set_errno(EFAULT);return -1;}memset(p,0,sizeof(*p));size_t n=8;sysctlbyname("hw.memsize",&p->totalram,&n,nullptr,0);vm_statistics64_data_t vm{};mach_msg_type_number_t count=HOST_VM_INFO64_COUNT;if(host_statistics64(mach_host_self(),HOST_VM_INFO64,(host_info64_t)&vm,&count)==KERN_SUCCESS)p->freeram=(vm.free_count+vm.inactive_count)*uint64_t(getpagesize());timeval boot{},now{};n=sizeof(boot);sysctlbyname("kern.boottime",&boot,&n,nullptr,0);gettimeofday(&now,nullptr);p->uptime=now.tv_sec-boot.tv_sec;double loads[3]{};getloadavg(loads,3);for(int i=0;i<3;i++)p->loads[i]=loads[i]*65536;p->mem_unit=1;return 0;}
static long guest_syscall(long number,long a,long b,long c,long,long,long){if(number==178){uint64_t tid;pthread_threadid_np(nullptr,&tid);return long(tid);}if(number==278){arc4random_buf((void*)a,size_t(b));return b;}fprintf(stderr,"[core-probe] unsupported Android syscall: %ld\n",number);shim::bionic::set_errno(38);return -1;}
static int unsupported_fd(){shim::bionic::set_errno(38);return -1;}
static void* unsupported_map(){shim::bionic::set_errno(38);return (void*)-1;}
static int guest_eventfd_read(int fd,uint64_t* v){return read(fd,v,8)==8?0:-1;}
static int guest_eventfd_write(int fd,uint64_t v){return write(fd,&v,8)==8?0:-1;}
static int guest_ppoll(pollfd* f,nfds_t n,const timespec* timeout,const void* mask){if(mask){shim::bionic::set_errno(38);return -1;}int ms=timeout?int(timeout->tv_sec*1000+(timeout->tv_nsec+999999)/1000000):-1;int r=poll(f,n,ms);shim::bionic::update_errno();return r;}
static void* guest_memrchr(const void* p,int c,size_t n){auto b=(const unsigned char*)p;while(n)if(b[--n]==(unsigned char)c)return (void*)(b+n);return nullptr;}
struct Destructors {std::vector<std::pair<void(*)(void*),void*>> list;~Destructors(){while(!list.empty()){auto p=list.back();list.pop_back();p.first(p.second);}}};
static thread_local Destructors destructors;
static int guest_thread_atexit(void(*fn)(void*),void* p,void*){destructors.list.emplace_back(fn,p);return 0;}
static void add_extra_symbols(std::unordered_map<std::string,void*>&);
void add_platform_symbols(std::unordered_map<std::string,void*>& m){
 m["fcntl"]=(void*)android_fcntl;
 // libc-shim's Minecraft profile binds abs to an aborting placeholder.
 // CoreFoundation requires the real operation when constructing a timezone.
 m["abs"]=(void*)(int(*)(int))::abs;
 add_extra_symbols(m);m["sysinfo"]=(void*)guest_sysinfo;m["syscall"]=(void*)guest_syscall;m["eventfd_read"]=(void*)guest_eventfd_read;m["eventfd_write"]=(void*)guest_eventfd_write;m["timerfd_create"]=(void*)unsupported_fd;m["timerfd_settime"]=(void*)unsupported_fd;m["signalfd"]=(void*)unsupported_fd;m["mremap"]=(void*)unsupported_map;m["ppoll"]=(void*)guest_ppoll;m["memrchr"]=(void*)guest_memrchr;m["__cxa_thread_atexit_impl"]=(void*)guest_thread_atexit;
 for(const char* name:{"labs","llabs","getprogname","getentropy","mkdtemp","mktemp","getopt_long","optarg","optind","optopt","opterr","strlcat","sigsuspend"})if(void* p=dlsym(RTLD_DEFAULT,name))m[name]=p;
}
#include <pwd.h>
#undef NOFILE
#include "../android-libc-shim/src/pthreads.h"
#include "../android-libc-shim/src/dirent.h"
struct GuestPasswd {char* name;char* password;uint32_t uid,gid;char* gecos;char* dir;char* shell;};
static thread_local GuestPasswd guestPasswd;
static thread_local char passwdBuffer[8192];
static GuestPasswd* copyPasswd(passwd* p){if(!p)return nullptr;guestPasswd={p->pw_name,p->pw_passwd,p->pw_uid,p->pw_gid,p->pw_gecos,p->pw_dir,p->pw_shell};return &guestPasswd;}
static GuestPasswd* guest_getpwuid(uid_t id){passwd p{},*out=nullptr;int r=getpwuid_r(id,&p,passwdBuffer,sizeof(passwdBuffer),&out);if(r){shim::bionic::set_errno(r);return nullptr;}return copyPasswd(out);}
static GuestPasswd* guest_getpwnam(const char* name){passwd p{},*out=nullptr;int r=getpwnam_r(name,&p,passwdBuffer,sizeof(passwdBuffer),&out);if(r){shim::bionic::set_errno(r);return nullptr;}return copyPasswd(out);}
static int guest_readdir_r(shim::bionic::DIR* d,shim::bionic::dirent* entry,shim::bionic::dirent** result){errno=0;auto p=shim::readdir(d);if(p){memcpy(entry,p,sizeof(*entry));*result=entry;return 0;}*result=nullptr;return errno;}
static int guest_attr_scope(shim::bionic::pthread_attr_t*,int scope){return scope==0?0:ENOTSUP;}
static int guest_attr_policy(shim::bionic::pthread_attr_t* a,int* policy){if(!a||!policy)return EINVAL;*policy=int(a->sched_policy);return 0;}
static int guest_sigmask(int,const uint64_t*,uint64_t*){return ENOTSUP;}
static int guest_fadvise(int,off_t,off_t,int){return 0;}
static void discard_syslog(int,const char*,void*){}
static void add_extra_symbols(std::unordered_map<std::string,void*>& m){m["getpwuid"]=(void*)guest_getpwuid;m["getpwnam"]=(void*)guest_getpwnam;m["readdir_r"]=(void*)guest_readdir_r;m["pthread_attr_setscope"]=(void*)guest_attr_scope;m["pthread_attr_getschedpolicy"]=(void*)guest_attr_policy;m["pthread_sigmask"]=(void*)guest_sigmask;m["posix_fadvise"]=(void*)guest_fadvise;m["vsyslog"]=(void*)discard_syslog;}
