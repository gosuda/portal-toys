# Framework & App API

*Header:* `<cwist/app.h>` (Proposed)

High-level abstractions for building web applications quickly.

### `cwist_app_create`
```c
cwist_app *cwist_app_create(void);
```
Initializes a new web application instance with default security settings.

### `cwist_app_use_https`
```c
cwist_error_t cwist_app_use_https(cwist_app *app, const char *cert_path, const char *key_path);
```
Enables HTTPS for the application.

## Routing

### `cwist_app_get` / `cwist_app_post`
Registers standard HTTP handlers. Supports path parameters using `:name` syntax.

```c
void user_handler(cwist_http_request *req, cwist_http_response *res) {
    char *user_id = cwist_query_map_get(req->path_params, "id");
    // ...
}

cwist_app_get(app, "/users/:id", user_handler);
```


### `cwist_app_ws`
Registers a WebSocket handler.
```c
void cwist_app_ws(cwist_app *app, const char *path, cwist_ws_handler_func handler);
```

## Error Handling

### `cwist_app_set_error_handler`
Registers a global error handler for status codes >= 400 (e.g., 404 Not Found).
```c
void cwist_app_set_error_handler(cwist_app *app, cwist_error_handler_func handler);
```

### Unused Variables
Use the `CWIST_UNUSED()` macro to suppress compiler warnings for unused handler parameters.
```c
void my_handler(cwist_http_request *req, cwist_http_response *res) {
    CWIST_UNUSED(req);
    cwist_sstring_assign(res->body, "Hello");
}
```

## Concurrency & Thread-Safety

- **Multithreading**: `cwist_app_listen` starts a multithreaded server (one thread per request). Handlers and middleware MUST be thread-safe.
- **Global State**: Avoid using global variables in your handlers. If you need shared state, ensure it is protected by appropriate synchronization primitives (e.g., `pthread_mutex_t`).
- **Context Passing**: Custom middleware and handlers operate within the context of a single request. Use `req->private_data` if you need to pass data between middlewares.

## Memory Ownership Rules

- **Framework-Owned**: `cwist_http_request` and `cwist_http_response` objects passed to handlers are owned by the framework. Do NOT destroy them inside the handler.
- **Borrowed Strings**: Strings returned by `cwist_http_header_get` or `cwist_json_get_raw` are borrowed. If you need to keep them after the builder or request is destroyed, you must copy them.
- **Explicit Destruction**: Objects created with `_create` (e.g., `cwist_json_builder_create`, `cwist_websocket_upgrade`) MUST be destroyed by the caller using the corresponding `_destroy` function.

### `cwist_app_listen`
```c
int cwist_app_listen(cwist_app *app, int port);
```
Starts the server loop on the specified port.
