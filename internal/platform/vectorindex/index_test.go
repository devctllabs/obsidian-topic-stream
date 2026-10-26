package vectorindex

import (
	"context"
	"testing"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"github.com/stretchr/testify/require"
)

type backendSpy struct {
	exists         bool
	createRequest  *qdrant.CreateCollection
	fieldIndexes   []*qdrant.CreateFieldIndexCollection
	upsertRequest  *qdrant.UpsertPoints
	queryRequests  []*qdrant.QueryPoints
	queryResult    []*qdrant.ScoredPoint
	batchRequest   *qdrant.QueryBatchPoints
	batchResult    []*qdrant.BatchResult
	getRequests    []*qdrant.GetPoints
	getResult      []*qdrant.RetrievedPoint
	collectionInfo *qdrant.CollectionInfo
	updateRequests []*qdrant.UpdateCollection
	healthVersion  string
	healthCalls    int
	healthErr      error
}

// HealthCheck returns the configured Qdrant version without network I/O.
func (b *backendSpy) HealthCheck(context.Context) (*qdrant.HealthCheckReply, error) {
	b.healthCalls++
	version := b.healthVersion
	if version == "" {
		version = "1.19.1"
	}
	return &qdrant.HealthCheckReply{Version: version}, b.healthErr
}

// CreateFieldIndex records the payload index request and returns success.
func (b *backendSpy) CreateFieldIndex(_ context.Context, request *qdrant.CreateFieldIndexCollection) (*qdrant.UpdateResult, error) {
	b.fieldIndexes = append(b.fieldIndexes, request)
	return &qdrant.UpdateResult{}, nil
}

// CollectionExists returns the configured collection-existence result.
func (b *backendSpy) CollectionExists(context.Context, string) (bool, error) {
	return b.exists, nil
}

// GetCollectionInfo returns the configured collection metadata.
func (b *backendSpy) GetCollectionInfo(context.Context, string) (*qdrant.CollectionInfo, error) {
	return b.collectionInfo, nil
}

// CreateCollection records the collection creation request without contacting Qdrant.
func (b *backendSpy) CreateCollection(_ context.Context, request *qdrant.CreateCollection) error {
	b.createRequest = request
	return nil
}

// UpdateCollection merges metadata using the same semantics as Qdrant.
func (b *backendSpy) UpdateCollection(_ context.Context, request *qdrant.UpdateCollection) error {
	b.updateRequests = append(b.updateRequests, request)
	if request.HnswConfig != nil {
		b.collectionInfo.Config.HnswConfig.FullScanThreshold = request.HnswConfig.FullScanThreshold
	}
	if b.collectionInfo.Config.Metadata == nil {
		b.collectionInfo.Config.Metadata = make(map[string]*qdrant.Value)
	}
	for key, value := range request.Metadata {
		b.collectionInfo.Config.Metadata[key] = value
	}
	return nil
}

// Upsert records the point upsert request and returns success.
func (b *backendSpy) Upsert(_ context.Context, request *qdrant.UpsertPoints) (*qdrant.UpdateResult, error) {
	b.upsertRequest = request
	return &qdrant.UpdateResult{}, nil
}

// Query records the point query and returns configured matches.
func (b *backendSpy) Query(_ context.Context, request *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error) {
	b.queryRequests = append(b.queryRequests, request)
	return b.queryResult, nil
}

// QueryBatch records the batch query and returns configured matches.
func (b *backendSpy) QueryBatch(_ context.Context, request *qdrant.QueryBatchPoints) ([]*qdrant.BatchResult, error) {
	b.batchRequest = request
	return b.batchResult, nil
}

// Get records the point request and returns configured retrieved points.
func (b *backendSpy) Get(_ context.Context, request *qdrant.GetPoints) ([]*qdrant.RetrievedPoint, error) {
	b.getRequests = append(b.getRequests, request)
	return b.getResult, nil
}

func TestIndexEnsuresDenseAndBM25Representations(t *testing.T) {
	t.Parallel()
	backend := &backendSpy{}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)

	require.NoError(t, index.Ensure(context.Background()))

	request := backend.createRequest
	require.NotNil(t, request)
	require.Equal(t, "topic_stream_items_v1", request.CollectionName)
	dense := request.VectorsConfig.GetParamsMap().Map[DenseVectorName]
	require.Equal(t, uint64(3), dense.Size)
	require.Equal(t, qdrant.Distance_Cosine, dense.Distance)
	sparse := request.SparseVectorsConfig.Map[SparseVectorName]
	require.Equal(t, qdrant.Modifier_Idf, sparse.GetModifier())
	expectedIndexes := map[string]qdrant.FieldType{
		"workspaceId":                        qdrant.FieldType_FieldTypeKeyword,
		"sourceIds":                          qdrant.FieldType_FieldTypeKeyword,
		"kind":                               qdrant.FieldType_FieldTypeKeyword,
		"publishedAt":                        qdrant.FieldType_FieldTypeDatetime,
		"discoveredAt":                       qdrant.FieldType_FieldTypeDatetime,
		"projectionVersion":                  qdrant.FieldType_FieldTypeInteger,
		"published":                          qdrant.FieldType_FieldTypeBool,
		"hidden":                             qdrant.FieldType_FieldTypeBool,
		"contentAt":                          qdrant.FieldType_FieldTypeDatetime,
		"projectionRevision":                 qdrant.FieldType_FieldTypeKeyword,
		"feedback.sentiment":                 qdrant.FieldType_FieldTypeKeyword,
		"topicCorrections[].topicId":         qdrant.FieldType_FieldTypeKeyword,
		"topicCorrections[].membership":      qdrant.FieldType_FieldTypeKeyword,
		"topicCorrections[].correctionEpoch": qdrant.FieldType_FieldTypeInteger,
	}
	require.Len(t, backend.fieldIndexes, len(expectedIndexes))
	for _, index := range backend.fieldIndexes {
		require.Equal(t, expectedIndexes[index.FieldName], index.GetFieldType(), index.FieldName)
		delete(expectedIndexes, index.FieldName)
	}
	require.Empty(t, expectedIndexes)
	require.True(t, backend.fieldIndexes[0].GetFieldIndexParams().GetKeywordIndexParams().GetIsTenant())
}

func TestIndexDoesNotRecreateExistingCollection(t *testing.T) {
	t.Parallel()
	backend := &backendSpy{exists: true, collectionInfo: validCollectionInfo(3)}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)

	require.NoError(t, index.Ensure(context.Background()))
	require.Nil(t, backend.createRequest)
	require.Len(t, backend.fieldIndexes, 14, "existing collections are reconciled with every required payload index")
}

func TestIndexEnsureSkipsExistingPayloadIndexesAndRepairsMissingOnes(t *testing.T) {
	t.Parallel()
	info := validCollectionInfo(3)
	info.PayloadSchema = map[string]*qdrant.PayloadSchemaInfo{
		"workspaceId":                        {DataType: qdrant.PayloadSchemaType_Keyword, Params: qdrant.NewPayloadIndexParamsKeyword(&qdrant.KeywordIndexParams{IsTenant: qdrant.PtrOf(true)})},
		"sourceIds":                          {DataType: qdrant.PayloadSchemaType_Keyword},
		"kind":                               {DataType: qdrant.PayloadSchemaType_Keyword},
		"publishedAt":                        {DataType: qdrant.PayloadSchemaType_Datetime},
		"discoveredAt":                       {DataType: qdrant.PayloadSchemaType_Datetime},
		"projectionVersion":                  {DataType: qdrant.PayloadSchemaType_Integer},
		"published":                          {DataType: qdrant.PayloadSchemaType_Bool},
		"hidden":                             {DataType: qdrant.PayloadSchemaType_Bool},
		"contentAt":                          {DataType: qdrant.PayloadSchemaType_Datetime},
		"projectionRevision":                 {DataType: qdrant.PayloadSchemaType_Keyword},
		"feedback.sentiment":                 {DataType: qdrant.PayloadSchemaType_Keyword},
		"topicCorrections[].topicId":         {DataType: qdrant.PayloadSchemaType_Keyword},
		"topicCorrections[].membership":      {DataType: qdrant.PayloadSchemaType_Keyword},
		"topicCorrections[].correctionEpoch": {DataType: qdrant.PayloadSchemaType_Integer},
	}
	backend := &backendSpy{exists: true, collectionInfo: info}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)
	for range 3 {
		require.NoError(t, index.Ensure(t.Context()))
	}
	require.Empty(t, backend.fieldIndexes, "healthy collection checks must not issue payload-index writes")

	delete(info.PayloadSchema, "hidden")
	require.NoError(t, index.Ensure(t.Context()))
	require.Len(t, backend.fieldIndexes, 1, "a removed index must be reconciled without cached readiness")
	require.Equal(t, "hidden", backend.fieldIndexes[0].FieldName)

	backend.fieldIndexes = nil
	backend.collectionInfo = validCollectionInfo(3)
	require.NoError(t, index.Ensure(t.Context()))
	require.Len(t, backend.fieldIndexes, 14, "a replaced collection must receive all indexes")
}

func TestIndexRejectsExistingCollectionWithDifferentVectorSchema(t *testing.T) {
	t.Parallel()
	backend := &backendSpy{exists: true, collectionInfo: validCollectionInfo(4)}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)

	err = index.Ensure(t.Context())

	require.ErrorContains(t, err, "dense vector dimensions")
	require.Nil(t, backend.createRequest, "schema mismatch must never delete or recreate a collection")
	require.Empty(t, backend.fieldIndexes)
}

func TestIndexEnsureDoesNotSkipIncompatiblePayloadIndexes(t *testing.T) {
	t.Parallel()
	for name, schema := range map[string]*qdrant.PayloadSchemaInfo{
		"wrong type":             {DataType: qdrant.PayloadSchemaType_Integer},
		"missing tenant setting": {DataType: qdrant.PayloadSchemaType_Keyword},
		"tenant disabled":        {DataType: qdrant.PayloadSchemaType_Keyword, Params: qdrant.NewPayloadIndexParamsKeyword(&qdrant.KeywordIndexParams{IsTenant: qdrant.PtrOf(false)})},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			info := validCollectionInfo(3)
			info.PayloadSchema = map[string]*qdrant.PayloadSchemaInfo{"workspaceId": schema}
			backend := &backendSpy{exists: true, collectionInfo: info}
			index, err := New(backend, "topic_stream_items_v1", 3)
			require.NoError(t, err)
			require.NoError(t, index.Ensure(t.Context()))
			require.Len(t, backend.fieldIndexes, 14)
			require.Equal(t, "workspaceId", backend.fieldIndexes[0].FieldName)
		})
	}
}

func validCollectionInfo(dimensions uint64) *qdrant.CollectionInfo {
	modifier := qdrant.Modifier_Idf
	return &qdrant.CollectionInfo{Config: &qdrant.CollectionConfig{Params: &qdrant.CollectionParams{
		VectorsConfig: qdrant.NewVectorsConfigMap(map[string]*qdrant.VectorParams{
			DenseVectorName: {Size: dimensions, Distance: qdrant.Distance_Cosine},
		}),
		SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{
			SparseVectorName: {Modifier: &modifier},
		}),
	}}}
}

func TestIndexQueriesRecentItemsWithPreFilters(t *testing.T) {
	t.Parallel()
	itemID := uuid.MustParse("3c596944-90f4-45ec-90ab-452d59b6781a")
	backend := &backendSpy{queryResult: []*qdrant.ScoredPoint{{
		Id: qdrant.NewIDUUID(itemID.String()),
		Payload: qdrant.NewValueMap(map[string]any{
			"sourceIds": []any{"d5066990-128a-4a48-a16f-e86c8515bb14"},
		}),
	}}}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)
	workspaceID := uuid.MustParse("3a42b474-e10d-4740-9bca-a301329fa12a")

	hits, err := index.QueryRecent(context.Background(), Filter{WorkspaceID: workspaceID}, 20)

	require.NoError(t, err)
	require.Equal(t, itemID, hits[0].ItemID)
	require.Len(t, backend.queryRequests, 1)
	request := backend.queryRequests[0]
	require.Equal(t, "discoveredAt", request.GetQuery().GetOrderBy().GetKey())
	require.Equal(t, qdrant.Direction_Desc, request.GetQuery().GetOrderBy().GetDirection())
	require.Equal(t, uint64(20), request.GetLimit())
	require.True(t, request.GetWithPayload().GetEnable())
	require.True(t, request.GetWithVectors().GetEnable())
}

func TestIndexUpsertsTypedDocumentWithDenseAndBM25Vectors(t *testing.T) {
	t.Parallel()
	backend := &backendSpy{}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)
	itemID := uuid.MustParse("3c596944-90f4-45ec-90ab-452d59b6781a")
	workspaceID := uuid.MustParse("3a42b474-e10d-4740-9bca-a301329fa12a")
	sourceID := uuid.MustParse("d5066990-128a-4a48-a16f-e86c8515bb14")
	publishedAt := time.Date(2026, time.September, 20, 12, 0, 0, 0, time.UTC)
	projectionRevision := uuid.MustParse("0199a6e5-4c00-7000-8000-000000000123")
	topicID := uuid.New()
	comment := "deep implementation detail"

	err = index.Upsert(context.Background(), Document{
		ItemID: itemID, WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID}, Kind: domain.ItemKindPaper,
		Text: "KV-cache architecture\nA technical paper", Dense: []float32{0.1, 0.2, 0.3},
		PublishedAt: &publishedAt, DiscoveredAt: publishedAt.Add(time.Hour),
		ProjectionRevision: projectionRevision,
		Feedback:           &domain.Feedback{Sentiment: "like", Comment: &comment, UpdatedAt: publishedAt},
		TopicCorrections: []domain.TopicCorrectionProjection{{
			TopicID: topicID, Membership: domain.TopicMembershipInclude, CorrectionEpoch: 4,
		}},
	})

	require.NoError(t, err)
	require.True(t, backend.upsertRequest.GetWait())
	require.Equal(t, "topic_stream_items_v1", backend.upsertRequest.CollectionName)
	require.Len(t, backend.upsertRequest.Points, 1)
	point := backend.upsertRequest.Points[0]
	require.Equal(t, itemID.String(), point.Id.GetUuid())
	vectors := point.Vectors.GetVectors().Vectors
	require.Equal(t, []float32{0.1, 0.2, 0.3}, vectors[DenseVectorName].GetDense().Data)
	require.Equal(t, BM25Model, vectors[SparseVectorName].GetDocument().Model)
	require.Equal(t, "KV-cache architecture\nA technical paper", vectors[SparseVectorName].GetDocument().Text)
	require.Equal(t, workspaceID.String(), point.Payload["workspaceId"].GetStringValue())
	require.Equal(t, string(domain.ItemKindPaper), point.Payload["kind"].GetStringValue())
	require.Equal(t, projectionRevision.String(), point.Payload["projectionRevision"].GetStringValue())
	require.Equal(t, "like", point.Payload["feedback"].GetStructValue().Fields["sentiment"].GetStringValue())
	require.Equal(t, comment, point.Payload["feedback"].GetStructValue().Fields["comment"].GetStringValue())
	correction := point.Payload["topicCorrections"].GetListValue().Values[0].GetStructValue().Fields
	require.Equal(t, topicID.String(), correction["topicId"].GetStringValue())
	require.Equal(t, "include", correction["membership"].GetStringValue())
	require.Equal(t, int64(4), correction["correctionEpoch"].GetIntegerValue())
}

func TestIndexUpsertsCanonicalProjectionSnapshot(t *testing.T) {
	t.Parallel()

	backend := &backendSpy{}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)
	workspaceID, itemID, sourceID := uuid.New(), uuid.New(), uuid.New()
	revision, topicID := uuid.New(), uuid.New()
	comment := "practical"
	projection := domain.ItemProjection{
		Revision: revision,
		Item: domain.Item{
			ID: itemID, WorkspaceID: workspaceID, SourceIDs: []uuid.UUID{sourceID}, Kind: domain.ItemKindArticle,
			Title: "Typed projection", ContentMarkdown: "Details", DiscoveredAt: time.Now(),
		},
		Feedback: &domain.Feedback{Sentiment: "like", Comment: &comment, UpdatedAt: time.Now()},
		TopicCorrections: []domain.TopicCorrectionProjection{{
			TopicID: topicID, Membership: domain.TopicMembershipExclude, CorrectionEpoch: 2,
		}},
	}

	err = index.UpsertProjection(t.Context(), projection, []float32{0.1, 0.2, 0.3})

	require.NoError(t, err)
	payload := backend.upsertRequest.Points[0].Payload
	require.Equal(t, revision.String(), payload["projectionRevision"].GetStringValue())
	require.Equal(t, "like", payload["feedback"].GetStructValue().Fields["sentiment"].GetStringValue())
	correction := payload["topicCorrections"].GetListValue().Values[0].GetStructValue().Fields
	require.Equal(t, topicID.String(), correction["topicId"].GetStringValue())
	require.Equal(t, "exclude", correction["membership"].GetStringValue())
}

func TestIndexQueriesDenseAndSparseWithPreFilters(t *testing.T) {
	t.Parallel()
	itemID := uuid.MustParse("3c596944-90f4-45ec-90ab-452d59b6781a")
	topicID := uuid.New()
	backend := &backendSpy{queryResult: []*qdrant.ScoredPoint{{
		Id: qdrant.NewIDUUID(itemID.String()), Score: 0.75,
		Payload: qdrant.NewValueMap(map[string]any{
			"sourceIds":   []any{"d5066990-128a-4a48-a16f-e86c8515bb14"},
			"publishedAt": "2026-09-20T12:00:00Z",
			"feedback": map[string]any{
				"sentiment": "like", "comment": "deep", "updatedAt": "2026-09-21T12:00:00Z",
			},
			"topicCorrections": []any{map[string]any{
				"topicId": topicID.String(), "membership": "exclude", "correctionEpoch": int64(3),
			}},
		}),
	}}}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)
	workspaceID := uuid.MustParse("3a42b474-e10d-4740-9bca-a301329fa12a")
	from := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	filter := Filter{WorkspaceID: workspaceID, Kinds: []domain.ItemKind{domain.ItemKindPaper}, PublishedAfter: &from}

	denseHits, err := index.QueryDense(context.Background(), []float32{0.3, 0.2, 0.1}, filter, 20)
	require.NoError(t, err)
	sparseHits, err := index.QuerySparse(context.Background(), "KV cache", filter, 20)
	require.NoError(t, err)

	require.Equal(t, denseHits, sparseHits)
	require.Equal(t, itemID, denseHits[0].ItemID)
	require.InDelta(t, 0.75, denseHits[0].Score, 0.0001)
	require.Equal(t, "like", denseHits[0].Feedback.Sentiment)
	require.Equal(t, "deep", *denseHits[0].Feedback.Comment)
	require.Equal(t, []domain.TopicCorrectionProjection{{
		TopicID: topicID, Membership: domain.TopicMembershipExclude, CorrectionEpoch: 3,
	}}, denseHits[0].TopicCorrections)
	require.Len(t, backend.queryRequests, 2)
	require.Equal(t, DenseVectorName, backend.queryRequests[0].GetUsing())
	require.Equal(t, []float32{0.3, 0.2, 0.1}, backend.queryRequests[0].GetQuery().GetNearest().GetDense().Data)
	require.Equal(t, SparseVectorName, backend.queryRequests[1].GetUsing())
	document := backend.queryRequests[1].GetQuery().GetNearest().GetDocument()
	require.Equal(t, BM25Model, document.Model)
	require.Equal(t, "KV cache", document.Text)
	for _, request := range backend.queryRequests {
		require.Equal(t, uint64(20), request.GetLimit())
		require.Len(t, request.GetFilter().Must, 3)
		require.True(t, request.GetWithPayload().GetEnable())
		require.True(t, request.GetWithVectors().GetEnable())
	}
}

func TestIndexCollectsCandidateCentricFeedbackWithIndependentActivation(t *testing.T) {
	t.Parallel()

	workspaceID, candidateID := uuid.New(), uuid.New()
	now := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	likes := make([]*qdrant.ScoredPoint, 0, 5)
	likes = append(likes, feedbackPoint(candidateID, "like", now, 1))
	for score := 0.9; len(likes) < 5; score -= 0.1 {
		likes = append(likes, feedbackPoint(uuid.New(), "like", now, float32(score)))
	}
	dislikes := make([]*qdrant.ScoredPoint, 0, 4)
	for score := 0.8; len(dislikes) < 4; score -= 0.1 {
		dislikes = append(dislikes, feedbackPoint(uuid.New(), "dislike", now, float32(score)))
	}
	backend := &backendSpy{batchResult: []*qdrant.BatchResult{{Result: likes}, {Result: dislikes}}}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)

	result, err := index.RetrievePersonal(t.Context(), ranking.PersonalIndexRequest{
		CandidateIDs: []uuid.UUID{candidateID}, Filter: Filter{WorkspaceID: workspaceID}, NeighborLimit: 12,
		Direct: map[uuid.UUID]domain.Feedback{
			candidateID: {Sentiment: "like", UpdatedAt: now},
		},
	})

	require.NoError(t, err)
	require.Len(t, backend.batchRequest.QueryPoints, 2)
	for _, request := range backend.batchRequest.QueryPoints {
		require.Equal(t, uint64(13), request.GetLimit(), "one extra result allows self exclusion")
		require.Len(t, request.GetFilter().Must, 2)
		require.Equal(t, []string{"feedback.sentiment", "feedback.updatedAt"}, request.GetWithPayload().GetInclude().GetFields())
		require.False(t, request.GetWithVectors().GetEnable())
	}
	require.Len(t, result.Positive, 4)
	require.Empty(t, result.Negative, "four Dislikes do not activate negative neighborhood evidence")
	require.Equal(t, "like", result.Direct[candidateID].Sentiment)
	for _, neighborhood := range result.Positive {
		require.Len(t, neighborhood.Hits, 1)
		require.Equal(t, candidateID, neighborhood.Hits[0].ItemID)
	}
}

func TestIndexQueriesNearestManualIncludesForTheCurrentEpoch(t *testing.T) {
	t.Parallel()

	workspaceID, topicID, itemID := uuid.New(), uuid.New(), uuid.New()
	backend := &backendSpy{queryResult: []*qdrant.ScoredPoint{{
		Id: qdrant.NewIDUUID(itemID.String()),
		Payload: qdrant.NewValueMap(map[string]any{
			"topicCorrections": []any{map[string]any{
				"topicId": topicID.String(), "membership": "include", "correctionEpoch": int64(4),
			}},
		}),
	}}}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)

	hits, err := index.QueryTopicCorrections(t.Context(), ranking.TopicCorrectionIndexRequest{
		TopicID: topicID, CorrectionEpoch: 4, Membership: domain.TopicMembershipInclude,
		QueryVector: []float32{0.3, 0.2, 0.1}, Filter: Filter{WorkspaceID: workspaceID}, Limit: 10,
	})

	require.NoError(t, err)
	require.Equal(t, itemID, hits[0].ItemID)
	require.Len(t, backend.queryRequests, 1)
	request := backend.queryRequests[0]
	require.Equal(t, uint64(10), request.GetLimit())
	require.Equal(t, DenseVectorName, request.GetUsing())
	require.Equal(t, []float32{0.3, 0.2, 0.1}, request.GetQuery().GetNearest().GetDense().Data)
	require.Len(t, request.GetFilter().Must, 2)
	nested := request.GetFilter().Must[1].GetNested()
	require.Equal(t, "topicCorrections", nested.Key)
	require.Len(t, nested.Filter.Must, 3)
	require.True(t, request.GetWithVectors().GetEnable())
}

func feedbackPoint(itemID uuid.UUID, sentiment string, updatedAt time.Time, score float32) *qdrant.ScoredPoint {
	return &qdrant.ScoredPoint{
		Id: qdrant.NewIDUUID(itemID.String()), Score: score,
		Payload: qdrant.NewValueMap(map[string]any{
			"feedback": map[string]any{
				"sentiment": sentiment, "updatedAt": updatedAt.Format(time.RFC3339Nano),
			},
			"sourceIds": []any{"invalid and irrelevant to personal evidence"},
		}),
	}
}

func TestIndexRejectsWrongDenseDimensionsBeforeUpsert(t *testing.T) {
	t.Parallel()
	backend := &backendSpy{}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)

	err = index.Upsert(context.Background(), Document{ItemID: uuid.New(), WorkspaceID: uuid.New(), Dense: []float32{1, 2}})

	require.ErrorContains(t, err, "dimensions")
	require.Nil(t, backend.upsertRequest)
}

func TestIndexLoadsExactDenseVectorsInBoundedBatches(t *testing.T) {
	t.Parallel()

	workspaceID, firstID, secondID := uuid.New(), uuid.New(), uuid.New()
	backend := &backendSpy{getResult: []*qdrant.RetrievedPoint{
		retrievedPoint(firstID, workspaceID, []float32{1, 0, 0}),
		retrievedPoint(secondID, workspaceID, []float32{0, 1, 0}),
	}}
	index, err := New(backend, "topic_stream_items_v1", 3)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 257)
	ids[0], ids[1] = firstID, secondID
	for position := 2; position < len(ids); position++ {
		ids[position] = uuid.New()
	}

	vectors, err := index.DenseVectors(t.Context(), workspaceID, ids)

	require.NoError(t, err)
	require.Equal(t, []float32{1, 0, 0}, vectors[firstID])
	require.Equal(t, []float32{0, 1, 0}, vectors[secondID])
	require.Len(t, backend.getRequests, 2)
	require.Len(t, backend.getRequests[0].Ids, 256)
	require.Len(t, backend.getRequests[1].Ids, 1)
	require.True(t, backend.getRequests[0].GetWithPayload().GetEnable())
	require.Equal(t, []string{DenseVectorName}, backend.getRequests[0].GetWithVectors().GetInclude().Names)
}

func retrievedPoint(itemID, workspaceID uuid.UUID, vector []float32) *qdrant.RetrievedPoint {
	return &qdrant.RetrievedPoint{
		Id:      qdrant.NewIDUUID(itemID.String()),
		Payload: qdrant.NewValueMap(map[string]any{"workspaceId": workspaceID.String()}),
		Vectors: &qdrant.VectorsOutput{VectorsOptions: &qdrant.VectorsOutput_Vectors{
			Vectors: &qdrant.NamedVectorsOutput{Vectors: map[string]*qdrant.VectorOutput{
				DenseVectorName: {Vector: &qdrant.VectorOutput_Dense{Dense: &qdrant.DenseVector{Data: vector}}},
			}},
		}},
	}
}

// OverwritePayload accepts the payload replacement without contacting Qdrant.
func (b *backendSpy) OverwritePayload(_ context.Context, request *qdrant.SetPayloadPoints) (*qdrant.UpdateResult, error) {
	return &qdrant.UpdateResult{}, nil
}

func TestIndexDoesNotAcknowledgeMissingConditionalWrite(t *testing.T) {
	t.Parallel()
	index, err := New(&backendSpy{}, "fixture", 3)
	require.NoError(t, err)
	err = index.UpsertProjection(t.Context(), domain.ItemProjection{Version: 1, Revision: uuid.New(), Item: domain.Item{ID: uuid.New(), WorkspaceID: uuid.New(), Title: "Missing point", DiscoveredAt: time.Now()}}, []float32{1, 0, 0})
	require.ErrorIs(t, err, domain.ErrConflict)
}
