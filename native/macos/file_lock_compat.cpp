#include "file_lock_compat.h"
#include "../android-libc-shim/src/file_misc.h"
#include "../android-libc-shim/src/errno.h"
#include <fcntl.h>
#include <cerrno>
#include <cstddef>
static_assert(sizeof(AndroidFlock)==32 && offsetof(AndroidFlock,start)==8);
namespace {
int hostLock(int value){switch(value){case 0:return F_RDLCK;case 1:return F_WRLCK;case 2:return F_UNLCK;default:return -1;}}
int androidLock(int value){switch(value){case F_RDLCK:return 0;case F_WRLCK:return 1;case F_UNLCK:return 2;default:return -1;}}
int flagsToAndroid(int flags){
 int result=flags&O_ACCMODE;
 if(flags&O_APPEND)result|=02000;
 if(flags&O_NONBLOCK)result|=04000;
 return result;
}
}
int android_fcntl(int fd,int command,void* argument){
 shim::bionic::sync_errno();int result=-1;auto value=int(reinterpret_cast<intptr_t>(argument));
 switch(command){
 case 0:result=::fcntl(fd,F_DUPFD,value);break;
 case 1:result=::fcntl(fd,F_GETFD);break;
 case 2:result=::fcntl(fd,F_SETFD,value);break;
 case 3:result=::fcntl(fd,F_GETFL);if(result>=0)result=flagsToAndroid(result);break;
 case 4:result=::fcntl(fd,F_SETFL,shim::bionic::to_host_file_status_flags(static_cast<shim::bionic::file_status_flags>(value)));break;
 case 5:case 6:case 7:{
  if(!argument){errno=EFAULT;break;}
  auto& android=*static_cast<AndroidFlock*>(argument);int type=hostLock(android.type);
  if(type<0){errno=EINVAL;break;}
  struct flock host{};host.l_type=type;host.l_whence=android.whence;host.l_start=android.start;host.l_len=android.length;host.l_pid=android.pid;
  result=::fcntl(fd,command==5?F_GETLK:command==6?F_SETLK:F_SETLKW,&host);
  if(result==0 && command==5){android.type=androidLock(host.l_type);android.whence=host.l_whence;android.start=host.l_start;android.length=host.l_len;android.pid=host.l_pid;}
  break;
 }
 case 1030:result=::fcntl(fd,F_DUPFD_CLOEXEC,value);break;
 default:errno=EINVAL;break;
 }
 shim::bionic::update_errno();return result;
}
