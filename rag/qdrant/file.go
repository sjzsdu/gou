package qdrant

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/yaoapp/gou/rag"
)

// FileUploader implements the FileUpload interface for Qdrant
type FileUploader struct {
	engine *Engine
}

// NewFileUploader creates a new FileUploader instance
func NewFileUploader(engine *Engine) *FileUploader {
	return &FileUploader{engine: engine}
}

// Upload processes content from a reader
func (f *FileUploader) Upload(ctx context.Context, reader io.Reader, opts rag.FileUploadOptions) (*rag.FileUploadResult, error) {
	if opts.ChunkSize <= 0 {
		opts.ChunkSize = 1000 // default chunk size
	}

	scanner := bufio.NewScanner(reader)
	var buffer string
	var documents []*rag.Document
	docID := 1

	// Read content in chunks
	for scanner.Scan() {
		line := scanner.Text()
		buffer += line + "\n"

		if len(buffer) >= opts.ChunkSize {
			doc := &rag.Document{
				DocID:        fmt.Sprintf("00000000-0000-0000-0000-%012d", docID),
				Content:      buffer,
				ChunkSize:    opts.ChunkSize,
				ChunkOverlap: opts.ChunkOverlap,
				Metadata: map[string]interface{}{
					"chunk_number": docID,
				},
			}
			documents = append(documents, doc)
			buffer = buffer[max(0, len(buffer)-opts.ChunkOverlap):]
			docID++
		}
	}

	// Handle any remaining content
	if len(buffer) > 0 {
		doc := &rag.Document{
			DocID:        fmt.Sprintf("00000000-0000-0000-0000-%012d", docID),
			Content:      buffer,
			ChunkSize:    opts.ChunkSize,
			ChunkOverlap: opts.ChunkOverlap,
			Metadata: map[string]interface{}{
				"chunk_number": docID,
			},
		}
		documents = append(documents, doc)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading content: %w", err)
	}

	result := &rag.FileUploadResult{
		Documents: documents,
	}

	// If async processing is requested, index documents in batch
	if opts.Async && len(documents) > 0 {
		taskID, err := f.engine.IndexBatch(ctx, opts.IndexName, documents)
		if err != nil {
			return nil, fmt.Errorf("error indexing documents: %w", err)
		}
		result.TaskID = taskID
	} else if len(documents) > 0 {
		// Synchronous processing
		for _, doc := range documents {
			if err := f.engine.IndexDoc(ctx, opts.IndexName, doc); err != nil {
				return nil, fmt.Errorf("error indexing document: %w", err)
			}
		}
	}

	return result, nil
}

// UploadFile processes content from a file path
func (f *FileUploader) UploadFile(ctx context.Context, path string, opts rag.FileUploadOptions) (*rag.FileUploadResult, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("error opening file: %w", err)
	}
	defer file.Close()

	// Add filename to metadata if not already set
	if opts.IndexName == "" {
		opts.IndexName = filepath.Base(path)
	}

	return f.Upload(ctx, file, opts)
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
