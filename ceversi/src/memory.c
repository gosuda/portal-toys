#include "memory.h"

#include <cjson/cJSON.h>
#include <string.h>
#include <stdio.h>

#include <ttak/mem/mem.h>
#include <ttak/timing/timing.h>

#define CEV_MEM_DEFAULT_TTL TT_MINUTE(10)
#define CEV_MEM_JSON_TTL TT_SECOND(30)
/* Strict mode in libttak currently corrupts bookkeeping headers, so stick to alignment only. */
#define CEV_MEM_FLAGS TTAK_MEM_CACHE_ALIGNED

static uint64_t cev_mem_now(void) {
    return ttak_get_tick_count_ns();
}

static void *cev_mem_alloc_internal(size_t size, uint64_t lifetime_ns, ttak_mem_flags_t flags) {
    if (size == 0) {
        return NULL;
    }
    uint64_t now = cev_mem_now();
    uint64_t ttl = lifetime_ns ? lifetime_ns : CEV_MEM_DEFAULT_TTL;
    void *ptr = ttak_mem_alloc_with_flags(size, ttl, now, flags);
    if (!ptr) {
        tt_autoclean_dirty_pointers(now);
        ptr = ttak_mem_alloc_with_flags(size, ttl, cev_mem_now(), flags);
    }
    if (!ptr) {
        fprintf(stderr, "[ceversi] libttak allocation failed (size=%zu)\n", size);
    }
    return ptr;
}

static void *cev_cjson_malloc(size_t size) {
    return cev_mem_alloc_internal(size, CEV_MEM_JSON_TTL, CEV_MEM_FLAGS);
}

static void cev_cjson_free(void *ptr) {
    if (!ptr) return;
    ttak_mem_free(ptr);
}

void cev_mem_bootstrap(void) {
    static int initialized = 0;
    if (initialized) return;
    cJSON_Hooks hooks = {
        .malloc_fn = cev_cjson_malloc,
        .free_fn = cev_cjson_free
    };
    cJSON_InitHooks(&hooks);
    initialized = 1;
}

void *cev_mem_alloc(size_t size) {
    return cev_mem_alloc_internal(size, CEV_MEM_DEFAULT_TTL, CEV_MEM_FLAGS);
}

void *cev_mem_alloc_ttl(size_t size, uint64_t lifetime_ns) {
    return cev_mem_alloc_internal(size, lifetime_ns, CEV_MEM_FLAGS);
}

char *cev_mem_strdup(const char *src) {
    if (!src) return NULL;
    size_t len = strlen(src) + 1;
    char *copy = cev_mem_alloc(len);
    if (!copy) return NULL;
    memcpy(copy, src, len);
    return copy;
}

void cev_mem_free(void *ptr) {
    if (!ptr) return;
    ttak_mem_free(ptr);
}

void cev_mem_collect(void) {
    tt_autoclean_dirty_pointers(cev_mem_now());
}
