package main

// module imports
import (
	"database/sql"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/russross/blackfriday/v2"
	_ "modernc.org/sqlite"
)

// Post represents a blog post
type Post struct {
	UniqueID   string    `json:"id"`
	Title      string    `json:"title"`
	PostedDate time.Time `json:"posted_date"`
	Slug       string    `json:"slug"`
	Tags       []string  `json:"tags"`
	Subpart    string
}

// PageData represents data to be displayed on webpage
type PageData struct {
	Message string
}

// home template
var tmpl = template.Must(template.ParseFiles("../templates/index2.html"))
var db *sql.DB

// initialize Databbase
func initDB() {
	var err error
	db, err = sql.Open("sqlite", "./blog.db")
	if err != nil {
		log.Fatalf("Failed  to connect to the database: %v", err)
	}
	createTableQuery := `
	CREATE TABLE IF NOT EXISTS posts (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,	
		posted_date DATE NOT NULL,
		slug TEXT NOT NULL,
		tags TEXT, -- Comma-separated tags
    	subpart TEXT -- Blog preview
	);`

	_, err = db.Exec(createTableQuery)
	if err != nil {
		log.Fatalf("Failed to create table: %v", err)
	}
}

// save post data to database
func savePostToDB(post Post) error {

	query := `INSERT INTO posts (id, title, posted_date, slug, tags, subpart) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := db.Exec(query, post.UniqueID, post.Title, post.PostedDate.Format("2006-01-02"), post.Slug, post.Tags, post.Subpart)
	return err
}

// fetch post data for homepage from database
func fetchPostsFromDB() []Post {
	rows, err := db.Query("SELECT id, title, posted_date, slug, tags, subpart FROM posts ORDER BY posted_date DESC")
	if err != nil {
		log.Fatalf("Query error: %v", err)
	}
	defer rows.Close()

	var posts []Post
	for rows.Next() {
		var post Post
		var dateStr string
		if err := rows.Scan(&post.UniqueID, &post.Title, &dateStr, &post.Slug, &post.Tags, &post.Subpart); err != nil {
			log.Println("Row scan error:", err)
			continue
		}

		// parse date to display properly
		parsedTime, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			log.Fatalf("parse date error: %v", err)
		}
		post.PostedDate = parsedTime.Truncate(24 * time.Hour)

		// append post to posts
		posts = append(posts, post)
	}

	if err := rows.Err(); err != nil {
		log.Fatalf("Rows error: %v", err)
	}

	return posts
}

// GenerateSlug creates a URL-friendly slug from a given title
func GenerateSlug(title string) string {
	// Convert the title to lowercase
	slug := strings.ToLower(title)

	// Replace spaces with hyphens
	slug = strings.ReplaceAll(slug, " ", "-")

	// Remove any characters that are not letters, numbers, or hyphens
	re := regexp.MustCompile(`[^a-z0-9-]+`)
	slug = re.ReplaceAllString(slug, "")

	// Trim any trailing hyphens
	slug = strings.Trim(slug, "-")

	return slug
}

// function to group posts by year
func groupPostsByYear(posts []Post) map[int][]Post {
	postsByYear := make(map[int][]Post)
	for _, post := range posts {
		year := post.PostedDate.Year()
		postsByYear[year] = append(postsByYear[year], post)
	}
	return postsByYear
}

// get markdown file and render as html page - for blog posts
func renderPostMarkdown(w http.ResponseWriter, r *http.Request) {

	id := r.URL.Query().Get("id")
	fileName := id + ".md" // Extract file from URL
	markdownPath := filepath.Join("uploads", fileName)

	content, err := os.ReadFile(markdownPath)
	if err != nil {
		http.Error(w, "File not found.", 404)
		return
	}

	// Parse markdown to HTML
	htmlContent := blackfriday.Run(content)

	// render html template
	tmpl, err := template.ParseFiles("../templates/layout.html")
	if err != nil {
		http.Error(w, "Could not load template.", 500)
		return
	}

	tmpl.Execute(w, template.HTML(htmlContent))
}

// render template for home page
func homeHandler(w http.ResponseWriter, r *http.Request) {
	posts := fetchPostsFromDB()

	// Group post by year
	postsByYear := groupPostsByYear(posts)

	if err := tmpl.Execute(w, postsByYear); err != nil {
		http.Error(w, "Template error", http.StatusInternalServerError)
		log.Println("Template error:", err)
	}
}

// submit and save blog post
func submitPostsHandler(w http.ResponseWriter, r *http.Request) {
	// check if POST request
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusBadRequest)
		return
	}

	// Parse form data
	err := r.ParseMultipartForm(10 << 20) // limit to 10 MB
	if err != nil {
		http.Error(w, "Error parsing form data", http.StatusBadRequest)
		return
	}

	// retrieve content data
	id := uuid.New().String()
	title := r.FormValue("title")
	tags := r.FormValue("tags")
	subpart := r.FormValue("subpart")
	slug := GenerateSlug(title)

	// Split the string by comma and strip whitespace
	tagsSlice := strings.Split(tags, ",")

	// Trim any extra whitespace from each tag
	for i, tag := range tagsSlice {
		tagsSlice[i] = strings.TrimSpace(tag)
	}

	// check legnt of characters for subpart, send error if exceeds 20
	if len(subpart) > 20 {
		http.Error(w, "Subpart exceeds 20 characters", http.StatusBadRequest)
		return
	}

	// Handle uploaded Markdown file
	file, handler, err := r.FormFile("content")
	if err != nil {
		http.Error(w, "Error uploading file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	fileContent, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Error reading file", http.StatusInternalServerError)
		return
	}

	// Save the markdown content as a .md file in a local directory
	savePath := filepath.Join("uploads", id+".md")
	err = os.MkdirAll("uploads", os.ModePerm)
	if err != nil {
		http.Error(w, "Error creating upload directory", http.StatusInternalServerError)
		return
	}

	err = os.WriteFile(savePath, fileContent, os.ModePerm)
	if err != nil {
		http.Error(w, "Error saving file", http.StatusInternalServerError)
		return
	}

	// Post struct
	post := Post{
		UniqueID:   id,
		Title:      title,
		PostedDate: time.Now(),
		Slug:       slug,
		Tags:       tagsSlice,
		Subpart:    subpart,
	}

	// save post to database
	err = savePostToDB(post)
	log.Println("Saving post to DB")
	if err != nil {
		http.Error(w, "Failed to save post data", http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(w, "Post submitted successfully with title: %s and content saved as %s", title, handler.Filename)
}

// submit post template handler
func templateHandler(w http.ResponseWriter, r *http.Request) {
	// get template path
	tmplPath := filepath.Join("../templates", "createPost.html")
	tmpl, err := template.ParseFiles(tmplPath)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	data := PageData{
		Message: "Submit a New Blog Post",
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func main() {
	// init db
	initDB()
	defer db.Close()

	// initialize router
	router := mux.NewRouter()

	// Serve static files
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", http.FileServer(http.Dir("../static/"))))

	router.HandleFunc("/", homeHandler)
	router.HandleFunc("/blog/{slug}", renderPostMarkdown)
	router.HandleFunc("/blog/admin/post", templateHandler)
	router.HandleFunc("/blog/admin/submit-post", submitPostsHandler)

	log.Println("Server started at http://localhost:8080")
	log.Fatal(http.ListenAndServe("0.0.0.0:8080", router))
}
