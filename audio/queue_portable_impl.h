#define MINIAUDIO_IMPLEMENTATION
#define MA_NO_DECODING
#define MA_NO_ENCODING
#define MA_NO_RESOURCE_MANAGER
#define MA_NO_NODE_GRAPH
#define MA_ENABLE_ONLY_SPECIFIC_BACKENDS
#ifdef _WIN32
#define MA_ENABLE_WASAPI
#else
#define MA_ENABLE_ALSA
#define MA_ENABLE_PULSEAUDIO
#endif
#include "miniaudio.h"
#include "queue.h"
#include <atomic>
#include <chrono>
#include <cstring>
namespace {
struct PCMQueue {ma_device device{};ma_pcm_rb ring{};uint32_t frameBytes=0,rate=0;bool started=false;std::atomic<bool> paused{false},aborted{false};std::atomic<uint64_t> completed{0};};
std::atomic<double> outputRate{0};
void callback(ma_device* device,void* output,const void*,ma_uint32 frames){
 auto q=static_cast<PCMQueue*>(device->pUserData);memset(output,0,size_t(frames)*q->frameBytes);if(q->paused.load())return;auto out=static_cast<unsigned char*>(output);
 while(frames){ma_uint32 n=frames;void* input=nullptr;if(ma_pcm_rb_acquire_read(&q->ring,&n,&input)!=MA_SUCCESS||!n)break;memcpy(out,input,size_t(n)*q->frameBytes);ma_pcm_rb_commit_read(&q->ring,n);q->completed.fetch_add(n);frames-=n;out+=size_t(n)*q->frameBytes;}
}
}
void* pcm_queue_new(uint32_t rate,uint32_t channels,uint32_t bits,float volume){
 auto q=new PCMQueue;q->rate=rate;q->frameBytes=channels*(bits/8);ma_format format=bits==16?ma_format_s16:bits==24?ma_format_s24:bits==32?ma_format_s32:ma_format_unknown;if(format==ma_format_unknown){delete q;return nullptr;}
 if(ma_pcm_rb_init(format,channels,32768,nullptr,nullptr,&q->ring)!=MA_SUCCESS){delete q;return nullptr;}
 auto config=ma_device_config_init(ma_device_type_playback);config.playback.format=format;config.playback.channels=channels;config.sampleRate=rate;config.dataCallback=callback;config.pUserData=q;
 if(ma_device_init(nullptr,&config,&q->device)!=MA_SUCCESS){ma_pcm_rb_uninit(&q->ring);delete q;return nullptr;}ma_device_set_master_volume(&q->device,volume);outputRate.store(q->device.playback.internalSampleRate);return q;
}
int pcm_queue_write(void* pointer,const void* data,uint32_t bytes){auto q=static_cast<PCMQueue*>(pointer);if(bytes%q->frameBytes)return -1;auto input=static_cast<const unsigned char*>(data);auto start=std::chrono::steady_clock::now();
 while(bytes){if(q->aborted.load())return -1;if(q->paused.load()){ma_sleep(2);start=std::chrono::steady_clock::now();continue;}ma_uint32 frames=bytes/q->frameBytes;void* output=nullptr;ma_result code=ma_pcm_rb_acquire_write(&q->ring,&frames,&output);if(code!=MA_SUCCESS)return int(code);if(!frames){if(std::chrono::steady_clock::now()-start>std::chrono::seconds(5))return -1;ma_sleep(1);continue;}memcpy(output,input,size_t(frames)*q->frameBytes);ma_pcm_rb_commit_write(&q->ring,frames);input+=size_t(frames)*q->frameBytes;bytes-=frames*q->frameBytes;
 if(!q->started&&ma_pcm_rb_available_read(&q->ring)>=12288){code=ma_device_start(&q->device);if(code!=MA_SUCCESS)return int(code);q->started=true;}}
 return 0;}
int pcm_queue_pause(void* p,int pause){static_cast<PCMQueue*>(p)->paused.store(pause!=0);return 0;}
int pcm_queue_volume(void* p,float volume){return int(ma_device_set_master_volume(&static_cast<PCMQueue*>(p)->device,volume));}
double pcm_queue_get_volume(void* p){float volume=0;return ma_device_get_master_volume(&static_cast<PCMQueue*>(p)->device,&volume)==MA_SUCCESS?volume:-1;}
double pcm_queue_position(void* p){auto q=static_cast<PCMQueue*>(p);return double(q->completed.load())/q->rate;}
double pcm_queue_hardware_rate(){return outputRate.load();}
int pcm_queue_start(void* p){auto q=static_cast<PCMQueue*>(p);if(q->started||q->paused.load())return 0;auto code=ma_device_start(&q->device);if(code==MA_SUCCESS)q->started=true;return int(code);}
void pcm_queue_abort(void* p){static_cast<PCMQueue*>(p)->aborted.store(true);}
double pcm_queue_close(void* p){if(!p)return 0;auto q=static_cast<PCMQueue*>(p);if(!q->aborted.load())q->paused.store(false);if(!q->aborted.load()&&!q->started&&ma_pcm_rb_available_read(&q->ring)){ma_device_start(&q->device);q->started=true;}auto start=std::chrono::steady_clock::now();while(!q->aborted.load()&&q->started&&ma_pcm_rb_available_read(&q->ring)&&std::chrono::steady_clock::now()-start<std::chrono::seconds(5))ma_sleep(1);ma_device_uninit(&q->device);double seconds=double(q->completed.load())/q->rate;ma_pcm_rb_uninit(&q->ring);delete q;return seconds;}
