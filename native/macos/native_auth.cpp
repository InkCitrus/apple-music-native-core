#include "native_auth.h"
#include "crash_probe.h"
#include "arm-reference/cJSON.h"
#include <cstdio>
#include <cstdlib>
#include <cstring>
#include <mutex>
#include <vector>
#include <array>
#include <chrono>
#include <poll.h>
#include <unistd.h>
#include <sys/stat.h>
#include <fstream>
#include <iostream>
#include <stdexcept>
#include <algorithm>
#include <cctype>
extern "C" void* __loader_dlsym(void*,const char*,const void*);
extern "C" void native_call_sret1(void*,void*,void*);
namespace {
struct NativeString { alignas(8) unsigned char bytes[24]{}; };
struct ErrorCode { int value; int padding; void* category; };
struct NativeVector {NativeShared* begin;NativeShared* end;NativeShared* capacity;};
struct State {
 void* library=nullptr;void* presentation=nullptr;
 std::string username,password;AuthOutcome outcome;bool active=false;
 std::string dialogKind="unknown";
} state;
std::mutex outputMutex;
void wipe(std::string& value){volatile char* p=value.empty()?nullptr:&value[0];for(size_t i=0;i<value.size();++i)p[i]=0;value.clear();}
void emit(cJSON* event){std::lock_guard<std::mutex> lock(outputMutex);char* text=cJSON_PrintUnformatted(event);if(text){fputs(text,stdout);fputc('\n',stdout);fflush(stdout);free(text);}cJSON_Delete(event);}
cJSON* event(const char* name){auto e=cJSON_CreateObject();cJSON_AddStringToObject(e,"event",name);return e;}
void* symbol(const char* name){auto p=__loader_dlsym(state.library,name,nullptr);if(!p)throw std::runtime_error("required native authorization symbol missing");return p;}
template<typename F> F fn(const char* name){return reinterpret_cast<F>(symbol(name));}
NativeString stringArgument(const std::string& value){NativeString s;if(value.size()<=22){s.bytes[0]=static_cast<unsigned char>(value.size()<<1);memcpy(s.bytes+1,value.data(),value.size());}else{struct Long {size_t capacity,size;const char* data;};size_t capacity=(value.size()+2)&~size_t(1);Long l{capacity|1,value.size(),value.c_str()};memcpy(s.bytes,&l,sizeof(l));}return s;}
std::string stringValue(const void* value){if(!value)return {};auto p=static_cast<const unsigned char*>(value);if(!(p[0]&1)){size_t n=p[0]>>1;if(n>22)return {};return std::string(reinterpret_cast<const char*>(p+1),n);}struct Long {size_t capacity,size;const char* data;};Long l;memcpy(&l,p,sizeof(l));if(!l.data||l.size>4096)return {};return std::string(l.data,l.size);}
std::string classify(std::string value){std::transform(value.begin(),value.end(),value.begin(),[](unsigned char c){return char(std::tolower(c));});
 if(value.find("verification")!=std::string::npos||value.find("验证码")!=std::string::npos||value.find("验证代码")!=std::string::npos)return "verification_rejected";
 if(value.find("expired")!=std::string::npos||value.find("过期")!=std::string::npos)return "session_expired";
 if(value.find("locked")!=std::string::npos||value.find("disabled")!=std::string::npos||value.find("锁定")!=std::string::npos)return "account_locked";
 if(value.find("terms")!=std::string::npos||value.find("条款")!=std::string::npos)return "terms_required";
 if(value.find("connect")!=std::string::npos||value.find("network")!=std::string::npos||value.find("网络")!=std::string::npos)return "network_error";
 if(value.find("account information")!=std::string::npos||value.find("password")!=std::string::npos||value.find("密码")!=std::string::npos)return "account_rejected";
 return "apple_rejected";
}
bool waitForCode(int challenge,std::string& code){
 const auto deadline=std::chrono::steady_clock::now()+std::chrono::seconds(180);
 while(std::chrono::steady_clock::now()<deadline){
  if(std::cin.rdbuf()->in_avail()<=0){pollfd p{STDIN_FILENO,POLLIN,0};auto ms=std::chrono::duration_cast<std::chrono::milliseconds>(deadline-std::chrono::steady_clock::now()).count();int r=poll(&p,1,int(ms));if(r<0&&errno==EINTR)continue;if(r<=0)return false;}
  char line[2048]{};if(!std::cin.getline(line,sizeof(line)))return false;
  cJSON* item=cJSON_Parse(line);memset(line,0,sizeof(line));if(!item)continue;
  const auto kind=cJSON_GetObjectItemCaseSensitive(item,"method"), ticket=cJSON_GetObjectItemCaseSensitive(item,"challenge"), value=cJSON_GetObjectItemCaseSensitive(item,"code");
  bool valid=cJSON_IsString(kind)&&strcmp(kind->valuestring,"verify-code")==0&&cJSON_IsNumber(ticket)&&ticket->valueint==challenge&&cJSON_IsString(value)&&strlen(value->valuestring)==6;
  if(valid)for(const char* p=value->valuestring;*p;++p)if(*p<'0'||*p>'9')valid=false;
  if(valid)code=value->valuestring;
  if(cJSON_IsString(value)){size_t n=strlen(value->valuestring);volatile char* p=value->valuestring;for(size_t i=0;i<n;++i)p[i]=0;}
  cJSON_Delete(item);if(valid)return true;
  auto e=event("verificationInputIgnored");cJSON_AddNumberToObject(e,"challenge",challenge);emit(e);
 }
 return false;
}
void credentials(NativeShared* request,NativeShared*){
 bool need=fn<uint8_t(*)(void*)>("_ZNK17storeservicescore18CredentialsRequest28requiresHSA2VerificationCodeEv")(request->object)!=0;
 bool cancelled=!state.active;std::string supplied=state.password;
 if(need&&!cancelled){
  state.outcome.codeRequested=true;int ticket=++state.outcome.challengeCount;
  if(ticket>3)cancelled=true;
  else{crash_probe_stage("verification_wait");auto e=event("twoFactorRequired");cJSON_AddNumberToObject(e,"challenge",ticket);cJSON_AddNumberToObject(e,"timeoutSeconds",180);emit(e);std::string code;
   if(waitForCode(ticket,code)){supplied+=code;wipe(code);state.outcome.codeSubmitted=true;crash_probe_stage("verification_submitted");auto e=event("twoFactorSubmitted");cJSON_AddNumberToObject(e,"challenge",ticket);emit(e);}else{cancelled=true;state.outcome.cancelled=true;state.outcome.reason="verification_cancelled";emit(event("twoFactorCancelled"));}
  }
 }else if(!need&&!cancelled){native_auth_stage("credentials_response");}
 // Native presentation responses are retained after the callback returns.
 // Use a heap control block, as Adam does, rather than the legacy stack response.
 auto memory=static_cast<unsigned char*>(calloc(1,80));if(!memory)throw std::bad_alloc();
 *reinterpret_cast<void**>(memory)=static_cast<unsigned char*>(symbol("_ZTVNSt6__ndk120__shared_ptr_emplaceIN17storeservicescore19CredentialsResponseENS_9allocatorIS2_EEEE"))+16;
 NativeShared response{memory+24,memory};fn<void(*)(void*)>("_ZN17storeservicescore19CredentialsResponseC1Ev")(response.object);
 std::string empty;auto username=stringArgument(cancelled?empty:state.username);auto password=stringArgument(cancelled?empty:supplied);
 fn<void(*)(void*,NativeString*)>("_ZN17storeservicescore19CredentialsResponse11setUserNameERKNSt6__ndk112basic_stringIcNS1_11char_traitsIcEENS1_9allocatorIcEEEE")(response.object,&username);
 fn<void(*)(void*,NativeString*)>("_ZN17storeservicescore19CredentialsResponse11setPasswordERKNSt6__ndk112basic_stringIcNS1_11char_traitsIcEENS1_9allocatorIcEEEE")(response.object,&password);
 fn<void(*)(void*,int)>("_ZN17storeservicescore19CredentialsResponse15setResponseTypeENS0_12ResponseTypeE")(response.object,cancelled?0:2);
 if(!need&&!cancelled)native_auth_stage("credentials_submitted");
 fn<void(*)(void*,NativeShared*)>("_ZN20androidstoreservices28AndroidPresentationInterface25handleCredentialsResponseERKNSt6__ndk110shared_ptrIN17storeservicescore19CredentialsResponseEEE")(state.presentation,&response);
 wipe(supplied);
}
void dialog(int64_t identifier,NativeShared* request,NativeShared*){
 auto title=stringValue(fn<void*(*)(void*)>("_ZNK17storeservicescore14ProtocolDialog5titleEv")(request->object));
 auto message=stringValue(fn<void*(*)(void*)>("_ZNK17storeservicescore14ProtocolDialog7messageEv")(request->object));
 bool signIn=title=="Sign In";
 if(!signIn){state.dialogKind=classify(title+" "+message);auto e=event("appleDialog");cJSON_AddStringToObject(e,"kind",state.dialogKind.c_str());emit(e);}
 auto memory=static_cast<unsigned char*>(calloc(1,72));if(!memory)throw std::bad_alloc();
 *reinterpret_cast<void**>(memory)=static_cast<unsigned char*>(symbol("_ZTVNSt6__ndk120__shared_ptr_emplaceIN17storeservicescore22ProtocolDialogResponseENS_9allocatorIS2_EEEE"))+16;
 NativeShared response{memory+24,memory};fn<void(*)(void*)>("_ZN17storeservicescore22ProtocolDialogResponseC1Ev")(response.object);
 auto buttons=fn<NativeVector*(*)(void*)>("_ZNK17storeservicescore14ProtocolDialog7buttonsEv")(request->object);
 NativeShared* selected=nullptr;
 if(buttons&&buttons->begin&&buttons->end>=buttons->begin&&buttons->end-buttons->begin<=32){
  for(auto b=buttons->begin;b!=buttons->end;++b){auto label=stringValue(fn<void*(*)(void*)>("_ZNK17storeservicescore14ProtocolButton5titleEv")(b->object));
   if((signIn&&label=="Use Existing Apple ID")||(!signIn&&(label=="Cancel"||label=="Not Now"||label=="OK"))){selected=b;break;}}
  if(!signIn&&!selected&&buttons->begin!=buttons->end)selected=buttons->begin;
 }
 if(selected)fn<void(*)(void*,NativeShared*)>("_ZN17storeservicescore22ProtocolDialogResponse17setSelectedButtonERKNSt6__ndk110shared_ptrINS_14ProtocolButtonEEE")(response.object,selected);
 fn<void(*)(void*,int64_t*,NativeShared*)>("_ZN20androidstoreservices28AndroidPresentationInterface28handleProtocolDialogResponseERKlRKNSt6__ndk110shared_ptrIN17storeservicescore22ProtocolDialogResponseEEE")(state.presentation,&identifier,&response);
}
}
void native_auth_stage(const char* stage){crash_probe_stage(stage);auto e=event("stage");cJSON_AddStringToObject(e,"stage",stage);emit(e);}
void native_auth_setup(void* library,uintptr_t base,const char* username,const char* password){
 native_auth_finish();state.library=library;state.presentation=reinterpret_cast<NativeShared*>(base+0x22fa0)->object;
 state.username=username;state.password=password;state.outcome=AuthOutcome{};state.dialogKind="unknown";state.active=true;
 fn<void(*)(void*,void(*)(NativeShared*,NativeShared*))>("_ZN20androidstoreservices28AndroidPresentationInterface21setCredentialsHandlerEPFvNSt6__ndk110shared_ptrIN17storeservicescore18CredentialsRequestEEENS2_INS_33AndroidCredentialsResponseHandlerEEEE")(state.presentation,credentials);
 fn<void(*)(void*,void(*)(int64_t,NativeShared*,NativeShared*))>("_ZN20androidstoreservices28AndroidPresentationInterface16setDialogHandlerEPFvlNSt6__ndk110shared_ptrIN17storeservicescore14ProtocolDialogEEENS2_INS_36AndroidProtocolDialogResponseHandlerEEEE")(state.presentation,dialog);
}
AuthOutcome native_auth_run(NativeShared context){
 native_auth_stage("account_authentication");NativeShared flow{};
 native_call_sret1(symbol("_ZNSt6__ndk110shared_ptrIN17storeservicescore16AuthenticateFlowEE11make_sharedIJRNS0_INS1_14RequestContextEEEEEES3_DpOT_"),&flow,&context);
 if(!flow.object){state.outcome.reason="flow_creation_failed";return state.outcome;}
 fn<void(*)(void*)>("_ZN17storeservicescore16AuthenticateFlow3runEv")(flow.object);
 auto response=fn<NativeShared*(*)(void*)>("_ZNK17storeservicescore16AuthenticateFlow8responseEv")(flow.object);
 if(!response||!response->object){state.outcome.reason="no_response";return state.outcome;}
 state.outcome.responseType=fn<int(*)(void*)>("_ZNK17storeservicescore20AuthenticateResponse12responseTypeEv")(response->object);state.outcome.accepted=state.outcome.responseType==6;
 if(state.outcome.accepted)state.outcome.reason="accepted";
 else{
  auto customer=__loader_dlsym(state.library,"_ZNK17storeservicescore20AuthenticateResponse15customerMessageEv",nullptr);
  if(customer){auto text=stringValue(reinterpret_cast<void*(*)(void*)>(customer)(response->object));if(!text.empty())state.outcome.reason=classify(text);wipe(text);}
  auto error=fn<NativeShared*(*)(void*)>("_ZNK17storeservicescore20AuthenticateResponse5errorEv")(response->object);
  if(error&&error->object){auto code=fn<ErrorCode*(*)(void*)>("_ZNK17storeservicescore19StoreErrorCondition9errorCodeEv")(error->object);if(code)state.outcome.errorCode=code->value;
   auto external=__loader_dlsym(state.library,"_ZNK17storeservicescore19StoreErrorCondition17externalErrorCodeEv",nullptr);if(external){auto value=reinterpret_cast<const int64_t*(*)(void*)>(external)(error->object);if(value)state.outcome.externalErrorCode=*value;}
   const char* text=fn<const char*(*)(void*)>("_ZNK17storeservicescore19StoreErrorCondition4whatEv")(error->object);if(text)state.outcome.reason=classify(text);
  }
  if(state.dialogKind!="unknown")state.outcome.reason=state.dialogKind;
  if(state.outcome.cancelled)state.outcome.reason="verification_cancelled";
 }
 return state.outcome;
}
void native_auth_finish(){state.active=false;wipe(state.username);wipe(state.password);}
void native_auth_result(const AuthOutcome& outcome,bool ready,const char* stage,const char* directory){
 auto e=event("authResult");cJSON_AddBoolToObject(e,"nativeSignInVerified",ready);cJSON_AddBoolToObject(e,"accountAccepted",outcome.accepted);cJSON_AddBoolToObject(e,"twoFactorRequested",outcome.codeRequested);cJSON_AddBoolToObject(e,"twoFactorSubmitted",outcome.codeSubmitted);cJSON_AddBoolToObject(e,"cancelled",outcome.cancelled);cJSON_AddNumberToObject(e,"challengeCount",outcome.challengeCount);cJSON_AddNumberToObject(e,"responseType",outcome.responseType);cJSON_AddNumberToObject(e,"errorCode",outcome.errorCode);cJSON_AddNumberToObject(e,"externalErrorCode",double(outcome.externalErrorCode));cJSON_AddStringToObject(e,"stage",stage);cJSON_AddStringToObject(e,"reason",outcome.reason.c_str());cJSON_AddBoolToObject(e,"credentialsLogged",false);
 char* text=cJSON_PrintUnformatted(e);if(text){std::string path=std::string(directory)+"/sign-in-result.json";std::ofstream f(path,std::ios::trunc);f<<text<<'\n';f.close();chmod(path.c_str(),0600);free(text);}emit(e);
}

#ifdef NATIVE_AUTH_TEST
extern "C" int native_auth_code_test(int challenge,char* output,size_t capacity){std::string value;bool ok=waitForCode(challenge,value);if(ok&&capacity>value.size()){memcpy(output,value.c_str(),value.size()+1);wipe(value);return 1;}wipe(value);return 0;}
#endif
