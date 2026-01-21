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
    
    const char *schema = "CREATE TABLE IF NOT EXISTS games (room_id INTEGER PRIMARY KEY, board TEXT, turn INTEGER, status TEXT, players INTEGER, mode TEXT);";
    
    cwist_db_exec(db_conn, schema);
    
    // Primitive migration for existing tables
    cwist_db_exec(db_conn, "ALTER TABLE games ADD COLUMN mode TEXT;");
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
            "INSERT INTO games (room_id, board, turn, status, players, mode) VALUES (%d, '%s', %d, '%s', %d, '%s');",
            room_id, board_str, *turn, status, *players, mode);
        cwist_db_exec(db_conn, insert);
    } else {
        cJSON *row = cJSON_GetArrayItem(res, 0);
        cJSON *b = cJSON_GetObjectItem(row, "board");
        if(b && b->valuestring) deserialize_board(b->valuestring, board);
        
        cJSON *t = cJSON_GetObjectItem(row, "turn");
        if(t && t->valuestring) *turn = atoi(t->valuestring);
        else *turn = BLACK;

        cJSON *s = cJSON_GetObjectItem(row, "status");
        if(s && s->valuestring) strcpy(status, s->valuestring);
        else strcpy(status, "waiting");

        cJSON *p = cJSON_GetObjectItem(row, "players");
        if(p && p->valuestring) *players = atoi(p->valuestring);
        else *players = 0;

        cJSON *m = cJSON_GetObjectItem(row, "mode");
        if(m && m->valuestring) strcpy(mode, m->valuestring);
        else strcpy(mode, "othello");
    }
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
}

void update_game_state(int room_id, int board[SIZE][SIZE], int turn, const char *status, int players, const char *mode) {
    char board_str[1024];
    serialize_board(board, board_str);
    
    char sql[2048];
    snprintf(sql, sizeof(sql), 
        "UPDATE games SET board='%s', turn=%d, status='%s', players=%d, mode='%s' WHERE room_id=%d;",
        board_str, turn, status, players, mode, room_id);
    
    pthread_mutex_lock(&db_mutex);
    cwist_db_exec(db_conn, sql);
    pthread_mutex_unlock(&db_mutex);
}
