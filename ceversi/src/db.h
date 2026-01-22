#ifndef DB_H
#define DB_H

#include <cwist/sql.h>
#include "common.h"

extern cwist_db *db_conn;

void init_db(cwist_db *db);
void cleanup_stale_rooms(cwist_db *db);
void get_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int *turn, char *status, int *players, char *mode, const char *requested_mode);
void update_game_state(cwist_db *db, int room_id, int board[SIZE][SIZE], int turn, const char *status, int players, const char *mode);
int db_join_game(cwist_db *db, int room_id, const char *requested_mode, int *player_id, char *mode);

#endif