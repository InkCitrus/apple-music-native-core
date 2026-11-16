#include "bridge.h"
#include "ALACDecoder.h"
#include "ALACBitUtilities.h"
#include <new>
void* alac_decoder_create(const void* cookie,uint32_t size){auto d=new(std::nothrow) ALACDecoder;if(!d)return nullptr;if(d->Init(const_cast<void*>(cookie),size)!=0){delete d;return nullptr;}return d;}
void alac_decoder_close(void* decoder){delete static_cast<ALACDecoder*>(decoder);}
int alac_decoder_decode(void* decoder,const void* packet,uint32_t size,void* pcm,uint32_t capacity,uint32_t* frames){auto d=static_cast<ALACDecoder*>(decoder);if(!d||!packet||!size||!pcm||!frames)return -1;uint64_t needed=uint64_t(d->mConfig.frameLength)*d->mConfig.numChannels*(d->mConfig.bitDepth/8);if(needed>capacity)return -1;BitBuffer bits;BitBufferInit(&bits,(unsigned char*)packet,size);return d->Decode(&bits,(unsigned char*)pcm,d->mConfig.frameLength,d->mConfig.numChannels,frames);}
