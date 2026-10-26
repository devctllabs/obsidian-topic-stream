package summaries

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/jobs"
)

const maxGenerationCalls = 128
const maxReductionLevels = 8

type hierarchicalSummary struct {
	service           *Service
	attempt           summaryAttempt
	calls             int
	languageReference string
}

func (s *Service) generateText(ctx context.Context, attempt summaryAttempt) (string, error) {
	generation := hierarchicalSummary{service: s, attempt: attempt, languageReference: sourceLanguageReference(attempt.input.Markdown)}
	return generation.generate(ctx)
}

func (g *hierarchicalSummary) generate(ctx context.Context) (string, error) {
	configuration := g.attempt.configuration
	limit := configuration.MaxInputChars
	if limit < 1 {
		return "", jobs.Permanent(domain.ErrSummaryUnavailable)
	}
	length := utf8.RuneCountInString(g.attempt.input.Markdown)
	if length <= limit {
		return g.call(ctx, "final", g.attempt.input.Markdown, configuration.MaxSummaryCharacters)
	}
	if length > limit*(maxGenerationCalls-1) {
		return "", generationBudgetError()
	}
	facts, err := g.parts(ctx, "chunk", splitMarkdownWithin(g.attempt.input.Markdown, limit))
	if err != nil {
		return "", err
	}
	for level := 0; utf8.RuneCountInString(facts) > limit; level++ {
		if level >= maxReductionLevels {
			return "", generationBudgetError()
		}
		reduced, err := g.parts(ctx, "reduce", splitMarkdownWithin(facts, limit))
		if err != nil {
			return "", err
		}
		if utf8.RuneCountInString(reduced) >= utf8.RuneCountInString(facts) {
			return "", errors.New("summary reduction did not make progress")
		}
		facts = reduced
	}
	return g.call(ctx, "final", facts, configuration.MaxSummaryCharacters)
}

func (g *hierarchicalSummary) parts(ctx context.Context, stage string, parts []string) (string, error) {
	// Reserve a final call before starting another whole level.
	if g.calls+len(parts)+1 > maxGenerationCalls {
		return "", generationBudgetError()
	}
	limit := max(1, min(4000, g.attempt.configuration.MaxInputChars/4))
	facts := make([]string, 0, len(parts))
	for _, part := range parts {
		text, err := g.call(ctx, stage, part, limit)
		if err != nil {
			return "", err
		}
		facts = append(facts, text)
	}
	return strings.Join(facts, "\n\n"), nil
}

func (g *hierarchicalSummary) call(ctx context.Context, stage, text string, limit int) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if g.calls >= maxGenerationCalls {
		return "", generationBudgetError()
	}
	g.calls++
	configuration := g.attempt.configuration
	instruction := configuration.FinalInstruction
	switch stage {
	case "chunk":
		instruction = configuration.ChunkInstruction
	case "reduce":
		instruction = configuration.ReduceInstruction
	}
	return g.service.call(ctx, Prompt{Endpoint: configuration.Endpoint, Model: configuration.Model, Stage: stage, Instruction: instruction, Language: configuration.Language, SourceLanguageReference: g.languageReference, Title: g.attempt.input.Title, Text: text, MaxOutputCharacters: limit})
}

func generationBudgetError() error {
	return jobs.Permanent(errors.New("summary input exceeds the generation call budget"))
}

func sourceLanguageReference(text string) string {
	count := 0
	for index := range text {
		if count == 512 {
			return text[:index]
		}
		count++
	}
	return text
}

func splitMarkdownWithin(text string, limit int) []string {
	runes := []rune(text)
	var parts []string
	for len(runes) > limit {
		cut := markdownBoundary(runes[:limit])
		if part := strings.TrimSpace(string(runes[:cut])); part != "" {
			parts = append(parts, part)
		}
		runes = runes[cut:]
	}
	if part := strings.TrimSpace(string(runes)); part != "" {
		parts = append(parts, part)
	}
	return parts
}

func markdownBoundary(runes []rune) int {
	cut := len(runes)
	for index := len(runes) / 2; index+1 < len(runes); index++ {
		if runes[index] != '\n' {
			continue
		}
		switch runes[index+1] {
		case '\n':
			cut = index + 2
		case '#':
			cut = index + 1
		}
	}
	return cut
}
