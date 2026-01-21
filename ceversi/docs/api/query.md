# Query & URI API

*Header:* `<cwist/query.h>`

Advanced query string parsing using `liburiparser`.

### `cwist_query_map_parse`
```c
void cwist_query_map_parse(cwist_query_map *map, const char *raw_query);
```
Parses a query string (e.g., `a=1&b=2`) into the map using `liburiparser`.

### `cwist_query_map_get`
```c
const char *cwist_query_map_get(cwist_query_map *map, const char *key);
```
Retrieves a query parameter value in O(1) average time (SipHash).
