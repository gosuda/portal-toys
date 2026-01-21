# WebSocket API

The WebSocket module allows upgrading standard HTTP connections to persistent WebSocket connections.

## Functions

### `cwist_websocket_upgrade`
Upgrades an incoming HTTP request to a WebSocket connection.
```c
cwist_websocket *cwist_websocket_upgrade(cwist_http_request *req, int client_fd);
```
- Returns a `cwist_websocket` context on success, or `NULL` on failure.
- Automatically handles the handshake and sends the `101 Switching Protocols` response.
- **Ownership:** The returned pointer is owned by the caller and must be destroyed with `cwist_websocket_destroy`.

### `cwist_websocket_receive`
Receives a single WebSocket frame.
```c
cwist_ws_frame *cwist_websocket_receive(cwist_websocket *ws);
```
- Blocks until a frame is available.
- Returns `NULL` if the connection is closed or an error occurs.
- **Ownership:** The returned frame must be freed with `cwist_websocket_frame_destroy`.

### `cwist_websocket_send`
Sends data as a WebSocket frame.
```c
int cwist_websocket_send(cwist_websocket *ws, cwist_ws_opcode_t opcode, const uint8_t *data, size_t len);
```

## Opcode Types
- `CWIST_WS_FRAME_TEXT`: Text data (UTF-8)
- `CWIST_WS_FRAME_BINARY`: Binary data
- `CWIST_WS_FRAME_CLOSE`: Connection close
- `CWIST_WS_FRAME_PING` / `CWIST_WS_FRAME_PONG`: Heartbeat

## Integration with `cwist_app`
Use `cwist_app_ws` to register a WebSocket route.

```c
void my_ws_handler(cwist_websocket *ws) {
    // Handling logic
}

cwist_app_ws(app, "/chat", my_ws_handler);
```
