#include "queue.h"
#include <AudioToolbox/AudioToolbox.h>
#include <CoreAudio/CoreAudio.h>
#include <atomic>
#include <mutex>
#include <condition_variable>
#include <vector>
#include <cstring>
#include <chrono>
namespace {
struct Queue {
 AudioQueueRef queue=nullptr;
 std::mutex mutex;std::condition_variable ready;
 std::vector<AudioQueueBufferRef> free;
 std::atomic<bool> aborted{false},paused{false};unsigned queued=0;bool started=false,failed=false;uint32_t bytesPerFrame=0;double rate=0;uint64_t completedFrames=0;
};
void available(void* user,AudioQueueRef,AudioQueueBufferRef buffer){auto q=static_cast<Queue*>(user);std::lock_guard<std::mutex> lock(q->mutex);q->completedFrames+=buffer->mAudioDataByteSize/q->bytesPerFrame;q->free.push_back(buffer);if(q->queued)--q->queued;q->ready.notify_all();}
}
void* pcm_queue_new(uint32_t rate,uint32_t channels,uint32_t bits,float volume){
 auto q=new Queue;q->rate=rate;q->bytesPerFrame=channels*(bits/8);AudioStreamBasicDescription format{};format.mSampleRate=rate;format.mFormatID=kAudioFormatLinearPCM;format.mFormatFlags=kAudioFormatFlagIsSignedInteger|kAudioFormatFlagIsPacked;format.mFramesPerPacket=1;format.mChannelsPerFrame=channels;format.mBitsPerChannel=bits;format.mBytesPerFrame=q->bytesPerFrame;format.mBytesPerPacket=q->bytesPerFrame;
 if(AudioQueueNewOutput(&format,available,q,nullptr,nullptr,0,&q->queue)){delete q;return nullptr;}
 for(int i=0;i<8;++i){AudioQueueBufferRef buffer=nullptr;if(AudioQueueAllocateBuffer(q->queue,4096*q->bytesPerFrame,&buffer)){AudioQueueDispose(q->queue,true);delete q;return nullptr;}q->free.push_back(buffer);}
 AudioQueueSetParameter(q->queue,kAudioQueueParam_Volume,volume);return q;
}
int pcm_queue_write(void* ptr,const void* input,uint32_t bytes){
 auto q=static_cast<Queue*>(ptr);auto data=static_cast<const unsigned char*>(input);if(bytes%q->bytesPerFrame)return -1;
 while(bytes){AudioQueueBufferRef buffer;std::atomic<bool> aborted{false},paused{false};unsigned queued=0;{
   std::unique_lock<std::mutex> lock(q->mutex);while(q->free.empty()&&!q->aborted.load())q->ready.wait_for(lock,std::chrono::milliseconds(50));if(q->aborted.load())return -1;buffer=q->free.back();q->free.pop_back();queued=++q->queued;}
  uint32_t size=bytes<buffer->mAudioDataBytesCapacity?bytes:buffer->mAudioDataBytesCapacity;memcpy(buffer->mAudioData,data,size);buffer->mAudioDataByteSize=size;
  OSStatus error=AudioQueueEnqueueBuffer(q->queue,buffer,0,nullptr);if(error){q->failed=true;return int(error);}data+=size;bytes-=size;
  if(!q->paused.load()&&!q->started&&queued>=3){error=AudioQueueStart(q->queue,nullptr);if(error){q->failed=true;return int(error);}q->started=true;}
 }
 return 0;
}
int pcm_queue_pause(void* ptr,int pause){auto q=static_cast<Queue*>(ptr);q->paused.store(pause!=0);return pause?AudioQueuePause(q->queue):AudioQueueStart(q->queue,nullptr);}
int pcm_queue_volume(void* ptr,float volume){auto q=static_cast<Queue*>(ptr);return AudioQueueSetParameter(q->queue,kAudioQueueParam_Volume,volume);}
double pcm_queue_get_volume(void* ptr){auto q=static_cast<Queue*>(ptr);AudioQueueParameterValue volume=0;return AudioQueueGetParameter(q->queue,kAudioQueueParam_Volume,&volume)?-1:volume;}
double pcm_queue_position(void* ptr){auto q=static_cast<Queue*>(ptr);AudioTimeStamp timestamp{};if(AudioQueueGetCurrentTime(q->queue,nullptr,&timestamp,nullptr))return -1;return timestamp.mFlags&kAudioTimeStampSampleTimeValid?timestamp.mSampleTime/q->rate:-1;}
double pcm_queue_hardware_rate(){
 AudioDeviceID device=0;UInt32 size=sizeof(device);AudioObjectPropertyAddress address{kAudioHardwarePropertyDefaultOutputDevice,kAudioObjectPropertyScopeGlobal,kAudioObjectPropertyElementMain};if(AudioObjectGetPropertyData(kAudioObjectSystemObject,&address,0,nullptr,&size,&device))return 0;
 double rate=0;size=sizeof(rate);address={kAudioDevicePropertyNominalSampleRate,kAudioObjectPropertyScopeGlobal,kAudioObjectPropertyElementMain};if(AudioObjectGetPropertyData(device,&address,0,nullptr,&size,&rate))return 0;return rate;
}
int pcm_queue_start(void* p){auto q=static_cast<Queue*>(p);return q->paused.load()?0:AudioQueueStart(q->queue,nullptr);}
void pcm_queue_abort(void* p){auto q=static_cast<Queue*>(p);q->aborted.store(true);q->ready.notify_all();}
double pcm_queue_close(void* ptr){if(!ptr)return 0;auto q=static_cast<Queue*>(ptr);if(!q->failed&&!q->aborted.load()){AudioQueueStart(q->queue,nullptr);std::unique_lock<std::mutex> lock(q->mutex);q->ready.wait_for(lock,std::chrono::seconds(5),[&]{return q->queued==0;});}AudioQueueStop(q->queue,true);AudioQueueDispose(q->queue,true);double seconds=q->completedFrames/q->rate;delete q;return seconds;}
