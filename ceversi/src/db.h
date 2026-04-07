#ifndef DB_H
#define DB_H

#include <cwist/core/db/sql.h>
#include <cjson/cJSON.h>
#include "common.h"

extern cwist_db *db_conn;

void init_db(cwist_db *db);
void cleanup_stale_rooms(cwist_db *db);
void get_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int *turn, char *status, int *players, char *mode, const char *requested_mode);
void update_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int turn, const char *status, int players, const char *mode);
int db_join_game(cwist_db *db, int room_id, const char *requested_mode, int *player_id, char *mode, int user_id);
void db_leave_game(cwist_db *db, int room_id, int player_id, int user_id);
void db_reset_room(cwist_db *db, int room_id);
void db_record_result(cwist_db *db, int room_id, int winner_pid);

int db_register_user(cwist_db *db, const char *username, const char *password_hash);
int db_login_user(cwist_db *db, const char *username, const char *password_hash);
cJSON *db_get_rankings(cwist_db *db);
cJSON *db_get_user_info(cwist_db *db, int user_id);
cJSON *db_get_multiplayer_rooms(cwist_db *db);
int db_log_game_session(cwist_db *db, const char *identity, const char *session_type, const char *mode, const char *difficulty, int room_id);
cJSON *db_get_recent_sessions(cwist_db *db, const char *identity, const char *session_type, int limit);

void db_refresh_betting_slots(cwist_db *db);
cJSON *db_get_betting_slots(cwist_db *db);
int db_get_betting_points(cwist_db *db, const char *identity, int *points);
int db_apply_bet(cwist_db *db, const char *identity, int slot_id, const char *outcome, int amount, cJSON **result_json);
cJSON *db_get_betting_rankings(cwist_db *db);
int db_place_multiplayer_bet(cwist_db *db, const char *identity, int room_id, int target_player, int amount, cJSON **result_json);
int db_settle_multiplayer_bets(cwist_db *db, int room_id, int winner_player, cJSON **settle_json);
cJSON *db_get_multiplayer_bet_history(cwist_db *db, const char *identity, int room_id);

#endif
