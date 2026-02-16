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

	"encoding/json" // For JSON unmarshalling

	"cloud.google.com/go/storage"
	"golang.org/x/net/http2"     // Import http2 package
	"golang.org/x/net/http2/h2c" // Import h2c for cleartext HTTP/2
)

var (
	bucketName      = os.Getenv("GCS_BUCKET")
	filesBucketName = os.Getenv("GCS_FILES_BUCKET")
	indexObject     = getEnv("GCS_INDEX_OBJECT", "index.xml")
	port            = getEnv("PORT", "8080")
	gcsClient       *storage.Client
	cachedContent   string
	cacheMutex      sync.RWMutex
	cacheTime       time.Time
	cacheTTL        = 60 * time.Second
)

// StorageObjectData represents the data for a GCS object event.
type StorageObjectData struct {
	Name   string `json:"name"`
	Bucket string `json:"bucket"`
}

// CloudEvent represents a CloudEvents v1.0 payload.
type CloudEvent struct {
	Data        StorageObjectData `json:"data"`
	ID          string            `json:"id"`
	Source      string            `json:"source"`
	SpecVersion string            `json:"specversion"`
	Type        string            `json:"type"`
	Time        string            `json:"time"` // RFC3339 format
	Subject     string            `json:"subject"`
}

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

	if filesBucketName == "" {
		log.Fatal("GCS_FILES_BUCKET not set")
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

func processFile(ctx context.Context, objectName string) error {
	log.Println("Starting file processing for %q...", objectName)

	// Get file metadata from GCS bucket
	attrs, err := gcsClient.Bucket(filesBucketName).Object(objectName).Attrs(ctx)
	if err != nil {
		return fmt.Errorf("error reading object: %w", err)
	}

	if !isAudio(attrs.Name) {
		return nil
	}

	log.Printf("processing object=%q size=%d", attrs.Name, attrs.Size)

	title := titleFromName(attrs.Name)
	date := time.Now().Format("Mon 02 Jan 2006 03:04:05 PM MST")

	item := fmt.Sprintf(xmlItemTemplate, title, date, attrs.Name, attrs.Size)

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

	buf.WriteString(item)
	buf.WriteString("\n")
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

	log.Printf("Updated index.xml")
	return nil
}

func isAudio(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".mp3") || strings.HasSuffix(n, ".m4a")
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

	// Generate a signed URL for the GCS object
	url, err := gcsClient.Bucket(filesBucketName).SignedURL(filename, &storage.SignedURLOptions{
		Method:  http.MethodGet,
		Expires: time.Now().Add(15 * time.Minute), // URL valid for 15 minutes
	})

	if err != nil {
		log.Printf("Error generating signed URL for %s: %v", filename, err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to generate signed URL for podcast file"}`)
		return
	}

	// Redirect the client to the signed URL
	http.Redirect(w, r, url, http.StatusFound)
}

func processHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 55*time.Minute)
	defer cancel()

	// Decode the Eventarc trigger payload
	body, err := io.ReadAll(r.Body)
	defer r.Body.Close() // Ensure body is closed
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error":"Failed to read request body"}`)
		return
	}

	var event CloudEvent
	err = json.Unmarshal(body, &event)
	if err != nil {
		log.Printf("Error unmarshalling event payload: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":"Failed to parse event payload"}`)
		return
	}

	objectName := event.Data.Name
	log.Printf("Received Eventarc trigger for GCS object: %s in bucket: %s", objectName, event.Data.Bucket)

	err = processFile(ctx, objectName)
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

	// Use a new ServeMux for custom server configuration
	router := http.NewServeMux()

	router.HandleFunc("/health", healthHandler)
	router.HandleFunc("/feed", feedHandler)
	router.HandleFunc("/files/{file}", fileHandler)
	router.HandleFunc("/index.xml", feedHandler)
	router.HandleFunc("/process", processHandler)
	router.HandleFunc("/", feedHandler)

	// Configure HTTP/2 over cleartext (h2c) for Cloud Run.
	// Cloud Run can proxy requests and forward them as HTTP/2 to the container
	// if the container is configured to handle it (e.g., using h2c).
	server := &http.Server{
		Addr:    ":" + port,
		Handler: h2c.NewHandler(router, &http2.Server{}), // Wrap the router with h2c.NewHandler
	}

	log.Printf("Starting server on port %s (HTTP/2 enabled via h2c)", port)
	if err := server.ListenAndServe(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
