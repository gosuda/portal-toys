# Database API

*Header:* `<cwist/sql.h>`

Wrapper for SQLite3 database operations.

### `cwist_db_open`
```c
cwist_error_t cwist_db_open(cwist_db **db, const char *path);
```
Opens a connection to a SQLite database file.

### `cwist_db_exec`
```c
cwist_error_t cwist_db_exec(cwist_db *db, const char *sql);
```
Executes a non-query SQL command (INSERT, UPDATE, DELETE).

### `cwist_db_query`
```c
cwist_error_t cwist_db_query(cwist_db *db, const char *sql, cJSON **result);
```
Executes a SELECT query. `result` is populated with a cJSON Array of Objects.
