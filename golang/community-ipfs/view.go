package main

import (
	_ "embed"
	"fmt"
	"html/template"
	"net/http"
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

// In-memory store (for demo). Later this can be backed by Pebble + IPFS.
type Store struct {
	mu     sync.RWMutex
	posts  map[string]*Post
	order  []string
	lastID int64
}

func NewStore() *Store {
	return &Store{
		posts: make(map[string]*Post),
		order: make([]string, 0, 64),
	}
}

func (s *Store) nextID() string {
	s.lastID++
	// Timestamp plus zero-padded counter; string-sorts by creation time.
	return time.Now().Format("20060102150405") + "-" + fmt.Sprintf("%06d", s.lastID)
}

func (s *Store) AddPost(author, title, body string) *Post {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID()
	p := &Post{
		ID:        id,
		Author:    author,
		Title:     title,
		Body:      body,
		CreatedAt: time.Now(),
		Comments:  []Comment{},
	}
	s.posts[id] = p
	s.order = append([]string{id}, s.order...)
	return p
}

func (s *Store) AddComment(postID, author, body string) *Comment {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.posts[postID]
	if !ok {
		return nil
	}
	id := s.nextID()
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

func (s *Store) ListPosts() []*Post {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Post, 0, len(s.order))
	for _, id := range s.order {
		if p, ok := s.posts[id]; ok {
			out = append(out, p)
		}
	}
	return out
}

//go:embed static/index.html
var indexHTML string

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

// NewHandler builds the HTTP handler for the community UI.
func NewHandler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			posts := store.ListPosts()
			_ = indexTmpl.Execute(w, posts)
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			kind := r.FormValue("kind")
			author := r.FormValue("author")
			if author == "" {
				author = "anon"
			}
			body := r.FormValue("body")

			switch kind {
			case "post":
				title := r.FormValue("title")
				if title == "" || body == "" {
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}
				store.AddPost(author, title, body)
			case "comment":
				postID := r.FormValue("post_id")
				if postID == "" || body == "" {
					http.Redirect(w, r, "/", http.StatusSeeOther)
					return
				}
				store.AddComment(postID, author, body)
			}

			http.Redirect(w, r, "/", http.StatusSeeOther)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}
