#ifndef BETTING_LOGIC_H
#define BETTING_LOGIC_H

#include <stdint.h>

#define BETTING_START_POINTS 1000
#define BETTING_MIN_POINTS -1000

int betting_clamp_points(long long value);
int betting_should_reset(int points);
int betting_reset_if_needed(int points);
int betting_can_wager(int current_points, int amount);
int betting_single_delta(int amount, double odds, int success);
long long betting_multiplayer_reward(int winner_player, int target_player, int amount, long long total_pool, long long total_winner_bet);

#endif
