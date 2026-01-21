# CWIST API Reference

## Error Handling

### `make_error`
- `cwist_error_t make_error(cwist_errtype_t type)`
- Creates a `cwist_error_t` with the requested error type.

## SString (`cwist_sstring`)

### Lifecycle
- `cwist_sstring *cwist_sstring_create(void)`
- `void cwist_sstring_destroy(cwist_sstring *str)`
- `cwist_error_t cwist_sstring_init(cwist_sstring *str)`

### Core helpers
- `size_t cwist_sstring_get_size(cwist_sstring *str)`
- `cwist_error_t cwist_sstring_change_size(cwist_sstring *str, size_t size, bool blow_data)`
- `cwist_error_t cwist_sstring_assign(cwist_sstring *str, char *data)`

### Trimming
- `cwist_error_t cwist_sstring_ltrim(cwist_sstring *str)`
- `cwist_error_t cwist_sstring_rtrim(cwist_sstring *str)`
- `cwist_error_t cwist_sstring_trim(cwist_sstring *str)`

### Append / Copy
- `cwist_error_t cwist_sstring_append(cwist_sstring *str, const char *data)`
- `cwist_error_t cwist_sstring_append_sstring(cwist_sstring *str, const cwist_sstring *from)`
- `cwist_error_t cwist_sstring_copy(cwist_sstring *origin, char *destination)`
- `cwist_error_t cwist_sstring_copy_sstring(cwist_sstring *origin, const cwist_sstring *from)`

### Compare / Query
- `int cwist_sstring_compare(cwist_sstring *str, const char *compare_to)`
- `int cwist_sstring_compare_sstring(cwist_sstring *left, const cwist_sstring *right)`
- `cwist_error_t cwist_sstring_seek(cwist_sstring *str, char *substr, int location)`
- `cwist_sstring *cwist_sstring_substr(cwist_sstring *str, int start, int length)`

## HTTP

### Request lifecycle
- `cwist_http_request *cwist_http_request_create(void)`
- `void cwist_http_request_destroy(cwist_http_request *req)`
- `cwist_http_request *cwist_http_parse_request(const char *raw_request)`

### Response lifecycle
- `cwist_http_response *cwist_http_response_create(void)`
- `void cwist_http_response_destroy(cwist_http_response *res)`
- `cwist_error_t cwist_http_send_response(int client_fd, cwist_http_response *res)`

### Header helpers
- `cwist_error_t cwist_http_header_add(cwist_http_header_node **head, const char *key, const char *value)`
- `char *cwist_http_header_get(cwist_http_header_node *head, const char *key)`
- `void cwist_http_header_free_all(cwist_http_header_node *head)`

### Method helpers
- `const char *cwist_http_method_to_string(cwist_http_method_t method)`
- `cwist_http_method_t cwist_http_string_to_method(const char *method_str)`

### Socket helpers
- `int cwist_make_socket_ipv4(struct sockaddr_in *sockv4, const char *address, uint16_t port, uint16_t backlog)`
- `cwist_error_t cwist_accept_socket(int server_fd, struct sockaddr *sockv4, void (*handler_func)(int client_fd))`
- `cwist_error_t cwist_http_server_loop(int server_fd, cwist_server_config *config, void (*handler)(int))`

## Session manager

### Arena
- `void session_arena_init(struct session_arena *arena, uint8_t *buffer, size_t capacity)`
- `void *session_arena_alloc(struct session_arena *arena, size_t size)`
- `void session_arena_reset(struct session_arena *arena)`

### Shared sessions (intrusive ref count)
- `void session_rc_init(struct session_rc_header *header, void (*destructor)(void *))`
- `void *session_shared_alloc(size_t payload_size, void (*destructor)(void *))`
- `void session_shared_inc(void *payload)`
- `void session_shared_dec(void *payload)`

### Manager
- `void session_manager_init(struct session_manager *manager, uint8_t *buffer, size_t capacity)`
- `void session_manager_reset(struct session_manager *manager)`
