#include "db.h"
#include <cwist/core/db/sql.h>
#include <cwist/sys/err/cwist_err.h>
#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>

cwist_db *db_conn = NULL;
static pthread_mutex_t db_mutex = PTHREAD_MUTEX_INITIALIZER;

/* Initializes the database schema. Creates 'games' and 'users' tables if they don't exist.
   Also includes rudimentary migrations for adding user-related columns to older DBs. */
void init_db(cwist_db *db) {
    pthread_mutex_lock(&db_mutex);
    cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS games (room_id INTEGER PRIMARY KEY, board TEXT, turn INTEGER, status TEXT, players INTEGER, mode TEXT, user1_id INTEGER DEFAULT 0, user2_id INTEGER DEFAULT 0, last_activity DATETIME DEFAULT CURRENT_TIMESTAMP);");
    cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE, password_hash TEXT, wins INTEGER DEFAULT 0, losses INTEGER DEFAULT 0, ties INTEGER DEFAULT 0);");
    
    // Migrations for existing DBs
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN mode TEXT;");
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN last_activity DATETIME DEFAULT CURRENT_TIMESTAMP;");
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN user1_id INTEGER DEFAULT 0;");
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN user2_id INTEGER DEFAULT 0;");
    pthread_mutex_unlock(&db_mutex);
}

void cleanup_stale_rooms(cwist_db *db) {
    pthread_mutex_lock(&db_mutex);
    // Mark as timed_out after 10 minutes
    cwist_db_exec(db, "UPDATE games SET status = 'timed_out' WHERE last_activity < datetime('now', '-10 minutes') AND status != 'timed_out';");
    // Delete after 11 minutes
    cwist_db_exec(db, "DELETE FROM games WHERE last_activity < datetime('now', '-11 minutes');");
    pthread_mutex_unlock(&db_mutex);
}

static void serialize_board(int board[SIZE][SIZE], char *out) {
    out[0] = '\0';
    char buf[16];
    for (int r=0; r<SIZE; r++) {
        for (int c=0; c<SIZE; c++) {
            sprintf(buf, "%d,", board[r][c]);
            strcat(out, buf);
        }
    }
    if(strlen(out) > 0) out[strlen(out)-1] = '\0';
}

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

void get_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int *turn, char *status, int *players, char *mode, const char *requested_mode) {
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT board, turn, status, players, mode FROM games WHERE room_id = %d;", room_id);
    
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    
    if (cJSON_GetArraySize(res) == 0) {
        memset(board, 0, sizeof(int)*SIZE*SIZE);
        if (requested_mode && strcmp(requested_mode, "reversi") == 0) {
             strcpy(mode, "reversi");
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
            "INSERT INTO games (room_id, board, turn, status, players, mode, user1_id, user2_id, last_activity) VALUES (%d, '%s', %d, '%s', %d, '%s', 0, 0, CURRENT_TIMESTAMP);",
            room_id, board_str, *turn, status, *players, mode);
        cwist_db_exec(db, insert);
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
    }
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
}

void update_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int turn, const char *status, int players, const char *mode) {
    char board_str[1024];
    serialize_board(board, board_str);
    char sql[2048];
    snprintf(sql, sizeof(sql), 
        "UPDATE games SET board='%s', turn=%d, status='%s', players=%d, mode='%s', last_activity=CURRENT_TIMESTAMP WHERE room_id=%d;",
        board_str, turn, status, players, mode, room_id);
    pthread_mutex_lock(&db_mutex);
    cwist_db_exec(db, sql);
    pthread_mutex_unlock(&db_mutex);
}

int db_join_game(cwist_db *db, int room_id, const char *requested_mode, int *player_id, char *mode, int user_id) {
    pthread_mutex_lock(&db_mutex);
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT board, turn, status, players, mode FROM games WHERE room_id = %d;", room_id);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    
    if (cJSON_GetArraySize(res) == 0) {
        int board[SIZE][SIZE];
        memset(board, 0, sizeof(int)*SIZE*SIZE);
        if (requested_mode && strcmp(requested_mode, "reversi") == 0) strcpy(mode, "reversi");
        else {
             strcpy(mode, "othello");
             board[3][3] = WHITE; board[3][4] = BLACK;
             board[4][3] = BLACK; board[4][4] = WHITE;
        }
        int turn = BLACK;
        const char *status = "waiting";
        int players = 1;
        *player_id = 1;
        char board_str[1024];
        serialize_board(board, board_str);
        char insert[2048];
        snprintf(insert, sizeof(insert), 
            "INSERT INTO games (room_id, board, turn, status, players, mode, user1_id, user2_id, last_activity) VALUES (%d, '%s', %d, '%s', %d, '%s', %d, 0, CURRENT_TIMESTAMP);",
            room_id, board_str, turn, status, players, mode, user_id);
        cwist_db_exec(db, insert);
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
            return -1;
        }
        current_players++;
        *player_id = current_players;
        cJSON *m = cJSON_GetObjectItem(row, "mode");
        if(m && m->valuestring) strcpy(mode, m->valuestring);
        else strcpy(mode, "othello");
        const char *new_status = (current_players == 2) ? "active" : "waiting";
        char update[256];
        snprintf(update, sizeof(update), "UPDATE games SET players=%d, status='%s', user%d_id=%d, last_activity=CURRENT_TIMESTAMP WHERE room_id=%d;", 
                current_players, new_status, current_players, user_id, room_id);
        cwist_db_exec(db, update);
    }
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
    return 0;
}

/* Records game results (wins, losses, ties) for authenticated users.
   Called when a game transitions to the 'finished' state. */
void db_record_result(cwist_db *db, int room_id, int winner_pid) {
    pthread_mutex_lock(&db_mutex);
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT user1_id, user2_id FROM games WHERE room_id = %d;", room_id);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    if (cJSON_GetArraySize(res) > 0) {
        cJSON *row = cJSON_GetArrayItem(res, 0);
        int u1 = cJSON_GetObjectItem(row, "user1_id")->valueint;
        int u2 = cJSON_GetObjectItem(row, "user2_id")->valueint;
        
        char update1[256], update2[256];
        if (winner_pid == 0) { // Tie
            if(u1 > 0) { snprintf(update1, sizeof(update1), "UPDATE users SET ties = ties + 1 WHERE id = %d;", u1); cwist_db_exec(db, update1); }
            if(u2 > 0) { snprintf(update2, sizeof(update2), "UPDATE users SET ties = ties + 1 WHERE id = %d;", u2); cwist_db_exec(db, update2); }
        } else if (winner_pid == 1) { // Black wins
            if(u1 > 0) { snprintf(update1, sizeof(update1), "UPDATE users SET wins = wins + 1 WHERE id = %d;", u1); cwist_db_exec(db, update1); }
            if(u2 > 0) { snprintf(update2, sizeof(update2), "UPDATE users SET losses = losses + 1 WHERE id = %d;", u2); cwist_db_exec(db, update2); }
        } else if (winner_pid == 2) { // White wins
            if(u1 > 0) { snprintf(update1, sizeof(update1), "UPDATE users SET losses = losses + 1 WHERE id = %d;", u1); cwist_db_exec(db, update1); }
            if(u2 > 0) { snprintf(update2, sizeof(update2), "UPDATE users SET wins = wins + 1 WHERE id = %d;", u2); cwist_db_exec(db, update2); }
        }
    }
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
}

/* Registers a new user with a hashed password. */
int db_register_user(cwist_db *db, const char *username, const char *password_hash) {
    char sql[512];
    snprintf(sql, sizeof(sql), "INSERT INTO users (username, password_hash) VALUES ('%s', '%s');", username, password_hash);
    pthread_mutex_lock(&db_mutex);
    cwist_error_t err = cwist_db_exec(db, sql);
    pthread_mutex_unlock(&db_mutex);
    return err.error.err_i16;
}

/* Validates user credentials and returns user ID if successful. */
int db_login_user(cwist_db *db, const char *username, const char *password_hash) {
    char sql[512];
    snprintf(sql, sizeof(sql), "SELECT id FROM users WHERE username = '%s' AND password_hash = '%s';", username, password_hash);
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    int id = -1;
    if (cJSON_GetArraySize(res) > 0) {
        id = cJSON_GetArrayItem(res, 0)->child->valueint;
    }
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
    return id;
}

cJSON *db_get_rankings(cwist_db *db) {
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, "SELECT username, wins, losses, ties FROM users ORDER BY wins DESC LIMIT 10;", &res);
    pthread_mutex_unlock(&db_mutex);
    return res;
}

cJSON *db_get_user_info(cwist_db *db, int user_id) {
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT username, wins, losses, ties FROM users WHERE id = %d;", user_id);
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    pthread_mutex_unlock(&db_mutex);
    if (cJSON_GetArraySize(res) > 0) {
        cJSON *row = cJSON_DetachItemFromArray(res, 0);
        cJSON_Delete(res);
        return row;
    }
    cJSON_Delete(res);
    return NULL;
}
