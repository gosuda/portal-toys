package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Post is a simple community post with optional comments.
type Post struct {
	ID        string
	Author    string
	Title     string
	Body      string
	CreatedAt time.Time
	Comments  []Comment
}

type Comment struct {
	ID        string
	PostID    string
	Author    string
	Body      string
	CreatedAt time.Time
}

// IndexPageData is the view model for the list page.
type IndexPageData struct {
	Posts      []*Post
	Page       int
	TotalPages int
}

var (
	storeMu sync.RWMutex
	posts   = make(map[string]*Post)
	order   = make([]string, 0, 64)
	lastID  int64
)

func nextID() string {
	storeMu.Lock()
	defer storeMu.Unlock()
	lastID++
	// Timestamp plus zero-padded counter; string-sorts by creation time.
	return time.Now().Format("20060102150405") + "-" + fmt.Sprintf("%06d", lastID)
}

func AddPost(author, title, body string) *Post {
	storeMu.Lock()
	defer storeMu.Unlock()
	body = strings.TrimSpace(body)
	id := nextID()
	p := &Post{
		ID:        id,
		Author:    author,
		Title:     title,
		Body:      body,
		CreatedAt: time.Now(),
		Comments:  []Comment{},
	}
	posts[id] = p
	order = append([]string{id}, order...)
	return p
}

func AddComment(postID, author, body string) *Comment {
	storeMu.Lock()
	defer storeMu.Unlock()
	p, ok := posts[postID]
	if !ok {
		return nil
	}
	body = strings.TrimSpace(body)
	id := nextID()
	c := Comment{
		ID:        id,
		PostID:    postID,
		Author:    author,
		Body:      body,
		CreatedAt: time.Now(),
	}
	p.Comments = append(p.Comments, c)
	return &c
}

func GetPost(id string) *Post {
	storeMu.RLock()
	defer storeMu.RUnlock()
	return posts[id]
}

func ListPosts() []*Post {
	storeMu.RLock()
	defer storeMu.RUnlock()
	out := make([]*Post, 0, len(order))
	for _, id := range order {
		if p, ok := posts[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

// ListPostsPaged returns a single page of posts (newest first) and
// the resolved current page and total pages for pagination UI.
func ListPostsPaged(page, perPage int) (out []*Post, currentPage, totalPages int) {
	storeMu.RLock()
	defer storeMu.RUnlock()
	if perPage <= 0 {
		perPage = 20
	}
	total := len(order)
	if total == 0 {
		return []*Post{}, 1, 1
	}
	totalPages = (total + perPage - 1) / perPage
	if totalPages < 1 {
		totalPages = 1
	}
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	out = make([]*Post, 0, end-start)
	for _, id := range order[start:end] {
		if p, ok := posts[id]; ok {
			out = append(out, p)
		}
	}
	return out, page, totalPages
}

type snapshot struct {
	Posts []*Post `json:"posts"`
}

func loadFromLedger(ctx context.Context, ledger *ipfsLedger) error {
	if ledger == nil {
		return nil
	}
	root, err := ledger.RootCID(ctx)
	if err != nil {
		return err
	}
	if !root.Defined() {
		return nil
	}
	data, err := ledger.GetRaw(ctx, root)
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return err
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	posts = make(map[string]*Post, len(snap.Posts))
	order = make([]string, 0, len(snap.Posts))
	lastID = 0
	for _, p := range snap.Posts {
		posts[p.ID] = p
		order = append(order, p.ID)
		parts := strings.Split(p.ID, "-")
		if len(parts) == 2 {
			if n, err := strconv.ParseInt(parts[1], 10, 64); err == nil && n > lastID {
				lastID = n
			}
		}
	}
	return nil
}

func saveToLedger(ctx context.Context, ledger *ipfsLedger) error {
	if ledger == nil {
		return nil
	}
	data, err := json.Marshal(snapshot{Posts: ListPosts()})
	if err != nil {
		return err
	}
	c, err := ledger.PutRaw(ctx, data)
	if err != nil {
		return err
	}
	return ledger.SetRootCID(ctx, c)
}
