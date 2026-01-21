#ifndef DB_H
#define DB_H

#include <cwist/sql.h>
#include "common.h"

extern cwist_db *db_conn;

void init_db();
void get_game_state(int room_id, int board[SIZE][SIZE], int *turn, char *status, int *players, char *mode, const char *requested_mode);
void update_game_state(int room_id, int board[SIZE][SIZE], int turn, const char *status, int players, const char *mode);

#endif
