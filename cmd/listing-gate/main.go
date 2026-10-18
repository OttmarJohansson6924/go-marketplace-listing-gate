package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"example.com/infrai-listing-gate/internal/storefront"
	"github.com/openai/openai-go"
)

const maxImageBytes = 12 << 20

type server struct {
	workflow *storefront.ListingWorkflow
}

func main() {
	key := os.Getenv("INFRAI_API_KEY")
	if key == "" {
		log.Fatal("INFRAI_API_KEY is required")
	}
	client := storefront.NewInfraiClient(key, nil)
	s := &server{workflow: storefront.NewListingWorkflow(client)}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders/{id}/listing", s.submitListing)
	log.Printf("listing gate listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func (s *server) submitListing(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes+1<<20)
	if err := r.ParseMultipartForm(maxImageBytes); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expected a multipart image and caption"})
		return
	}
	file, header, err := r.FormFile("image")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image is required"})
		return
	}
	defer file.Close()
	image, err := io.ReadAll(io.LimitReader(file, maxImageBytes+1))
	if err != nil || len(image) == 0 || len(image) > maxImageBytes {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image must be between 1 byte and 12 MiB"})
		return
	}
	mediaType := header.Header.Get("Content-Type")
	if !strings.HasPrefix(mediaType, "image/") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "image content type is required"})
		return
	}

	state, err := s.workflow.Submit(r.Context(), storefront.ListingSubmission{
		OrderID: r.PathValue("id"), Caption: r.FormValue("caption"), Filename: header.Filename,
		MediaType: mediaType, Image: image,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if state.Listing == "quarantined" {
		status = http.StatusAccepted
	}
	writeJSON(w, status, state)
}

func writeServiceError(w http.ResponseWriter, err error) {
	status := http.StatusBadGateway
	var infraiErr *storefront.InfraiError
	if errors.As(err, &infraiErr) && infraiErr.Status >= 400 && infraiErr.Status < 500 {
		status = infraiErr.Status
	}
	var openaiErr *openai.Error
	if errors.As(err, &openaiErr) && openaiErr.StatusCode >= 400 && openaiErr.StatusCode < 500 {
		status = openaiErr.StatusCode
	}
	writeJSON(w, status, map[string]string{"error": fmt.Sprintf("listing request: %v", err)})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("write response: %v", err)
	}
}
