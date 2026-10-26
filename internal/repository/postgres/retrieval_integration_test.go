//go:build integration

package postgres_test

import (
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"
)

func TestRetryingRebuildDoesNotReseedCompletedItemProjections(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	work, found, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, found)
	_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, FailedAt: f.now.Add(time.Second)}))
	start.StartedAt = f.now.Add(time.Minute)
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: start.ModelID, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, pending, "retry of one durable rebuild must preserve successful delivery progress")
}

func TestNewRebuildJobResumesRetainedModelProgress(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID,
		JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	work, exists, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, exists)
	_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
		WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: start.ModelID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{
		WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: start.JobID, FailedAt: f.now}))
	start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now.Add(time.Minute))
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{
		WorkspaceID: f.workspaceID, ModelID: start.ModelID, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, pending, "a different Job must resume the retained model instead of re-enqueuing successful deliveries")
}

func TestModelProgressRetainsServingAndTwoRecentSelections(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var targets []uuid.UUID
	for index := range 3 {
		model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
		require.NoError(t, err)
		model.ID = uuid.New()
		model.Name = model.ID.String()
		require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
		targets = append(targets, model.ID)
		start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model.ID,
			JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now.Add(time.Duration(index) * time.Minute)}
		_, err = f.store.Begin(t.Context(), start)
		require.NoError(t, err)
		require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{
			WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: start.JobID, FailedAt: f.now}))
	}
	rows, err := conn.Query(t.Context(), `SELECT embedding_model_id FROM workspace_model_progress WHERE workspace_id=$1 AND retained`, f.workspaceID)
	require.NoError(t, err)
	retained, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	require.NoError(t, err)
	require.ElementsMatch(t, []uuid.UUID{domain.DefaultEmbeddingModelID, targets[1], targets[2]}, retained)
	var oldGeneration uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT generation FROM workspace_model_progress WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, targets[0]).Scan(&oldGeneration))
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: targets[0],
		JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now.Add(time.Hour)}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	var newGeneration uuid.UUID
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT generation FROM workspace_model_progress WHERE workspace_id=$1 AND embedding_model_id=$2 AND retained`, f.workspaceID, targets[0]).Scan(&newGeneration))
	require.NotEqual(t, oldGeneration, newGeneration, "evicted state cannot authorize acknowledgements for a new retention")
}

func TestInactiveRetainedModelCoalescesChangesWithoutDelivery(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	model.ID, model.Name = uuid.New(), "Retained inactive"
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
	for _, modelID := range []uuid.UUID{model.ID, domain.DefaultEmbeddingModelID} {
		start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: modelID,
			JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now}
		_, err = f.store.Begin(t.Context(), start)
		require.NoError(t, err)
		work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
		require.NoError(t, err)
		_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
			WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: modelID, Revision: work.Projection.Revision})
		require.NoError(t, err)
		require.NoError(t, f.store.Activate(t.Context(), domain.ModelTransitionCutover{
			WorkspaceID: f.workspaceID, ModelID: modelID, JobID: start.JobID, ActivatedAt: f.now}))
	}
	for _, sentiment := range []string{"like", "dislike", "like"} {
		_, _, err = f.store.SetFeedback(t.Context(), f.workspaceID, itemID,
			domain.Feedback{Sentiment: sentiment, CreatedAt: f.now, UpdatedAt: f.now})
		require.NoError(t, err)
	}
	pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{
		WorkspaceID: f.workspaceID, ModelID: model.ID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, pending, 1, "edits while inactive must coalesce into one retained revision")
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.Equal(t, work.Projection.Revision, pending[0].ProjectionRevision)
	require.Len(t, work.Models, 1, "inactive retained work must not enter provider delivery")
	require.Equal(t, domain.DefaultEmbeddingModelID, work.Models[0].ID)
	_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{
		WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	page, err := f.store.ListPendingGlobalItemProjections(t.Context(), domain.ProjectionRepairQuery{Limit: 100})
	require.NoError(t, err)
	require.Empty(t, page.Commits, "inactive-only work must not enqueue Jobs")
	manual, err := f.store.ListPendingItemProjections(t.Context(), f.workspaceID, nil, 100)
	require.NoError(t, err)
	require.Empty(t, manual, "Workspace repair must also exclude inactive-only work")
}

func TestEvictedModelAcknowledgementCannotCompleteReselectedGeneration(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	itemID := f.item(t)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	var first domain.ModelTransitionStart
	var delivery domain.ItemProjectionDelivery
	for index := range 3 {
		model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
		require.NoError(t, err)
		model.ID, model.Name = uuid.New(), uuid.NewString()
		require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
		start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model.ID,
			JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now.Add(time.Duration(index) * time.Minute)}
		_, err = f.store.Begin(t.Context(), start)
		require.NoError(t, err)
		if index == 0 {
			first = start
			work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
			require.NoError(t, err)
			delivery = domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID,
				ModelID: model.ID, Revision: work.Projection.Revision}
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT generation FROM workspace_model_progress
WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, model.ID).Scan(&delivery.ModelGeneration))
		}
		require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{
			WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: start.JobID, FailedAt: f.now}))
	}
	first.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	first.StartedAt = f.now.Add(time.Hour)
	_, err = f.store.Begin(t.Context(), first)
	require.NoError(t, err)
	completed, err := f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	require.False(t, completed, "late ACK with the same canonical revision must not erase a new model generation")
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT generation FROM workspace_model_progress
WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, first.ModelID).Scan(&delivery.ModelGeneration))
	completed, err = f.store.CompleteItemProjection(t.Context(), delivery)
	require.NoError(t, err)
	require.True(t, completed)
}

func TestModelCursorResumesAcrossJobsAndDiscardCleanupIsBounded(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url)
SELECT md5(n::text)::uuid,$1,'{"type":"article","article":{}}','Synthetic','https://fixture.example/'||n,'https://fixture.example/'||n FROM generate_series(1,2501) n`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at)
SELECT workspace_id,id,id,$2 FROM items WHERE workspace_id=$1`, f.workspaceID, f.now)
	require.NoError(t, err)
	var first domain.ModelTransitionStart
	for index := range 3 {
		model, err := f.store.ResolveModel(t.Context(), domain.DefaultEmbeddingModelID)
		require.NoError(t, err)
		model.ID, model.Name = uuid.New(), uuid.NewString()
		require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), model))
		start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: model.ID,
			JobID: enqueueTransitionJob(t, f.store, f.workspaceID, f.now), StartedAt: f.now.Add(time.Duration(index) * time.Minute)}
		_, err = f.store.Begin(t.Context(), start)
		require.NoError(t, err)
		if index == 0 {
			first = start
			// Simulate successful delivery of the first seed page, then lose this execution.
			_, err = conn.Exec(t.Context(), `DELETE FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, model.ID)
			require.NoError(t, err)
			require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{
				WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: start.JobID, FailedAt: f.now}))
			start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
			_, err = f.store.Begin(t.Context(), start)
			require.NoError(t, err)
			done, err := f.store.SeedModelProjections(t.Context(), domain.ModelProjectionSeedCommand{
				WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: start.JobID, Limit: 1000})
			require.NoError(t, err)
			require.True(t, done)
			var pending int
			require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, model.ID).Scan(&pending))
			require.Equal(t, 1501, pending, "a new Job resumes after the first 1000 delivered Items")
		}
		require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{
			WorkspaceID: f.workspaceID, ModelID: model.ID, JobID: start.JobID, FailedAt: f.now}))
	}
	var pending int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1 AND embedding_model_id=$2`, f.workspaceID, first.ModelID).Scan(&pending))
	require.Equal(t, 501, pending, "eviction removes at most 1000 discarded keys per transaction")
	first.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	first.StartedAt = f.now.Add(time.Hour)
	_, err = f.store.Begin(t.Context(), first)
	require.NoError(t, err)
	done := false
	for !done {
		done, err = f.store.SeedModelProjections(t.Context(), domain.ModelProjectionSeedCommand{
			WorkspaceID: f.workspaceID, ModelID: first.ModelID, JobID: first.JobID, Limit: 1000})
		require.NoError(t, err)
	}
	var current int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending p
JOIN workspace_model_progress progress ON progress.workspace_id=p.workspace_id AND progress.embedding_model_id=p.embedding_model_id
AND progress.generation=p.model_generation WHERE p.workspace_id=$1 AND p.embedding_model_id=$2`, f.workspaceID, first.ModelID).Scan(&current))
	require.Equal(t, 2501, current, "old cleanup must not delete the reselected generation")
}

func TestRebuildKeepsCanonicalChangesDuringBackoffAndFencesSupersededJobs(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	target, found, err := f.store.EmbeddingModels().Get(t.Context(), domain.DefaultEmbeddingModelID)
	require.NoError(t, err)
	require.True(t, found)
	target.ID, target.Name = uuid.New(), "Rebuild target"
	require.NoError(t, f.store.EmbeddingModels().Create(t.Context(), target))
	itemID := f.item(t)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: target.ID, JobID: jobID, StartedAt: f.now}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	work, _, err := f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	_, err = f.store.CompleteItemProjection(t.Context(), domain.ItemProjectionDelivery{WorkspaceID: f.workspaceID, ItemID: itemID, ModelID: target.ID, Revision: work.Projection.Revision})
	require.NoError(t, err)
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID, ModelID: target.ID, JobID: jobID, FailedAt: f.now}))
	_, _, err = f.store.SetFeedback(t.Context(), f.workspaceID, itemID, domain.Feedback{Sentiment: "like", CreatedAt: f.now, UpdatedAt: f.now})
	require.NoError(t, err)
	newID := f.item(t)
	pending, err := f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: target.ID, Limit: 10})
	require.NoError(t, err)
	var ids []uuid.UUID
	for _, commit := range pending {
		ids = append(ids, commit.ItemID)
	}
	require.ElementsMatch(t, []uuid.UUID{itemID, newID}, ids, "resumable target receives writes between attempts")
	published, err := f.store.PublishFeed(t.Context(), domain.FeedPublishCommand{WorkspaceID: f.workspaceID, PublishedAt: f.now, Publications: []domain.FeedPublication{{ItemID: itemID, ModelID: domain.DefaultEmbeddingModelID, Pipeline: "backoff"}}})
	require.NoError(t, err)
	require.Len(t, published.Publications, 1)
	work, _, err = f.store.LoadItemProjection(t.Context(), f.workspaceID, itemID)
	require.NoError(t, err)
	require.True(t, work.Projection.Published)
	pending, err = f.store.ListPendingModelProjections(t.Context(), domain.ModelProjectionQuery{WorkspaceID: f.workspaceID, ModelID: target.ID, Limit: 10})
	require.NoError(t, err)
	for _, commit := range pending {
		if commit.ItemID == itemID {
			require.Equal(t, work.Projection.Revision, commit.ProjectionRevision)
		}
	}
	start.JobID = enqueueTransitionJob(t, f.store, f.workspaceID, f.now.Add(time.Minute))
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	require.NoError(t, f.store.Fail(t.Context(), domain.RetrievalIndexFailure{WorkspaceID: f.workspaceID, ModelID: target.ID, JobID: start.JobID, FailedAt: f.now}))
	start.JobID = jobID
	_, err = f.store.Begin(t.Context(), start)
	require.ErrorIs(t, err, domain.ErrConflict, "older Job cannot resume after another rebuild supersedes it")
}

func TestRebuildInitializationUsesBoundedResumableBatchesBeforeCutover(t *testing.T) {
	f := newFeedFixture(t)
	require.NoError(t, f.store.EnsureDefaultEmbeddingModel(t.Context(), testutil.DefaultEmbeddingDescriptor(), f.now))
	_, err := f.store.GetOrCreateActive(t.Context(), f.workspaceID, domain.DefaultEmbeddingModelID, f.now)
	require.NoError(t, err)
	conn, err := pgx.Connect(t.Context(), f.dsn)
	require.NoError(t, err)
	defer func() { require.NoError(t, conn.Close(t.Context())) }()
	_, err = conn.Exec(t.Context(), `INSERT INTO items(id,workspace_id,details,title,url,canonical_url)
SELECT md5(n::text)::uuid,$1,'{"type":"article","article":{}}','Synthetic','https://fixture.example/'||n,'https://fixture.example/'||n FROM generate_series(1,2001) n`, f.workspaceID)
	require.NoError(t, err)
	_, err = conn.Exec(t.Context(), `INSERT INTO item_projection_state(workspace_id,item_id,revision,updated_at) SELECT workspace_id,id,$2,$3 FROM items WHERE workspace_id=$1`, f.workspaceID, uuid.New(), f.now)
	require.NoError(t, err)
	jobID := enqueueTransitionJob(t, f.store, f.workspaceID, f.now)
	start := domain.ModelTransitionStart{WorkspaceID: f.workspaceID, ModelID: domain.DefaultEmbeddingModelID, JobID: jobID, StartedAt: f.now, Force: true}
	_, err = f.store.Begin(t.Context(), start)
	require.NoError(t, err)
	var count int
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&count))
	require.Equal(t, 1000, count, "Begin cannot initialize the entire backlog under one lock")
	_, err = conn.Exec(t.Context(), `DELETE FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID)
	require.NoError(t, err)
	require.Error(t, f.store.Activate(t.Context(), domain.ModelTransitionCutover{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, ActivatedAt: f.now}), "empty pending before seed completion cannot activate")
	done, err := f.store.SeedModelProjections(t.Context(), domain.ModelProjectionSeedCommand{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, Limit: 250})
	require.NoError(t, err)
	require.False(t, done)
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&count))
	require.Equal(t, 250, count)
	for !done {
		done, err = f.store.SeedModelProjections(t.Context(), domain.ModelProjectionSeedCommand{WorkspaceID: f.workspaceID, ModelID: start.ModelID, JobID: jobID, Limit: 250})
		require.NoError(t, err)
	}
	require.NoError(t, conn.QueryRow(t.Context(), `SELECT count(*) FROM retrieval_projection_pending WHERE workspace_id=$1`, f.workspaceID).Scan(&count))
	require.Equal(t, 1001, count, "initially completed batch is not seeded twice")
}
