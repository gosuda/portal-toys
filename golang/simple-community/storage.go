package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cockroachdb/pebble/v2"
)

// Post is a simple community post with optional comments.
type Post struct {
	ID        string
	Author    string
	Title     string
	Body      string
	Password  string
	CreatedAt time.Time
	Upvotes   int
	Downvotes int
	Comments  []Comment
}

type Comment struct {
	ID        string
	PostID    string
	Author    string
	Body      string
	Password  string
	CreatedAt time.Time
}

// IndexPost is a view model for the list page, carrying
// a display number in addition to the post contents.
type IndexPost struct {
	*Post
	Number int
}

// IndexPageData is the view model for the list page.
type IndexPageData struct {
	Posts      []*IndexPost
	Page       int
	TotalPages int
}

var (
	storeMu sync.RWMutex
	posts   = make(map[string]*Post)
	order   = make([]string, 0, 64)
	lastID  int64

	db *pebble.DB
)

func nextID() string {
	lastID++
	// Timestamp plus zero-padded counter; string-sorts by creation time.
	return time.Now().Format("20060102150405") + "-" + fmt.Sprintf("%06d", lastID)
}

func AddPost(author, title, body, password string) *Post {
	storeMu.Lock()
	defer storeMu.Unlock()
	body = strings.TrimSpace(body)
	if len(order) > 0 {
		if last := posts[order[0]]; last != nil {
			if last.Author == author && last.Title == title && last.Body == body {
				// Reuse the existing post instead of creating a new one.
				return last
			}
		}
	}
	id := nextID()
	p := &Post{
		ID:        id,
		Author:    author,
		Title:     title,
		Body:      body,
		Password:  password,
		CreatedAt: time.Now(),
		Comments:  []Comment{},
	}
	posts[id] = p
	order = append([]string{id}, order...)
	return p
}

func AddComment(postID, author, body, password string) *Comment {
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
		Password:  password,
		CreatedAt: time.Now(),
	}
	p.Comments = append(p.Comments, c)
	return &c
}

// DeletePost deletes a post if the password matches (or if the stored password is empty).
// Returns true if the post was deleted.
func DeletePost(id, password string) bool {
	storeMu.Lock()
	defer storeMu.Unlock()
	p, ok := posts[id]
	if !ok {
		return false
	}
	if p.Password != "" && p.Password != password {
		return false
	}
	delete(posts, id)
	for i, pid := range order {
		if pid == id {
			order = append(order[:i], order[i+1:]...)
			break
		}
	}
	return true
}

// DeleteComment deletes a single comment on a post if the password matches
// (or if the stored password is empty). Returns true if deleted.
func DeleteComment(postID, commentID, password string) bool {
	storeMu.Lock()
	defer storeMu.Unlock()
	p, ok := posts[postID]
	if !ok {
		return false
	}
	for i, c := range p.Comments {
		if c.ID != commentID {
			continue
		}
		if c.Password != "" && c.Password != password {
			return false
		}
		p.Comments = append(p.Comments[:i], p.Comments[i+1:]...)
		return true
	}
	return false
}

// VotePost applies a delta to the upvote/downvote counters for a post.
// Positive deltas increase the counters; negative deltas decrease them.
func VotePost(id string, upDelta, downDelta int) *Post {
	storeMu.Lock()
	defer storeMu.Unlock()
	p, ok := posts[id]
	if !ok {
		return nil
	}
	p.Upvotes += upDelta
	p.Downvotes += downDelta
	if p.Upvotes < 0 {
		p.Upvotes = 0
	}
	if p.Downvotes < 0 {
		p.Downvotes = 0
	}
	return p
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

// InitStore initializes the Pebble-backed datastore for snapshots.
func InitStore(dir string) error {
	if dir == "" {
		return nil
	}
	dbPath := filepath.Join(dir, "pebble-snapshot")
	pdb, err := pebble.Open(dbPath, &pebble.Options{})
	if err != nil {
		return fmt.Errorf("open pebble db: %w", err)
	}
	db = pdb
	return nil
}

// LoadSnapshotBytes applies a snapshot JSON blob (same format as SaveSnapshot uses).
func LoadSnapshotBytes(data []byte) error {
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

// LoadSnapshot loads the last stored snapshot from the Pebble datastore, if any.
func LoadSnapshot(ctx context.Context) error {
	if db == nil {
		return nil
	}
	data, closer, err := db.Get([]byte("snapshot-json"))
	if err != nil {
		if err == pebble.ErrNotFound {
			return nil
		}
		return fmt.Errorf("load snapshot: %w", err)
	}
	defer closer.Close()
	buf := make([]byte, len(data))
	copy(buf, data)
	return LoadSnapshotBytes(buf)
}

// SaveSnapshot writes the current snapshot to the Pebble datastore.
func SaveSnapshot(ctx context.Context) error {
	if db == nil {
		return nil
	}
	data, err := json.Marshal(snapshot{Posts: ListPosts()})
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	return db.Set([]byte("snapshot-json"), data, pebble.Sync)
}
