# HTTP Core API

*Header:* `<cwist/http.h>`

Low-level HTTP structures and parsing logic.

### `cwist_http_request_create`
```c
cwist_http_request *cwist_http_request_create(void);
```
Allocates a new HTTP request structure.

### `cwist_http_parse_request`
```c
cwist_http_request *cwist_http_parse_request(const char *raw_request);
```
Parses a raw HTTP string into a `cwist_http_request` object.

### `cwist_http_response_create`
```c
cwist_http_response *cwist_http_response_create(void);
```
Allocates a new HTTP response structure. Default status is 200 OK.

### `cwist_http_header_add`
```c
cwist_error_t cwist_http_header_add(cwist_http_header_node **head, const char *key, const char *value);
```
Adds a header to the linked list.

### `cwist_http_header_get`
```c
char *cwist_http_header_get(cwist_http_header_node *head, const char *key);
```
Retrieves the value of a header by key. Returns `NULL` if not found.

## Server Core

### `cwist_http_server_loop`
```c
cwist_error_t cwist_http_server_loop(int server_fd, cwist_server_config *config, void (*handler)(int, void *), void *ctx);
```
Starts the main server loop. 
- Supports iterative, forking, and multithreaded models via `config`.
- `ctx` is passed to the handler for thread-safe state management.

### `cwist_accept_socket`
```c
cwist_error_t cwist_accept_socket(int server_fd, struct sockaddr *sockv4, void (*handler_func)(int, void *), void *ctx);
```
Low-level accept loop wrapper.

