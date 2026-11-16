#pragma once
#include <cstdint>
// AArch64 Bionic flock layout; Darwin uses another layout and lock constants.
struct AndroidFlock {
 int16_t type,whence;
 int64_t start,length;
 int32_t pid;
};
int android_fcntl(int fd,int command,void* argument);
