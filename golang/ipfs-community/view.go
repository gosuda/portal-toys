package main

import (
	"embed"
	"fmt"
	"html/template"
	"net/http"
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

// In-memory store (for demo). Later this can be backed by Pebble + IPFS.
type Store struct {
	mu     sync.RWMutex
	posts  map[string]*Post
	order  []string
	lastID int64
}

// IndexPageData is the view model for the list page.
type IndexPageData struct {
	Posts      []*Post
	Page       int
	TotalPages int
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

func (s *Store) GetPost(id string) *Post {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.posts[id]
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

// ListPostsPaged returns a single page of posts (newest first) and
// the resolved current page and total pages for pagination UI.
func (s *Store) ListPostsPaged(page, perPage int) (posts []*Post, currentPage, totalPages int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if perPage <= 0 {
		perPage = 20
	}
	total := len(s.order)
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
	start := min((page-1)*perPage, total)
	end := start + perPage
	if end > total {
		end = total
	}
	out := make([]*Post, 0, end-start)
	for _, id := range s.order[start:end] {
		if p, ok := s.posts[id]; ok {
			out = append(out, p)
		}
	}
	return out, page, totalPages
}

//go:embed static/*
var staticFS embed.FS

var (
	indexTmpl = template.Must(
		template.New("index").
			Funcs(template.FuncMap{
				"inc": func(i int) int { return i + 1 },
				"dec": func(i int) int {
					if i <= 1 {
						return 1
					}
					return i - 1
				},
			}).
			ParseFS(staticFS, "static/index.html"),
	)
	postTmpl  = template.Must(template.New("post").ParseFS(staticFS, "static/post.html"))
	writeTmpl = template.Must(template.New("write").ParseFS(staticFS, "static/write.html"))
)

// NewHandler builds the HTTP handler for the community UI.
func NewHandler(store *Store) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/static/style.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		data, err := staticFS.ReadFile("static/style.css")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(data)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Only exact "/" goes to the list page; others fall through.
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		switch r.Method {
		case http.MethodGet:
			page := 1
			if ps := r.URL.Query().Get("page"); ps != "" {
				if n, err := strconv.Atoi(ps); err == nil && n > 0 {
					page = n
				}
			}
			posts, cur, totalPages := store.ListPostsPaged(page, 20)
			data := IndexPageData{
				Posts:      posts,
				Page:       cur,
				TotalPages: totalPages,
			}
			_ = indexTmpl.Execute(w, data)
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			author := r.FormValue("author")
			if author == "" {
				author = "anon"
			}
			title := r.FormValue("title")
			body := r.FormValue("body")
			if title == "" || body == "" {
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			store.AddPost(author, title, body)
			http.Redirect(w, r, "/", http.StatusSeeOther)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/write", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = writeTmpl.Execute(w, nil)
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			author := r.FormValue("author")
			if author == "" {
				author = "anon"
			}
			title := r.FormValue("title")
			body := r.FormValue("body")
			if title == "" || body == "" {
				http.Redirect(w, r, "/write", http.StatusSeeOther)
				return
			}
			post := store.AddPost(author, title, body)
			http.Redirect(w, r, "/post/"+post.ID, http.StatusSeeOther)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/post/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/post/")
		id = strings.TrimSuffix(id, "/")
		if id == "" {
			http.NotFound(w, r)
			return
		}

		switch r.Method {
		case http.MethodGet:
			post := store.GetPost(id)
			if post == nil {
				http.NotFound(w, r)
				return
			}
			_ = postTmpl.Execute(w, post)
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			post := store.GetPost(id)
			if post == nil {
				http.NotFound(w, r)
				return
			}
			author := r.FormValue("author")
			if author == "" {
				author = "anon"
			}
			body := r.FormValue("body")
			if body == "" {
				http.Redirect(w, r, "/post/"+id, http.StatusSeeOther)
				return
			}
			store.AddComment(id, author, body)
			http.Redirect(w, r, "/post/"+id, http.StatusSeeOther)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}
