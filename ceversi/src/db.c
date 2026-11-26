#include "db.h"
#include "betting_logic.h"
#include "memory.h"
#include <cwist/core/db/sql.h>
#include <cwist/sys/err/cwist_err.h>
#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <strings.h>
#include <pthread.h>
#include <time.h>
#include <limits.h>

cwist_db *db_conn = NULL;
static pthread_mutex_t db_mutex = PTHREAD_MUTEX_INITIALIZER;
static int betting_db_ready = 0;
static int betting_db_warning_logged = 0;
static int safe_add_points(int base, long long delta) {
    long long sum = (long long)base + delta;
    return betting_clamp_points(sum);
}

static const char *canonical_bet_outcome(const char *value) {
    if (!value) return NULL;
    if (strcasecmp(value, "win") == 0 || strcasecmp(value, "success") == 0) return "win";
    if (strcasecmp(value, "lose") == 0 || strcasecmp(value, "loss") == 0 ||
        strcasecmp(value, "failed") == 0 || strcasecmp(value, "fail") == 0) return "lose";
    if (strcasecmp(value, "draw") == 0 || strcasecmp(value, "tie") == 0) return "draw";
    return NULL;
}

static void sql_escape(const char *in, char *out, size_t out_sz) {
    if (!out || out_sz == 0) return;
    if (!in) {
        out[0] = '\0';
        return;
    }
    size_t j = 0;
    for (size_t i = 0; in[i] != '\0' && j + 1 < out_sz; i++) {
        if (in[i] == '\'') {
            if (j + 2 >= out_sz) break;
            out[j++] = '\'';
            out[j++] = '\'';
        } else {
            out[j++] = in[i];
        }
    }
    out[j] = '\0';
}

static int json_to_int(cJSON *obj, const char *key, int fallback) {
    if (!obj || !key) return fallback;
    cJSON *item = cJSON_GetObjectItem(obj, key);
    if (!item) return fallback;
    if (item->valuestring) return atoi(item->valuestring);
    if (cJSON_IsNumber(item)) return item->valueint;
    return fallback;
}

static int betting_db_available(void) {
    if (!betting_db_ready && !betting_db_warning_logged) {
        fprintf(stderr, "betting database is not attached; betting endpoints are unavailable\n");
        betting_db_warning_logged = 1;
    }
    return betting_db_ready;
}

static int ensure_betting_db_attached(cwist_db *db) {
    char *main_file = NULL;
    int already_attached = 0;
    cJSON *dbs = NULL;
    cwist_db_query(db, "PRAGMA database_list;", &dbs);
    if (dbs) {
        int n = cJSON_GetArraySize(dbs);
        for (int i = 0; i < n; i++) {
            cJSON *row = cJSON_GetArrayItem(dbs, i);
            cJSON *name = cJSON_GetObjectItem(row, "name");
            if (name && name->valuestring && strcmp(name->valuestring, "betting") == 0) {
                already_attached = 1;
                break;
            }
            if (name && name->valuestring && strcmp(name->valuestring, "main") == 0) {
                cJSON *file = cJSON_GetObjectItem(row, "file");
                if (file && file->valuestring && file->valuestring[0] != '\0') {
                    free(main_file);
                    main_file = strdup(file->valuestring);
                }
            }
        }
        cJSON_Delete(dbs);
    }
    if (already_attached) {
        free(main_file);
        return 1;
    }

    char *base_dir = NULL;
    if (main_file && main_file[0] == '/') {
        const char *slash = strrchr(main_file, '/');
        if (slash) {
            size_t dir_len = (size_t)(slash - main_file);
            base_dir = (char *)malloc(dir_len + 1);
            if (!base_dir) {
                free(main_file);
                fprintf(stderr, "Failed to allocate memory for betting db path\n");
                return 0;
            }
            memcpy(base_dir, main_file, dir_len);
            base_dir[dir_len] = '\0';
        }
    }
    if (!base_dir) {
        base_dir = realpath(".", NULL);
    }
    free(main_file);
    if (!base_dir) {
        fprintf(stderr, "Failed to resolve deterministic base path for betting.db\n");
        return 0;
    }

    size_t path_len = strlen(base_dir) + strlen("/betting.db") + 1;
    char *betting_db_path = (char *)malloc(path_len);
    if (!betting_db_path) {
        free(base_dir);
        fprintf(stderr, "Failed to allocate betting db path\n");
        return 0;
    }
    snprintf(betting_db_path, path_len, "%s/betting.db", base_dir);
    free(base_dir);

    size_t esc_len = (strlen(betting_db_path) * 2) + 3;
    char *esc_path = (char *)malloc(esc_len);
    if (!esc_path) {
        free(betting_db_path);
        fprintf(stderr, "Failed to allocate escaped betting db path\n");
        return 0;
    }
    sql_escape(betting_db_path, esc_path, esc_len);
    free(betting_db_path);

    const char *attach_prefix = "ATTACH DATABASE '";
    const char *attach_suffix = "' AS betting;";
    size_t attach_len = strlen(attach_prefix) + strlen(esc_path) + strlen(attach_suffix) + 1;
    char *attach_sql = (char *)malloc(attach_len);
    if (!attach_sql) {
        free(esc_path);
        fprintf(stderr, "Failed to allocate attach SQL buffer\n");
        return 0;
    }
    snprintf(attach_sql, attach_len, "%s%s%s", attach_prefix, esc_path, attach_suffix);
    free(esc_path);
    cwist_error_t err = cwist_db_exec(db, attach_sql);
    free(attach_sql);
    if (err != CWIST_OK) {
        fprintf(stderr, "Failed to attach betting.db: %s\n", cwist_strerror(err));
        return 0;
    }

    return 1;
}

/* Initializes the database schema. Creates 'games' and 'users' tables if they don't exist.
   Also includes rudimentary migrations for adding user-related columns to older DBs. */
void init_db(cwist_db *db) {
    pthread_mutex_lock(&db_mutex);
    cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS games (room_id INTEGER PRIMARY KEY, board TEXT, turn INTEGER, status TEXT, players INTEGER, mode TEXT, user1_id INTEGER DEFAULT 0, user2_id INTEGER DEFAULT 0, session_type TEXT DEFAULT 'multiplayer', last_activity DATETIME DEFAULT CURRENT_TIMESTAMP);");
    cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY AUTOINCREMENT, username TEXT UNIQUE, password_hash TEXT, wins INTEGER DEFAULT 0, losses INTEGER DEFAULT 0, ties INTEGER DEFAULT 0);");
    cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS game_sessions (id INTEGER PRIMARY KEY AUTOINCREMENT, identity TEXT NOT NULL, session_type TEXT NOT NULL, mode TEXT, difficulty TEXT, room_id INTEGER DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);");
    betting_db_ready = ensure_betting_db_attached(db);
    if (betting_db_ready) betting_db_warning_logged = 0;
    if (betting_db_ready) {
        cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS betting.betting_users (identity TEXT PRIMARY KEY, points INTEGER DEFAULT 1000, created_at DATETIME DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP);");
        cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS betting.betting_slots (slot_id INTEGER PRIMARY KEY, difficulty TEXT, odds_win REAL, odds_lose REAL, odds_draw REAL, result TEXT, refresh_mark INTEGER, updated_at DATETIME DEFAULT CURRENT_TIMESTAMP);");
        cwist_db_exec(db, "CREATE TABLE IF NOT EXISTS betting.multiplayer_bets (id INTEGER PRIMARY KEY AUTOINCREMENT, room_id INTEGER NOT NULL, identity TEXT NOT NULL, target_player INTEGER NOT NULL, amount INTEGER NOT NULL, settled INTEGER DEFAULT 0, created_at DATETIME DEFAULT CURRENT_TIMESTAMP);");
    }
    
    // Migrations for existing DBs
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN mode TEXT;");
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN last_activity DATETIME DEFAULT CURRENT_TIMESTAMP;");
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN user1_id INTEGER DEFAULT 0;");
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN user2_id INTEGER DEFAULT 0;");
    cwist_db_exec(db, "ALTER TABLE games ADD COLUMN session_type TEXT DEFAULT 'multiplayer';");
    
    // Trigger: When status becomes 'dropped', delete the row.
    cwist_db_exec(db, "CREATE TRIGGER IF NOT EXISTS drop_game_on_leave AFTER UPDATE ON games WHEN NEW.status = 'dropped' BEGIN DELETE FROM games WHERE room_id = OLD.room_id; END;");
    
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
    char *dup = cev_mem_strdup(in);
    if (!dup) return;
    char *token = strtok(dup, ",");
    int r=0, c=0;
    while (token) {
        board[r][c] = atoi(token);
        c++;
        if (c >= SIZE) { c=0; r++; }
        token = strtok(NULL, ",");
    }
    cev_mem_free(dup);
}

void get_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int *turn, char *status, int *players, char *mode, const char *requested_mode) {
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT board, turn, status, players, mode FROM games WHERE room_id = %d AND session_type='multiplayer';", room_id);
    
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
        if (requested_mode) {
            char board_str[1024];
            serialize_board(board, board_str);
            char insert[2048];
            snprintf(insert, sizeof(insert), 
                "INSERT INTO games (room_id, board, turn, status, players, mode, user1_id, user2_id, session_type, last_activity) VALUES (%d, '%s', %d, '%s', %d, '%s', 0, 0, 'multiplayer', CURRENT_TIMESTAMP);",
                room_id, board_str, *turn, status, *players, mode);
            cwist_db_exec(db, insert);
        }
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
        "UPDATE games SET board='%s', turn=%d, status='%s', players=%d, mode='%s', last_activity=CURRENT_TIMESTAMP WHERE room_id=%d AND session_type='multiplayer';",
        board_str, turn, status, players, mode, room_id);
    pthread_mutex_lock(&db_mutex);
    cwist_db_exec(db, sql);
    pthread_mutex_unlock(&db_mutex);
}

int db_join_game(cwist_db *db, int room_id, const char *requested_mode, int *player_id, char *mode, int user_id) {
    pthread_mutex_lock(&db_mutex);
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT board, turn, status, players, mode, user1_id, user2_id FROM games WHERE room_id = %d AND session_type='multiplayer';", room_id);
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
            "INSERT INTO games (room_id, board, turn, status, players, mode, user1_id, user2_id, session_type, last_activity) VALUES (%d, '%s', %d, '%s', %d, '%s', %d, 0, 'multiplayer', CURRENT_TIMESTAMP);",
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
        int user1_id = 0;
        int user2_id = 0;
        cJSON *u1 = cJSON_GetObjectItem(row, "user1_id");
        cJSON *u2 = cJSON_GetObjectItem(row, "user2_id");
        if (u1) {
            if (u1->valuestring) user1_id = atoi(u1->valuestring);
            else if (cJSON_IsNumber(u1)) user1_id = u1->valueint;
        }
        if (u2) {
            if (u2->valuestring) user2_id = atoi(u2->valuestring);
            else if (cJSON_IsNumber(u2)) user2_id = u2->valueint;
        }
        if (user_id > 0 && user_id == user1_id) {
            *player_id = 1;
            cJSON *m = cJSON_GetObjectItem(row, "mode");
            if(m && m->valuestring) strcpy(mode, m->valuestring);
            else strcpy(mode, "othello");
            char touch[128];
            snprintf(touch, sizeof(touch), "UPDATE games SET last_activity=CURRENT_TIMESTAMP WHERE room_id=%d AND session_type='multiplayer';", room_id);
            cwist_db_exec(db, touch);
            cJSON_Delete(res);
            pthread_mutex_unlock(&db_mutex);
            return 0;
        }
        if (user_id > 0 && user_id == user2_id) {
            *player_id = 2;
            cJSON *m = cJSON_GetObjectItem(row, "mode");
            if(m && m->valuestring) strcpy(mode, m->valuestring);
            else strcpy(mode, "othello");
            char touch[128];
            snprintf(touch, sizeof(touch), "UPDATE games SET last_activity=CURRENT_TIMESTAMP WHERE room_id=%d AND session_type='multiplayer';", room_id);
            cwist_db_exec(db, touch);
            cJSON_Delete(res);
            pthread_mutex_unlock(&db_mutex);
            return 0;
        }
        if (current_players >= 2) {
            cJSON_Delete(res);
            pthread_mutex_unlock(&db_mutex);
            return -1;
        }
        int assigned_slot = 0;
        if (user_id > 0) {
            if (user1_id == 0) assigned_slot = 1;
            else if (user2_id == 0) assigned_slot = 2;
            else {
                cJSON_Delete(res);
                pthread_mutex_unlock(&db_mutex);
                return -1;
            }
        } else {
            assigned_slot = current_players + 1;
        }
        current_players++;
        *player_id = assigned_slot;
        cJSON *m = cJSON_GetObjectItem(row, "mode");
        if(m && m->valuestring) strcpy(mode, m->valuestring);
        else strcpy(mode, "othello");
        int new_players = current_players;
        if (user_id > 0) {
            int filled = (user1_id != 0) + (user2_id != 0);
            if (assigned_slot == 1 && user1_id == 0) filled++;
            if (assigned_slot == 2 && user2_id == 0) filled++;
            new_players = filled;
        }
        const char *new_status = (new_players == 2) ? "active" : "waiting";
        char update[256];
        if (user_id > 0) {
            snprintf(update, sizeof(update), "UPDATE games SET players=%d, status='%s', user%d_id=%d, last_activity=CURRENT_TIMESTAMP WHERE room_id=%d AND session_type='multiplayer';", 
                    new_players, new_status, assigned_slot, user_id, room_id);
        } else {
            snprintf(update, sizeof(update), "UPDATE games SET players=%d, status='%s', last_activity=CURRENT_TIMESTAMP WHERE room_id=%d AND session_type='multiplayer';", 
                    new_players, new_status, room_id);
        }
        cwist_db_exec(db, update);
    }
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
    return 0;
}

void db_leave_game(cwist_db *db, int room_id, int player_id, int user_id) {
    pthread_mutex_lock(&db_mutex);
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT players, user1_id, user2_id FROM games WHERE room_id = %d AND session_type='multiplayer';", room_id);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    if (cJSON_GetArraySize(res) == 0) {
        cJSON_Delete(res);
        pthread_mutex_unlock(&db_mutex);
        return;
    }

    cJSON *row = cJSON_GetArrayItem(res, 0);
    int current_players = 0;
    cJSON *p = cJSON_GetObjectItem(row, "players");
    if (p) {
        if (p->valuestring) current_players = atoi(p->valuestring);
        else if (cJSON_IsNumber(p)) current_players = p->valueint;
    }

    int user1_id = 0;
    int user2_id = 0;
    cJSON *u1 = cJSON_GetObjectItem(row, "user1_id");
    cJSON *u2 = cJSON_GetObjectItem(row, "user2_id");
    if (u1) {
        if (u1->valuestring) user1_id = atoi(u1->valuestring);
        else if (cJSON_IsNumber(u1)) user1_id = u1->valueint;
    }
    if (u2) {
        if (u2->valuestring) user2_id = atoi(u2->valuestring);
        else if (cJSON_IsNumber(u2)) user2_id = u2->valueint;
    }

    int leaving_slot = 0;
    if (player_id == 1 || player_id == 2) {
        leaving_slot = player_id;
    } else if (user_id > 0 && user_id == user1_id) {
        leaving_slot = 1;
    } else if (user_id > 0 && user_id == user2_id) {
        leaving_slot = 2;
    }

    if (leaving_slot == 0) {
        cJSON_Delete(res);
        pthread_mutex_unlock(&db_mutex);
        return;
    }

    if (current_players >= 2 || current_players <= 1) {
        char drop_sql[128];
        snprintf(drop_sql, sizeof(drop_sql), "UPDATE games SET status = 'dropped' WHERE room_id = %d AND session_type='multiplayer';", room_id);
        cwist_db_exec(db, drop_sql);
        cJSON_Delete(res);
        pthread_mutex_unlock(&db_mutex);
        return;
    }

    int new_players = current_players - 1;
    if (new_players < 0) new_players = 0;
    if (user_id > 0) {
        if (leaving_slot == 1) user1_id = 0;
        if (leaving_slot == 2) user2_id = 0;
        new_players = (user1_id != 0) + (user2_id != 0);
    }
    const char *new_status = (new_players == 2) ? "active" : "waiting";
    char update[256];
    snprintf(update, sizeof(update), "UPDATE games SET players=%d, status='%s', user1_id=%d, user2_id=%d, last_activity=CURRENT_TIMESTAMP WHERE room_id=%d AND session_type='multiplayer';",
             new_players, new_status, user1_id, user2_id, room_id);
    cwist_db_exec(db, update);
    cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
}

void db_reset_room(cwist_db *db, int room_id) {
    char sql[256];
    // Trigger the drop via UPDATE status
    snprintf(sql, sizeof(sql), "UPDATE games SET status = 'dropped' WHERE room_id = %d AND session_type='multiplayer';", room_id);
    pthread_mutex_lock(&db_mutex);
    cwist_db_exec(db, sql);
    pthread_mutex_unlock(&db_mutex);
}

/* Records game results (wins, losses, ties) for authenticated users.
   Called when a game transitions to the 'finished' state. */
void db_record_result(cwist_db *db, int room_id, int winner_pid) {
    pthread_mutex_lock(&db_mutex);
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT user1_id, user2_id FROM games WHERE room_id = %d AND session_type='multiplayer';", room_id);
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
    if (res && cJSON_GetArraySize(res) > 0) {
        cJSON *row = cJSON_GetArrayItem(res, 0);
        cJSON *id_item = cJSON_GetObjectItem(row, "id");
        if (id_item) {
            if (cJSON_IsNumber(id_item)) id = id_item->valueint;
            else if (id_item->valuestring) id = atoi(id_item->valuestring);
        }
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
    if (!res) return cJSON_CreateArray();
    return res;
}

cJSON *db_get_user_info(cwist_db *db, int user_id) {
    char sql[256];
    snprintf(sql, sizeof(sql), "SELECT username, wins, losses, ties FROM users WHERE id = %d;", user_id);
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    pthread_mutex_unlock(&db_mutex);
    if (res && cJSON_GetArraySize(res) > 0) {
        cJSON *row = cJSON_DetachItemFromArray(res, 0);
        cJSON_Delete(res);
        return row;
    }
    if (res) cJSON_Delete(res);
    return NULL;
}

cJSON *db_get_multiplayer_rooms(cwist_db *db) {
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, "SELECT room_id, mode, status, players, last_activity FROM games WHERE session_type='multiplayer' ORDER BY room_id ASC LIMIT 50;", &res);
    pthread_mutex_unlock(&db_mutex);
    if (!res) return cJSON_CreateArray();
    return res;
}

int db_log_game_session(cwist_db *db, const char *identity, const char *session_type, const char *mode, const char *difficulty, int room_id) {
    if (!identity || !session_type || strlen(identity) == 0 || strlen(session_type) == 0) return -1;
    const char *safe_mode = (mode && strlen(mode) > 0) ? mode : "othello";
    const char *safe_difficulty = (difficulty && strlen(difficulty) > 0) ? difficulty : "";
    char esc_identity[256];
    char esc_type[64];
    char esc_mode[64];
    char esc_difficulty[64];
    sql_escape(identity, esc_identity, sizeof(esc_identity));
    sql_escape(session_type, esc_type, sizeof(esc_type));
    sql_escape(safe_mode, esc_mode, sizeof(esc_mode));
    sql_escape(safe_difficulty, esc_difficulty, sizeof(esc_difficulty));
    char sql[1024];
    snprintf(sql, sizeof(sql),
             "INSERT INTO game_sessions (identity, session_type, mode, difficulty, room_id, created_at) VALUES ('%s', '%s', '%s', '%s', %d, CURRENT_TIMESTAMP);",
             esc_identity, esc_type, esc_mode, esc_difficulty, room_id);
    pthread_mutex_lock(&db_mutex);
    cwist_error_t err = cwist_db_exec(db, sql);
    pthread_mutex_unlock(&db_mutex);
    return err.error.err_i16;
}

cJSON *db_get_recent_sessions(cwist_db *db, const char *identity, const char *session_type, int limit) {
    if (!identity || !session_type || strlen(identity) == 0 || strlen(session_type) == 0) return cJSON_CreateArray();
    if (limit <= 0) limit = 8;
    if (limit > 100) limit = 100;
    char esc_identity[256];
    char esc_type[64];
    sql_escape(identity, esc_identity, sizeof(esc_identity));
    sql_escape(session_type, esc_type, sizeof(esc_type));
    char sql[1024];
    snprintf(sql, sizeof(sql),
             "SELECT id, session_type, mode, difficulty, room_id, created_at FROM game_sessions WHERE identity='%s' AND session_type='%s' ORDER BY id DESC LIMIT %d;",
             esc_identity, esc_type, limit);
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    pthread_mutex_unlock(&db_mutex);
    if (!res) return cJSON_CreateArray();
    return res;
}

void db_refresh_betting_slots(cwist_db *db) {
    if (!betting_db_available()) return;
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, "SELECT COUNT(*) AS cnt FROM betting.betting_slots;", &res);
    int cnt = 0;
    if (res && cJSON_GetArraySize(res) > 0) {
        cJSON *row = cJSON_GetArrayItem(res, 0);
        cJSON *item = cJSON_GetObjectItem(row, "cnt");
        if (item) cnt = item->valueint;
    }
    if (res) cJSON_Delete(res);

    if (cnt == 10) {
        pthread_mutex_unlock(&db_mutex);
        return;
    }

    cwist_db_exec(db, "DELETE FROM betting.betting_slots;");

    for (int slot = 1; slot <= 10; slot++) {
        const char *difficulty = (slot <= 4) ? "easy" : (slot <= 7 ? "medium" : "hard");
        double p_win = 0.34 + ((rand() % 7) - 3) * 0.01;
        double p_lose = 0.34 + ((rand() % 7) - 3) * 0.01;
        if (p_win < 0.28) p_win = 0.28;
        if (p_lose < 0.28) p_lose = 0.28;
        double p_draw = 1.0 - p_win - p_lose;
        if (p_draw < 0.20) {
            p_draw = 0.20;
            p_win = 0.40;
            p_lose = 0.40;
        }
        double odds_win = 1.0 / p_win;
        double odds_lose = 1.0 / p_lose;
        double odds_draw = 1.0 / p_draw;

        int roll = rand() % 1000;
        const char *result = "draw";
        if (roll < (int)(p_win * 1000.0)) result = "win";
        else if (roll < (int)((p_win + p_lose) * 1000.0)) result = "lose";

        char ins[512];
        snprintf(ins, sizeof(ins),
            "INSERT INTO betting.betting_slots (slot_id, difficulty, odds_win, odds_lose, odds_draw, result, refresh_mark, updated_at) VALUES (%d, '%s', %.3f, %.3f, %.3f, '%s', 0, CURRENT_TIMESTAMP);",
            slot, difficulty, odds_win, odds_lose, odds_draw, result);
        cwist_db_exec(db, ins);
    }
    pthread_mutex_unlock(&db_mutex);
}

cJSON *db_get_betting_slots(cwist_db *db) {
    if (!betting_db_available()) return cJSON_CreateArray();
    db_refresh_betting_slots(db);
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, "SELECT slot_id, difficulty, odds_win, odds_lose, odds_draw, updated_at FROM betting.betting_slots ORDER BY slot_id ASC;", &res);
    pthread_mutex_unlock(&db_mutex);
    if (!res) return cJSON_CreateArray();
    return res;
}

int db_get_betting_points(cwist_db *db, const char *identity, int *points) {
    if (!betting_db_available()) return -1;
    if (!identity || strlen(identity) == 0) return -1;
    char esc_identity[256];
    sql_escape(identity, esc_identity, sizeof(esc_identity));
    pthread_mutex_lock(&db_mutex);
    char sql[512];
    snprintf(sql, sizeof(sql), "SELECT points FROM betting.betting_users WHERE identity = '%s';", esc_identity);
    cJSON *res = NULL;
    cwist_db_query(db, sql, &res);
    if (res && cJSON_GetArraySize(res) > 0) {
        cJSON *row = cJSON_GetArrayItem(res, 0);
        *points = json_to_int(row, "points", BETTING_START_POINTS);
        int normalized = betting_reset_if_needed(*points);
        if (normalized != *points) {
            char upd[512];
            snprintf(upd, sizeof(upd), "UPDATE betting.betting_users SET points = %d, updated_at=CURRENT_TIMESTAMP WHERE identity='%s';", normalized, esc_identity);
            cwist_db_exec(db, upd);
            *points = normalized;
        }
    } else {
        char ins[512];
        snprintf(ins, sizeof(ins), "INSERT INTO betting.betting_users (identity, points, updated_at) VALUES ('%s', %d, CURRENT_TIMESTAMP);", esc_identity, BETTING_START_POINTS);
        cwist_db_exec(db, ins);
        *points = BETTING_START_POINTS;
    }
    if (res) cJSON_Delete(res);
    pthread_mutex_unlock(&db_mutex);
    return 0;
}

int db_apply_bet(cwist_db *db, const char *identity, int slot_id, const char *outcome, int amount, cJSON **result_json) {
    if (!betting_db_available()) return -1;
    if (!identity || !outcome || amount <= 0) return -1;
    db_refresh_betting_slots(db);
    char esc_identity[256];
    sql_escape(identity, esc_identity, sizeof(esc_identity));
    pthread_mutex_lock(&db_mutex);

    int points = 0;
    char q_user[512];
    snprintf(q_user, sizeof(q_user), "SELECT points FROM betting.betting_users WHERE identity = '%s';", esc_identity);
    cJSON *user_res = NULL;
    cwist_db_query(db, q_user, &user_res);
    if (user_res && cJSON_GetArraySize(user_res) > 0) {
        cJSON *row = cJSON_GetArrayItem(user_res, 0);
        points = json_to_int(row, "points", BETTING_START_POINTS);
    } else {
        char ins[512];
        snprintf(ins, sizeof(ins), "INSERT INTO betting.betting_users (identity, points, updated_at) VALUES ('%s', %d, CURRENT_TIMESTAMP);", esc_identity, BETTING_START_POINTS);
        cwist_db_exec(db, ins);
        points = BETTING_START_POINTS;
    }
    if (user_res) cJSON_Delete(user_res);
    points = betting_reset_if_needed(points);

    char q_slot[256];
    snprintf(q_slot, sizeof(q_slot), "SELECT odds_win, odds_lose, odds_draw, result FROM betting.betting_slots WHERE slot_id = %d;", slot_id);
    cJSON *slot_res = NULL;
    cwist_db_query(db, q_slot, &slot_res);
    if (!slot_res || cJSON_GetArraySize(slot_res) == 0) {
        if (slot_res) cJSON_Delete(slot_res);
        pthread_mutex_unlock(&db_mutex);
        return -2;
    }

    cJSON *slot = cJSON_GetArrayItem(slot_res, 0);
    cJSON *result_item = cJSON_GetObjectItem(slot, "result");
    const char *actual_raw = (result_item && cJSON_IsString(result_item)) ? result_item->valuestring : NULL;
    const char *actual_result = canonical_bet_outcome(actual_raw);
    const char *picked_outcome = canonical_bet_outcome(outcome);
    if (!actual_result || !picked_outcome) {
        cJSON_Delete(slot_res);
        pthread_mutex_unlock(&db_mutex);
        return -4;
    }

    double odds = 1.0;
    if (strcmp(picked_outcome, "win") == 0) odds = cJSON_GetObjectItem(slot, "odds_win")->valuedouble;
    else if (strcmp(picked_outcome, "lose") == 0) odds = cJSON_GetObjectItem(slot, "odds_lose")->valuedouble;
    else if (strcmp(picked_outcome, "draw") == 0) odds = cJSON_GetObjectItem(slot, "odds_draw")->valuedouble;

    if (!betting_can_wager(points, amount)) {
        cJSON_Delete(slot_res);
        pthread_mutex_unlock(&db_mutex);
        return -3;
    }

    int success = strcmp(picked_outcome, actual_result) == 0;
    int delta = betting_single_delta(amount, odds, success);
    points = safe_add_points(points, delta);

    char upd[512];
    snprintf(upd, sizeof(upd), "UPDATE betting.betting_users SET points = %d, updated_at=CURRENT_TIMESTAMP WHERE identity='%s';", points, esc_identity);
    cwist_db_exec(db, upd);

    cJSON *result = cJSON_CreateObject();
    cJSON_AddBoolToObject(result, "success", success);
    cJSON_AddNumberToObject(result, "delta", delta);
    cJSON_AddNumberToObject(result, "points", points);
    cJSON_AddStringToObject(result, "result", actual_result);
    cJSON_AddNumberToObject(result, "odds", odds);
    *result_json = result;

    cJSON_Delete(slot_res);
    pthread_mutex_unlock(&db_mutex);
    return 0;
}

cJSON *db_get_betting_rankings(cwist_db *db) {
    if (!betting_db_available()) return cJSON_CreateArray();
    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    cwist_db_query(db, "SELECT identity, points, updated_at FROM betting.betting_users ORDER BY points DESC, updated_at ASC LIMIT 20;", &res);
    pthread_mutex_unlock(&db_mutex);
    if (!res) return cJSON_CreateArray();
    return res;
}

int db_place_multiplayer_bet(cwist_db *db, const char *identity, int room_id, int target_player, int amount, cJSON **result_json) {
    if (!betting_db_available()) return -1;
    if (!identity || room_id <= 0 || amount <= 0) return -1;
    if (target_player != 1 && target_player != 2) return -1;
    char esc_identity[256];
    sql_escape(identity, esc_identity, sizeof(esc_identity));
    pthread_mutex_lock(&db_mutex);

    int points = BETTING_START_POINTS;
    char q_user[512];
    snprintf(q_user, sizeof(q_user), "SELECT points FROM betting.betting_users WHERE identity = '%s';", esc_identity);
    cJSON *user_res = NULL;
    cwist_db_query(db, q_user, &user_res);
    if (user_res && cJSON_GetArraySize(user_res) > 0) {
        cJSON *row = cJSON_GetArrayItem(user_res, 0);
        points = json_to_int(row, "points", BETTING_START_POINTS);
    } else {
        char ins[512];
        snprintf(ins, sizeof(ins), "INSERT INTO betting.betting_users (identity, points, updated_at) VALUES ('%s', %d, CURRENT_TIMESTAMP);", esc_identity, BETTING_START_POINTS);
        cwist_db_exec(db, ins);
    }
    if (user_res) cJSON_Delete(user_res);
    points = betting_reset_if_needed(points);

    if (!betting_can_wager(points, amount)) {
        pthread_mutex_unlock(&db_mutex);
        return -3;
    }

    points = safe_add_points(points, -((long long)amount));
    char upd[512];
    snprintf(upd, sizeof(upd), "UPDATE betting.betting_users SET points = %d, updated_at=CURRENT_TIMESTAMP WHERE identity='%s';", points, esc_identity);
    cwist_db_exec(db, upd);

    char ins_bet[512];
    snprintf(ins_bet, sizeof(ins_bet),
             "INSERT INTO betting.multiplayer_bets (room_id, identity, target_player, amount, settled, created_at) VALUES (%d, '%s', %d, %d, 0, CURRENT_TIMESTAMP);",
             room_id, esc_identity, target_player, amount);
    cwist_db_exec(db, ins_bet);

    cJSON *result = cJSON_CreateObject();
    cJSON_AddStringToObject(result, "identity", identity);
    cJSON_AddNumberToObject(result, "room_id", room_id);
    cJSON_AddNumberToObject(result, "target_player", target_player);
    cJSON_AddNumberToObject(result, "amount", amount);
    cJSON_AddNumberToObject(result, "points", points);
    *result_json = result;

    pthread_mutex_unlock(&db_mutex);
    return 0;
}

int db_settle_multiplayer_bets(cwist_db *db, int room_id, int winner_player, cJSON **settle_json) {
    if (!betting_db_available()) return -1;
    if (room_id <= 0) return -1;
    pthread_mutex_lock(&db_mutex);

    char q_bets[256];
    snprintf(q_bets, sizeof(q_bets), "SELECT id, identity, target_player, amount FROM betting.multiplayer_bets WHERE room_id=%d AND settled=0 ORDER BY id ASC;", room_id);
    cJSON *bets = NULL;
    cwist_db_query(db, q_bets, &bets);
    if (!bets || cJSON_GetArraySize(bets) == 0) {
        if (bets) cJSON_Delete(bets);
        pthread_mutex_unlock(&db_mutex);
        return 0;
    }

    long long total_pool = 0;
    long long total_winner_bet = 0;
    int n = cJSON_GetArraySize(bets);
    for (int i = 0; i < n; i++) {
        cJSON *row = cJSON_GetArrayItem(bets, i);
        int amount = json_to_int(row, "amount", 0);
        int target = json_to_int(row, "target_player", 0);
        total_pool += amount;
        if (winner_player != 0 && target == winner_player) total_winner_bet += amount;
    }

    cJSON *payouts = cJSON_CreateArray();
    for (int i = 0; i < n; i++) {
        cJSON *row = cJSON_GetArrayItem(bets, i);
        int bet_id = json_to_int(row, "id", 0);
        const char *identity = cJSON_GetObjectItem(row, "identity")->valuestring;
        int amount = json_to_int(row, "amount", 0);
        int target = json_to_int(row, "target_player", 0);

        long long reward = betting_multiplayer_reward(winner_player, target, amount, total_pool, total_winner_bet);

        char esc_identity[256];
        sql_escape(identity, esc_identity, sizeof(esc_identity));
        int points = BETTING_START_POINTS;
        char q_user[512];
        snprintf(q_user, sizeof(q_user), "SELECT points FROM betting.betting_users WHERE identity='%s';", esc_identity);
        cJSON *u = NULL;
        cwist_db_query(db, q_user, &u);
        if (u && cJSON_GetArraySize(u) > 0) {
            cJSON *urow = cJSON_GetArrayItem(u, 0);
            points = json_to_int(urow, "points", BETTING_START_POINTS);
        } else {
            char ins_u[512];
            snprintf(ins_u, sizeof(ins_u), "INSERT INTO betting.betting_users (identity, points, updated_at) VALUES ('%s', %d, CURRENT_TIMESTAMP);", esc_identity, BETTING_START_POINTS);
            cwist_db_exec(db, ins_u);
        }
        if (u) cJSON_Delete(u);

        points = safe_add_points(points, reward);
        char upd[512];
        snprintf(upd, sizeof(upd), "UPDATE betting.betting_users SET points=%d, updated_at=CURRENT_TIMESTAMP WHERE identity='%s';", points, esc_identity);
        cwist_db_exec(db, upd);

        char mark[128];
        snprintf(mark, sizeof(mark), "UPDATE betting.multiplayer_bets SET settled=1 WHERE id=%d;", bet_id);
        cwist_db_exec(db, mark);

        cJSON *entry = cJSON_CreateObject();
        cJSON_AddStringToObject(entry, "identity", identity);
        cJSON_AddNumberToObject(entry, "reward", (double)reward);
        cJSON_AddNumberToObject(entry, "points", points);
        cJSON_AddItemToArray(payouts, entry);
    }

    cJSON *summary = cJSON_CreateObject();
    cJSON_AddNumberToObject(summary, "room_id", room_id);
    cJSON_AddNumberToObject(summary, "winner_player", winner_player);
    cJSON_AddNumberToObject(summary, "total_pool", (double)total_pool);
    cJSON_AddItemToObject(summary, "payouts", payouts);
    if (settle_json) *settle_json = summary;
    else cJSON_Delete(summary);

    cJSON_Delete(bets);
    pthread_mutex_unlock(&db_mutex);
    return 0;
}

cJSON *db_get_multiplayer_bet_history(cwist_db *db, const char *identity, int room_id) {
    if (!betting_db_available()) return cJSON_CreateArray();
    if (!identity || strlen(identity) == 0) return cJSON_CreateArray();
    char esc_identity[256];
    sql_escape(identity, esc_identity, sizeof(esc_identity));

    pthread_mutex_lock(&db_mutex);
    cJSON *res = NULL;
    char sql[768];
    if (room_id > 0) {
        snprintf(sql, sizeof(sql),
                 "SELECT id, room_id, target_player, amount, settled, created_at "
                 "FROM betting.multiplayer_bets WHERE identity='%s' AND room_id=%d "
                 "ORDER BY id DESC LIMIT 30;",
                 esc_identity, room_id);
    } else {
        snprintf(sql, sizeof(sql),
                 "SELECT id, room_id, target_player, amount, settled, created_at "
                 "FROM betting.multiplayer_bets WHERE identity='%s' "
                 "ORDER BY id DESC LIMIT 30;",
                 esc_identity);
    }
    cwist_db_query(db, sql, &res);
    pthread_mutex_unlock(&db_mutex);
    if (!res) return cJSON_CreateArray();
    return res;
}
