package email

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/felipeafreitas/agregado/internal/ai"
	"github.com/felipeafreitas/agregado/internal/broker"
	"github.com/felipeafreitas/agregado/internal/domain"
)

type Headers map[string]string

type Payload struct {
	From string		`json:"from"`
	To string		`json:"to"`
	Subject string	`json:"subject"`
	Headers Headers	`json:"headers"`
	Text string		`json:"text"`
	Html string		`json:"html"`
}

type Handler struct {
	secret string
	parser *Parser
	sources SourceRepository
	publisher *broker.Publisher
	provider ai.Provider
}

type SourceRepository interface {
	FindByEmailSender(ctx context.Context, email string) (*domain.Source, error)
	Create(ctx context.Context, source domain.Source) (*domain.Source, error)
}

func NewHandler(secret string, parser *Parser, sources SourceRepository, publisher *broker.Publisher, provider ai.Provider) *Handler {
	return &Handler{
		secret:    secret,
		parser:    parser,
		sources:   sources,
		publisher: publisher,
		provider:  provider,
	}
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	if (r.Header.Get("X-Webhook-Secret") != h.secret) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var payload Payload
	err := json.NewDecoder(r.Body).Decode(&payload)

	if (err != nil) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
      	return
	}

	article, senderEmail, err := h.parser.Parse(payload)
	if (err != nil) {
		http.Error(w, "Failed to parse email", http.StatusInternalServerError)
      	return
	}

	source, err := h.sources.FindByEmailSender(r.Context(), senderEmail)
	if (err != nil) {
		source, err = h.sources.Create(context.Background(), domain.Source{
			Type: domain.Newsletter,
			Name: senderEmail,
			IsActive: true,
			EmailSender: &senderEmail,
		})
		if err != nil {
			http.Error(w, "Failed to create source", http.StatusInternalServerError)
      		return
		}
	}

	article.SourceID = &source.ID

	if source.Summarize {
		if summary, err := h.provider.Summarize(r.Context(), []domain.Article{*article}); err == nil {
			article.Summary = &summary
		}
	}

	body, err := json.Marshal(article)
	if err != nil {
		http.Error(w, "Failed to marshal article", http.StatusInternalServerError)
  		return
	}

	err = h.publisher.Publish("articles.ingest", "newsletter", body)
	if err != nil {
		http.Error(w, "Failed to publish email to queue", http.StatusInternalServerError)
  		return
	}

	w.WriteHeader(http.StatusOK)

}
