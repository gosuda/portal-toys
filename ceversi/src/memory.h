#ifndef MEMORY_H
#define MEMORY_H

#include <stddef.h>
#include <stdint.h>

/* Initializes the libttak-backed memory hooks (idempotent). */
void cev_mem_bootstrap(void);

/* Strict, tracked allocation helpers. */
void *cev_mem_alloc(size_t size);
void *cev_mem_alloc_ttl(size_t size, uint64_t lifetime_ns);
char *cev_mem_strdup(const char *src);

/* Frees memory previously returned from the helpers or libttak-backed cJSON. */
void cev_mem_free(void *ptr);

/* Trigger background collection of expired pointers. */
void cev_mem_collect(void);

#endif /* MEMORY_H */
