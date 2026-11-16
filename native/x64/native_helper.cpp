#include "../../third_party/cJSON.h"
#include "../../third_party/drm_types.h"
#include "../../third_party/adam-drm/native/mygo_diagnostics.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <cstdint>
#include <string>
#include <vector>
#include <filesystem>
#include <fstream>
#include <iostream>
#include <mutex>
#include <random>
#include <thread>
#include <condition_variable>
#include <chrono>
#include <memory>
#ifdef _WIN32
#include <windows.h>
#include <io.h>
#define dup _dup
#define fdopen _fdopen
#else
#include <dlfcn.h>
#include <unistd.h>
#include <sys/stat.h>
#endif
#include "crash_diagnostics.h"
namespace fs=std::filesystem;
namespace {
FILE* output=nullptr;std::mutex outputMutex;bool authenticating=false;int challenge=0;
void emit(cJSON* value){std::lock_guard<std::mutex> lock(outputMutex);char* line=cJSON_PrintUnformatted(value);if(line){fputs(line,output);fputc('\n',output);fflush(output);free(line);}cJSON_Delete(value);}
cJSON* event(const char* name){auto v=cJSON_CreateObject();cJSON_AddStringToObject(v,"event",name);return v;}
void wipe(std::string& s){volatile char* p=s.empty()?nullptr:&s[0];for(size_t i=0;i<s.size();++i)p[i]=0;s.clear();}
void eraseJSON(cJSON* value){if(!value)return;if(cJSON_IsString(value)&&value->valuestring){volatile char* p=value->valuestring;for(size_t i=0,n=strlen(value->valuestring);i<n;++i)p[i]=0;}for(cJSON* c=value->child;c;c=c->next)eraseJSON(c);}
std::string stringField(cJSON* value,const char* key){auto v=cJSON_GetObjectItemCaseSensitive(value,key);return cJSON_IsString(v)?v->valuestring:"";}
cJSON* input(size_t limit){std::string line;if(!std::getline(std::cin,line)||line.size()>limit){wipe(line);return nullptr;}auto v=cJSON_ParseWithLength(line.data(),line.size());wipe(line);return v;}
void quiet(){output=fdopen(dup(1),"w");if(!output)std::exit(2);
#ifdef _WIN32
 SetErrorMode(SEM_FAILCRITICALERRORS|SEM_NOGPFAULTERRORBOX);
 HANDLE nullHandle=CreateFileW(L"NUL",GENERIC_WRITE,FILE_SHARE_READ|FILE_SHARE_WRITE,nullptr,OPEN_EXISTING,FILE_ATTRIBUTE_NORMAL,nullptr);
 if(nullHandle!=INVALID_HANDLE_VALUE){SetStdHandle(STD_OUTPUT_HANDLE,nullHandle);SetStdHandle(STD_ERROR_HANDLE,nullHandle);}
 freopen("NUL","w",stdout);freopen("NUL","w",stderr);
#else
 freopen("/dev/null","w",stdout);freopen("/dev/null","w",stderr);
#endif
}
void failure(const char* stage,const char* reason,int code=-1){auto v=event("failure");cJSON_AddStringToObject(v,"stage",stage);cJSON_AddStringToObject(v,"reason",reason);cJSON_AddNumberToObject(v,"code",code);cJSON_AddBoolToObject(v,"credentialsLogged",false);emit(v);}
void diagnostic(const char* kind,int code){if(!kind)return;for(const char* allowed:{"guard_abort","guard_cpp_exception","login_response_type","login_error_code","login_empty_response","credentials_requested","two_factor_requested","credentials_cancelled","required_symbol_hash"})if(strcmp(kind,allowed)==0){auto e=event("nativeDiagnostic");cJSON_AddStringToObject(e,"kind",allowed);cJSON_AddNumberToObject(e,"code",code);emit(e);return;}}
void errorDetail(const mygo_error_detail* detail){
 static_assert(sizeof(mygo_error_detail)==32,"diagnostic ABI");
 if(!detail||detail->abi_version!=1||detail->depth>2||detail->condition_kind>2||(detail->flags&~63u))return;
 auto e=event("nativeErrorDetail");cJSON_AddNumberToObject(e,"abiVersion",1);cJSON_AddNumberToObject(e,"depth",detail->depth);
 cJSON_AddStringToObject(e,"condition",detail->condition_kind==1?"store_services":detail->condition_kind==2?"media_platform":"unknown");
 cJSON_AddBoolToObject(e,"categoryVerified",detail->flags&MYGO_ERROR_CATEGORY_VERIFIED);
 cJSON_AddBoolToObject(e,"nestedConditionPresent",detail->flags&MYGO_ERROR_NESTED);
 cJSON_AddBoolToObject(e,"truncated",detail->flags&MYGO_ERROR_TRUNCATED);
 cJSON_AddBoolToObject(e,"getterMissing",detail->flags&MYGO_ERROR_GETTER_MISSING);
 if(detail->flags&MYGO_ERROR_CODE)cJSON_AddNumberToObject(e,"errorCode",detail->code);
 // cJSON numbers are doubles. Keep every signed 64-bit error code exactly.
 if(detail->flags&MYGO_ERROR_EXTERNAL)cJSON_AddStringToObject(e,"externalErrorCode",std::to_string(detail->external_code).c_str());
 emit(e);
}
std::string classify(const char* raw){std::string s=raw?raw:"";for(auto& c:s)c=char(std::tolower(static_cast<unsigned char>(c)));if(s.find("locked")!=std::string::npos||s.find("disabled")!=std::string::npos)return "account_locked";if(s.find("verification")!=std::string::npos)return "verification_failed";if(s.find("network")!=std::string::npos||s.find("connect")!=std::string::npos)return "network_error";return "native_initialization_failed";}
void auth(const char* type,char* buffer,int capacity,void*){
 if(capacity<=0)return;memset(buffer,0,size_t(capacity));if(!authenticating||!type||strcmp(type,"2fa"))return;
 int ticket=++challenge;if(ticket>3)return;auto e=event("twoFactorRequired");cJSON_AddNumberToObject(e,"challenge",ticket);cJSON_AddNumberToObject(e,"timeoutSeconds",180);emit(e);
 while(auto v=input(4096)){auto t=cJSON_GetObjectItemCaseSensitive(v,"challenge");auto code=stringField(v,"code");bool ok=stringField(v,"method")=="verify-code"&&cJSON_IsNumber(t)&&t->valueint==ticket&&code.size()==6&&code.find_first_not_of("0123456789")==std::string::npos;
  if(ok&&capacity>=7)memcpy(buffer,code.data(),6);wipe(code);eraseJSON(v);cJSON_Delete(v);if(ok){auto sent=event("twoFactorSubmitted");cJSON_AddNumberToObject(sent,"challenge",ticket);emit(sent);return;}}
}
void state(const char* name,void*){if(!name)return;
 for(const char* allowed:{"PHASE_WRAPPER_SYMBOLS","PHASE_DEVICE_CONFIG","PHASE_DEVICE_STRINGS","PHASE_FOOTHILL_CONFIG","PHASE_DEVICE_GUID","PHASE_DEVICE_GUID_CONFIG","PHASE_CALLBACKS","PHASE_REQUEST_CONTEXT","PHASE_CONTEXT_PATH","PHASE_CONTEXT_CREATE","PHASE_CONTEXT_CONFIG_CREATE","PHASE_CONTEXT_CALLBACKS","PHASE_CONTEXT_MANAGER","PHASE_CONTEXT_INIT","PHASE_CONTEXT_FAIRPLAY_PATH","PHASE_PRESENTATION_CREATE","PHASE_PRESENTATION_BIND","PHASE_CONTEXT_READY","PHASE_ACCOUNT_LOGIN","PHASE_PLAYBACK_LEASE","PHASE_ACCOUNT_TOKENS","PHASE_LOGIN_FLOW_CREATE","PHASE_LOGIN_FLOW_RUN","PHASE_LOGIN_FLOW_RESPONSE","PHASE_LOGIN_RESPONSE_TYPE","PHASE_LOGIN_RESPONSE_ERROR","PHASE_AUTH_FLOW_READY"})if(strcmp(name,allowed)==0){crash_diagnostics::phase=allowed;auto e=event("nativePhase");cJSON_AddStringToObject(e,"stage",allowed);emit(e);return;}
 for(const char* allowed:{"STARTING","LOGIN","WAITING_2FA","RUNNING","INITIALIZING_FAIRPLAY","FAILED"})if(strcmp(name,allowed)==0){auto e=event("state");cJSON_AddStringToObject(e,"state",allowed);emit(e);break;}}
bool writePrivate(const fs::path& path,const std::string& value){auto temporary=path;temporary+=".tmp";FILE* f=fopen(temporary.string().c_str(),"wb");if(!f)return false;bool ok=fwrite(value.data(),1,value.size(),f)==value.size();if(fflush(f))ok=false;
#ifdef _WIN32
 if(_commit(_fileno(f)))ok=false;
#else
 if(fsync(fileno(f)))ok=false;
#endif
 if(fclose(f))ok=false;
#ifndef _WIN32
 chmod(temporary.string().c_str(),0600);
#endif
 if(!ok)return false;std::error_code error;
#ifdef _WIN32
 fs::remove(path,error);error.clear();
#endif
 fs::rename(temporary,path,error);return !error;}
std::string device(const fs::path& profile){auto path=profile/"device-id";std::string id;{std::ifstream f(path);f>>id;}if(id.size()!=16||id[0]=='0'||id.find_first_not_of("0123456789abcdef")!=std::string::npos){std::random_device random;const char* hex="0123456789abcdef";id.resize(16);for(auto& c:id)c=hex[random()&15];if(id[0]=='0')id[0]='1';writePrivate(path,id);}return "Music/4.9/Android/10/MyGo Desktop/7663313/en-US/en-US/"+id;}
struct API {
#ifdef _WIN32
 HMODULE library=nullptr;
#else
 void* library=nullptr;
#endif
 int(*initialize)(const drm_config*)=nullptr;
 int(*backend)(const char*,const char*,const char*)=nullptr;
 int(*prepare)(const char*,const char*,const char*)=nullptr;
 int(*probe_auth)(const char*,const char*,const char*)=nullptr;
 void(*set_diagnostic)(void(*)(const char*,int))=nullptr;
 void(*set_error_detail)(mygo_error_detail_callback)=nullptr;
 int(*session)(const char*,const char*,const char*,const char*,const char*,drm_auth_callback_t,void*)=nullptr;
 void*(*open)(const char*,const char*,unsigned*)=nullptr;
 int(*decrypt)(void*,unsigned,uint8_t*,uint32_t)=nullptr;
 char*(*account)()=nullptr;const char*(*error)()=nullptr;void(*shutdown)()=nullptr;
 void* symbol(const char* name){
#ifdef _WIN32
 return reinterpret_cast<void*>(GetProcAddress(library,name));
#else
 return dlsym(library,name);
#endif
 }
 bool load(const fs::path& path){
#ifdef _WIN32
 SetDllDirectoryW(path.parent_path().wstring().c_str());library=LoadLibraryW(path.wstring().c_str());
#else
 library=dlopen(path.string().c_str(),RTLD_NOW|RTLD_LOCAL);
#endif
 if(!library)return false;
#define RESOLVE(member,name) member=reinterpret_cast<decltype(member)>(symbol(name));if(!member)return false
 RESOLVE(initialize,"drm_init");RESOLVE(backend,"hybris_backend_init");RESOLVE(session,"hybris_fairplay_init");RESOLVE(open,"hybris_open_kd_ctx_from_uri");RESOLVE(decrypt,"hybris_backend_decrypt");RESOLVE(account,"drm_lib_get_account");RESOLVE(error,"hybris_last_error");RESOLVE(shutdown,"drm_lib_shutdown");
#undef RESOLVE
 prepare=reinterpret_cast<decltype(prepare)>(symbol("hybris_session_probe"));
 probe_auth=reinterpret_cast<decltype(probe_auth)>(symbol("hybris_auth_flow_probe"));
 set_diagnostic=reinterpret_cast<decltype(set_diagnostic)>(symbol("mygo_set_diagnostic_callback"));
 if(set_diagnostic)set_diagnostic(diagnostic);
 set_error_detail=reinterpret_cast<decltype(set_error_detail)>(symbol("mygo_set_error_detail_callback"));
 if(set_error_detail)set_error_detail(errorDetail);
 return true;}
};
// The SDK has no complete shutdown operation. Its recovery worker is detached
// and waits on global condition variables. Bound callback reset, then let the
// owning process release the native runtime without running its static dtors.
void resetCallbacks(API& api,bool report){
 struct Latch{std::mutex mutex;std::condition_variable wake;bool done=false;bool failed=false;};
 auto latch=std::make_shared<Latch>();bool completed=false;
 if(report){auto e=event("cleanupStarting");cJSON_AddStringToObject(e,"stage","native_callbacks_reset");emit(e);}
 try{
  auto reset=api.shutdown;
  std::thread([latch,reset]{bool failed=false;try{reset();}catch(...){failed=true;}{std::lock_guard<std::mutex> lock(latch->mutex);latch->failed=failed;latch->done=true;}latch->wake.notify_one();}).detach();
  std::unique_lock<std::mutex> lock(latch->mutex);
  completed=latch->wake.wait_for(lock,std::chrono::seconds(2),[&]{return latch->done;})&&!latch->failed;
 }catch(...){completed=false;}
 if(report){auto e=event("cleanupCompleted");cJSON_AddBoolToObject(e,"callbacksReset",completed);cJSON_AddBoolToObject(e,"cleanupBounded",true);cJSON_AddBoolToObject(e,"staticDestructorsSkipped",true);emit(e);}
}
bool saveAccount(API& api,const fs::path& profile){char* raw=api.account();if(!raw)return false;auto json=cJSON_Parse(raw);if(!json)return false;auto sf=stringField(json,"storefront_id"),dev=stringField(json,"dev_token"),mut=stringField(json,"music_token");bool ok=!sf.empty()&&!dev.empty()&&!mut.empty()&&writePrivate(profile/"STOREFRONT_ID",sf)&&writePrivate(profile/"DEVELOPER_TOKEN",dev)&&writePrivate(profile/"MUSIC_TOKEN",mut);wipe(sf);wipe(dev);wipe(mut);eraseJSON(json);cJSON_Delete(json);volatile char* p=raw;for(size_t i=0,n=strlen(raw);i<n;++i)p[i]=0;return ok;}
std::string encode(const std::vector<uint8_t>& bytes){const char* alphabet="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";std::string s;s.reserve((bytes.size()+2)/3*4);for(size_t i=0;i<bytes.size();i+=3){uint32_t n=uint32_t(bytes[i])<<16;if(i+1<bytes.size())n|=uint32_t(bytes[i+1])<<8;if(i+2<bytes.size())n|=bytes[i+2];s+=alphabet[(n>>18)&63];s+=alphabet[(n>>12)&63];s+=i+1<bytes.size()?alphabet[(n>>6)&63]:'=';s+=i+2<bytes.size()?alphabet[n&63]:'=';}return s;}
bool decode(const std::string& text,std::vector<uint8_t>& out){if(text.empty()||text.size()>6*1024*1024||text.size()%4)return false;const std::string alphabet="ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";uint32_t n=0;int bits=0;bool padding=false;for(char c:text){if(c=='='){padding=true;continue;}if(padding)return false;auto index=alphabet.find(c);if(index==std::string::npos)return false;n=(n<<6)|uint32_t(index);bits+=6;if(bits>=8){bits-=8;out.push_back(uint8_t(n>>bits));}}return !out.empty()&&out.size()<=4*1024*1024;}
int serve(API& api){void* key=nullptr;unsigned epoch=0;while(auto v=input(6*1024*1024+1024)){auto id=cJSON_GetObjectItemCaseSensitive(v,"id");double ticket=cJSON_IsNumber(id)?id->valuedouble:0;auto method=stringField(v,"method");auto response=cJSON_CreateObject();cJSON_AddNumberToObject(response,"id",ticket);bool ok=false;int code=0;
 if(method=="openKey"){auto adam=stringField(v,"trackID"),uri=stringField(v,"uri");if(!adam.empty()&&adam.size()<=20&&adam.find_first_not_of("0123456789")==std::string::npos&&uri.rfind("skd://itunes.apple.com/",0)==0&&uri.size()<256){key=api.open(adam.c_str(),uri.c_str(),&epoch);ok=key!=nullptr;}}
 else if(method=="decrypt"){std::vector<uint8_t> bytes;if(key&&decode(stringField(v,"data"),bytes)&&bytes.size()%16==0){code=api.decrypt(key,epoch,bytes.data(),uint32_t(bytes.size()));ok=code==0;if(ok)cJSON_AddStringToObject(response,"data",encode(bytes).c_str());}}
 else if(method=="close"){cJSON_AddBoolToObject(response,"ok",true);emit(response);cJSON_Delete(v);return 0;}
 cJSON_AddBoolToObject(response,"ok",ok);if(!ok){cJSON_AddNumberToObject(response,"code",code);cJSON_AddStringToObject(response,"reason","native_operation_failed");}emit(response);cJSON_Delete(v);
 }return 0;}
}
int runHelper(int argc,char** argv){quiet();
#ifdef _WIN32
 crash_diagnostics::install(_fileno(output));
#else
 crash_diagnostics::install(fileno(output));
#endif
 std::string mode="selfcheck";fs::path runtime,profile,client;
 for(int i=1;i+1<argc;i+=2){std::string arg=argv[i];if(arg=="--mode")mode=argv[i+1];else if(arg=="--runtime")runtime=fs::absolute(argv[i+1]);else if(arg=="--session")profile=fs::absolute(argv[i+1]);else if(arg=="--client")client=fs::absolute(argv[i+1]);else{failure("arguments","unknown_argument");return 2;}}
 if(mode!="selfcheck"&&mode!="probe"&&mode!="initialize"&&mode!="auth-preflight"&&mode!="auth"&&mode!="resume"&&mode!="cached-diagnose"&&mode!="serve"){failure("arguments","unknown_mode");return 2;}
#ifdef _WIN32
 if(client.empty())client=runtime/"libdrm_client.dll";auto core=runtime/"libhybris-core.dll";
#else
 if(client.empty())client=runtime/"libdrm_client.so";auto core=runtime/"libhybris-core.so";
#endif
 API api;if(runtime.empty()||!api.load(client)){failure("runtime_load","native_client_unavailable");return 1;}auto e=event("abiLoaded");cJSON_AddBoolToObject(e,"clientSymbolsResolved",true);cJSON_AddBoolToObject(e,"errorDetailsAvailable",api.set_error_detail!=nullptr);cJSON_AddBoolToObject(e,"corePresent",fs::exists(core));cJSON_AddBoolToObject(e,"linkerPresent",fs::exists(runtime/"hybris-linker/q.so"));cJSON_AddBoolToObject(e,"androidLibrariesPresent",fs::exists(runtime/"rootfs/system/lib64/libandroidappmusic.so"));emit(e);if(mode=="selfcheck")return 0;
 if(mode=="cached-diagnose"&&!api.set_error_detail){failure("preflight","updated_error_diagnostics_required");return 1;}
 auto linker=(runtime/"hybris-linker").string(),lib64=(runtime/"rootfs/system/lib64").string();int loaded=api.backend(linker.c_str(),lib64.c_str(),core.string().c_str());if(loaded){failure("android_load","android_runtime_unavailable",loaded);return 1;}e=event("runtimeLoaded");cJSON_AddBoolToObject(e,"androidRuntimeLoaded",true);emit(e);
#ifdef _WIN32
 auto core_module=GetModuleHandleW(core.filename().wstring().c_str());
 if(core_module)crash_diagnostics::guest_location=reinterpret_cast<crash_diagnostics::guest_dladdr>(GetProcAddress(core_module,"android_dladdr"));
 if(!crash_diagnostics::guest_location){auto q_module=GetModuleHandleW(L"q.so");if(q_module)crash_diagnostics::guest_location=reinterpret_cast<crash_diagnostics::guest_dladdr>(GetProcAddress(q_module,"android_dladdr"));}
#endif
 if(mode=="probe")return 0;
 if(profile.empty()){failure("session","profile_required");return 2;}std::error_code error;
 if(mode=="cached-diagnose"&&(!fs::is_directory(profile)||fs::is_empty(profile)||!fs::exists(profile/"device-id"))){failure("session","existing_private_context_required");return 1;}
 if((mode=="initialize"||mode=="auth-preflight")&&fs::exists(profile)&&!fs::is_empty(profile)){failure("preflight","empty_disposable_directory_required");return 2;}
 fs::create_directories(profile/"mpl_db",error);if(error){failure("session","profile_create_failed");return 1;}
#ifndef _WIN32
 chmod(profile.string().c_str(),0700);chmod((profile/"mpl_db").string().c_str(),0700);
#endif
 std::string username,password;if(mode=="auth"){auto v=input(4096);if(v){username=stringField(v,"username");password=stringField(v,"password");eraseJSON(v);cJSON_Delete(v);}if(username.empty()||password.empty()||username.size()>319||password.size()>700){wipe(username);wipe(password);failure("authorization","credentials_required");return 1;}authenticating=true;}
 else if(mode!="initialize"&&mode!="auth-preflight"&&mode!="cached-diagnose"&&!fs::exists(profile/"MUSIC_TOKEN")){failure("authorization","login_required");return 1;}
 if(mode=="cached-diagnose"){e=event("cachedSessionAttempt");cJSON_AddBoolToObject(e,"credentialsSubmitted",false);cJSON_AddBoolToObject(e,"interactiveChallengesAllowed",false);emit(e);}
 auto base=profile.string(),identity=device(profile);drm_config cfg{};cfg.base_directory=base.c_str();cfg.lib64_directory=lib64.c_str();cfg.device_info=identity.c_str();cfg.state_callback=state;cfg.auth_callback=auth;
 if(api.initialize(&cfg)){failure("client_config","native_configuration_failed");return 1;}
 if(mode=="initialize"||mode=="auth-preflight"){
  if(!api.prepare||!api.probe_auth||!api.set_diagnostic){failure("preflight","updated_native_client_required");return 1;}
  int result=(mode=="initialize"?api.prepare:api.probe_auth)(base.c_str(),identity.c_str(),lib64.c_str());
  if(result){failure("native_preflight",mode=="initialize"?"request_context_initialization_failed":"account_free_auth_flow_failed",result);return 1;}
  e=event(mode=="initialize"?"sessionContextReady":"accountFreeAuthFlowCancelled");cJSON_AddBoolToObject(e,"authFlowConstructed",true);cJSON_AddBoolToObject(e,"credentialsUsed",false);cJSON_AddBoolToObject(e,"nativeSignInVerified",false);cJSON_AddBoolToObject(e,"playbackLeaseRequested",false);emit(e);return 0;
 }
 e=event("initializing");cJSON_AddStringToObject(e,"stage","native_session");emit(e);int initialized=api.session(base.c_str(),identity.c_str(),lib64.c_str(),username.empty()?nullptr:username.c_str(),password.empty()?nullptr:password.c_str(),auth,nullptr);authenticating=false;wipe(username);wipe(password);
 if(initialized){failure("native_session",classify(api.error()).c_str(),initialized);return 1;}if(!saveAccount(api,profile)){failure("account_tokens","native_tokens_unavailable");return 1;}
 e=event("authResult");cJSON_AddBoolToObject(e,"nativeSignInVerified",true);cJSON_AddBoolToObject(e,"accountAccepted",true);cJSON_AddBoolToObject(e,"credentialsLogged",false);cJSON_AddStringToObject(e,"stage","account_tokens");emit(e);
 if(mode=="serve"){e=event("decryptPipeReady");cJSON_AddBoolToObject(e,"authenticated",true);emit(e);int status=serve(api);resetCallbacks(api,false);return status;}
 resetCallbacks(api,true);e=event("helperCompleted");cJSON_AddBoolToObject(e,"nativeOperationCompleted",true);cJSON_AddBoolToObject(e,"sessionPersisted",true);cJSON_AddNumberToObject(e,"operationStatus",0);emit(e);return 0;
}

int main(int argc,char** argv){
 int status=runHelper(argc,argv);
 if(output)fflush(output);
 // Preserve the operation's status, including every native authentication or
 // persistence failure. This never promotes a cancelled older run to success.
#ifdef _WIN32
 // ExitProcess would still call DLL detach routines from the SDK. All work
 // and private file writes are complete; terminate this owned helper only.
 TerminateProcess(GetCurrentProcess(),static_cast<UINT>(status));
 return status;
#else
 std::_Exit(status);
#endif
}
