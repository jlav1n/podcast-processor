package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
)

var (
	bucketName    = os.Getenv("GCS_BUCKET")
	indexObject   = getEnv("GCS_INDEX_OBJECT", "index.xml")
	port          = getEnv("PORT", "8080")
	projectID     = os.Getenv("GCP_PROJECT_ID")
	gcsClient     *storage.Client
	cachedContent string
	cacheMutex    sync.RWMutex
	cacheTime     time.Time
	cacheTTL      = 60 * time.Second
)

const xmlItemTemplate = `     <item>
         <title>%s</title>
         <pubDate>%s</pubDate>
         <enclosure url="https://joshlavin.com/feeds/%s" length="%d" type="audio/mpeg" />
     </item>`

func getEnv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func init() {
	if bucketName == "" {
		log.Fatal("GCS_BUCKET not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var err error
	gcsClient, err = storage.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to create GCS client: %v", err)
	}
}

func getIndexXML(ctx context.Context) (string, error) {
	cacheMutex.RLock()
	if cachedContent != "" && time.Since(cacheTime) < cacheTTL {
		defer cacheMutex.RUnlock()
		return cachedContent, nil
	}
	cacheMutex.RUnlock()

	reader, err := gcsClient.Bucket(bucketName).Object(indexObject).NewReader(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to read index.xml: %w", err)
	}
	defer reader.Close()

	content, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read index.xml content: %w", err)
	}

	cacheMutex.Lock()
	cachedContent = string(content)
	cacheTime = time.Now()
	cacheMutex.Unlock()

	return cachedContent, nil
}

func processFiles(ctx context.Context) error {
	log.Println("Starting file processing...")

	// List files in GCS bucket
	it := gcsClient.Bucket(bucketName).Objects(ctx, &storage.Query{Prefix: "files/"})

	var items []string

	for {
		attrs, err := it.Next()
		if err == storage.ErrObjectNotExist {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to list objects: %w", err)
		}

		if !strings.HasSuffix(strings.ToLower(attrs.Name), ".mp3") && !strings.HasSuffix(strings.ToLower(attrs.Name), ".m4a") {
			continue
		}

		log.Printf("Processing: %s", attrs.Name)

		// Extract metadata from filename
		title := strings.TrimSuffix(filepath.Base(attrs.Name), filepath.Ext(attrs.Name))
		title = sanitizeTitle(title)

		date := time.Now().Format("Mon 02 Jan 2006 03:04:05 PM MST")
		size := attrs.Size

		item := fmt.Sprintf(xmlItemTemplate, title, date, attrs.Name, size)
		items = append(items, item)

		log.Printf("  Title: %s (Size: %d)", title, size)
	}

	// Read existing index.xml
	existingContent, err := getIndexXML(ctx)
	if err != nil {
		log.Printf("Warning: Could not read existing index.xml, starting fresh: %v", err)
		existingContent = ""
	}

	// Remove closing tags from existing content
	if existingContent != "" {
		existingContent = strings.TrimSuffix(existingContent, "</channel>\n</rss>\n")
		existingContent = strings.TrimSuffix(existingContent, "</channel>\n")
		existingContent = strings.TrimSuffix(existingContent, "</rss>\n")
	}

	// Build new content
	var buf bytes.Buffer
	if existingContent != "" {
		buf.WriteString(existingContent)
		buf.WriteString("\n")
	}
	for _, item := range items {
		buf.WriteString(item)
		buf.WriteString("\n")
	}
	buf.WriteString("</channel>\n</rss>\n")

	newContent := buf.String()

	// Write back to GCS
	writer := gcsClient.Bucket(bucketName).Object(indexObject).NewWriter(ctx)
	writer.ContentType = "application/rss+xml; charset=utf-8"

	_, err = io.WriteString(writer, newContent)
	if err != nil {
		writer.Close()
		return fmt.Errorf("failed to write index.xml: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("failed to close writer: %w", err)
	}

	// Clear cache
	cacheMutex.Lock()
	cachedContent = ""
	cacheMutex.Unlock()

	log.Printf("Updated index.xml with %d items", len(items))
	return nil
}

func sanitizeTitle(s string) string {
	// Replace underscores and digits with spaces
	for i := 0; i < len(s); i++ {
		if s[i] == '_' || (s[i] >= '0' && s[i] <= '9') {
			s = s[:i] + " " + s[i+1:]
		}
	}
	// Trim spaces
	return strings.TrimSpace(s)
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok"}`)
}

func feedHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	content, err := getIndexXML(ctx)
	if err != nil {
		log.Printf("Error fetching index.xml: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to fetch podcast feed"}`)
		return
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	fmt.Fprint(w, content)
}

func processHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Minute)
	defer cancel()

	err := processFiles(ctx)
	if err != nil {
		log.Printf("Error processing files: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Processing failed"}`)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"processing completed"}`)
}

func main() {
	defer gcsClient.Close()

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/", feedHandler)
	http.HandleFunc("/feed", feedHandler)
	http.HandleFunc("/index.xml", feedHandler)
	http.HandleFunc("/process", processHandler)

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
