package summaries

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

const (
	algorithmVersion   = "item-summary-hierarchical-v1"
	instructionVersion = "item-summary-instructions-v1"
	schemaVersion      = "item-summary-output-v1"
)

func summaryConfiguration(settings domain.WorkspaceSettings) (domain.SummaryConfiguration, string) {
	configuration := domain.SummaryConfiguration{Endpoint: strings.TrimRight(strings.TrimSpace(*settings.Generation.Endpoint), "/"), Model: strings.TrimSpace(*settings.Generation.SummaryModel), Language: settings.Summary.Language,
		MaxInputChars: settings.Summary.MaxInputChars, MaxSummaryCharacters: settings.Summary.MaxSummaryCharacters,
		ChunkInstruction: settings.Instructions.SummaryChunk, ReduceInstruction: settings.Instructions.SummaryReduce, FinalInstruction: settings.Instructions.SummaryFinal,
		AlgorithmVersion: algorithmVersion, SchemaVersion: schemaVersion}
	instructions, _ := json.Marshal(struct{ Version, Chunk, Reduce, Final string }{instructionVersion, configuration.ChunkInstruction, configuration.ReduceInstruction, configuration.FinalInstruction})
	version := sha256.Sum256(instructions)
	configuration.InstructionsVersion = hex.EncodeToString(version[:])
	payload, _ := json.Marshal(configuration)
	fingerprint := sha256.Sum256(payload)
	return configuration, hex.EncodeToString(fingerprint[:])
}
