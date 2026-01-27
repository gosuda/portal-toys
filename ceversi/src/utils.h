#ifndef UTILS_H
#define UTILS_H

char *read_file_content(const char *path);
void hash_password(const char *password, char *out_hash);

#endif
