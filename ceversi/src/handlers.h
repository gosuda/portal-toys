#ifndef HANDLERS_H
#define HANDLERS_H

#include <cwist/net/http/http.h>

void root_handler(cwist_http_request *req, cwist_http_response *res);
void join_handler(cwist_http_request *req, cwist_http_response *res);
void leave_handler(cwist_http_request *req, cwist_http_response *res);
void state_handler(cwist_http_request *req, cwist_http_response *res);
void move_handler(cwist_http_request *req, cwist_http_response *res);

void login_handler(cwist_http_request *req, cwist_http_response *res);
void register_handler(cwist_http_request *req, cwist_http_response *res);
void rankings_handler(cwist_http_request *req, cwist_http_response *res);
void user_info_handler(cwist_http_request *req, cwist_http_response *res);
void rooms_handler(cwist_http_request *req, cwist_http_response *res);
void sessions_handler(cwist_http_request *req, cwist_http_response *res);
void sessions_log_handler(cwist_http_request *req, cwist_http_response *res);
void betting_enter_handler(cwist_http_request *req, cwist_http_response *res);
void betting_slots_handler(cwist_http_request *req, cwist_http_response *res);
void betting_place_handler(cwist_http_request *req, cwist_http_response *res);
void betting_rankings_handler(cwist_http_request *req, cwist_http_response *res);
void betting_multiplayer_place_handler(cwist_http_request *req, cwist_http_response *res);
void betting_multiplayer_history_handler(cwist_http_request *req, cwist_http_response *res);

#endif
