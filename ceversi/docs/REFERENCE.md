# CWIST Library Reference

CWIST is a custom C library for building secure, scalable web applications. It includes modules for string handling, HTTP/HTTPS, URI parsing, database access, and cryptographic hashing.

## Modules

### 1. SString (Smart String)
A safe, dynamic string implementation.
- **Header:** `<cwist/sstring.h>`
- **Features:** Automatic resizing, concatenation, safe manipulation.

### 2. HTTP Server
A multi-process/threaded HTTP server framework.
- **Header:** `<cwist/http.h>`
- **Structures:** `cwist_http_request`, `cwist_http_response`
- **Features:** 
  - Request parsing
  - Response serialization
  - Helper functions for headers/status codes.

### 3. HTTPS Support
Secure transport layer using OpenSSL.
- **Header:** `<cwist/https.h>`
- **Features:**
  - `cwist_https_init_context`: Loads Cert/Key.
  - `cwist_https_accept`: SSL Handshake.
  - `cwist_https_send_response`: Encrypted sending.
  - **Note:** Internally reuses `cwist/http.h` for logic, adding a security layer.

### 4. Query Parsing
Robust query string parsing using `liburiparser`.
- **Header:** `<cwist/query.h>`
- **Function:** `cwist_query_map_parse(map, raw_query)`
- **Behavior:** Parses `key=value&key2=val2` into a hash map (SipHash).

### 5. Database (SQL)
A wrapper around `sqlite3` for persistent storage.
- **Header:** `<cwist/sql.h>`
- **Features:**
  - `cwist_db_open`: Connect to DB file.
  - `cwist_db_exec`: Run commands (CREATE/INSERT/UPDATE).
  - `cwist_db_query`: Run SELECT and get results as `cJSON` array.

### 6. SipHash
Cryptographic hash function for hash maps (used in Query/Headers).
- **Header:** `<cwist/siphash.h>`

### 7. Error Handling
Unified error handling system using `cwist_error_t`.
- **Header:** `<cwist/err/cwist_err.h>`
- **Features:** Can return simple integer codes or complex JSON objects (e.g., OpenSSL/SQLite errors).

## Example: Othello Web
Located in `example/othello-web/`.
- **Stack:** HTML/CSS/JS (Frontend), C (Backend).
- **Features:**
  - HTTPS (port 31744).
  - Multiplayer (SQLite backed).
  - Rooms support (via `?room=ID`).
  - Strategic Hints (Frontend JS).

## Dependencies
- `libssl-dev` (OpenSSL)
- `libcjson-dev` (JSON)
- `liburiparser-dev` (URI Parsing)
- `libsqlite3-dev` (Database)

## Build
Run `make` to build the static library `libcwist.a`.
Run `make test` to run unit tests.
