#include "utils.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <openssl/sha.h>

/* Reads the entire content of a file into a dynamically allocated buffer.
   Used for loading templates or static assets. */
char *read_file_content(const char *path) {
    FILE *f = fopen(path, "rb");
    if (!f) return NULL;
    fseek(f, 0, SEEK_END);
    long len = ftell(f);
    fseek(f, 0, SEEK_SET);
    char *buf = malloc(len + 1);
    fread(buf, 1, len, f);
    buf[len] = '\0';
    fclose(f);
    return buf;
}

/* Hashes a password using SHA256. The output is a hex-encoded string.
   We use SHA256 to avoid storing plain-text passwords in the database. */
void hash_password(const char *password, char *out_hash) {
    unsigned char hash[SHA256_DIGEST_LENGTH];
    SHA256((unsigned char*)password, strlen(password), hash);
    for(int i = 0; i < SHA256_DIGEST_LENGTH; i++) {
        sprintf(out_hash + (i * 2), "%02x", hash[i]);
    }
    out_hash[SHA256_DIGEST_LENGTH * 2] = '\0';
}
