package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimension() int
}

var ActiveEmbedder Embedder

func InitEmbedder() {
	provider := os.Getenv("EMBEDDING_PROVIDER")
	if provider == "" {
		provider = "mock" // fallback to mock if not configured
	}

	dimStr := os.Getenv("EMBEDDING_DIMENSION")
	dim := 384 // default to 384 for all-minilm
	if dimStr != "" {
		if d, err := strconv.Atoi(dimStr); err == nil {
			dim = d
		}
	}

	switch strings.ToLower(provider) {
	case "ollama":
		host := os.Getenv("OLLAMA_HOST")
		if host == "" {
			host = "http://localhost:11434"
		}
		model := os.Getenv("OLLAMA_MODEL")
		if model == "" {
			model = "all-minilm"
		}
		log.Printf("Initializing Ollama Embedder (Host: %s, Model: %s, Dim: %d)...", host, model, dim)
		ActiveEmbedder = &OllamaEmbedder{
			Host:      host,
			Model:     model,
			DimensionSize: dim,
		}
	default:
		log.Printf("Initializing Mock Embedder (Dim: %d)...", dim)
		ActiveEmbedder = &MockEmbedder{
			DimensionSize: dim,
		}
	}
}

// 1. Ollama Embedder

type OllamaEmbedder struct {
	Host          string
	Model         string
	DimensionSize int
}

type ollamaRequest struct {
	Model  string `json:"model"`
	Prompt string `json:"prompt"`
}

type ollamaResponse struct {
	Embedding []float32 `json:"embedding"`
}

func (o *OllamaEmbedder) Dimension() int {
	return o.DimensionSize
}

func (o *OllamaEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// Clean text
	text = strings.TrimSpace(text)
	if text == "" {
		return make([]float32, o.DimensionSize), nil
	}

	reqBody, err := json.Marshal(ollamaRequest{
		Model:  o.Model,
		Prompt: text,
	})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", o.Host+"/api/embeddings", bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Ollama: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned status code: %d", resp.StatusCode)
	}

	var res ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}

	// Validate dimension size
	if len(res.Embedding) != o.DimensionSize {
		// Log warning and pad/truncate vector to prevent database errors
		log.Printf("Warning: Ollama returned vector of size %d, expected %d. Adjusting...", len(res.Embedding), o.DimensionSize)
		adjusted := make([]float32, o.DimensionSize)
		copy(adjusted, res.Embedding)
		return adjusted, nil
	}

	return res.Embedding, nil
}

// 2. Mock Embedder (Deterministic Hash-Based Bag-of-Words)

type MockEmbedder struct {
	DimensionSize int
}

func (m *MockEmbedder) Dimension() int {
	return m.DimensionSize
}

// hashWord hashes a word to a number in the range [0, dimensionSize)
func hashWord(word string, max int) uint32 {
	h := fnv.New32a()
	h.Write([]byte(word))
	return h.Sum32() % uint32(max)
}

func (m *MockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	vec := make([]float32, m.DimensionSize)
	words := strings.Fields(strings.ToLower(text))
	
	if len(words) == 0 {
		return vec, nil
	}

	// Calculate word frequencies projected onto dimension coordinates
	for _, w := range words {
		// Strip punctuation
		w = strings.Trim(w, ".,!?;:()[]{}'\"")
		if w == "" {
			continue
		}
		idx := hashWord(w, m.DimensionSize)
		vec[idx] += 1.0
	}

	// Normalize vector (L2 normalization) so cosine similarity is equal to dot product
	var sumSq float64
	for _, val := range vec {
		sumSq += float64(val * val)
	}

	if sumSq > 0 {
		norm := float32(math.Sqrt(sumSq))
		for i := range vec {
			vec[i] /= norm
		}
	}

	return vec, nil
}
