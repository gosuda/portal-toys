#ifndef DB_H
#define DB_H

#include <cwist/core/db/sql.h>
#include "common.h"

extern cwist_db *db_conn;

void init_db(cwist_db *db);
void cleanup_stale_rooms(cwist_db *db);
void get_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int *turn, char *status, int *players, char *mode, const char *requested_mode);
void update_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int turn, const char *status, int players, const char *mode);
int db_join_game(cwist_db *db, int room_id, const char *requested_mode, int *player_id, char *mode, int user_id);
void db_record_result(cwist_db *db, int room_id, int winner_pid);

int db_register_user(cwist_db *db, const char *username, const char *password_hash);
int db_login_user(cwist_db *db, const char *username, const char *password_hash);
cJSON *db_get_rankings(cwist_db *db);
cJSON *db_get_user_info(cwist_db *db, int user_id);

#endif
