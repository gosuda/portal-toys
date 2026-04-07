#ifndef UTILS_H
#define UTILS_H

/* Caller must release the returned buffer with cev_mem_free. */
char *read_file_content(const char *path);
void hash_password(const char *password, char *out_hash);

#endif
