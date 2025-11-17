package main

import (
	"embed"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

//go:embed static/*
var staticFS embed.FS

var (
	viewFuncMap = template.FuncMap{
		"inc": func(i int) int { return i + 1 },
		"dec": func(i int) int {
			if i <= 1 {
				return 1
			}
			return i - 1
		},
		// Renders trusted HTML in post body (no escaping).
		"safeHTML": func(s string) template.HTML { return template.HTML(s) },
	}

	viewTmpl = template.Must(
		template.New("").
			Funcs(viewFuncMap).
			ParseFS(staticFS,
				"static/index.html",
				"static/post.html",
				"static/write.html",
			),
	)
)

// NewHandler builds the HTTP handler for the community UI.
func NewHandler(ledger *ipfsLedger) http.Handler {
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
			posts, cur, totalPages := ListPostsPaged(page, 20)
			data := IndexPageData{
				Posts:      posts,
				Page:       cur,
				TotalPages: totalPages,
			}
			_ = viewTmpl.ExecuteTemplate(w, "index.html", data)
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
			AddPost(author, title, body)
			_ = saveToLedger(r.Context(), ledger)
			http.Redirect(w, r, "/", http.StatusSeeOther)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/write", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			_ = viewTmpl.ExecuteTemplate(w, "write.html", nil)
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
			post := AddPost(author, title, body)
			_ = saveToLedger(r.Context(), ledger)
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
			post := GetPost(id)
			if post == nil {
				http.NotFound(w, r)
				return
			}
			_ = viewTmpl.ExecuteTemplate(w, "post.html", post)
		case http.MethodPost:
			if err := r.ParseForm(); err != nil {
				http.Error(w, "bad form", http.StatusBadRequest)
				return
			}
			post := GetPost(id)
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
			AddComment(id, author, body)
			_ = saveToLedger(r.Context(), ledger)
			http.Redirect(w, r, "/post/"+id, http.StatusSeeOther)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return mux
}
