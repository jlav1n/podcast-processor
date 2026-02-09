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
	"strconv"
	"strings"
	"sync"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/iterator"
)

var (
	bucketName    = os.Getenv("GCS_BUCKET")
	indexObject   = getEnv("GCS_INDEX_OBJECT", "index.xml")
	port          = getEnv("PORT", "8080")
	gcsClient     *storage.Client
	cachedContent string
	cacheMutex    sync.RWMutex
	cacheTime     time.Time
	cacheTTL      = 60 * time.Second
)

const xmlItemTemplate = `     <item>
         <title>%s</title>
         <pubDate>%s</pubDate>
         <enclosure url="https://podcasts.jlavin.com/files/%s" length="%d" type="audio/mpeg" />
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

func getObject(ctx context.Context, filename string) (io.ReadCloser, int64, error) {
	file := "files/" + filename
	obj := gcsClient.Bucket(bucketName).Object(file)

	// Get metadata (no data load)
	attrs, err := obj.Attrs(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get attrs for %s: %w", filename, err)
	}

	// Open streaming reader
	reader, err := obj.NewReader(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read %s: %w", filename, err)
	}

	return reader, attrs.Size, nil
}

func processFiles(ctx context.Context) error {
	log.Println("Starting file processing...")

	// Get files in GCS bucket
	it := gcsClient.Bucket(bucketName).Objects(ctx, &storage.Query{})

	var items []string

	for {
		attrs, err := it.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return fmt.Errorf("list objects: %w", err)
		}

		// Ignore anything that is "inside a folder"
		if strings.Contains(attrs.Name, "/") {
			continue
		}

		if !isAudio(attrs.Name) {
			continue
		}

		log.Printf("processing object=%q size=%d", attrs.Name, attrs.Size)

		// Move it under files/ before proceeding
		if err := moveUnderFiles(ctx, gcsClient, bucketName, attrs.Name); err != nil {
			return fmt.Errorf("moving object %q: %w", attrs.Name, err)
		}

		title := titleFromName(attrs.Name)

		date := time.Now().Format("Mon 02 Jan 2006 03:04:05 PM MST")

		item := fmt.Sprintf(xmlItemTemplate, title, date, attrs.Name, attrs.Size)
		items = append(items, item)
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

func isAudio(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".mp3") || strings.HasSuffix(n, ".m4a")
}

func moveUnderFiles(ctx context.Context, client *storage.Client, bucketName, srcName string) error {
	src := client.Bucket(bucketName).Object(srcName)
	destName := "files/" + srcName
	dest := client.Bucket(bucketName).Object(destName)

	copier := dest.CopierFrom(src)
	_, err := copier.Run(ctx)
	if err != nil {
		return fmt.Errorf("copy %q → %q: %w", srcName, destName, err)
	}

	if err := src.Delete(ctx); err != nil {
		return fmt.Errorf("delete %q: %w", srcName, err)
	}

	return nil
}

func titleFromName(name string) string {
	base := filepath.Base(name)
	title := strings.TrimSuffix(base, filepath.Ext(base))
	return sanitizeTitle(title)
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

func fileHandler(w http.ResponseWriter, r *http.Request) {
	filename := r.PathValue("file")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rc, length, err := getObject(ctx, filename)
	if err != nil {
		log.Printf("Error fetching %s: %v", filename, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to fetch podcast file"}`)
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", strconv.FormatInt(length, 10))

	_, err = io.Copy(w, rc)
	if err != nil {
		log.Printf("Error streaming data for %s: %v", filename, err)
		fmt.Fprintf(w, `{"error":"Failed to fetch podcast file"}`)
		return
	}
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
	http.HandleFunc("/feed", feedHandler)
	http.HandleFunc("/files/{file}", fileHandler)
	http.HandleFunc("/index.xml", feedHandler)
	http.HandleFunc("/process", processHandler)
	http.HandleFunc("/", feedHandler)

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
