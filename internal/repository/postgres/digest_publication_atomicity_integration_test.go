//go:build integration

package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestDigestPublicationRollsBackSnapshotConsumptionAndWatermark(t *testing.T) {
	for _, statement := range []string{
		`ALTER TABLE digest_consumed_items ADD CHECK(item_id='00000000-0000-0000-0000-000000000000')`,
		`ALTER TABLE digest_definitions ADD CHECK(last_successful_at IS NULL)`,
		`CREATE FUNCTION reject_digest_commit() RETURNS trigger LANGUAGE plpgsql AS $body$ BEGIN RAISE EXCEPTION 'commit failure'; RETURN NEW; END; $body$;
 CREATE CONSTRAINT TRIGGER reject_digest_commit AFTER UPDATE ON digest_definitions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION reject_digest_commit()`,
	} {
		t.Run(statement, func(t *testing.T) {
			f, request := newDigestPublicationFixture(t)
			conn, err := pgx.Connect(t.Context(), f.dsn)
			require.NoError(t, err)
			defer func() { require.NoError(t, conn.Close(t.Context())) }()
			_, err = conn.Exec(t.Context(), statement)
			require.NoError(t, err)
			_, err = f.store.PublishDigestIssue(t.Context(), request)
			require.Error(t, err)
			assertNoDigestPublication(t, f, request)
		})
	}
}

func TestDigestPublicationRechecksLocksAfterCancellationDeletionAndLeaseRecovery(t *testing.T) {
	cases := []struct{ name, statement string }{
		{"cancellation", `UPDATE jobs SET cancellation_requested=true WHERE id=$1`},
		{"new attempt same owner", `UPDATE jobs SET attempt=attempt+1 WHERE id=$1`},
		{"new owner", `UPDATE jobs SET attempt=attempt+1,lease_owner='replacement-worker' WHERE id=$1`},
		{"expiry while locked", `UPDATE jobs SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`},
		{"deletion", `UPDATE digest_definitions SET deleted_at=clock_timestamp(),enabled=false,next_run_at=NULL WHERE id=$1`},
	}
	for _, scenario := range cases {
		t.Run(scenario.name, func(t *testing.T) {
			f, request := newDigestPublicationFixture(t)
			conn, err := pgx.Connect(t.Context(), f.dsn)
			require.NoError(t, err)
			defer func() { require.NoError(t, conn.Close(t.Context())) }()
			tx, err := conn.Begin(t.Context())
			require.NoError(t, err)
			defer func() { _ = tx.Rollback(t.Context()) }()
			id := request.Issue.GenerationJobID
			if scenario.name == "deletion" {
				id = request.Issue.DigestID
			}
			_, err = tx.Exec(t.Context(), scenario.statement, id)
			require.NoError(t, err)
			started := make(chan struct{})
			done := make(chan error, 1)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			go func() { close(started); _, err := f.store.PublishDigestIssue(ctx, request); done <- err }()
			<-started
			require.NoError(t, tx.Commit(t.Context()))
			require.ErrorIs(t, <-done, domain.ErrDigestPublicationRejected)
			_, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, request.Issue.ID)
			require.NoError(t, err)
			require.False(t, found)
		})
	}
}

func TestDigestPublicationAndAdmissionShareLockOrder(t *testing.T) {
	f, request := newDigestPublicationFixture(t)
	definition := domain.DigestDefinition{ID: request.Issue.DigestID, WorkspaceID: f.workspaceID}
	manual := manualDigestRequest(t, definition, uuid.New())
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	start := make(chan struct{})
	done := make(chan error, 2)
	go func() { <-start; _, err := f.store.PublishDigestIssue(ctx, request); done <- err }()
	go func() { <-start; _, err := f.store.AdmitDigestBuild(ctx, manual); done <- err }()
	close(start)
	require.NoError(t, <-done)
	require.NoError(t, <-done)
}

// assertNoDigestPublication checks all three facts of a rejected publication transaction.
func assertNoDigestPublication(t *testing.T, f feedFixture, request domain.DigestPublication) {
	t.Helper()
	_, found, err := f.store.GetDigestIssue(t.Context(), f.workspaceID, request.Issue.ID)
	require.NoError(t, err)
	require.False(t, found)
	definition, found, err := f.store.GetDigest(t.Context(), f.workspaceID, request.Issue.DigestID)
	require.NoError(t, err)
	require.True(t, found)
	require.Nil(t, definition.LastSuccessfulAt)
	consumed, err := f.store.FindConsumedDigestItemIDs(t.Context(), f.workspaceID, request.Issue.DigestID, []uuid.UUID{request.Issue.Sections[0].Items[0].Item.ID})
	require.NoError(t, err)
	require.Empty(t, consumed)
}
