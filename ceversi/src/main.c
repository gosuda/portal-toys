#define _GNU_SOURCE
#include <cwist/https.h>
#include <cwist/http.h>
#include <cwist/sql.h>
#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <signal.h>
#include <errno.h>

#include "common.h"
#include "db.h"
#include "handlers.h"
#include "utils.h"
#include <pthread.h>

#define PORT 31744
#define MAX_REQUEST_SIZE (1024 * 1024) // 1MB limit

void *cleanup_thread(void *arg) {
    (void)arg;
    while(1) {
        sleep(60);
        cleanup_stale_rooms();
    }
    return NULL;
}

void handle_client_https(cwist_https_connection *conn, void *ctx) {
    (void)ctx;
    cwist_http_request *req = cwist_https_receive_request(conn);
    if (!req) return;

    cwist_http_response *res = cwist_http_response_create();
    handle_request(req, res);

    cwist_https_send_response(conn, res);
    cwist_http_response_destroy(res);
    cwist_http_request_destroy(req);
}

static char* read_http_request(int client_fd) {
    size_t capacity = 4096;
    size_t total_read = 0;
    char *buffer = malloc(capacity);
    if (!buffer) return NULL;

    while (1) {
        ssize_t n = recv(client_fd, buffer + total_read, capacity - total_read - 1, 0);
        if (n <= 0) {
            if (n < 0 && (errno == EAGAIN || errno == EINTR)) continue;
            free(buffer);
            return NULL;
        }
        total_read += n;
        buffer[total_read] = '\0';

        char *header_end = strstr(buffer, "\r\n\r\n");
        if (header_end) {
            // Check for Content-Length
            char *cl_str = strcasestr(buffer, "Content-Length:");
            if (cl_str) {
                long content_length = atol(cl_str + 15);
                size_t header_len = (header_end - buffer) + 4;
                size_t body_len = total_read - header_len;

                if (header_len + content_length > MAX_REQUEST_SIZE) {
                    free(buffer);
                    return NULL;
                }

                if (content_length > (long)body_len) {
                    capacity = header_len + content_length + 1;
                    buffer = realloc(buffer, capacity);
                    if (!buffer) return NULL;

                    while (total_read < header_len + content_length) {
                        n = recv(client_fd, buffer + total_read, header_len + content_length - total_read, 0);
                        if (n <= 0) {
                            if (n < 0 && (errno == EAGAIN || errno == EINTR)) continue;
                            free(buffer);
                            return NULL;
                        }
                        total_read += n;
                    }
                    buffer[total_read] = '\0';
                }
            }
            return buffer;
        }

        if (total_read >= capacity - 1) {
            if (capacity >= MAX_REQUEST_SIZE) {
                free(buffer);
                return NULL;
            }
            capacity *= 2;
            char *new_buf = realloc(buffer, capacity);
            if (!new_buf) {
                free(buffer);
                return NULL;
            }
            buffer = new_buf;
        }
    }
}

void handle_client_http(int client_fd, void *ctx) {
    (void)ctx;
    char *request_raw = read_http_request(client_fd);
    if (!request_raw) {
        close(client_fd);
        return;
    }

    cwist_http_request *req = cwist_http_parse_request(request_raw);
    if (!req) {
        free(request_raw);
        close(client_fd);
        return;
    }
    
    // Manually populate body if library didn't
    if (!req->body->data || strlen(req->body->data) == 0) {
        char *body_start = strstr(request_raw, "\r\n\r\n");
        if (body_start) {
            body_start += 4;
            if (strlen(body_start) > 0) {
                cwist_sstring_assign(req->body, body_start);
            }
        }
    }
    free(request_raw);

    cwist_http_response *res = cwist_http_response_create();
    handle_request(req, res);

    // Ensure connection is closed if not keep-alive (simplified)
    cwist_http_header_add(&res->headers, "Connection", "close");

    cwist_http_send_response(client_fd, res);
    cwist_http_response_destroy(res);
    cwist_http_request_destroy(req);
    close(client_fd);
}

int main(int argc, char **argv) {
    int use_https = 1;
    int port = PORT;
    char *port_env = getenv("PORT");
    if (port_env) {
        port = atoi(port_env);
    }

    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "--no-certs") == 0) {
            use_https = 0;
        }
    }

    signal(SIGPIPE, SIG_IGN);

    init_db();

    pthread_t tid;
    pthread_create(&tid, NULL, cleanup_thread, NULL);
    pthread_detach(tid);

    struct sockaddr_in addr;
    int server_fd = cwist_make_socket_ipv4(&addr, "0.0.0.0", port, 10);
    
    if (server_fd < 0) {
        printf("Socket creation failed: %d\n", server_fd);
        cwist_db_close(db_conn);
        return 1;
    }

    int opt = 1;
    setsockopt(server_fd, SOL_SOCKET, SO_REUSEADDR, &opt, sizeof(opt));

    if (use_https) {
        cwist_https_context *ctx = NULL;
        cwist_error_t err = cwist_https_init_context(&ctx, "server.crt", "server.key");
        
        if (err.errtype == CWIST_ERR_JSON) {
            printf("Init Failed: %s\n", cJSON_GetStringValue(cJSON_GetObjectItem(err.error.err_json, "openssl_error")));
            close(server_fd);
            cwist_db_close(db_conn);
            return 1;
        }

        printf("Starting HTTPS Othello Server on port %d...\n", port);
        cwist_https_server_loop(server_fd, ctx, handle_client_https, NULL);
        cwist_https_destroy_context(ctx);
    } else {
        printf("Starting HTTP Othello Server on port %d...\n", port);
        cwist_server_config config = { .use_threading = true };
        cwist_http_server_loop(server_fd, &config, handle_client_http, NULL);
    }

    close(server_fd);
    cwist_db_close(db_conn);
    return 0;
}
