#define _GNU_SOURCE
#include <cwist/app.h>
#include <cwist/http.h>
#include <cwist/sql.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <signal.h>
#include <pthread.h>

#include "common.h"
#include "db.h"
#include "handlers.h"
#include "utils.h"

#define PORT 31744

void *cleanup_thread(void *arg) {
    cwist_db *db = (cwist_db *)arg;
    while(1) {
        sleep(60);
        cleanup_stale_rooms(db);
    }
    return NULL;
}

int main(int argc, char **argv) {
    int port = PORT;
    char *port_env = getenv("PORT");
    if (port_env) {
        port = atoi(port_env);
    }

    int use_https = 1;
    for (int i = 1; i < argc; i++) {
        if (strcmp(argv[i], "--no-certs") == 0) {
            use_https = 0;
        }
    }

    signal(SIGPIPE, SIG_IGN);

    cwist_app *app = cwist_app_create();
    if (!app) {
        fprintf(stderr, "Failed to create cwist app\n");
        return 1;
    }

    if (use_https) {
        cwist_app_use_https(app, "server.crt", "server.key");
    }

    cwist_app_use_db(app, "othello.db");
    
    cwist_db *db = cwist_app_get_db(app);
    init_db(db); 

    pthread_t tid;
    pthread_create(&tid, NULL, cleanup_thread, db);
    pthread_detach(tid);

    // Static assets
    cwist_app_static(app, "/", "./"); 

    // API Routes
    cwist_app_post(app, "/join", join_handler);
    cwist_app_get(app, "/state", state_handler);
    cwist_app_post(app, "/move", move_handler);

    printf("Starting %s Othello Server on port %d...\n", use_https ? "HTTPS" : "HTTP", port);
    
    return cwist_app_listen(app, port);
}