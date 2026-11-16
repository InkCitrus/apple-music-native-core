#pragma once
#include <stdint.h>
#ifdef __cplusplus
extern "C" {
#endif
void* alac_decoder_create(const void* cookie, uint32_t size);
void alac_decoder_close(void* decoder);
int alac_decoder_decode(void* decoder, const void* packet, uint32_t size, void* pcm, uint32_t capacity, uint32_t* frames);
#ifdef __cplusplus
}
#endif
