#include "betting_logic.h"
#include <limits.h>

int betting_clamp_points(long long value) {
    if (value < BETTING_MIN_POINTS) return BETTING_MIN_POINTS;
    if (value > INT_MAX) return INT_MAX;
    if (value < INT_MIN) return INT_MIN;
    return (int)value;
}

int betting_should_reset(int points) {
    return points <= BETTING_MIN_POINTS;
}

int betting_reset_if_needed(int points) {
    if (betting_should_reset(points)) return BETTING_START_POINTS;
    return points;
}

int betting_can_wager(int current_points, int amount) {
    if (amount <= 0) return 0;
    return current_points - amount >= BETTING_MIN_POINTS;
}

int betting_single_delta(int amount, double odds, int success) {
    if (amount <= 0) return 0;
    if (!success) return -amount;
    double profit_rate = 0.0;
    if (odds > 1.0) profit_rate = odds - 1.0;
    else if (odds > 0.0) profit_rate = odds;
    else profit_rate = 1.0;
    int profit = (int)((double)amount * profit_rate);
    if (profit <= 0) profit = amount;
    return profit;
}

long long betting_multiplayer_reward(int winner_player, int target_player, int amount, long long total_pool, long long total_winner_bet) {
    if (amount <= 0) return 0;
    if (winner_player == 0) return amount;
    if (target_player == winner_player && total_winner_bet > 0) {
        return ((long long)amount * total_pool) / total_winner_bet;
    }
    return 0;
}
