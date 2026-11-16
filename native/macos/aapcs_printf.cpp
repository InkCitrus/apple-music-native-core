// Convert Android AAPCS64 variadic arguments to Darwin's stack va_list.
// Used only by the isolated compatibility probe; no credentials are logged.
#include <cstdint>
#include <cstddef>
#include <cstdio>
#include <cstring>
#include <cerrno>
#include <vector>
#include <string>
#include <cwchar>
#include <cctype>
struct GuestVA {char* stack;char* gr_top;char* vr_top;int gr_offs;int vr_offs;};
struct GuestFrame {uint64_t gp[8]; alignas(16) unsigned char fp[8][16]; void* stack;};
static uint64_t take(GuestVA& a,bool fp){uint64_t v;if(fp&&a.vr_offs<0){memcpy(&v,a.vr_top+a.vr_offs,8);a.vr_offs+=16;}else if(!fp&&a.gr_offs<0){memcpy(&v,a.gr_top+a.gr_offs,8);a.gr_offs+=8;}else{a.stack=(char*)((uintptr_t(a.stack)+7)&~uintptr_t(7));memcpy(&v,a.stack,8);a.stack+=8;}return v;}
static bool pack(const char* fmt,GuestVA a,std::vector<uint64_t>& v){
 if(!fmt)return false;
 for(auto p=fmt;*p;++p){if(*p!='%')continue;++p;if(*p=='%')continue;
  while(*p&&strchr("-+ #0'",*p))++p;
  if(*p=='*'){v.push_back(take(a,false));++p;}else while(isdigit((unsigned char)*p))++p;
  if(*p=='$')return false;
  if(*p=='.'){++p;if(*p=='*'){v.push_back(take(a,false));++p;}else while(isdigit((unsigned char)*p))++p;}
  bool wideFloat=false;if(*p=='L'){wideFloat=true;++p;}else if(*p&&strchr("hljztq",*p)){char c=*p++;if((c=='l'||c=='h')&&*p==c)++p;}
  if(!*p||wideFloat)return false;
  if(strchr("diouxXcspn",*p))v.push_back(take(a,false));else if(strchr("aAeEfFgG",*p))v.push_back(take(a,true));else if(*p!='m')return false;
  if(v.size()>128)return false;
 }
 // Keep va_list non-null even for a literal with no arguments.
 if(v.empty())v.push_back(0);return true;
}
extern "C" int guest_vsnprintf(char* dst,size_t cap,const char* fmt,GuestVA* ap){std::vector<uint64_t> v;if(!ap||!pack(fmt,*ap,v)){errno=EINVAL;return -1;}return vsnprintf(dst,cap,fmt,reinterpret_cast<va_list>(v.data()));}
extern "C" int guest_vsprintf(char* dst,const char* fmt,GuestVA* ap){std::vector<uint64_t> v;if(!ap||!pack(fmt,*ap,v)){errno=EINVAL;return -1;}return vsprintf(dst,fmt,reinterpret_cast<va_list>(v.data()));}
extern "C" int guest_vprintf(const char* fmt,GuestVA* ap){std::vector<uint64_t> v;if(!ap||!pack(fmt,*ap,v)){errno=EINVAL;return -1;}return vprintf(fmt,reinterpret_cast<va_list>(v.data()));}
extern "C" int guest_printf_frame(GuestFrame* f,int kind){
 unsigned fixed=kind==0?3:kind==1?2:kind==2?1:5;
 GuestVA a{(char*)f->stack,(char*)(f->gp+8),(char*)(f->fp+8),-int(8-fixed)*8,-128};
 if(kind==0)return guest_vsnprintf((char*)f->gp[0],f->gp[1],(char*)f->gp[2],&a);
 if(kind==1)return guest_vsprintf((char*)f->gp[0],(char*)f->gp[1],&a);
 if(kind==2)return guest_vprintf((char*)f->gp[0],&a);
 return guest_vsnprintf((char*)f->gp[0],f->gp[1],(char*)f->gp[4],&a);
}
extern "C" int guest___vsnprintf_chk(char* dst,size_t cap,int,size_t objectCap,const char* fmt,GuestVA* ap){if(cap>objectCap){errno=EOVERFLOW;return -1;}return guest_vsnprintf(dst,cap,fmt,ap);}
#include "../android-libc-shim/src/cstdio.h"
#include <unistd.h>
static bool packScan(const char* fmt,GuestVA a,std::vector<uint64_t>& v){
 if(!fmt)return false;for(auto p=fmt;*p;++p){if(*p!='%')continue;++p;if(*p=='%')continue;bool suppress=*p=='*';if(suppress)++p;while(isdigit((unsigned char)*p))++p;while(*p&&strchr("hljztLm",*p))++p;if(*p=='['){++p;if(*p=='^')++p;if(*p==']')++p;while(*p&&*p!=']')++p;}if(!*p)return false;if(!suppress)v.push_back(take(a,false));if(v.size()>128)return false;}if(v.empty())v.push_back(0);return true;
}
extern "C" int guest_vsscanf(const char* src,const char* fmt,GuestVA* ap){std::vector<uint64_t> v;if(!ap||!packScan(fmt,*ap,v)){errno=EINVAL;return -1;}return vsscanf(src,fmt,reinterpret_cast<va_list>(v.data()));}
extern "C" int guest_vfprintf(shim::bionic::FILE* fp,const char* fmt,GuestVA* ap){std::vector<uint64_t> v;if(!fp||!ap||!pack(fmt,*ap,v)){errno=EINVAL;return -1;}return vfprintf(fp->wrapped,fmt,reinterpret_cast<va_list>(v.data()));}
extern "C" int guest_vasprintf(char** dst,const char* fmt,GuestVA* ap){std::vector<uint64_t> v;if(!ap||!pack(fmt,*ap,v)){errno=EINVAL;return -1;}return vasprintf(dst,fmt,reinterpret_cast<va_list>(v.data()));}
static int guest_wformat(GuestFrame* f,GuestVA* a){auto fmt=(wchar_t*)f->gp[2];std::string narrow;for(auto p=fmt;*p;++p)narrow+=*p<128?char(*p):' ';std::vector<uint64_t> v;if(!pack(narrow.c_str(),*a,v)){errno=EINVAL;return -1;}return vswprintf((wchar_t*)f->gp[0],f->gp[1],fmt,reinterpret_cast<va_list>(v.data()));}
extern "C" int guest_more_printf_frame(GuestFrame* f,int kind){unsigned fixed=kind==8?3:2;GuestVA a{(char*)f->stack,(char*)(f->gp+8),(char*)(f->fp+8),-int(8-fixed)*8,-128};if(kind==8)return guest_wformat(f,&a);if(kind==4)return guest_vsscanf((char*)f->gp[0],(char*)f->gp[1],&a);if(kind==5)return guest_vfprintf((shim::bionic::FILE*)f->gp[0],(char*)f->gp[1],&a);if(kind==6)return guest_vasprintf((char**)f->gp[0],(char*)f->gp[1],&a);std::vector<uint64_t> v;if(!pack((char*)f->gp[1],a,v)){errno=EINVAL;return -1;}return vdprintf(int(f->gp[0]),(char*)f->gp[1],reinterpret_cast<va_list>(v.data()));}
extern "C" int guest___vsprintf_chk(char* dst,int,size_t cap,const char* fmt,GuestVA* ap){return guest_vsnprintf(dst,cap,fmt,ap);}
extern "C" int guest_vfscanf(shim::bionic::FILE* fp,const char* fmt,GuestVA* ap){std::vector<uint64_t> v;if(!fp||!ap||!packScan(fmt,*ap,v)){errno=EINVAL;return -1;}return vfscanf(fp->wrapped,fmt,reinterpret_cast<va_list>(v.data()));}
extern "C" int guest_scan_frame(GuestFrame* f,int kind){unsigned fixed=kind==9?2:1;GuestVA a{(char*)f->stack,(char*)(f->gp+8),(char*)(f->fp+8),-int(8-fixed)*8,-128};if(kind==9)return guest_vfscanf((shim::bionic::FILE*)f->gp[0],(char*)f->gp[1],&a);std::vector<uint64_t> v;if(!packScan((char*)f->gp[0],a,v)){errno=EINVAL;return -1;}return vscanf((char*)f->gp[0],reinterpret_cast<va_list>(v.data()));}
