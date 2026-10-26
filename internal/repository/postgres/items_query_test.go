package postgres

import (
	"encoding/json"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/stretchr/testify/require"
)

func TestEncodeItemRecordStoresTypedSourceEntryDetails(t *testing.T) {
	t.Parallel()
	doi := "10.1000/retrieval"
	details := domain.SourceEntryDetails{
		Type: domain.ItemKindPaper,
		Paper: &domain.SourcePaperDetails{
			Abstract: "Typed details", Categories: []string{"cs.IR"}, DOI: &doi,
		},
	}

	record, err := encodeItemRecord(domain.Item{}, domain.SourceEntry{Details: details})

	require.NoError(t, err)
	var stored domain.SourceEntryDetails
	require.NoError(t, json.Unmarshal(record.sourceDetails, &stored))
	require.Equal(t, details, stored)
}
