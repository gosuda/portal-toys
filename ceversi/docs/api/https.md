# HTTPS API

*Header:* `<cwist/https.h>`

Secure transport layer utilizing OpenSSL.

### `cwist_https_init_context`
```c
cwist_error_t cwist_https_init_context(cwist_https_context **ctx, const char *cert_path, const char *key_path);
```
Initializes OpenSSL context and loads certificates.

### `cwist_https_accept`
```c
cwist_error_t cwist_https_accept(cwist_https_context *ctx, int client_fd, cwist_https_connection **conn);
```
Performs the SSL handshake on an accepted TCP socket.

### `cwist_https_send_response`
```c
cwist_error_t cwist_https_send_response(cwist_https_connection *conn, cwist_http_response *res);
```
Serializes and encrypts the response, sending it to the client.
