#include <unordered_map>
#include <string>
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cmath>
#include <cctype>
#include <dlfcn.h>
#include <sys/file.h>
#include <sys/stat.h>
#include <signal.h>
#include <sys/ucontext.h>
#include <unistd.h>
#include <iostream>
#include <fstream>
#include <vector>
#include "arm-reference/cJSON.h"
#include "native_auth.h"
#include "crash_probe.h"
#include "android_syscall_bridge.h"
#include "native_tokens.h"
#include <libc_shim.h>
#include "z_symbols.inc"
#include "../android-libc-shim/src/file_misc.h"
extern "C" int guest_vsnprintf();extern "C" int guest_vsprintf();extern "C" int guest_vprintf();extern "C" int guest_snprintf();extern "C" int guest_sprintf();extern "C" int guest_printf();extern "C" int guest___snprintf_chk();extern "C" int guest___vsnprintf_chk();
extern "C" int guest_vsscanf();extern "C" int guest_sscanf();extern "C" int guest_fprintf();extern "C" int guest_vfprintf();extern "C" int guest_asprintf();extern "C" int guest_vasprintf();extern "C" int guest_dprintf();extern "C" int guest___vsprintf_chk();
extern "C" int guest_swprintf();
void add_platform_symbols(std::unordered_map<std::string,void*>&);
extern "C" int guest_fscanf();extern "C" int guest_scanf();extern "C" int guest_vfscanf();
namespace linker {size_t get_library_base(void*); void init(); void* load_library(const char*,const std::unordered_map<std::string,void*>&);void update_LD_LIBRARY_PATH(const char*);void* dlopen(const char*,int);void* dlsym(void*,const char*);char* dlerror();}
// The public API's inline forwarding functions are emitted locally.
extern "C" void* __loader_dlopen(const char*,int,const void*);
extern "C" void* __loader_dlsym(void*,const char*,const void*);
extern "C" void native_call_sret1(void*,void*,void*);
extern "C" char* __loader_dlerror();
extern "C" void __loader_android_update_LD_LIBRARY_PATH(const char*);
static void no_finalize(void*){}
static int no_atexit(void(*)(void*),void*,void*){return 0;}
static int fin(double x){return std::isfinite(x);}static int finl(long double x){return std::isfinite(x);}static int finf(float x){return std::isfinite(x);}
static int nanf_(float x){return std::isnan(x);}static int nanl_(long double x){return std::isnan(x);}static int norm(double x){return std::isnormal(x);}static int norml(long double x){return std::isnormal(x);}
static void clear_memory(void* p,size_t n){volatile unsigned char* b=(volatile unsigned char*)p;while(n--)*b++=0;}
static void entry_not_supported(){std::abort();}
static void use_host_dns(unsigned,const char**,int,const char*){}
static int android_log_sink(){return 0;}
static void assertion(const char*,int,const char*,const char*){std::abort();}
static int storage_check(void* library,const char* directory){
 std::string name=std::string(directory)+"/.storage-check-XXXXXX";std::vector<char> file(name.begin(),name.end());file.push_back(0);
 int fd=mkstemp(file.data());if(fd<0)return -1;close(fd);
 auto openDB=(int(*)(const char*,void**))__loader_dlsym(library,"sqlite3_open",nullptr);
 auto exec=(int(*)(void*,const char*,void*,void*,char**))__loader_dlsym(library,"sqlite3_exec",nullptr);
 auto extended=(int(*)(void*))__loader_dlsym(library,"sqlite3_extended_errcode",nullptr);
 auto closeDB=(int(*)(void*))__loader_dlsym(library,"sqlite3_close",nullptr);
 int code=-1;void* db=nullptr;
 if(openDB&&exec&&extended&&closeDB){code=openDB(file.data(),&db);if(code==0){code=exec(db,"CREATE TABLE storage_probe(value INTEGER); BEGIN IMMEDIATE; INSERT INTO storage_probe VALUES(42); COMMIT;",nullptr,nullptr,nullptr);if(code)code=extended(db);}if(db)closeDB(db);}
 unlink(file.data());unlink((std::string(file.data())+"-journal").c_str());return code;
}
int main(int argc,char**argv){if(argc<2)return 2;linker::init();std::unordered_map<std::string,void*> libc={
 {"malloc",(void*)malloc},{"free",(void*)free},{"memset",(void*)memset},{"memmove",(void*)memmove},
 {"__cxa_finalize",(void*)no_finalize},{"__cxa_atexit",(void*)no_atexit},{"__assert2",(void*)assertion},
 {"__isfinite",(void*)fin},{"__isfinitef",(void*)finf},{"__isfinitel",(void*)finl},{"__isnanf",(void*)nanf_},{"__isnanl",(void*)nanl_},
 {"isnanf",(void*)nanf_},{"__isnormal",(void*)norm},{"__isnormall",(void*)norml},{"ldexp",(void*)(double(*)(double,int))ldexp},{"isxdigit",(void*)isxdigit}};
 for (auto &s : shim::get_shimmed_symbols()) libc[s.name]=s.value;
 libc["vsnprintf"]=(void*)guest_vsnprintf;libc["vsprintf"]=(void*)guest_vsprintf;libc["vprintf"]=(void*)guest_vprintf;libc["snprintf"]=(void*)guest_snprintf;libc["sprintf"]=(void*)guest_sprintf;libc["printf"]=(void*)guest_printf;libc["__snprintf_chk"]=(void*)guest___snprintf_chk;libc["__vsnprintf_chk"]=(void*)guest___vsnprintf_chk;
 libc["__libc_init"]=(void*)entry_not_supported;libc["_resolv_set_nameservers_for_net"]=(void*)use_host_dns;libc["scanf"]=(void*)guest_scanf;libc["fscanf"]=(void*)guest_fscanf;libc["vfscanf"]=(void*)guest_vfscanf;
 libc["swprintf"]=(void*)guest_swprintf;libc["sscanf"]=(void*)guest_sscanf;libc["vsscanf"]=(void*)guest_vsscanf;libc["fprintf"]=(void*)guest_fprintf;libc["vfprintf"]=(void*)guest_vfprintf;libc["asprintf"]=(void*)guest_asprintf;libc["vasprintf"]=(void*)guest_vasprintf;libc["dprintf"]=(void*)guest_dprintf;libc["__vsprintf_chk"]=(void*)guest___vsprintf_chk;
 add_platform_symbols(libc);
 libc["open"]=(void*)shim::open_3; libc["flock"]=(void*)::flock;
 if(argc>2 && std::string(argv[2])=="--libc-symbols"){for(auto& s:libc)puts(s.first.c_str());return 0;}
 linker::load_library("libc.so",libc);
 void* z=::dlopen("/usr/lib/libz.1.dylib",RTLD_NOW); std::unordered_map<std::string,void*> zm;for(auto n=guestZSymbols;*n;++n){if(void* p=::dlsym(z,*n))zm[*n]=p;} linker::load_library("libz.so",zm);
 std::unordered_map<std::string,void*> cpp;for(const char* n:{"__cxa_guard_acquire","__cxa_guard_release","__cxa_guard_abort","__cxa_pure_virtual"}){if(void* p=::dlsym(RTLD_DEFAULT,n))cpp[n]=p;}linker::load_library("libstdc++.so",cpp);
 std::unordered_map<std::string,void*> logs;for(const char* n:{"__android_log_print","__android_log_write","__android_log_vprint","__android_log_buf_print","__android_log_buf_write","__android_log_assert"})logs[n]=(void*)android_log_sink;linker::load_library("liblog.so",logs);
 linker::load_library("libandroid.so",{});linker::load_library("libOpenSLES.so",{});__loader_android_update_LD_LIBRARY_PATH(argv[1]);
 std::string path=std::string(argv[1])+"/libm.so";void* m=__loader_dlopen(path.c_str(),RTLD_NOW,nullptr);
 if(!m){std::fprintf(stderr,"%s\n",__loader_dlerror());return 1;}
 auto sinfn=(double(*)(double))__loader_dlsym(m,"sin",nullptr);
 double v=sinfn(0.5);std::printf("{\"androidARM64LibraryLoaded\":true,\"nativeFunctionCorrect\":%s,\"result\":%.16f}\n",std::abs(v-std::sin(0.5))<1e-12?"true":"false",v);
 path=std::string(argv[1])+"/libBlocksRuntime.so";void* b=__loader_dlopen(path.c_str(),RTLD_NOW,nullptr);
 if(!b){std::fprintf(stderr,"%s\n",__loader_dlerror());return 1;}
 auto copy=(void*(*)(const void*))__loader_dlsym(b,"_Block_copy",nullptr);
 std::printf("{\"blocksRuntimeLoaded\":true,\"nullBlockCopyCorrect\":%s}\n",copy(nullptr)==nullptr?"true":"false");
 if(argc>2){path=std::string(argv[1])+"/"+argv[2];void* core=__loader_dlopen(path.c_str(),RTLD_NOW,nullptr);if(!core){std::fprintf(stderr,"%s\n",__loader_dlerror());return 1;}std::printf("{\"targetLibraryLoaded\":true}\n");
 if(argc>4 && std::string(argv[3])=="--crash-test"){install_crash_probe(argv[4],argv[1]);crash_probe_stage("native_context");raise(SIGSEGV);return 1;}
 if(argc>5 && std::string(argv[3])=="--time-syscall-test"){
 void* test=__loader_dlopen(argv[5],RTLD_NOW,nullptr);if(!test)return 1;install_crash_probe(argv[4],argv[1]);register_crash_probe_module(argv[5],test);
 auto get=(long(*)(AndroidTimeval*))__loader_dlsym(test,"android_test_gettimeofday",nullptr);if(!get||reinterpret_cast<uint32_t*>(get)[2]!=(0xd4200000|(0x4d47u<<5)))return 1;
 bool good=true;for(int i=0;i<32;++i){struct{uint64_t before;AndroidTimeval value;uint64_t after;}guard{0x123456789abcdef0,{INT64_MIN,INT64_MIN},0xfedcba9876543210};time_t now=time(nullptr);long r=get(&guard.value);good=good&&r==0&&llabs(guard.value.seconds-now)<3&&guard.value.microseconds>=0&&guard.value.microseconds<1000000&&guard.before==0x123456789abcdef0&&guard.after==0xfedcba9876543210;}
 bool nullAccepted=get(nullptr)==0;bool badPointerRejected=get(reinterpret_cast<AndroidTimeval*>(1))==-14;
 std::printf("{\"androidTimeSyscallPassed\":%s,\"nullAccepted\":%s,\"badPointerRejected\":%s,\"translatedCalls\":%u}\n",good?"true":"false",nullAccepted?"true":"false",badPointerRejected?"true":"false",android_syscall_count());return good&&nullAccepted&&badPointerRejected?0:1;
 }
 if(argc>4 && std::string(argv[3])=="--sqlite-test"){
 void* mp=__loader_dlopen("libmediaplatform.so",RTLD_NOW,nullptr);if(!mp)return 1;
 auto openDB=(int(*)(const char*,void**))__loader_dlsym(mp,"sqlite3_open",nullptr);
 auto exec=(int(*)(void*,const char*,void*,void*,char**))__loader_dlsym(mp,"sqlite3_exec",nullptr);
 auto extended=(int(*)(void*))__loader_dlsym(mp,"sqlite3_extended_errcode",nullptr);
 auto closeDB=(int(*)(void*))__loader_dlsym(mp,"sqlite3_close",nullptr);
 void* db=nullptr;int opened=openDB(argv[4],&db);int written=opened==0?exec(db,"CREATE TABLE IF NOT EXISTS compatibility_probe(value INTEGER); INSERT INTO compatibility_probe VALUES(42);",nullptr,nullptr,nullptr):-1;
 int code=db?extended(db):-1;if(db)closeDB(db);
 std::printf("{\"sqliteProbe\":true,\"openCode\":%d,\"writeCode\":%d,\"extendedCode\":%d,\"passed\":%s}\n",opened,written,code,opened==0&&written==0?"true":"false");return opened==0&&written==0?0:1;
 }
 if(argc>3 && (std::string(argv[3])=="--wrapper-help" || std::string(argv[3])=="--wrapper-init" || std::string(argv[3])=="--context-init" || std::string(argv[3])=="--authenticate" || std::string(argv[3])=="--resume-session" || std::string(argv[3])=="--license-probe" || std::string(argv[3])=="--decrypt-fd")){
 auto entry=(int(*)(int,char**))(linker::get_library_base(core)+66256);
 char name[]="mygo-core-probe";char help[]="--help";
 if(std::string(argv[3])=="--wrapper-help"){char* args[]={name,help,nullptr};return entry(2,args);}
 if(argc<5)return 2;
 install_crash_probe(argv[4],argv[1]);char baseFlag[]="--base-dir";char fileFlag[]="--code-from-file";char* args[]={name,baseFlag,argv[4],fileFlag,nullptr};
 if(std::string(argv[3])=="--context-init" || std::string(argv[3])=="--authenticate" || std::string(argv[3])=="--resume-session" || std::string(argv[3])=="--license-probe" || std::string(argv[3])=="--decrypt-fd"){
 void* mp=__loader_dlopen("libmediaplatform.so",RTLD_NOW,nullptr);int storage=mp?storage_check(mp,argv[4]):-1;
 std::printf("{\"event\":\"storageProbe\",\"nativeStorageReady\":%s,\"sqliteCode\":%d}\n",storage==0?"true":"false",storage);std::fflush(stdout);
 if(storage!=0){AuthOutcome failure;failure.reason="storage_error";failure.errorCode=storage;native_auth_result(failure,false,"native_storage",argv[4]);return 1;}
 auto base=linker::get_library_base(core);
 auto parse=(int(*)(int,char**,void*))(base+0x11ca0);
 if(parse(4,args,(void*)(base+0x22d68))!=0)return 1;
 std::string identity;std::string idPath=std::string(argv[4])+"/device-id";{std::ifstream f(idPath);f>>identity;}
 if(identity.size()!=16 || identity[0]=='0' || identity.find_first_not_of("0123456789abcdef")!=std::string::npos){unsigned char bytes[8];do{arc4random_buf(bytes,8);}while(bytes[0]<16);char hex[17];for(int i=0;i<8;i++)std::snprintf(hex+2*i,3,"%02x",bytes[i]);identity=hex;std::ofstream(idPath)<<identity;chmod(idPath.c_str(),0600);}
 const char* device[]={"Music","4.9","Android","10","MyGo Desktop","7663313","en-US","en-US",identity.c_str()};
 std::memcpy((void*)(base+0x22e88),device,sizeof(device));
 auto init=(void(*)())(base+0x10a90);init();
 struct Context{void* obj;void* control;};
 auto create=(Context(*)())(base+0x10bbc);Context context=create();std::memcpy((void*)(base+0x22d30),&context,sizeof(context));
 std::printf("{\"nativeContextCreated\":%s,\"signedIn\":false,\"losslessVerified\":false}\n",context.obj&&context.control?"true":"false");std::fflush(stdout);if(!context.obj||!context.control)return 1;
 NativeShared cachedAccount{};void* accountFn=__loader_dlsym(core,"_ZNK17storeservicescore14RequestContext7accountEv",nullptr);
 if(accountFn)native_call_sret1(accountFn,&cachedAccount,context.obj);
 std::printf("{\"event\":\"sessionMetadata\",\"cachedAccountPresent\":%s}\n",cachedAccount.object?"true":"false");std::fflush(stdout);
 if(std::string(argv[3])=="--context-init")return 0;
 std::vector<char> username(320),password(1024);AuthOutcome outcome;
 if(std::string(argv[3])=="--resume-session" || std::string(argv[3])=="--license-probe" || std::string(argv[3])=="--decrypt-fd"){
 if(!cachedAccount.object){outcome.reason="no_cached_account";native_auth_result(outcome,false,"account_authentication",argv[4]);return 1;}
 // Reuse the account already accepted by the native runtime; never offer old
 // credentials if the runtime asks to authenticate again during resumption.
 native_auth_setup(core,base,"","");native_auth_finish();outcome.accepted=true;outcome.reason="cached_session";
 }else{
 char line[4096]{};if(!std::cin.getline(line,sizeof(line)))return 1;
 cJSON* command=cJSON_Parse(line);if(!command)return 1;
 auto user=cJSON_GetObjectItemCaseSensitive(command,"username");auto pass=cJSON_GetObjectItemCaseSensitive(command,"password");
 if(!cJSON_IsString(user)||!cJSON_IsString(pass)||strlen(user->valuestring)==0||strlen(pass->valuestring)==0||strlen(user->valuestring)>319||strlen(pass->valuestring)>700){cJSON_Delete(command);return 1;}
 std::strcpy(username.data(),user->valuestring);std::strcpy(password.data(),pass->valuestring);clear_memory(line,sizeof(line));clear_memory(pass->valuestring,strlen(pass->valuestring));clear_memory(user->valuestring,strlen(user->valuestring));cJSON_Delete(command);
 *(char**)(base+0x22ed0)=username.data();*(char**)(base+0x22ed8)=password.data();
 native_auth_setup(core,base,username.data(),password.data());
 outcome=native_auth_run({context.obj,context.control});
 }
 bool ok=outcome.accepted;const char* lastStage="account_authentication";

 if(ok){
 auto ctor=(void(*)(void*,void*,void*))__loader_dlsym(core,"_ZN22SVPlaybackLeaseManagerC2ERKNSt6__ndk18functionIFvRKiEEERKNS1_IFvRKNS0_10shared_ptrIN17storeservicescore19StoreErrorConditionEEEEEE",nullptr);
 auto refresh=(void(*)(void*,uint8_t*))__loader_dlsym(core,"_ZN22SVPlaybackLeaseManager25refreshLeaseAutomaticallyERKb",nullptr);
 auto lease=(void(*)(void*,uint8_t*))__loader_dlsym(core,"_ZN22SVPlaybackLeaseManager12requestLeaseERKb",nullptr);uint8_t yes=1;
 lastStage="session_lease";native_auth_stage(lastStage);
 if(!ctor||!refresh||!lease){native_auth_result(outcome,false,"native_symbols",argv[4]);return 1;}ctor((void*)(base+0x22d40),(void*)(base+0x22ff0),(void*)(base+0x23020));refresh((void*)(base+0x22d40),&yes);lease((void*)(base+0x22d40),&yes);
 std::printf("{\"event\":\"leaseReturned\",\"translatedSyscalls\":%u}\n",android_syscall_count());std::fflush(stdout);
 *(void**)(base+0x22ee0)=((void*(*)())(base+0xe770))();
 lastStage="account_tokens";native_auth_stage(lastStage);
 NativeTokens tokens;ok=native_account_tokens(core,base,{context.obj,context.control},tokens)&&save_native_tokens(argv[4],tokens);
 }
 clear_memory(password.data(),password.size());clear_memory(username.data(),username.size());
 native_auth_finish();native_auth_result(outcome,ok,lastStage,argv[4]);
 if(ok && std::string(argv[3])=="--license-probe"){
  char line[8192]{};if(!std::cin.getline(line,sizeof(line)))return 1;auto plan=cJSON_Parse(line);if(!plan)return 1;
  auto id=cJSON_GetObjectItemCaseSensitive(plan,"trackID"),uris=cJSON_GetObjectItemCaseSensitive(plan,"uris");if(!cJSON_IsString(id)||strlen(id->valuestring)>20||strspn(id->valuestring,"0123456789")!=strlen(id->valuestring)||!cJSON_IsArray(uris)||cJSON_GetArraySize(uris)>4){cJSON_Delete(plan);return 1;}
  auto open=(void*(*)(const char*,const char*))(base+61628);bool all=true;
  native_auth_stage("license_context");for(int i=0;i<cJSON_GetArraySize(uris);++i){auto uri=cJSON_GetArrayItem(uris,i);if(!cJSON_IsString(uri)||strncmp(uri->valuestring,"skd://itunes.apple.com/",23)){all=false;break;}bool prefetch=strstr(uri->valuestring,"P000000000")!=nullptr;void* key=open(prefetch?"0":id->valuestring,uri->valuestring);all=all&&key;std::printf("{\"event\":\"licenseContext\",\"prefetch\":%s,\"contextCreated\":%s,\"translatedSyscalls\":%u}\n",prefetch?"true":"false",key?"true":"false",android_syscall_count());std::fflush(stdout);if(!key)break;}
  cJSON_Delete(plan);return all?0:1;
 }
 if(ok && std::string(argv[3])=="--decrypt-fd"){
  int fd=argc>5?atoi(argv[5]):-1;if(fd<3||fd>64)return 1;native_auth_stage("audio_decryption");std::printf("{\"event\":\"decryptPipeReady\",\"authenticated\":true}\n");std::fflush(stdout);((void(*)(int))(base+0xede0))(fd);close(fd);return 0;
 }
 return ok?0:1;
 }
 return entry(4,args);
 }
 if(argc>3 && std::string(argv[3])=="--cf-string"){
 void* cf=__loader_dlopen("libCoreFoundation.so",RTLD_NOW,nullptr);
 auto create=(void*(*)(void*,const char*,uint32_t))__loader_dlsym(cf,"CFStringCreateWithCString",nullptr);
 auto length=(long(*)(void*))__loader_dlsym(cf,"CFStringGetLength",nullptr);
 auto release=(void(*)(void*))__loader_dlsym(cf,"CFRelease",nullptr);
 void* str=create(nullptr,"MyGo 原生核心",0x08000100);
 bool ok=str&&length(str)==9;if(str)release(str);
 std::printf("{\"coreFoundationStringVerified\":%s}\n",ok?"true":"false");if(!ok)return 1;
 }
 if(argc>3 && std::string(argv[3])=="--cf-timezone"){
 void* cf=__loader_dlopen("libCoreFoundation.so",RTLD_NOW,nullptr);
 auto create=(void*(*)(void*,double))__loader_dlsym(cf,"CFTimeZoneCreateWithTimeIntervalFromGMT",nullptr);
 auto offset=(double(*)(void*,double))__loader_dlsym(cf,"CFTimeZoneGetSecondsFromGMT",nullptr);
 auto release=(void(*)(void*))__loader_dlsym(cf,"CFRelease",nullptr);
 bool ok=true;for(double expected:{-28800.,0.,28800.}){void* zone=create(nullptr,expected);bool matched=zone&&offset(zone,0)==expected;if(zone)release(zone);ok=ok&&matched;}
 std::printf("{\"coreFoundationTimeZonesVerified\":%s}\n",ok?"true":"false");if(!ok)return 1;
 }}
 return 0;}
