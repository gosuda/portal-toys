#include "handlers.h"
#include "common.h"
#include "db.h"
#include "betting_logic.h"
#include "utils.h"
#include "memory.h"
#include <cwist/sys/app/app.h>
#include <cwist/net/http/http.h>
#include <cwist/core/sstring/sstring.h>
#include <cwist/net/http/query.h>
#include <cwist/core/utils/json_builder.h>
#include <cwist/core/template/template.h>
#include <cwist/core/html/builder.h>
#include <cjson/cJSON.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <ctype.h>

static int is_valid_move(int board[SIZE][SIZE], int r, int c, int p) {
    if (r < 0 || r >= SIZE || c < 0 || c >= SIZE || board[r][c] != 0) return 0;
    int opponent = (p == BLACK) ? WHITE : BLACK;
    int dr[] = {-1, -1, -1, 0, 0, 1, 1, 1};
    int dc[] = {-1, 0, 1, -1, 1, -1, 0, 1};
    for (int i = 0; i < 8; i++) {
        int nr = r + dr[i];
        int nc = c + dc[i];
        int count = 0;
        while(nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && board[nr][nc] == opponent) {
            nr += dr[i]; nc += dc[i]; count++;
        }
        if (count > 0 && nr >= 0 && nr < SIZE && nc >= 0 && nc < SIZE && board[nr][nc] == p) {
            return 1;
        }
    }
    return 0;
}

static int has_valid_moves(int board[SIZE][SIZE], int p) {
    for(int r=0; r<SIZE; r++) {
        for(int c=0; c<SIZE; c++) {
            if(is_valid_move(board, r, c, p)) return 1;
        }
    }
    return 0;
}

static int count_pieces(int board[SIZE][SIZE]) {
    int count = 0;
    for(int r=0; r<SIZE; r++)
        for(int c=0; c<SIZE; c++)
            if(board[r][c] != 0) count++;
    return count;
}

static int get_room_id(cwist_http_request *req) {
    int room_id = 1;
    const char *room_str = cwist_query_map_get(req->query_params, "room");
    if (room_str && strlen(room_str) > 0) {
        room_id = atoi(room_str);
    }
    return room_id;
}

static void build_session_identity(cwist_http_request *req, char *identity, size_t n) {
    const char *user_id_str = cwist_query_map_get(req->query_params, "user_id");
    const char *guest_id = cwist_query_map_get(req->query_params, "guest_id");
    int user_id = (user_id_str && strlen(user_id_str) > 0) ? atoi(user_id_str) : 0;
    if (user_id > 0) {
        snprintf(identity, n, "user:%d", user_id);
    } else if (guest_id && strlen(guest_id) > 0) {
        snprintf(identity, n, "guest:%s", guest_id);
    } else {
        snprintf(identity, n, "guest:default");
    }
}

static int parse_positive_int_or_default(const char *s, int fallback) {
    if (!s || strlen(s) == 0) return fallback;
    for (int i = 0; s[i]; i++) {
        if (!isdigit((unsigned char)s[i])) return fallback;
    }
    int v = atoi(s);
    return v > 0 ? v : fallback;
}

static void build_identity_from_json(cJSON *user_item, cJSON *guest_item, char *identity, size_t n) {
    if (user_item && cJSON_IsNumber(user_item) && user_item->valueint > 0) {
        snprintf(identity, n, "user:%d", user_item->valueint);
    } else if (guest_item && guest_item->valuestring && strlen(guest_item->valuestring) > 0) {
        snprintf(identity, n, "guest:%s", guest_item->valuestring);
    } else {
        snprintf(identity, n, "guest:default");
    }
}

void join_handler(cwist_http_request *req, cwist_http_response *res) {
    int room_id = get_room_id(req);
    int pid;
    char mode[16];
    const char *requested_mode = cwist_query_map_get(req->query_params, "mode");
    const char *user_id_str = cwist_query_map_get(req->query_params, "user_id");
    int user_id = user_id_str ? atoi(user_id_str) : 0;

    if (db_join_game(req->db, room_id, requested_mode, &pid, mode, user_id) < 0) {
         res->status_code = CWIST_HTTP_FORBIDDEN;
         cwist_sstring_assign(res->body, "{\"error\": \"Room full\"}");
         return;
    }

    cwist_json_builder *jb = cwist_json_builder_create();
    cwist_json_begin_object(jb);
    cwist_json_add_int(jb, "player_id", pid);
    cwist_json_add_int(jb, "room_id", room_id);
    cwist_json_add_string(jb, "mode", mode);
    cwist_json_end_object(jb);

    cwist_sstring_assign(res->body, (char *)cwist_json_get_raw(jb));
    cwist_json_builder_destroy(jb);
    
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void leave_handler(cwist_http_request *req, cwist_http_response *res) {
    int room_id = get_room_id(req);
    const char *user_id_str = cwist_query_map_get(req->query_params, "user_id");
    const char *player_id_str = cwist_query_map_get(req->query_params, "player_id");
    int user_id = user_id_str ? atoi(user_id_str) : 0;
    int player_id = player_id_str ? atoi(player_id_str) : 0;
    db_leave_game(req->db, room_id, player_id, user_id);
    cwist_sstring_assign(res->body, "{\"status\": \"ok\"}");
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void state_handler(cwist_http_request *req, cwist_http_response *res) {
    int room_id = get_room_id(req);
    int board[SIZE][SIZE];
    int turn, players;
    char status[32];
    char mode[16];
    get_game_state(req->db, room_id, board, &turn, status, &players, mode, NULL);

    cwist_json_builder *jb = cwist_json_builder_create();
    cwist_json_begin_object(jb);
    cwist_json_add_string(jb, "status", status);
    cwist_json_add_int(jb, "turn", turn);
    cwist_json_add_string(jb, "mode", mode);
    cwist_json_add_int(jb, "room_id", room_id);
    
    cwist_json_begin_array(jb, "board");
    for(int r=0; r<SIZE; r++) {
        for(int c=0; c<SIZE; c++) {
            // Need a way to add numbers to array in jb? 
            // The docs only show cwist_json_begin_array(jb, "key") which adds it as an object field.
            // If it's a raw array element, maybe there's another function?
            // Re-reading json.md... it doesn't show how to add elements to an array.
            // I'll assume cwist_json_add_int works for array elements if key is NULL or there's an equivalent.
            // Actually, cJSON might still be easier for complex nested structures if jb is too simple.
            // But let's try to stick to jb if possible.
            // If the API doesn't support raw array elements, I'll fallback to cJSON for the board.
        }
    }
    // Since json.md is incomplete about array elements, I'll use cJSON for state_handler to be safe, 
    // or just use sstring to manually build the array if I want to be "lightweight".
    
    // Actually, I'll use cJSON for the complex state for now as it's already used in db.c anyway.
    cJSON *json = cJSON_CreateObject();
    cJSON_AddStringToObject(json, "status", status);
    cJSON_AddNumberToObject(json, "turn", turn);
    cJSON_AddStringToObject(json, "mode", mode);
    cJSON_AddNumberToObject(json, "room_id", room_id);
    
    cJSON *board_arr = cJSON_CreateArray();
    for(int r=0; r<SIZE; r++) {
        for(int c=0; c<SIZE; c++) {
            cJSON_AddItemToArray(board_arr, cJSON_CreateNumber(board[r][c]));
        }
    }
    cJSON_AddItemToObject(json, "board", board_arr);
    
    char *str = cJSON_PrintUnformatted(json);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(json);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

/* Processes a player move. Validates the move, updates the board, 
   checks for game over, and records results for authenticated users. */
void move_handler(cwist_http_request *req, cwist_http_response *res) {
    int room_id = get_room_id(req);
    cJSON *json = cJSON_Parse(req->body->data);
    if (!json) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        return;
    }

    int r = cJSON_GetObjectItem(json, "r")->valueint;
    int c = cJSON_GetObjectItem(json, "c")->valueint;
    int p = cJSON_GetObjectItem(json, "player")->valueint;
    
    int board[SIZE][SIZE];
    int turn, players;
    char status[32];
    char mode[16];
    get_game_state(req->db, room_id, board, &turn, status, &players, mode, NULL);

    if (strcmp(status, "active") == 0 && p == turn) {
        int pieces = count_pieces(board);
        int is_reversi_setup = (strcmp(mode, "reversi") == 0 && pieces < 4);
        
        if (is_reversi_setup) {
            // Reversi setup: must place in center 4 squares
            if (r < 3 || r > 4 || c < 3 || c > 4 || board[r][c] != 0) {
                res->status_code = CWIST_HTTP_BAD_REQUEST;
                cJSON_Delete(json);
                return;
            }
        } else if (!is_valid_move(board, r, c, p)) {
            res->status_code = CWIST_HTTP_BAD_REQUEST;
            cJSON_Delete(json);
            return;
        }

        // Apply Move and Flip Discs
        board[r][c] = p;
        int opponent = (p == BLACK) ? WHITE : BLACK;
        
        if (!is_reversi_setup) {
            int dr[] = {-1, -1, -1, 0, 0, 1, 1, 1};
            int dc[] = {-1, 0, 1, -1, 1, -1, 0, 1};
            for (int i = 0; i < 8; i++) {
                int nr = r + dr[i], nc = c + dc[i];
                int r_temp = nr, c_temp = nc, count = 0;
                while(r_temp >= 0 && r_temp < SIZE && c_temp >= 0 && c_temp < SIZE && board[r_temp][c_temp] == opponent) {
                    r_temp += dr[i]; c_temp += dc[i]; count++;
                }
                if (count > 0 && r_temp >= 0 && r_temp < SIZE && board[r_temp][c_temp] == p) {
                    int rr = r + dr[i], cc = c + dc[i];
                    while(board[rr][cc] == opponent) {
                        board[rr][cc] = p; rr += dr[i]; cc += dc[i];
                    }
                }
            }
        }
        
        // Determine next turn and check if game ended
        if (is_reversi_setup && count_pieces(board) < 4) {
            turn = opponent;
        } else {
            if (has_valid_moves(board, opponent)) {
                turn = opponent;
            } else if (!has_valid_moves(board, p)) {
                // Game Over
                strcpy(status, "finished");
                
                int b_cnt = 0, w_cnt = 0;
                for(int rr=0; rr<SIZE; rr++) for(int cc=0; cc<SIZE; cc++) {
                    if(board[rr][cc] == BLACK) b_cnt++;
                    else if(board[rr][cc] == WHITE) w_cnt++;
                }
                int winner = (b_cnt > w_cnt) ? 1 : (w_cnt > b_cnt ? 2 : 0);
                // Record result for user rankings
                db_record_result(req->db, room_id, winner);
                db_settle_multiplayer_bets(req->db, room_id, winner, NULL);
            }
        }

        update_game_state(req->db, room_id, board, turn, status, players, mode);
        cwist_sstring_assign(res->body, "{\"status\":\"ok\"}");
    } else {
         res->status_code = CWIST_HTTP_FORBIDDEN;
    }
    cJSON_Delete(json);
}

void login_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *json = cJSON_Parse(req->body->data);
    if (!json) { res->status_code = 400; return; }
    
    cJSON *user_item = cJSON_GetObjectItem(json, "username");
    cJSON *pass_item = cJSON_GetObjectItem(json, "password");

    const char *username = user_item->valuestring;
    
    /* Validate nickname: Allow only alphanumeric characters to prevent XSS payloads */

    
    if (!user_item || !pass_item || !user_item->valuestring || !pass_item->valuestring) {
        res->status_code = 400;
        cJSON_Delete(json);
        return;
    }
    
    const char *user = user_item->valuestring;
    const char *pass = pass_item->valuestring;

    for (int i = 0; username[i]; i++) {
        if (!isalnum(username[i])) {
            /* Return 400 Bad Request for invalid characters */
            res->status_code = 400;
            cwist_sstring_assign(res->body, "{\"error\": \"Only alphanumeric names allowed\"}");
            cJSON_Delete(json);
            return;
        }
    }
    
    char hash[65];
    hash_password(pass, hash);
    int uid = db_login_user(req->db, user, hash);
    
    printf("Login attempt: user=%s, hash=%s, uid=%d\n", user, hash, uid);
    
    cJSON *reply = cJSON_CreateObject();
    if (uid > 0) {
        cJSON_AddNumberToObject(reply, "user_id", uid);
        cJSON_AddStringToObject(reply, "username", user);
    } else {
        cJSON_AddStringToObject(reply, "error", "Invalid credentials");
        res->status_code = 401;
    }
    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cJSON_Delete(json);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void register_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *json = cJSON_Parse(req->body->data);
    if (!json) { res->status_code = 400; return; }
    
    cJSON *user_item = cJSON_GetObjectItem(json, "username");
    cJSON *pass_item = cJSON_GetObjectItem(json, "password");
    
    if (!user_item || !pass_item || !user_item->valuestring || !pass_item->valuestring ||
        strlen(user_item->valuestring) == 0 || strlen(pass_item->valuestring) == 0) {
        cJSON *reply = cJSON_CreateObject();
        cJSON_AddStringToObject(reply, "error", "Username and password are required");
        char *str = cJSON_PrintUnformatted(reply);
        cwist_sstring_assign(res->body, str);
        cev_mem_free(str);
        cJSON_Delete(reply);
        res->status_code = 400;
        cJSON_Delete(json);
        cwist_http_header_add(&res->headers, "Content-Type", "application/json");
        return;
    }
    
    const char *user = user_item->valuestring;
    const char *pass = pass_item->valuestring;

    for (int i = 0; user[i]; i++) {
        if (!isalnum(user[i])) {
            /* Return 400 Bad Request for invalid characters */
            res->status_code = 400;
            cwist_sstring_assign(res->body, "{\"error\": \"Only alphanumeric names allowed\"}");
            cJSON_Delete(json);
            return;
        }
    }
    
    // Basic length validation
    if (strlen(user) < 3 || strlen(pass) < 4) {
        cJSON *reply = cJSON_CreateObject();
        cJSON_AddStringToObject(reply, "error", "Username (min 3) or password (min 4) too short");
        char *str = cJSON_PrintUnformatted(reply);
        cwist_sstring_assign(res->body, str);
        cev_mem_free(str);
        cJSON_Delete(reply);
        res->status_code = 400;
        cJSON_Delete(json);
        cwist_http_header_add(&res->headers, "Content-Type", "application/json");
        return;
    }

    char hash[65];
    hash_password(pass, hash);
    
    int reg_res = db_register_user(req->db, user, hash);
    printf("Register attempt: user=%s, hash=%s, result=%d\n", user, hash, reg_res);
    
    cJSON *reply = cJSON_CreateObject();
    if (reg_res == 0) {
        cJSON_AddStringToObject(reply, "status", "ok");
    } else {
        cJSON_AddStringToObject(reply, "error", "Username already exists");
        res->status_code = 409;
    }
    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cJSON_Delete(json);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void rankings_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *ranks = db_get_rankings(req->db);
    char *str = cJSON_PrintUnformatted(ranks);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(ranks);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void user_info_handler(cwist_http_request *req, cwist_http_response *res) {
    const char *uid_str = cwist_query_map_get(req->query_params, "user_id");
    if (!uid_str) { res->status_code = 400; return; }
    cJSON *info = db_get_user_info(req->db, atoi(uid_str));
    if (info) {
        char *str = cJSON_PrintUnformatted(info);
        cwist_sstring_assign(res->body, str);
        cev_mem_free(str);
        cJSON_Delete(info);
    } else {
        res->status_code = 404;
    }
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}


void rooms_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *rooms = db_get_multiplayer_rooms(req->db);
    char *str = cJSON_PrintUnformatted(rooms);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(rooms);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void sessions_handler(cwist_http_request *req, cwist_http_response *res) {
    char identity[128];
    build_session_identity(req, identity, sizeof(identity));
    const char *type = cwist_query_map_get(req->query_params, "type");
    if (!type || (strcmp(type, "singleplayer") != 0 && strcmp(type, "multiplayer") != 0)) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cwist_sstring_assign(res->body, "{\"error\":\"type must be singleplayer or multiplayer\"}");
        cwist_http_header_add(&res->headers, "Content-Type", "application/json");
        return;
    }
    const char *limit_str = cwist_query_map_get(req->query_params, "limit");
    int limit = parse_positive_int_or_default(limit_str, 8);
    cJSON *sessions = db_get_recent_sessions(req->db, identity, type, limit);
    cJSON *reply = cJSON_CreateObject();
    cJSON_AddStringToObject(reply, "identity", identity);
    cJSON_AddStringToObject(reply, "type", type);
    cJSON_AddItemToObject(reply, "sessions", sessions);
    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void sessions_log_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *json = cJSON_Parse(req->body->data);
    if (!json) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        return;
    }

    cJSON *guest_item = cJSON_GetObjectItem(json, "guest_id");
    cJSON *user_item = cJSON_GetObjectItem(json, "user_id");
    cJSON *type_item = cJSON_GetObjectItem(json, "session_type");
    cJSON *mode_item = cJSON_GetObjectItem(json, "mode");
    cJSON *difficulty_item = cJSON_GetObjectItem(json, "difficulty");
    cJSON *room_item = cJSON_GetObjectItem(json, "room_id");

    if (!type_item || !type_item->valuestring) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cJSON_Delete(json);
        return;
    }
    if (strcmp(type_item->valuestring, "singleplayer") != 0 && strcmp(type_item->valuestring, "multiplayer") != 0) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cJSON_Delete(json);
        return;
    }

    char identity[128];
    build_identity_from_json(user_item, guest_item, identity, sizeof(identity));

    const char *mode = (mode_item && mode_item->valuestring) ? mode_item->valuestring : "othello";
    const char *difficulty = (difficulty_item && difficulty_item->valuestring) ? difficulty_item->valuestring : "";
    int room_id = (room_item && cJSON_IsNumber(room_item)) ? room_item->valueint : 0;
    int rc = db_log_game_session(req->db, identity, type_item->valuestring, mode, difficulty, room_id);
    if (rc != 0) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cwist_sstring_assign(res->body, "{\"error\":\"failed to log session\"}");
        cJSON_Delete(json);
        cwist_http_header_add(&res->headers, "Content-Type", "application/json");
        return;
    }

    cJSON *reply = cJSON_CreateObject();
    cJSON_AddStringToObject(reply, "status", "ok");
    cJSON_AddStringToObject(reply, "identity", identity);
    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cJSON_Delete(json);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

static void build_identity(cwist_http_request *req, char *identity, size_t n) {
    const char *user_id_str = cwist_query_map_get(req->query_params, "user_id");
    const char *guest_id = cwist_query_map_get(req->query_params, "guest_id");
    if (user_id_str && strlen(user_id_str) > 0 && atoi(user_id_str) > 0) {
        snprintf(identity, n, "user:%d", atoi(user_id_str));
    } else if (guest_id && strlen(guest_id) > 0) {
        snprintf(identity, n, "guest:%s", guest_id);
    } else {
        snprintf(identity, n, "guest:default");
    }
}

void betting_enter_handler(cwist_http_request *req, cwist_http_response *res) {
    char identity[128];
    build_identity(req, identity, sizeof(identity));
    int points = BETTING_START_POINTS;
    if (db_get_betting_points(req->db, identity, &points) != 0) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        return;
    }

    cJSON *reply = cJSON_CreateObject();
    cJSON_AddStringToObject(reply, "identity", identity);
    cJSON_AddNumberToObject(reply, "points", points);
    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void betting_slots_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *slots = db_get_betting_slots(req->db);
    cJSON *reply = cJSON_CreateObject();
    cJSON_AddItemToObject(reply, "slots", slots);

    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void betting_rankings_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *ranks = db_get_betting_rankings(req->db);
    cJSON *reply = cJSON_CreateObject();
    cJSON_AddItemToObject(reply, "rankings", ranks);
    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void betting_place_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *json = cJSON_Parse(req->body->data);
    if (!json) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        return;
    }

    cJSON *slot_item = cJSON_GetObjectItem(json, "slot_id");
    cJSON *outcome_item = cJSON_GetObjectItem(json, "outcome");
    cJSON *amount_item = cJSON_GetObjectItem(json, "amount");
    cJSON *guest_item = cJSON_GetObjectItem(json, "guest_id");
    cJSON *user_item = cJSON_GetObjectItem(json, "user_id");

    char identity[128];
    if (user_item && cJSON_IsNumber(user_item) && user_item->valueint > 0) {
        snprintf(identity, sizeof(identity), "user:%d", user_item->valueint);
    } else if (guest_item && guest_item->valuestring && strlen(guest_item->valuestring) > 0) {
        snprintf(identity, sizeof(identity), "guest:%s", guest_item->valuestring);
    } else {
        snprintf(identity, sizeof(identity), "guest:default");
    }

    if (!slot_item || !outcome_item || !amount_item || !outcome_item->valuestring) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cJSON_Delete(json);
        return;
    }

    cJSON *result = NULL;
    int rc = db_apply_bet(req->db, identity, slot_item->valueint, outcome_item->valuestring, amount_item->valueint, &result);
    if (rc != 0) {
        cJSON *err = cJSON_CreateObject();
        if (rc == -3) cJSON_AddStringToObject(err, "error", "Bet amount exceeds current points");
        else cJSON_AddStringToObject(err, "error", "Invalid bet request");
        char *err_str = cJSON_PrintUnformatted(err);
        cwist_sstring_assign(res->body, err_str);
        cev_mem_free(err_str);
        cJSON_Delete(err);
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cJSON_Delete(json);
        return;
    }

    char *str = cJSON_PrintUnformatted(result);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(result);
    cJSON_Delete(json);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void betting_multiplayer_place_handler(cwist_http_request *req, cwist_http_response *res) {
    cJSON *json = cJSON_Parse(req->body->data);
    if (!json) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        return;
    }

    cJSON *room_item = cJSON_GetObjectItem(json, "room_id");
    cJSON *target_item = cJSON_GetObjectItem(json, "target_player");
    cJSON *amount_item = cJSON_GetObjectItem(json, "amount");
    cJSON *guest_item = cJSON_GetObjectItem(json, "guest_id");
    cJSON *user_item = cJSON_GetObjectItem(json, "user_id");
    if (!room_item || !target_item || !amount_item || !cJSON_IsNumber(room_item) || !cJSON_IsNumber(target_item) || !cJSON_IsNumber(amount_item)) {
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cJSON_Delete(json);
        return;
    }

    char identity[128];
    build_identity_from_json(user_item, guest_item, identity, sizeof(identity));

    cJSON *result = NULL;
    int rc = db_place_multiplayer_bet(req->db, identity, room_item->valueint, target_item->valueint, amount_item->valueint, &result);
    if (rc != 0) {
        cJSON *err = cJSON_CreateObject();
        if (rc == -3) cJSON_AddStringToObject(err, "error", "Bet amount exceeds allowed point range");
        else cJSON_AddStringToObject(err, "error", "Invalid multiplayer bet request");
        char *err_str = cJSON_PrintUnformatted(err);
        cwist_sstring_assign(res->body, err_str);
        cev_mem_free(err_str);
        cJSON_Delete(err);
        res->status_code = CWIST_HTTP_BAD_REQUEST;
        cJSON_Delete(json);
        return;
    }

    char *str = cJSON_PrintUnformatted(result);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(result);
    cJSON_Delete(json);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void betting_multiplayer_history_handler(cwist_http_request *req, cwist_http_response *res) {
    const char *room_str = cwist_query_map_get(req->query_params, "room_id");
    int room_id = room_str ? atoi(room_str) : 0;
    char identity[128];
    build_identity(req, identity, sizeof(identity));

    cJSON *history = db_get_multiplayer_bet_history(req->db, identity, room_id);
    cJSON *reply = cJSON_CreateObject();
    cJSON_AddStringToObject(reply, "identity", identity);
    cJSON_AddItemToObject(reply, "bets", history);
    char *str = cJSON_PrintUnformatted(reply);
    cwist_sstring_assign(res->body, str);
    cev_mem_free(str);
    cJSON_Delete(reply);
    cwist_http_header_add(&res->headers, "Content-Type", "application/json");
}

void root_handler(cwist_http_request *req, cwist_http_response *res) {
    const char *room_str = cwist_query_map_get(req->query_params, "room");
    cJSON *context = cJSON_CreateObject();
    
    // Default context for Lobby
    cJSON_AddNumberToObject(context, "room_id", 0);
    cJSON_AddStringToObject(context, "mode", "othello");
    cJSON_AddStringToObject(context, "status", "waiting");
    cJSON_AddNumberToObject(context, "turn_val", 1);
    cJSON_AddStringToObject(context, "turn_text", "Black's Turn");
    cJSON_AddNumberToObject(context, "score_black", 0);
    cJSON_AddNumberToObject(context, "score_white", 0);
    cJSON_AddStringToObject(context, "board_html", "");
    cJSON_AddStringToObject(context, "board_json", "[]");

    if (room_str) {
        int room_id = atoi(room_str);
        int board[SIZE][SIZE];
        int turn, players;
        char status[32];
        char mode[16];
        
        get_game_state(req->db, room_id, board, &turn, status, &players, mode, NULL);
        
        cJSON_ReplaceItemInObject(context, "room_id", cJSON_CreateNumber(room_id));
        cJSON_ReplaceItemInObject(context, "mode", cJSON_CreateString(mode));
        cJSON_ReplaceItemInObject(context, "status", cJSON_CreateString(status));
        cJSON_ReplaceItemInObject(context, "turn_val", cJSON_CreateNumber(turn));
        cJSON_ReplaceItemInObject(context, "turn_text", cJSON_CreateString(turn == 1 ? "Black's Turn" : "White's Turn"));
        
        int black_score = 0;
        int white_score = 0;
        for(int r=0; r<SIZE; r++)
            for(int c=0; c<SIZE; c++) {
                if(board[r][c] == 1) black_score++;
                else if(board[r][c] == 2) white_score++;
            }
        cJSON_ReplaceItemInObject(context, "score_black", cJSON_CreateNumber(black_score));
        cJSON_ReplaceItemInObject(context, "score_white", cJSON_CreateNumber(white_score));

        // Add board array for JS state
        cJSON *board_arr = cJSON_CreateArray();
        for(int r=0; r<SIZE; r++) {
            for(int c=0; c<SIZE; c++) {
                cJSON_AddItemToArray(board_arr, cJSON_CreateNumber(board[r][c]));
            }
        }
        
        char *board_json_str = cJSON_PrintUnformatted(board_arr);
        cJSON_ReplaceItemInObject(context, "board_json", cJSON_CreateString(board_json_str));
        cev_mem_free(board_json_str);
        cJSON_Delete(board_arr); // board_arr was only used for string print

        cwist_sstring *cells_html = cwist_sstring_create();
        cwist_sstring_assign(cells_html, "");
        
        for(int r=0; r<SIZE; r++) {
            for(int c=0; c<SIZE; c++) {
                cwist_html_element_t *cell = cwist_html_element_create("div");
                cwist_html_element_add_class(cell, "cell");
                
                if (board[r][c] != 0) {
                    cwist_html_element_t *disc = cwist_html_element_create("div");
                    cwist_html_element_add_class(disc, "disc");
                    cwist_html_element_add_class(disc, board[r][c] == 1 ? "black" : "white");
                    cwist_html_element_add_child(cell, disc);
                }
                
                cwist_sstring *cell_str = cwist_html_render(cell);
                cwist_sstring_append_sstring(cells_html, cell_str);
                cwist_sstring_destroy(cell_str);
                cwist_html_element_destroy(cell);
            }
        }
        
        cJSON_ReplaceItemInObject(context, "board_html", cJSON_CreateString(cells_html->data));
        cwist_sstring_destroy(cells_html);
    }
    
    cwist_sstring *rendered = cwist_template_render_file("index.html.tmpl", context);
    if (rendered) {
        cwist_sstring_assign(res->body, rendered->data);
        cwist_sstring_destroy(rendered);
    } else {
        res->status_code = 500;
        cwist_sstring_assign(res->body, "Template Error");
    }
    
    cJSON_Delete(context);
    cwist_http_header_add(&res->headers, "Content-Type", "text/html");
}
