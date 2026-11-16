#include "file_lock_compat.h"
#include "../android-libc-shim/src/errno.h"
#include <unistd.h>
#include <sys/wait.h>
#include <fcntl.h>
#include <cstdio>
#include <cstdlib>
static AndroidFlock lockOf(int type){AndroidFlock l{};l.type=type;l.whence=SEEK_SET;l.start=19;l.length=1;return l;}
static bool childProbe(int fd,int expectedType,bool mayWrite){
 pid_t parent=getpid(),child=fork();if(child<0)return false;
 if(child==0){
  auto q=lockOf(1);bool queried=android_fcntl(fd,5,&q)==0&&q.type==expectedType&&(expectedType==2||q.pid==parent);
  auto w=lockOf(1);int written=android_fcntl(fd,6,&w);bool exclusive=mayWrite?written==0:written==-1&&(*shim::bionic::get_errno()==11||*shim::bionic::get_errno()==13);
  if(mayWrite&&written==0){auto u=lockOf(2);exclusive=android_fcntl(fd,6,&u)==0&&exclusive;}
  _exit(queried&&exclusive?0:1);
 }
 int status=0;return waitpid(child,&status,0)==child&&WIFEXITED(status)&&WEXITSTATUS(status)==0;
}
int main(){
 char name[]="/tmp/mygo-lock-XXXXXX";int fd=mkstemp(name);if(fd<0)return 1;unlink(name);
 auto r=lockOf(0);bool shared=android_fcntl(fd,6,&r)==0&&childProbe(fd,0,false);
 auto u=lockOf(2);bool unlocked=android_fcntl(fd,6,&u)==0&&childProbe(fd,2,true);
 auto w=lockOf(1);bool exclusive=android_fcntl(fd,6,&w)==0&&childProbe(fd,1,false);
 android_fcntl(fd,6,&u);close(fd);
 printf("{\"readLockBlocksOtherProcessWrite\":%s,\"unlockAllowsOtherProcessWrite\":%s,\"writeLockReportedCorrectly\":%s}\n",shared?"true":"false",unlocked?"true":"false",exclusive?"true":"false");return shared&&unlocked&&exclusive?0:1;
}
