#pragma once
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
void* pcm_queue_new(uint32_t rate,uint32_t channels,uint32_t bits,float volume);
int pcm_queue_write(void* queue,const void* data,uint32_t bytes);
int pcm_queue_pause(void* queue,int pause);
int pcm_queue_volume(void* queue,float volume);
double pcm_queue_position(void* queue);
double pcm_queue_hardware_rate();
int pcm_queue_start(void* queue);
void pcm_queue_abort(void* queue);
double pcm_queue_close(void* queue);
double pcm_queue_get_volume(void* queue);
void pcm_queue_stats(void* queue,uint64_t* underruns,uint64_t* queued,uint64_t* capacity);
#ifdef __cplusplus
}
#endif
