package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// SummaryInputForItem selects actual article Markdown or README and fingerprints only model input facts.
func SummaryInputForItem(item Item) SummaryInput {
	origin := item.ContentOrigin
	if origin == "" {
		origin = ContentOriginUnknown
	}
	body := strings.TrimSpace(item.ContentMarkdown)
	if origin == ContentOriginArticleBody && item.Kind != ItemKindArticle {
		body = ""
	}
	if origin == ContentOriginRepositoryReadme {
		body = repositorySummaryText(item)
	}
	title := strings.TrimSpace(item.Title)
	payload, _ := json.Marshal(struct {
		Title, Markdown string
		Origin          ContentOrigin
	}{title, body, origin})
	digest := sha256.Sum256(payload)
	return SummaryInput{SummaryInputKey: SummaryInputKey{WorkspaceID: item.WorkspaceID, ItemID: item.ID, SourceVersion: hex.EncodeToString(digest[:])}, Title: title, Markdown: body, Origin: origin}
}

func repositorySummaryText(item Item) string {
	if item.Kind != ItemKindRepository || item.Details.Repository == nil {
		return ""
	}
	return strings.TrimSpace(item.Details.Repository.ReadmeMarkdown)
}
