# JSON Builder API

The JSON Builder provides a lightweight way to construct JSON strings without the overhead of a full DOM parser.

## Functions

### `cwist_json_builder_create`
```c
cwist_json_builder *cwist_json_builder_create(void);
```

### `cwist_json_begin_object` / `cwist_json_end_object`
Starts and ends a JSON object `{ ... }`.

### `cwist_json_add_string`
Adds a key-value pair where the value is a string.
```c
void cwist_json_add_string(cwist_json_builder *b, const char *key, const char *value);
```

### `cwist_json_get_raw`
Returns the constructed JSON string as a `const char *`.
```c
const char *cwist_json_get_raw(cwist_json_builder *b);
```
- **Ownership:** The string is owned by the builder and remains valid until the builder is destroyed. Do NOT free this pointer.

## Example
```c
cwist_json_builder *jb = cwist_json_builder_create();
cwist_json_begin_object(jb);
cwist_json_add_string(jb, "status", "success");
cwist_json_add_int(jb, "code", 200);
cwist_json_end_object(jb);

printf("%s\n", cwist_json_get_raw(jb));

cwist_json_builder_destroy(jb);
```

