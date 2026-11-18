#ifndef AMCORE_AAC_BRIDGE_H
#define AMCORE_AAC_BRIDGE_H

#include <stdint.h>

void *amcore_aac_open(const uint8_t *config, uint32_t length, int *reason);
void amcore_aac_close(void *decoder);
int amcore_aac_decode(void *decoder, const uint8_t *packet, uint32_t length,
                      uint8_t *pcm, uint32_t capacity, uint32_t *rate, uint32_t *channels);

#endif
