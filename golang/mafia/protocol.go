package main

import "encoding/json"

// ClientMessage is the envelope received from websocket clients.
type ClientMessage struct {
	Type   string          `json:"type"`
	Text   string          `json:"text,omitempty"`
	Target string          `json:"target,omitempty"`
	Index  int             `json:"index,omitempty"`
	Action string          `json:"action,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// ServerEvent is pushed to clients for any room update.
type ServerEvent struct {
	Type   string      `json:"type"`
	Body   string      `json:"body,omitempty"`
	Room   string      `json:"room,omitempty"`
	Phase  string      `json:"phase,omitempty"`
	State  interface{} `json:"state,omitempty"`
	Author string      `json:"author,omitempty"`
}
