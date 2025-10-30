package main

import (
	"encoding/json"
	"io"
	"time"
)

// Dump is the export/import JSON format: nickname -> list of URLs
type Dump map[string][]string

type queueItem struct {
	URL string `json:"url"`
	ID  string `json:"id,omitempty"`
	By  string `json:"by,omitempty"`
	TS  int64  `json:"ts,omitempty"`
}

// BuildPlaylistFromHistory reconstructs the current playlist (newest first)
// by replaying the hub history messages.
func BuildPlaylistFromHistory(history [][]byte) []queueItem {
	var playlist []queueItem // newest first
	for _, msg := range history {
		var peek struct {
			T string `json:"t"`
		}
		if err := json.Unmarshal(msg, &peek); err != nil {
			continue
		}
		switch peek.T {
		case "ytq-add":
			var m struct {
				URL string `json:"url"`
				ID  string `json:"id"`
				By  string `json:"by"`
				TS  int64  `json:"ts"`
			}
			if err := json.Unmarshal(msg, &m); err == nil {
				if m.TS == 0 {
					m.TS = time.Now().UnixMilli()
				}
				it := queueItem{URL: m.URL, ID: m.ID, By: m.By, TS: m.TS}
				// prepend (newest first)
				playlist = append([]queueItem{it}, playlist...)
			}
		case "ytq-clear":
			playlist = playlist[:0]
		case "ytq-del":
			var m struct {
				Idx *int   `json:"idx"`
				ID  string `json:"id"`
			}
			if err := json.Unmarshal(msg, &m); err == nil {
				if m.Idx != nil {
					i := *m.Idx
					if i >= 0 && i < len(playlist) {
						playlist = append(playlist[:i], playlist[i+1:]...)
						continue
					}
				}
				if m.ID != "" {
					for i, it := range playlist {
						if it.ID == m.ID {
							playlist = append(playlist[:i], playlist[i+1:]...)
							break
						}
					}
				}
			}
		}
	}
	return playlist
}

// BuildDumpFromHistory groups playlist by nickname into Dump.
func BuildDumpFromHistory(history [][]byte) Dump {
	pl := BuildPlaylistFromHistory(history)
	out := make(Dump)
	for _, it := range pl {
		who := it.By
		if who == "" {
			who = "anon"
		}
		out[who] = append(out[who], it.URL)
	}
	return out
}

// ParseDump reads a Dump from an io.Reader.
func ParseDump(r io.Reader) (Dump, error) {
	dec := json.NewDecoder(r)
	// decode into map[string][]any and coerce to []string
	var raw map[string][]interface{}
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	out := make(Dump)
	for nick, arr := range raw {
		for _, v := range arr {
			switch t := v.(type) {
			case string:
				if t != "" {
					out[nick] = append(out[nick], t)
				}
			case map[string]interface{}:
				if u, ok := t["url"].(string); ok && u != "" {
					out[nick] = append(out[nick], u)
				}
			}
		}
	}
	return out, nil
}
