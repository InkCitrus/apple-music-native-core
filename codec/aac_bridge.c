//go:build amcore_aac

#include "aac_bridge.h"
#include <stdlib.h>
#include <string.h>
#include "ixheaac_type_def.h"
#include "ixheaacd_apicmd_standards.h"
#include "ixheaacd_memory_standards.h"
#include "ixheaacd_aac_config.h"
#include "ixheaacd_error_handler.h"

extern IA_ERRORCODE ixheaacd_dec_api(void *, WORD32, WORD32, void *);

typedef struct {
    void *api, *tables, *blocks[8];
    uint8_t *input, *output;
    uint32_t input_size, output_size, ready;
} AAC;

static int call(AAC *d, WORD32 cmd, WORD32 index, void *value) {
    return ixheaacd_dec_api(d->api, cmd, index, value);
}

static int set_input(AAC *d, const uint8_t *input, uint32_t length) {
    if (!length || length > d->input_size) return -1;
    memcpy(d->input, input, length);
    memset(d->input + length, 0, 32);
    return call(d, IA_API_CMD_SET_INPUT_BYTES, 0, &length);
}

void amcore_aac_close(void *decoder) {
    AAC *d = decoder;
    if (!d) return;
    for (size_t i = 0; i < sizeof(d->blocks) / sizeof(d->blocks[0]); i++) free(d->blocks[i]);
    free(d->tables);
    free(d->api);
    free(d);
}

void *amcore_aac_open(const uint8_t *config, uint32_t length, int *reason) {
    uint32_t size = 0, count = 0, raw = 1, max_channels = 2, bits = 16;
    AAC *d = calloc(1, sizeof(*d));
    *reason = -1;
    if (!d) return NULL;
    if (call(d, IA_API_CMD_GET_API_SIZE, 0, &size) || !size || size > 4U << 20) goto fail;
    *reason = -2;
    d->api = calloc(1, size);
    // Keep the decoder from changing playback dynamics.
    if (!d->api ||
        call(d, IA_API_CMD_INIT, IA_CMD_TYPE_INIT_API_PRE_CONFIG_PARAMS, NULL) ||
        call(d, IA_API_CMD_SET_CONFIG_PARAM, IA_XHEAAC_DEC_CONFIG_PARAM_MP4FLAG, &raw) ||
        call(d, IA_API_CMD_SET_CONFIG_PARAM, IA_XHEAAC_DEC_CONFIG_PARAM_PEAK_LIMITER, &raw) ||
        call(d, IA_API_CMD_SET_CONFIG_PARAM, IA_ENHAACPLUS_DEC_CONFIG_PARAM_MAX_CHANNEL, &max_channels) ||
        call(d, IA_API_CMD_SET_CONFIG_PARAM, IA_XHEAAC_DEC_CONFIG_PARAM_PCM_WDSZ, &bits) ||
        call(d, IA_API_CMD_GET_MEMTABS_SIZE, 0, &size) || !size || size > 1U << 20) goto fail;
    d->tables = calloc(1, size);
    *reason = -3;
    if (!d->tables || call(d, IA_API_CMD_SET_MEMTABS_PTR, 0, d->tables) ||
        call(d, IA_API_CMD_INIT, IA_CMD_TYPE_INIT_API_POST_CONFIG_PARAMS, NULL) ||
        call(d, IA_API_CMD_GET_N_MEMTABS, 0, &count) || !count || count > sizeof(d->blocks) / sizeof(d->blocks[0])) goto fail;
    for (uint32_t i = 0; i < count; i++) {
        *reason = -4 - i;
        uint32_t align = 0, type = 0;
        if (call(d, IA_API_CMD_GET_MEM_INFO_SIZE, i, &size) ||
            call(d, IA_API_CMD_GET_MEM_INFO_ALIGNMENT, i, &align) ||
            call(d, IA_API_CMD_GET_MEM_INFO_TYPE, i, &type) ||
            !size || size > 16U << 20 || !align || align > 4096 || (align & (align - 1))) goto fail;
        d->blocks[i] = calloc(1, (size_t)size + align + 32);
        if (!d->blocks[i]) goto fail;
        void *aligned = (void *)(((uintptr_t)d->blocks[i] + align - 1) & ~(uintptr_t)(align - 1));
        if (call(d, IA_API_CMD_SET_MEM_PTR, i, aligned)) goto fail;
        if (type == IA_MEMTYPE_INPUT) { d->input = aligned; d->input_size = size; }
        if (type == IA_MEMTYPE_OUTPUT) { d->output = aligned; d->output_size = size; }
    }
    *reason = -20;
    if (!d->input || !d->output || set_input(d, config, length)) goto fail;
    *reason = call(d, IA_API_CMD_INIT, IA_CMD_TYPE_INIT_PROCESS, NULL);
    if (*reason) goto fail;
    *reason = call(d, IA_API_CMD_INIT, IA_CMD_TYPE_INIT_DONE_QUERY, &d->ready);
    if (*reason) goto fail;
    return d;
fail:
    amcore_aac_close(d);
    return NULL;
}

int amcore_aac_decode(void *decoder, const uint8_t *packet, uint32_t length,
                      uint8_t *pcm, uint32_t capacity, uint32_t *rate, uint32_t *channels) {
    AAC *d = decoder;
    uint32_t bytes = 0;
    if (set_input(d, packet, length)) return -1;
    if (!d->ready && (call(d, IA_API_CMD_INIT, IA_CMD_TYPE_INIT_PROCESS, NULL) ||
                     call(d, IA_API_CMD_INIT, IA_CMD_TYPE_INIT_DONE_QUERY, &d->ready) || !d->ready ||
                     set_input(d, packet, length))) return -2;
    if (call(d, IA_API_CMD_EXECUTE, IA_CMD_TYPE_DO_EXECUTE, NULL) ||
        call(d, IA_API_CMD_GET_CONFIG_PARAM, IA_XHEAAC_DEC_CONFIG_PARAM_SAMP_FREQ, rate) ||
        call(d, IA_API_CMD_GET_CONFIG_PARAM, IA_XHEAAC_DEC_CONFIG_PARAM_NUM_CHANNELS, channels) ||
        call(d, IA_API_CMD_GET_OUTPUT_BYTES, 0, &bytes)) return -3;
    if (bytes > capacity || bytes > d->output_size) return -6;
    memcpy(pcm, d->output, bytes);
    return (int)bytes;
}
