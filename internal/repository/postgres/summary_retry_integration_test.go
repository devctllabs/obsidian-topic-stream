//go:build integration

package postgres_test

import (
	"sync"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestSummaryRetryCoalescesAndPermanentlyReplaysTerminalSuccessor(t *testing.T) {
	f, publication := summaryPublicationFixture(t)
	input, found, err := f.store.Summaries().GetSummaryInput(t.Context(), publication.Input)
	require.NoError(t, err)
	require.True(t, found)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	sourceID := publication.Annotation.GenerationJobID
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='dead' WHERE id=$1`, sourceID)
	require.NoError(t, err)
	const callers = 8
	requests := make([]domain.SummaryAdmission, callers)
	for i := range requests {
		requests[i] = summaryAdmissionRequest(t, input)
		requests[i].RetrySourceJobID = &sourceID
	}
	results := make([]domain.SummaryAdmissionResult, callers)
	failures := make([]error, callers)
	var wg sync.WaitGroup
	for i := range requests {
		wg.Go(func() { results[i], failures[i] = f.store.Summaries().AdmitSummary(t.Context(), requests[i]) })
	}
	wg.Wait()
	created := 0
	for i := range results {
		require.NoError(t, failures[i])
		require.NotEqual(t, sourceID, results[i].Job.ID)
		require.Equal(t, results[0].Job.ID, results[i].Job.ID)
		if results[i].Created {
			created++
		}
	}
	require.Equal(t, 1, created)
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='dead' WHERE id=$1`, results[0].Job.ID)
	require.NoError(t, err)
	replay, err := f.store.Summaries().AdmitSummary(t.Context(), requests[0])
	require.NoError(t, err)
	require.False(t, replay.Created)
	require.Equal(t, results[0].Job.ID, replay.Job.ID)
	next := summaryAdmissionRequest(t, input)
	next.RetrySourceJobID = &replay.Job.ID
	successor, err := f.store.Summaries().AdmitSummary(t.Context(), next)
	require.NoError(t, err)
	require.True(t, successor.Created)
	require.NotEqual(t, replay.Job.ID, successor.Job.ID)
}

func TestSummaryRetryRejectsActiveSourceAndCommittedResult(t *testing.T) {
	f, publication := summaryPublicationFixture(t)
	input, found, err := f.store.Summaries().GetSummaryInput(t.Context(), publication.Input)
	require.NoError(t, err)
	require.True(t, found)
	request := summaryAdmissionRequest(t, input)
	request.RetrySourceJobID = &publication.Annotation.GenerationJobID
	_, err = f.store.Summaries().AdmitSummary(t.Context(), request)
	require.ErrorIs(t, err, domain.ErrJobNotRetryable)
	require.NoError(t, f.store.Summaries().PublishSummary(t.Context(), publication))
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `UPDATE jobs SET status='dead' WHERE id=$1`, publication.Annotation.GenerationJobID)
	require.NoError(t, err)
	_, err = f.store.Summaries().AdmitSummary(t.Context(), request)
	require.ErrorIs(t, err, domain.ErrJobNotRetryable)
}
