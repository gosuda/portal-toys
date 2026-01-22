#ifndef HANDLERS_H
#define HANDLERS_H

#include <cwist/http.h>

void join_handler(cwist_http_request *req, cwist_http_response *res);
void state_handler(cwist_http_request *req, cwist_http_response *res);
void move_handler(cwist_http_request *req, cwist_http_response *res);

#endif