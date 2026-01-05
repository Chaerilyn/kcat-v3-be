package main

import (
	"bytes"
	"errors"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"time"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	"github.com/pocketbase/pocketbase/tools/filesystem"
)

// CONFIGURATION
const (
	WORKER_URL    = "https://entered-durable-shake-statements.trycloudflare.com/convert"
	WORKER_SECRET = "super-secret-password-123"
	COLLECTION    = "contents" // Change to your actual collection name
)

func main() {
	// 1. Set output to Standard Out (Railway captures this)
	log.SetOutput(os.Stdout)

	// 2. Remove default timestamps (Railway adds its own, and this sometimes helps buffering)
	log.SetFlags(0)

	app := pocketbase.New()

	// Handler function for file upload events
	handleConversion := func(e *core.RecordEvent) error {
		log.Println("Hook Triggered for Collection:", e.Record.Collection().Name)

		record := e.Record

		// 1. Checks: Correct collection? Has video? Webp missing?
		if record.Collection().Name != COLLECTION {
			return e.Next()
		}
		if record.GetString("file") == "" {
			return e.Next()
		}
		// Prevent infinite loop: if webp exists, stop.
		if record.GetString("webp") != "" {
			return e.Next()
		}

		// Capture data for the goroutine
		recordId := record.Id
		filename := record.GetString("file")
		collectionId := record.Collection().Id

		// 2. Async Processing
		go func(recId, colId, fName string) {
			// Wait for R2 consistency (optional but recommended)
			time.Sleep(2 * time.Second)
			log.Printf("🔄 Starting conversion for %s...", recId)

			// Initialize Filesystem (Connect to R2/Local)
			fs, err := app.NewFilesystem()
			if err != nil {
				log.Println("❌ Error initializing filesystem:", err)
				return
			}
			defer fs.Close()

			// Retrieve the video file
			fileKey := colId + "/" + recId + "/" + fName
			r2File, err := fs.GetFile(fileKey)
			if err != nil {
				log.Println("❌ Error finding file in storage:", err)
				return
			}
			defer r2File.Close()

			// Send to Laptop Worker
			convertedBytes, err := sendToWorker(r2File, fName)
			if err != nil {
				log.Println("❌ Worker failed:", err)
				return
			}

			// Fetch FRESH record to update
			freshRecord, err := app.FindRecordById(COLLECTION, recId)
			if err != nil {
				log.Println("❌ Could not find record to update:", err)
				return
			}

			// Create new file object from result bytes
			newFile, err := filesystem.NewFileFromBytes(convertedBytes, "animated.webp")
			if err != nil {
				log.Println("❌ Error creating file object:", err)
				return
			}

			freshRecord.Set("webp", newFile)

			// Save (triggers hooks again, but "webp" check stops the loop)
			if err := app.Save(freshRecord); err != nil {
				log.Println("❌ Failed to save webp:", err)
			} else {
				log.Println("✅ Conversion complete and saved for:", recId)
			}

		}(recordId, collectionId, filename)

		return e.Next()
	}

	// Register hooks
	app.OnRecordAfterCreateSuccess(COLLECTION).BindFunc(handleConversion)
	app.OnRecordAfterUpdateSuccess(COLLECTION).BindFunc(handleConversion)

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}

// Helper: Sends file to your laptop and returns the converted bytes
func sendToWorker(fileReader io.Reader, filename string) ([]byte, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(part, fileReader); err != nil {
		return nil, err
	}
	writer.Close()

	req, err := http.NewRequest("POST", WORKER_URL, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+WORKER_SECRET)

	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, errors.New(resp.Status + ": " + string(bodyBytes))
	}

	return io.ReadAll(resp.Body)
}
