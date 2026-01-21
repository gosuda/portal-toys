#include "db.h"
#include <cwist/sql.h>
#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>

cwist_db *db_conn = NULL;
static pthread_mutex_t db_mutex = PTHREAD_MUTEX_INITIALIZER;

void init_db() {
    pthread_mutex_lock(&db_mutex);
    cwist_db_open(&db_conn, "othello.db");
    
    const char *schema = "CREATE TABLE IF NOT EXISTS games (room_id INTEGER PRIMARY KEY, board TEXT, turn INTEGER, status TEXT, players INTEGER, mode TEXT, last_activity DATETIME DEFAULT CURRENT_TIMESTAMP);";
    
    cwist_db_exec(db_conn, schema);
    
    // Primitive migration for existing tables
    cwist_db_exec(db_conn, "ALTER TABLE games ADD COLUMN mode TEXT;");
    cwist_db_exec(db_conn, "ALTER TABLE games ADD COLUMN last_activity DATETIME DEFAULT CURRENT_TIMESTAMP;");
    pthread_mutex_unlock(&db_mutex);
}

static void update_activity(int room_id) {
    char sql[128];
    snprintf(sql, sizeof(sql), "UPDATE games SET last_activity = CURRENT_TIMESTAMP WHERE room_id = %d;", room_id);
    cwist_db_exec(db_conn, sql);
}

void cleanup_stale_rooms() {
    pthread_mutex_lock(&db_mutex);
    // 5 minutes = 300 seconds
    cwist_db_exec(db_conn, "DELETE FROM games WHERE last_activity < datetime('now', '-5 minutes');");
    pthread_mutex_unlock(&db_mutex);
}

// Helper to serialize board to CSV string
static void serialize_board(int board[SIZE][SIZE], char *out) {
    out[0] = '\0';
    char buf[16];
    for (int r=0; r<SIZE; r++) {
        for (int c=0; c<SIZE; c++) {
            sprintf(buf, "%d,", board[r][c]);
            strcat(out, buf);
        }
    }
    // remove last comma
    if(strlen(out) > 0) out[strlen(out)-1] = '\0';
}

// Helper to deserialize
static void deserialize_board(const char *in, int board[SIZE][SIZE]) {
    if (!in) return;
    char *dup = strdup(in);
    char *token = strtok(dup, ",");
    int r=0, c=0;
    while (token) {
        board[r][c] = atoi(token);
        c++;
        if (c >= SIZE) { c=0; r++; }
        token = strtok(NULL, ",");
    }
    free(dup);
}

void get_game_state(int room_id, int board[SIZE][SIZE], int *turn, char *status, int *players, char *mode, const char *requested_mode) {
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT board, turn, status, players, mode FROM games WHERE room_id = %d;", room_id);
    
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db_conn, sql, &res);
    
    if (cJSON_GetArraySize(res) == 0) {
        // Create new game
        memset(board, 0, sizeof(int)*SIZE*SIZE);
        
        if (requested_mode && strcmp(requested_mode, "reversi") == 0) {
             strcpy(mode, "reversi");
             // Reversi starts empty
        } else {
             strcpy(mode, "othello");
             board[3][3] = WHITE; board[3][4] = BLACK;
             board[4][3] = BLACK; board[4][4] = WHITE;
        }

        *turn = BLACK;
        strcpy(status, "waiting");
        *players = 0;
        
        char board_str[1024];
        serialize_board(board, board_str);
        
        char insert[2048];
        snprintf(insert, sizeof(insert), 
            "INSERT INTO games (room_id, board, turn, status, players, mode, last_activity) VALUES (%d, '%s', %d, '%s', %d, '%s', CURRENT_TIMESTAMP);",
            room_id, board_str, *turn, status, *players, mode);
        cwist_db_exec(db_conn, insert);
    } else {
        cJSON *row = cJSON_GetArrayItem(res, 0);
        cJSON *b = cJSON_GetObjectItem(row, "board");
        memset(board, 0, sizeof(int)*SIZE*SIZE);
        if(b && b->valuestring) deserialize_board(b->valuestring, board);
        
        cJSON *t = cJSON_GetObjectItem(row, "turn");
        if (t) {
            if (t->valuestring) *turn = atoi(t->valuestring);
            else if (cJSON_IsNumber(t)) *turn = t->valueint;
            else *turn = BLACK;
        } else *turn = BLACK;

        cJSON *s = cJSON_GetObjectItem(row, "status");
        if(s && s->valuestring) strcpy(status, s->valuestring);
        else strcpy(status, "waiting");

        cJSON *p = cJSON_GetObjectItem(row, "players");
        if (p) {
            if (p->valuestring) *players = atoi(p->valuestring);
            else if (cJSON_IsNumber(p)) *players = p->valueint;
            else *players = 0;
        } else *players = 0;

        cJSON *m = cJSON_GetObjectItem(row, "mode");
        if(m && m->valuestring) strcpy(mode, m->valuestring);
        else strcpy(mode, "othello");
        
        update_activity(room_id);
    }
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
}

void update_game_state(int room_id, int board[SIZE][SIZE], int turn, const char *status, int players, const char *mode) {
    char board_str[1024];
    serialize_board(board, board_str);
    
    char sql[2048];
    snprintf(sql, sizeof(sql), 
        "UPDATE games SET board='%s', turn=%d, status='%s', players=%d, mode='%s', last_activity=CURRENT_TIMESTAMP WHERE room_id=%d;",
        board_str, turn, status, players, mode, room_id);
    
    pthread_mutex_lock(&db_mutex);
    cwist_db_exec(db_conn, sql);
    pthread_mutex_unlock(&db_mutex);
}

int db_join_game(int room_id, const char *requested_mode, int *player_id, char *mode) {
    int board[SIZE][SIZE];
    int turn, players;
    char status[32];
    
    // We reuse get_game_state but we need to call it under a new mutex lock or modify it.
    // Since get_game_state already locks db_mutex, we can't call it here if we want atomicity for the whole join process.
    // So we'll implement it manually here or refactor get_game_state.
    
    pthread_mutex_lock(&db_mutex);
    
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT board, turn, status, players, mode FROM games WHERE room_id = %d;", room_id);
    
    cJSON *res = NULL;
    cwist_db_query(db_conn, sql, &res);
    
    if (cJSON_GetArraySize(res) == 0) {
        // Create new game
        memset(board, 0, sizeof(int)*SIZE*SIZE);
        if (requested_mode && strcmp(requested_mode, "reversi") == 0) {
             strcpy(mode, "reversi");
        } else {
             strcpy(mode, "othello");
             board[3][3] = WHITE; board[3][4] = BLACK;
             board[4][3] = BLACK; board[4][4] = WHITE;
        }
        turn = BLACK;
        strcpy(status, "waiting");
        players = 1;
        *player_id = 1;

        char board_str[1024];
        serialize_board(board, board_str);
        char insert[2048];
        snprintf(insert, sizeof(insert), 
            "INSERT INTO games (room_id, board, turn, status, players, mode, last_activity) VALUES (%d, '%s', %d, '%s', %d, '%s', CURRENT_TIMESTAMP);",
            room_id, board_str, turn, status, players, mode);
        cwist_db_exec(db_conn, insert);
    } else {
        cJSON *row = cJSON_GetArrayItem(res, 0);
        cJSON *p = cJSON_GetObjectItem(row, "players");
        int current_players = 0;
        if (p) {
            if (p->valuestring) current_players = atoi(p->valuestring);
            else if (cJSON_IsNumber(p)) current_players = p->valueint;
        }

        if (current_players >= 2) {
            cJSON_Delete(res);
            pthread_mutex_unlock(&db_mutex);
            return -1; // Full
        }

        current_players++;
        *player_id = current_players;
        
        cJSON *m = cJSON_GetObjectItem(row, "mode");
        if(m && m->valuestring) strcpy(mode, m->valuestring);
        else strcpy(mode, "othello");

        const char *new_status = (current_players == 2) ? "active" : "waiting";
        
        char update[256];
        snprintf(update, sizeof(update), "UPDATE games SET players=%d, status='%s', last_activity=CURRENT_TIMESTAMP WHERE room_id=%d;", current_players, new_status, room_id);
        cwist_db_exec(db_conn, update);
    }
    
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
    return 0;
}
