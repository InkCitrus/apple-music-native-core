#include "native_tokens.h"
#include "arm-reference/cJSON.h"
#include <cstdlib>
#include <cstring>
#include <stdexcept>
#include <sys/time.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/stat.h>
extern "C" void* __loader_dlsym(void*,const char*,const void*);
extern "C" void native_call_sret1(void*,void*,void*);
namespace {
struct String {alignas(8) unsigned char bytes[24]{};};
void* lookup(void* lib,const char* name){auto p=__loader_dlsym(lib,name,nullptr);if(!p)throw std::runtime_error("native token symbol missing");return p;}
template<typename F> F fn(void* lib,const char* name){return reinterpret_cast<F>(lookup(lib,name));}
String* text(void* lib,const std::string& value){auto s=new String;fn<void*(*)(void*,const char*,size_t)>(lib,"_ZNSt6__ndk112basic_stringIcNS_11char_traitsIcEENS_9allocatorIcEEE6assignEPKcm")(s,value.data(),value.size());return s;}
void header(void* lib,void* message,const char* name,const char* value){auto a=text(lib,name),b=text(lib,value);fn<void(*)(void*,String*,String*)>(lib,"_ZN13mediaplatform11HTTPMessage9setHeaderERKNSt6__ndk112basic_stringIcNS1_11char_traitsIcEENS1_9allocatorIcEEEES9_")(message,a,b);}
void parameter(void* lib,void* request,const char* name,const char* value){auto a=text(lib,name),b=text(lib,value);fn<void(*)(void*,String*,String*)>(lib,"_ZN17storeservicescore10URLRequest19setRequestParameterERKNSt6__ndk112basic_stringIcNS1_11char_traitsIcEENS1_9allocatorIcEEEES9_")(request,a,b);}
std::string request(void* lib,NativeShared context,const char* url,const char* method,const std::string& body,bool developer){
 // These objects can be retained by the native asynchronous HTTP worker after
 // run() returns. Keep native control blocks and backing buffers off the stack.
 // This bounded two-request trial retains them until the helper process ends.
 auto memory=static_cast<unsigned char*>(calloc(1,2048));if(!memory)throw std::bad_alloc();
 *reinterpret_cast<void**>(memory)=static_cast<unsigned char*>(lookup(lib,"_ZTVNSt6__ndk120__shared_ptr_emplaceIN13mediaplatform11HTTPMessageENS_9allocatorIS2_EEEE"))+16;
 auto message=new NativeShared{memory+32,memory};auto address=text(lib,url),verb=text(lib,method);
 fn<void(*)(void*,String*,String*)>(lib,"_ZN13mediaplatform11HTTPMessageC2ENSt6__ndk112basic_stringIcNS1_11char_traitsIcEENS1_9allocatorIcEEEES7_")(message->object,address,verb);
 if(!body.empty()){
  header(lib,message->object,"Content-Type","application/json; charset=UTF-8");header(lib,message->object,"Expect","");header(lib,message->object,"X-Apple-Requesting-Bundle-Id","com.apple.android.music");header(lib,message->object,"X-Apple-Requesting-Bundle-Version","Music/4.9 Android/10 model/Samsung S9 build/7663313 (dt:66)");
  char* retained=strdup(body.c_str());fn<void(*)(void*,char*,size_t)>(lib,"_ZN13mediaplatform11HTTPMessage11setBodyDataEPcm")(message->object,retained,body.size());
 }
 auto request=calloc(1,2048);auto retainedContext=new NativeShared(context);
 fn<void(*)(void*,NativeShared*,NativeShared*)>(lib,"_ZN17storeservicescore10URLRequestC2ERKNSt6__ndk110shared_ptrIN13mediaplatform11HTTPMessageEEERKNS2_INS_14RequestContextEEE")(request,message,retainedContext);
 if(developer){parameter(lib,request,"clientId","musicAndroid");parameter(lib,request,"version","1");}
 fn<void(*)(void*)>(lib,"_ZN17storeservicescore10URLRequest3runEv")(request);
 auto error=fn<NativeShared*(*)(void*)>(lib,"_ZNK17storeservicescore10URLRequest5errorEv")(request);if(error&&error->object)return {};
 auto response=fn<NativeShared*(*)(void*)>(lib,"_ZNK17storeservicescore10URLRequest8responseEv")(request);if(!response||!response->object)return {};
 auto underlying=fn<NativeShared*(*)(void*)>(lib,"_ZNK17storeservicescore11URLResponse18underlyingResponseEv")(response->object);if(!underlying||!underlying->object)return {};
 void* data=*reinterpret_cast<void**>(static_cast<unsigned char*>(underlying->object)+48);if(!data)return {};
 size_t length=fn<size_t(*)(void*)>(lib,"_ZNK13mediaplatform4Data6lengthEv")(data);if(length>2*1024*1024)return {};
 auto bytes=fn<const char*(*)(void*)>(lib,"_ZNK13mediaplatform4Data5bytesEv")(data);return bytes?std::string(bytes,length):std::string();
}
std::string field(const std::string& body,const char* name){auto json=cJSON_ParseWithLength(body.data(),body.size());if(!json)return {};auto item=cJSON_GetObjectItemCaseSensitive(json,name);std::string result=cJSON_IsString(item)?item->valuestring:"";cJSON_Delete(json);return result;}
bool store(const std::string& path,const std::string& value){std::string temporary=path+".tmp";int fd=open(temporary.c_str(),O_WRONLY|O_CREAT|O_TRUNC|O_CLOEXEC,0600);if(fd<0)return false;size_t n=0;while(n<value.size()){ssize_t size=write(fd,value.data()+n,value.size()-n);if(size<=0){close(fd);unlink(temporary.c_str());return false;}n+=size;}bool ok=fsync(fd)==0;close(fd);if(!ok||rename(temporary.c_str(),path.c_str())){unlink(temporary.c_str());return false;}chmod(path.c_str(),0600);return true;}
}
bool native_account_tokens(void* lib,uintptr_t base,NativeShared context,NativeTokens& result){
 struct CContext{void* object;void* control;};char* storefront=reinterpret_cast<char*(*)(CContext)>(base+0xfacc)({context.object,context.control});if(!storefront||!*storefront)return false;result.storefront=storefront;free(storefront);
 result.developer=field(request(lib,context,"https://sf-api-token-service.itunes.apple.com/apiToken","GET",{},true),"token");if(result.developer.empty())return false;
 NativeShared guid{};native_call_sret1(lookup(lib,"_ZN17storeservicescore10DeviceGUID4guidEv"),&guid,reinterpret_cast<NativeShared*>(base+0x22e78)->object);if(!guid.object)return false;
 size_t size=fn<size_t(*)(void*)>(lib,"_ZNK13mediaplatform4Data6lengthEv")(guid.object);const char* data=fn<const char*(*)(void*)>(lib,"_ZNK13mediaplatform4Data5bytesEv")(guid.object);if(!data||size>4096)return false;std::string identifier(data,size);
 timeval now{};gettimeofday(&now,nullptr);auto json=cJSON_CreateObject();cJSON_AddStringToObject(json,"guid",identifier.c_str());cJSON_AddStringToObject(json,"assertion",result.developer.c_str());std::string time=std::to_string(now.tv_sec*1000LL+now.tv_usec/1000);cJSON_AddStringToObject(json,"tcc-acceptance-date",time.c_str());char* body=cJSON_PrintUnformatted(json);cJSON_Delete(json);if(!body)return false;std::string encoded=body;free(body);
 result.user=field(request(lib,context,"https://play.itunes.apple.com/WebObjects/MZPlay.woa/wa/createMusicToken","POST",encoded,false),"music_token");return !result.user.empty();
}
bool save_native_tokens(const char* directory,const NativeTokens& tokens){std::string root=directory;return store(root+"/STOREFRONT_ID",tokens.storefront)&&store(root+"/DEVELOPER_TOKEN",tokens.developer)&&store(root+"/MUSIC_TOKEN",tokens.user);}
