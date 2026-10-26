package postgres

import (
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAdvisoryPayloadRoundTripIsTypedAndWorkspaceScoped(t *testing.T) {
	t.Parallel()

	expected := domain.Advisory{
		Kind: domain.AdvisoryJobChanged, WorkspaceID: uuid.New(), ResourceID: uuid.New(), Origin: uuid.New(),
	}
	payload, err := encodeAdvisory(expected)
	require.NoError(t, err)
	actual, err := decodeAdvisory(payload)
	require.NoError(t, err)
	require.Equal(t, expected, actual)

	_, err = decodeAdvisory(`{"kind":"jobChanged"}`)
	require.ErrorContains(t, err, "workspace")
}
