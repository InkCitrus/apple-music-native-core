#include <cstdio>
#include <cstring>
#include <cmath>
extern "C" int test_guest_format(char*,size_t);
extern "C" int test_guest_spill(char*,size_t);
extern "C" int test_guest_scan(int*,char*,double*);
int main(){char b[256]{};test_guest_format(b,sizeof(b));bool format=strcmp(b,"integer=42 text=ok decimal=1.25 padded=  7")==0;test_guest_spill(b,sizeof(b));bool spill=strcmp(b,"1,2,3,4,5,6,7,8,9")==0;int n=0;char s[5]{};double d=0;int count=test_guest_scan(&n,s,&d);bool scan=count==3&&n==42&&strcmp(s,"good")==0&&std::abs(d-1.25)<1e-9;printf("{\"aapcsMixedArguments\":%s,\"aapcsStackSpill\":%s,\"aapcsScanning\":%s}\n",format?"true":"false",spill?"true":"false",scan?"true":"false");return format&&spill&&scan?0:1;}
