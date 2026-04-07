#include "betting_logic.h"

int wasm_betting_single_delta(int amount, double odds_x1000, int success) {
    double odds = odds_x1000 / 1000.0;
    return betting_single_delta(amount, odds, success);
}

int wasm_betting_can_wager(int current_points, int amount) {
    return betting_can_wager(current_points, amount);
}

int wasm_betting_project_points(int current_points, int delta) {
    return betting_clamp_points((long long)current_points + delta);
}

long long wasm_betting_multiplayer_reward(int winner_player, int target_player, int amount, long long total_pool, long long total_winner_bet) {
    return betting_multiplayer_reward(winner_player, target_player, amount, total_pool, total_winner_bet);
}
