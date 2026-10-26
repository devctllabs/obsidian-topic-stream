package vectorindex

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/ranking"
	"github.com/google/uuid"
	"github.com/qdrant/go-client/qdrant"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	DenseVectorName  = "dense"
	SparseVectorName = "bm25"
	BM25Model        = "qdrant/bm25"
	denseVectorBatch = 256
)

// CollectionName returns the versioned shared collection name for an Embedding Model.
func CollectionName(modelID uuid.UUID) string {
	return "topic_stream_items_" + strings.ReplaceAll(modelID.String(), "-", "") + "_v1"
}

// Backend provides the Qdrant operations used by the concrete vector index.
type Backend interface {
	// HealthCheck returns the server version used to validate query-feature support.
	HealthCheck(ctx context.Context) (*qdrant.HealthCheckReply, error)
	// CollectionExists reports whether the named Qdrant collection exists.
	CollectionExists(ctx context.Context, collection string) (bool, error)
	// GetCollectionInfo returns the collection's vector configuration and metadata.
	GetCollectionInfo(ctx context.Context, collection string) (*qdrant.CollectionInfo, error)
	// CreateCollection creates a Qdrant collection with the requested vector configuration.
	CreateCollection(ctx context.Context, request *qdrant.CreateCollection) error
	// UpdateCollection merges collection metadata without replacing vectors or points.
	UpdateCollection(ctx context.Context, request *qdrant.UpdateCollection) error
	// CreateFieldIndex creates a payload index for filtered retrieval.
	CreateFieldIndex(ctx context.Context, request *qdrant.CreateFieldIndexCollection) (*qdrant.UpdateResult, error)
	// Upsert writes points and their vector and payload data.
	Upsert(ctx context.Context, request *qdrant.UpsertPoints) (*qdrant.UpdateResult, error)
	// OverwritePayload replaces point payloads selected by the request.
	OverwritePayload(ctx context.Context, request *qdrant.SetPayloadPoints) (*qdrant.UpdateResult, error)
	// Query executes a single Qdrant point query.
	Query(ctx context.Context, request *qdrant.QueryPoints) ([]*qdrant.ScoredPoint, error)
	// QueryBatch executes the supplied Qdrant point queries as a batch.
	QueryBatch(ctx context.Context, request *qdrant.QueryBatchPoints) ([]*qdrant.BatchResult, error)
	// Get retrieves the points selected by the Qdrant request.
	Get(ctx context.Context, request *qdrant.GetPoints) ([]*qdrant.RetrievedPoint, error)
}

// Index maps canonical projections and ranking queries to one Qdrant collection.
type Index struct {
	backend             Backend
	collection          string
	dimensions          int
	searchPolicy        SearchPolicy
	policyMu            sync.Mutex
	policyServerChecked bool
}

// Document is the complete vector and payload representation of an Item in a model collection.
type Document struct {
	Version            int64
	Published          bool
	Hidden             bool
	ItemID             uuid.UUID
	WorkspaceID        uuid.UUID
	SourceIDs          []uuid.UUID
	Kind               domain.ItemKind
	Text               string
	Dense              []float32
	PublishedAt        *time.Time
	DiscoveredAt       time.Time
	ProjectionRevision uuid.UUID
	Feedback           *domain.Feedback
	TopicCorrections   []domain.TopicCorrectionProjection
}

// Filter aliases the ranking scope applied before vector query limits.
type Filter = ranking.IndexFilter

// Hit aliases the ranking evidence decoded from one matching vector point.
type Hit = ranking.IndexHit

// New validates the backend, collection, and vector size without creating derived state.
func New(backend Backend, collection string, dimensions int) (*Index, error) {
	if backend == nil {
		return nil, errors.New("qdrant backend is required")
	}
	if collection == "" {
		return nil, errors.New("qdrant collection is required")
	}
	if dimensions <= 0 {
		return nil, errors.New("dense vector dimensions must be positive")
	}
	return &Index{backend: backend, collection: collection, dimensions: dimensions}, nil
}

// Ensure creates missing derived index structures and validates existing configuration.
func (i *Index) Ensure(ctx context.Context) error {
	if err := i.checkSearchSupport(ctx); err != nil {
		return err
	}
	var schema map[string]*qdrant.PayloadSchemaInfo
	var info *qdrant.CollectionInfo
	exists, err := i.backend.CollectionExists(ctx, i.collection)
	if err != nil {
		return fmt.Errorf("check Qdrant collection %q: %w", i.collection, err)
	}
	if !exists {
		modifier := qdrant.Modifier_Idf
		request := &qdrant.CreateCollection{
			CollectionName: i.collection,
			Metadata:       map[string]*qdrant.Value{incarnationMetadataKey: qdrant.NewValueString(uuid.NewString())},
			VectorsConfig: qdrant.NewVectorsConfigMap(map[string]*qdrant.VectorParams{
				DenseVectorName: {Size: uint64(i.dimensions), Distance: qdrant.Distance_Cosine},
			}),
			SparseVectorsConfig: qdrant.NewSparseVectorsConfig(map[string]*qdrant.SparseVectorParams{
				SparseVectorName: {Modifier: &modifier},
			}),
		}
		request.HnswConfig = i.searchPolicy.collectionConfig()
		if err := i.backend.CreateCollection(ctx, request); err != nil {
			if status.Code(err) != codes.AlreadyExists {
				return fmt.Errorf("create Qdrant collection %q: %w", i.collection, err)
			}
			if info, err = i.validateCollection(ctx); err != nil {
				return err
			}
		}
	} else if info, err = i.validateCollection(ctx); err != nil {
		return err
	}
	if info != nil {
		schema = info.PayloadSchema
	}
	if err := i.ensurePayloadIndexes(ctx, schema); err != nil {
		return err
	}
	return i.reconcileSearchPolicy(ctx, info)
}

func (i *Index) ensurePayloadIndexes(ctx context.Context, schema map[string]*qdrant.PayloadSchemaInfo) error {
	for _, field := range requiredPayloadIndexes() {
		if field.matches(schema[field.name]) {
			continue
		}
		if _, err := i.backend.CreateFieldIndex(ctx, field.request(i.collection)); err != nil {
			return fmt.Errorf("create Qdrant %s index for %q: %w", field.name, i.collection, err)
		}
	}
	return nil
}

func (i *Index) validateCollection(ctx context.Context) (*qdrant.CollectionInfo, error) {
	info, err := i.backend.GetCollectionInfo(ctx, i.collection)
	if err != nil {
		return nil, fmt.Errorf("inspect Qdrant collection %q: %w", i.collection, err)
	}
	if info == nil || info.Config == nil || info.Config.Params == nil {
		return nil, fmt.Errorf("qdrant collection %q has no readable schema", i.collection)
	}
	dense := info.Config.Params.GetVectorsConfig().GetParamsMap().GetMap()[DenseVectorName]
	if dense == nil {
		return nil, fmt.Errorf("qdrant collection %q has no %s vector", i.collection, DenseVectorName)
	}
	if dense.Size != uint64(i.dimensions) {
		return nil, fmt.Errorf("qdrant collection %q dense vector dimensions are %d, expected %d", i.collection, dense.Size, i.dimensions)
	}
	if dense.Distance != qdrant.Distance_Cosine {
		return nil, fmt.Errorf("qdrant collection %q dense vector distance is %s, expected cosine", i.collection, dense.Distance)
	}
	sparse := info.Config.Params.GetSparseVectorsConfig().GetMap()[SparseVectorName]
	if sparse == nil || sparse.GetModifier() != qdrant.Modifier_Idf {
		return nil, fmt.Errorf("qdrant collection %q has incompatible %s vector", i.collection, SparseVectorName)
	}
	return info, nil
}

type payloadIndex struct {
	name   string
	field  qdrant.FieldType
	tenant bool
}

func (i payloadIndex) matches(schema *qdrant.PayloadSchemaInfo) bool {
	if schema == nil {
		return false
	}
	schemaTypes := map[qdrant.FieldType]qdrant.PayloadSchemaType{
		qdrant.FieldType_FieldTypeKeyword:  qdrant.PayloadSchemaType_Keyword,
		qdrant.FieldType_FieldTypeInteger:  qdrant.PayloadSchemaType_Integer,
		qdrant.FieldType_FieldTypeDatetime: qdrant.PayloadSchemaType_Datetime,
		qdrant.FieldType_FieldTypeBool:     qdrant.PayloadSchemaType_Bool,
	}
	return schema.DataType == schemaTypes[i.field] && (!i.tenant || schema.GetParams().GetKeywordIndexParams().GetIsTenant())
}

func requiredPayloadIndexes() []payloadIndex {
	return []payloadIndex{
		{name: "workspaceId", field: qdrant.FieldType_FieldTypeKeyword, tenant: true},
		{name: "sourceIds", field: qdrant.FieldType_FieldTypeKeyword},
		{name: "kind", field: qdrant.FieldType_FieldTypeKeyword},
		{name: "publishedAt", field: qdrant.FieldType_FieldTypeDatetime},
		{name: "discoveredAt", field: qdrant.FieldType_FieldTypeDatetime},
		{name: "projectionVersion", field: qdrant.FieldType_FieldTypeInteger},
		{name: "published", field: qdrant.FieldType_FieldTypeBool},
		{name: "hidden", field: qdrant.FieldType_FieldTypeBool},
		{name: "contentAt", field: qdrant.FieldType_FieldTypeDatetime},
		{name: "projectionRevision", field: qdrant.FieldType_FieldTypeKeyword},
		{name: "feedback.sentiment", field: qdrant.FieldType_FieldTypeKeyword},
		{name: "topicCorrections[].topicId", field: qdrant.FieldType_FieldTypeKeyword},
		{name: "topicCorrections[].membership", field: qdrant.FieldType_FieldTypeKeyword},
		{name: "topicCorrections[].correctionEpoch", field: qdrant.FieldType_FieldTypeInteger},
	}
}

func (i payloadIndex) request(collection string) *qdrant.CreateFieldIndexCollection {
	wait := true
	request := &qdrant.CreateFieldIndexCollection{
		CollectionName: collection, Wait: &wait, FieldName: i.name, FieldType: i.field.Enum(),
	}
	if i.tenant {
		request.FieldIndexParams = qdrant.NewPayloadIndexParamsKeyword(&qdrant.KeywordIndexParams{
			IsTenant: qdrant.PtrOf(true),
		})
	}
	return request
}

// Upsert writes the document with dense and derived sparse vectors. Positive versions fence stale
// deliveries and are verified after writing; zero preserves the unversioned write path.
func (i *Index) Upsert(ctx context.Context, document Document) error {
	if document.ItemID == uuid.Nil || document.WorkspaceID == uuid.Nil {
		return errors.New("item and workspace IDs are required")
	}
	if len(document.Dense) != i.dimensions {
		return fmt.Errorf("dense vector has %d dimensions, expected %d", len(document.Dense), i.dimensions)
	}
	wait := true
	_, err := i.backend.Upsert(ctx, &qdrant.UpsertPoints{
		CollectionName: i.collection, Wait: &wait, UpdateFilter: projectionWriteFilter(document), Ordering: &qdrant.WriteOrdering{Type: qdrant.WriteOrderingType_Strong},
		Points: []*qdrant.PointStruct{{
			Id: qdrant.NewIDUUID(document.ItemID.String()),
			Vectors: qdrant.NewVectorsMap(map[string]*qdrant.Vector{
				DenseVectorName:  qdrant.NewVector(document.Dense...),
				SparseVectorName: qdrant.NewVectorDocument(&qdrant.Document{Model: BM25Model, Text: document.Text}),
			}),
			Payload: documentPayload(document),
		}},
	})
	if err != nil {
		return fmt.Errorf("upsert item %s into Qdrant: %w", document.ItemID, err)
	}
	if document.Version > 0 {
		return i.verifyProjection(ctx, document)
	}
	return nil
}

func documentPayload(document Document) map[string]*qdrant.Value {
	sourceIDs := make([]*qdrant.Value, len(document.SourceIDs))
	for index, sourceID := range document.SourceIDs {
		sourceIDs[index] = qdrant.NewValueString(sourceID.String())
	}
	contentAt := document.DiscoveredAt
	if document.PublishedAt != nil {
		contentAt = *document.PublishedAt
	}
	payload := map[string]*qdrant.Value{
		"projectionVersion": qdrant.NewValueInt(document.Version),
		"published":         qdrant.NewValueBool(document.Published),
		"hidden":            qdrant.NewValueBool(document.Hidden),
		"textFingerprint":   qdrant.NewValueString(textFingerprint(document.Text)),
		"contentAt":         timestampValue(contentAt),
		"itemId":            qdrant.NewValueString(document.ItemID.String()),
		"workspaceId":       qdrant.NewValueString(document.WorkspaceID.String()),
		"sourceIds":         qdrant.NewValueFromList(sourceIDs...),
		"kind":              qdrant.NewValueString(string(document.Kind)),
		"discoveredAt":      timestampValue(document.DiscoveredAt),
		"topicCorrections":  correctionsValue(document.TopicCorrections),
	}
	if document.ProjectionRevision != uuid.Nil {
		payload["projectionRevision"] = qdrant.NewValueString(document.ProjectionRevision.String())
	}
	if document.Feedback != nil {
		payload["feedback"] = feedbackValue(*document.Feedback)
	}
	if document.PublishedAt != nil {
		payload["publishedAt"] = timestampValue(*document.PublishedAt)
	}
	return payload
}

func feedbackValue(feedback domain.Feedback) *qdrant.Value {
	fields := map[string]*qdrant.Value{
		"sentiment": qdrant.NewValueString(feedback.Sentiment),
		"updatedAt": timestampValue(feedback.UpdatedAt),
	}
	if feedback.Comment != nil {
		fields["comment"] = qdrant.NewValueString(*feedback.Comment)
	}
	return qdrant.NewValueFromFields(fields)
}

func correctionsValue(corrections []domain.TopicCorrectionProjection) *qdrant.Value {
	values := make([]*qdrant.Value, len(corrections))
	for index, correction := range corrections {
		values[index] = qdrant.NewValueFromFields(map[string]*qdrant.Value{
			"topicId":         qdrant.NewValueString(correction.TopicID.String()),
			"membership":      qdrant.NewValueString(string(correction.Membership)),
			"correctionEpoch": qdrant.NewValueInt(correction.CorrectionEpoch),
		})
	}
	return qdrant.NewValueFromList(values...)
}

func timestampValue(value time.Time) *qdrant.Value {
	return qdrant.NewValueString(value.UTC().Format(time.RFC3339Nano))
}

// UpsertItem writes canonical Item content and its dense vector as a derived document.
func (i *Index) UpsertItem(ctx context.Context, item domain.Item, text string, vector []float32) error {
	return i.Upsert(ctx, Document{
		ItemID: item.ID, WorkspaceID: item.WorkspaceID, SourceIDs: item.SourceIDs, Kind: item.Kind,
		Text: text, Dense: vector, PublishedAt: item.PublishedAt, DiscoveredAt: item.DiscoveredAt,
	})
}

// UpsertProjection writes canonical projection state with the supplied dense vector.
func (i *Index) UpsertProjection(ctx context.Context, value domain.ItemProjection, vector []float32) error {
	document := projectionDocument(value)
	document.Dense = vector
	return i.Upsert(ctx, document)
}

// QueryDense retrieves bounded dense-vector matches satisfying the filter.
func (i *Index) QueryDense(ctx context.Context, vector []float32, filter Filter, limit int) ([]Hit, error) {
	if len(vector) != i.dimensions {
		return nil, fmt.Errorf("dense query has %d dimensions, expected %d", len(vector), i.dimensions)
	}
	return i.query(ctx, indexQuery{query: qdrant.NewQuery(vector...), vectorName: DenseVectorName, filter: filter, limit: limit})
}

// RetrievePersonal returns direct feedback and positive and negative similarity neighborhoods.
func (i *Index) RetrievePersonal(
	ctx context.Context,
	request ranking.PersonalIndexRequest,
) (ranking.PersonalRetrieval, error) {
	if len(request.CandidateIDs) == 0 {
		return ranking.PersonalRetrieval{Direct: map[uuid.UUID]domain.Feedback{}}, nil
	}
	if request.Filter.WorkspaceID == uuid.Nil || request.NeighborLimit <= 0 {
		return ranking.PersonalRetrieval{}, errors.New("personal retrieval requires a workspace and positive neighbor limit")
	}
	if err := i.checkSearchSupport(ctx); err != nil {
		return ranking.PersonalRetrieval{}, err
	}
	queries := make([]*qdrant.QueryPoints, 0, 2*len(request.CandidateIDs))
	for _, candidateID := range request.CandidateIDs {
		for _, sentiment := range []string{"like", "dislike"} {
			query := personalQuery(candidateID, sentiment, request)
			query.CollectionName = i.collection
			query.Params = i.searchPolicy.params()
			queries = append(queries, query)
		}
	}
	results, err := i.backend.QueryBatch(ctx, &qdrant.QueryBatchPoints{
		CollectionName: i.collection, QueryPoints: queries,
	})
	if err != nil {
		return ranking.PersonalRetrieval{}, fmt.Errorf("batch query Qdrant personal evidence: %w", err)
	}
	if len(results) != len(queries) {
		return ranking.PersonalRetrieval{}, fmt.Errorf(
			"qdrant returned %d personal results for %d queries", len(results), len(queries),
		)
	}
	return mapPersonalRetrieval(request, results)
}

func personalQuery(
	candidateID uuid.UUID,
	sentiment string,
	request ranking.PersonalIndexRequest,
) *qdrant.QueryPoints {
	conditions := []*qdrant.Condition{
		qdrant.NewMatch("workspaceId", request.Filter.WorkspaceID.String()),
		qdrant.NewMatch("feedback.sentiment", sentiment),
	}
	if len(request.FeedbackIDs) > 0 {
		pointIDs := make([]*qdrant.PointId, len(request.FeedbackIDs))
		for index, itemID := range request.FeedbackIDs {
			pointIDs[index] = qdrant.NewIDUUID(itemID.String())
		}
		conditions = append(conditions, qdrant.NewHasID(pointIDs...))
	}
	return &qdrant.QueryPoints{
		Query:  qdrant.NewQueryID(qdrant.NewIDUUID(candidateID.String())),
		Using:  qdrant.PtrOf(DenseVectorName),
		Filter: &qdrant.Filter{Must: conditions},
		Limit:  qdrant.PtrOf(uint64(request.NeighborLimit + 1)),
		WithPayload: qdrant.NewWithPayloadInclude(
			"feedback.sentiment",
			"feedback.updatedAt",
		),
		WithVectors: qdrant.NewWithVectors(false),
	}
}

func mapPersonalRetrieval(
	request ranking.PersonalIndexRequest,
	results []*qdrant.BatchResult,
) (ranking.PersonalRetrieval, error) {
	mapper := personalRetrievalMapper{
		result: ranking.PersonalRetrieval{Direct: make(map[uuid.UUID]domain.Feedback, len(request.Direct))},
		limit:  request.NeighborLimit,
	}
	for itemID, feedback := range request.Direct {
		mapper.result.Direct[itemID] = feedback
	}
	mapper.positive = feedbackNeighborhoodCollector{values: &mapper.result.Positive, positions: make(map[uuid.UUID]int)}
	mapper.negative = feedbackNeighborhoodCollector{values: &mapper.result.Negative, positions: make(map[uuid.UUID]int)}
	for index, candidateID := range request.CandidateIDs {
		if err := mapper.add(candidateID, results[2*index:2*index+2]); err != nil {
			return ranking.PersonalRetrieval{}, err
		}
	}
	return mapper.result, nil
}

var personalSentiments = [...]string{"like", "dislike"}

type personalRetrievalMapper struct {
	result   ranking.PersonalRetrieval
	positive feedbackNeighborhoodCollector
	negative feedbackNeighborhoodCollector
	limit    int
}

func (m *personalRetrievalMapper) add(candidateID uuid.UUID, batches []*qdrant.BatchResult) error {
	for polarity, batch := range batches {
		if err := m.addPolarity(candidateID, polarity, batch); err != nil {
			return err
		}
	}
	return nil
}

func (m *personalRetrievalMapper) addPolarity(
	candidateID uuid.UUID,
	polarity int,
	batch *qdrant.BatchResult,
) error {
	if batch == nil {
		return errors.New("qdrant personal batch result is empty")
	}
	hits, err := mapPersonalScoredPoints(batch.Result)
	if err != nil {
		return err
	}
	m.captureDirectFeedback(candidateID, hits)
	if m.availableFeedback(candidateID, polarity, hits) < feedbackActivationCount {
		return nil
	}
	appendCandidateEvidence(m.collector(polarity), candidateID, hits, m.limit)
	return nil
}

func (m *personalRetrievalMapper) captureDirectFeedback(candidateID uuid.UUID, hits []Hit) {
	for _, hit := range hits {
		if hit.ItemID == candidateID && hit.Feedback != nil {
			m.result.Direct[candidateID] = *hit.Feedback
		}
	}
}

func (m *personalRetrievalMapper) availableFeedback(candidateID uuid.UUID, polarity int, hits []Hit) int {
	available := len(hits)
	if direct, found := m.result.Direct[candidateID]; found &&
		direct.Sentiment == personalSentiments[polarity] && !containsHit(hits, candidateID) {
		available++
	}
	return available
}

func (m *personalRetrievalMapper) collector(polarity int) feedbackNeighborhoodCollector {
	if polarity == 1 {
		return m.negative
	}
	return m.positive
}

func containsHit(hits []Hit, itemID uuid.UUID) bool {
	for _, hit := range hits {
		if hit.ItemID == itemID {
			return true
		}
	}
	return false
}

const feedbackActivationCount = 5

func mapPersonalScoredPoints(points []*qdrant.ScoredPoint) ([]Hit, error) {
	hits := make([]Hit, len(points))
	for index, point := range points {
		if point == nil || point.Id == nil {
			return nil, errors.New("personal result has no point ID")
		}
		itemID, err := uuid.Parse(point.Id.GetUuid())
		if err != nil {
			return nil, fmt.Errorf("invalid personal result ID: %w", err)
		}
		feedback, err := feedbackFromPayload(point.Payload["feedback"])
		if err != nil {
			return nil, fmt.Errorf("decode personal result feedback: %w", err)
		}
		hits[index] = Hit{ItemID: itemID, Score: float64(point.Score), Feedback: feedback}
	}
	return hits, nil
}

type feedbackNeighborhoodCollector struct {
	values    *[]ranking.FeedbackNeighborhood
	positions map[uuid.UUID]int
}

func appendCandidateEvidence(
	collector feedbackNeighborhoodCollector,
	candidateID uuid.UUID,
	hits []Hit,
	limit int,
) {
	added := 0
	for _, hit := range hits {
		if hit.ItemID == candidateID || hit.Feedback == nil || added >= limit {
			continue
		}
		position, found := collector.positions[hit.ItemID]
		if !found {
			position = len(*collector.values)
			collector.positions[hit.ItemID] = position
			*collector.values = append(*collector.values, ranking.FeedbackNeighborhood{Feedback: domain.ItemFeedback{
				ItemID: hit.ItemID, Feedback: *hit.Feedback,
			}})
		}
		(*collector.values)[position].Hits = append((*collector.values)[position].Hits, ranking.IndexHit{
			ItemID: candidateID, Score: hit.Score,
		})
		added++
	}
}

// QuerySparse retrieves bounded lexical matches satisfying the filter.
func (i *Index) QuerySparse(ctx context.Context, text string, filter Filter, limit int) ([]Hit, error) {
	if text == "" {
		return nil, errors.New("sparse query text is required")
	}
	query := qdrant.NewQueryNearest(qdrant.NewVectorInputDocument(&qdrant.Document{Model: BM25Model, Text: text}))
	return i.query(ctx, indexQuery{query: query, vectorName: SparseVectorName, filter: filter, limit: limit})
}

// QueryRecent returns recent Items satisfying the filter, bounded by limit.
func (i *Index) QueryRecent(ctx context.Context, filter Filter, limit int) ([]Hit, error) {
	direction := qdrant.Direction_Desc
	return i.query(ctx, indexQuery{query: qdrant.NewQueryOrderBy(&qdrant.OrderBy{
		Key: "discoveredAt", Direction: &direction,
	}), filter: filter, limit: limit})
}

// DenseVectors returns stored dense vectors for the requested Items.
func (i *Index) DenseVectors(
	ctx context.Context,
	workspaceID uuid.UUID,
	itemIDs []uuid.UUID,
) (map[uuid.UUID][]float32, error) {
	if workspaceID == uuid.Nil {
		return nil, errors.New("workspace ID is required")
	}
	result := make(map[uuid.UUID][]float32, len(itemIDs))
	for offset := 0; offset < len(itemIDs); offset += denseVectorBatch {
		end := min(offset+denseVectorBatch, len(itemIDs))
		ids := make([]*qdrant.PointId, end-offset)
		for position, itemID := range itemIDs[offset:end] {
			ids[position] = qdrant.NewIDUUID(itemID.String())
		}
		points, err := i.backend.Get(ctx, &qdrant.GetPoints{
			CollectionName: i.collection, Ids: ids,
			WithPayload: qdrant.NewWithPayload(true),
			WithVectors: qdrant.NewWithVectorsInclude(DenseVectorName),
		})
		if err != nil {
			return nil, fmt.Errorf("get exact Qdrant vectors: %w", err)
		}
		for _, point := range points {
			itemID, vector, err := denseVectorFromPoint(point, workspaceID, i.dimensions)
			if err != nil {
				return nil, err
			}
			result[itemID] = vector
		}
	}
	return result, nil
}

func denseVectorFromPoint(
	point *qdrant.RetrievedPoint,
	workspaceID uuid.UUID,
	dimensions int,
) (uuid.UUID, []float32, error) {
	if point == nil || point.Id == nil {
		return uuid.Nil, nil, errors.New("retrieved Qdrant point has no ID")
	}
	itemID, err := uuid.Parse(point.Id.GetUuid())
	if err != nil {
		return uuid.Nil, nil, fmt.Errorf("invalid retrieved item ID: %w", err)
	}
	if point.Payload["workspaceId"].GetStringValue() != workspaceID.String() {
		return uuid.Nil, nil, fmt.Errorf("retrieved item %s belongs to another workspace", itemID)
	}
	vector := point.GetVectors().GetVectors().GetVectors()[DenseVectorName]
	if vector == nil {
		return uuid.Nil, nil, fmt.Errorf("retrieved item %s has no dense vector", itemID)
	}
	dense := vector.GetDense().GetData()
	if len(dense) != dimensions {
		return uuid.Nil, nil, fmt.Errorf("retrieved item %s has %d dense dimensions, expected %d", itemID, len(dense), dimensions)
	}
	return itemID, append([]float32(nil), dense...), nil
}

// QueryTopicCorrections retrieves bounded explicit membership evidence for the current Topic correction
// epoch.
func (i *Index) QueryTopicCorrections(
	ctx context.Context,
	request ranking.TopicCorrectionIndexRequest,
) ([]Hit, error) {
	if request.TopicID == uuid.Nil || request.CorrectionEpoch < 1 || request.Limit <= 0 ||
		(request.Membership != domain.TopicMembershipInclude && request.Membership != domain.TopicMembershipExclude) {
		return nil, errors.New("topic correction query requires topic, correction epoch, membership, and positive limit")
	}
	nested := qdrant.NewNestedFilter("topicCorrections", &qdrant.Filter{Must: []*qdrant.Condition{
		qdrant.NewMatch("topicId", request.TopicID.String()),
		qdrant.NewMatchInt("correctionEpoch", request.CorrectionEpoch),
		qdrant.NewMatch("membership", string(request.Membership)),
	}})
	input := indexQuery{filter: request.Filter, limit: request.Limit, conditions: []*qdrant.Condition{nested}}
	if len(request.QueryVector) > 0 {
		input.query = qdrant.NewQueryNearest(qdrant.NewVectorInput(request.QueryVector...))
		input.vectorName = DenseVectorName
	} else {
		direction := qdrant.Direction_Desc
		input.query = qdrant.NewQueryOrderBy(&qdrant.OrderBy{Key: "discoveredAt", Direction: &direction})
	}
	return i.query(ctx, input)
}

type indexQuery struct {
	query      *qdrant.Query
	vectorName string
	filter     Filter
	limit      int
	conditions []*qdrant.Condition
}

func (i *Index) query(ctx context.Context, input indexQuery) ([]Hit, error) {
	if input.limit <= 0 {
		return nil, errors.New("query limit must be positive")
	}
	conditions, err := filterConditions(input.filter)
	if err != nil {
		return nil, err
	}
	conditions = append(conditions, input.conditions...)
	request := &qdrant.QueryPoints{
		CollectionName: i.collection, Query: input.query,
		Filter: &qdrant.Filter{Must: conditions}, Limit: qdrant.PtrOf(uint64(input.limit)),
		WithPayload: qdrant.NewWithPayload(true), WithVectors: qdrant.NewWithVectors(true),
	}
	if input.vectorName != "" {
		request.Using = qdrant.PtrOf(input.vectorName)
	}
	if input.vectorName == DenseVectorName {
		if err := i.checkSearchSupport(ctx); err != nil {
			return nil, err
		}
		request.Params = i.searchPolicy.params()
	}
	results, err := i.backend.Query(ctx, request)
	if err != nil {
		return nil, fmt.Errorf("query Qdrant %s vector: %w", input.vectorName, err)
	}
	hits := make([]Hit, 0, len(results))
	for _, result := range results {
		hit, err := mapHit(result)
		if err != nil {
			return nil, fmt.Errorf("decode Qdrant result: %w", err)
		}
		hits = append(hits, hit)
	}
	return hits, nil
}

func filterConditions(filter Filter) ([]*qdrant.Condition, error) {
	if filter.WorkspaceID == uuid.Nil {
		return nil, errors.New("workspace filter is required")
	}
	conditions := []*qdrant.Condition{qdrant.NewMatch("workspaceId", filter.WorkspaceID.String())}
	if filter.FeedCandidates {
		conditions = append(conditions, qdrant.NewRange("projectionVersion", &qdrant.Range{Gte: qdrant.PtrOf(float64(1))}), qdrant.NewMatchBool("published", false), qdrant.NewMatchBool("hidden", false),
			qdrant.NewFilterAsCondition(&qdrant.Filter{MustNot: []*qdrant.Condition{qdrant.NewMatch("feedback.sentiment", "dislike")}}))
	}
	if len(filter.SourceIDs) > 0 {
		values := make([]string, len(filter.SourceIDs))
		for index, value := range filter.SourceIDs {
			values[index] = value.String()
		}
		conditions = append(conditions, qdrant.NewMatchKeywords("sourceIds", values...))
	}
	if len(filter.Kinds) > 0 {
		values := make([]string, len(filter.Kinds))
		for index, value := range filter.Kinds {
			values[index] = string(value)
		}
		conditions = append(conditions, qdrant.NewMatchKeywords("kind", values...))
	}
	conditions = append(conditions, publicationDateConditions(filter)...)

	return conditions, nil
}

func mapHit(result *qdrant.ScoredPoint) (Hit, error) {
	if result == nil || result.Id == nil {
		return Hit{}, errors.New("result has no point ID")
	}
	itemID, err := uuid.Parse(result.Id.GetUuid())
	if err != nil {
		return Hit{}, fmt.Errorf("invalid item ID: %w", err)
	}
	hit := Hit{ItemID: itemID, Score: float64(result.Score)}
	hit.SourceID, err = sourceIDFromPayload(result.Payload["sourceIds"])
	if err != nil {
		return Hit{}, err
	}
	hit.PublishedAt, err = optionalTimestamp(result.Payload["publishedAt"])
	if err != nil {
		return Hit{}, err
	}
	hit.Embedding = denseVector(result)
	hit.Feedback, err = feedbackFromPayload(result.Payload["feedback"])
	if err != nil {
		return Hit{}, err
	}
	hit.TopicCorrections, err = correctionsFromPayload(result.Payload["topicCorrections"])
	if err != nil {
		return Hit{}, err
	}
	return hit, nil
}

func sourceIDFromPayload(value *qdrant.Value) (uuid.UUID, error) {
	values := value.GetListValue().GetValues()
	if len(values) == 0 {
		return uuid.Nil, nil
	}
	sourceID, err := uuid.Parse(values[0].GetStringValue())
	if err != nil {
		return uuid.Nil, fmt.Errorf("invalid source ID: %w", err)
	}
	return sourceID, nil
}

func optionalTimestamp(value *qdrant.Value) (*time.Time, error) {
	encoded := value.GetStringValue()
	if encoded == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid published timestamp: %w", err)
	}
	return &parsed, nil
}

func denseVector(result *qdrant.ScoredPoint) []float32 {
	if result.Vectors == nil {
		return nil
	}
	vector := result.Vectors.GetVectors().GetVectors()[DenseVectorName]
	if vector == nil {
		return nil
	}
	return append([]float32(nil), vector.GetDense().GetData()...)
}

func feedbackFromPayload(value *qdrant.Value) (*domain.Feedback, error) {
	if value == nil || value.GetStructValue() == nil {
		return nil, nil
	}
	fields := value.GetStructValue().Fields
	sentiment := fields["sentiment"].GetStringValue()
	if sentiment != "like" && sentiment != "dislike" {
		return nil, errors.New("invalid feedback sentiment in Qdrant payload")
	}
	updatedAt, err := time.Parse(time.RFC3339Nano, fields["updatedAt"].GetStringValue())
	if err != nil {
		return nil, fmt.Errorf("invalid feedback timestamp: %w", err)
	}
	feedback := &domain.Feedback{Sentiment: sentiment, UpdatedAt: updatedAt}
	if comment := fields["comment"].GetStringValue(); comment != "" {
		feedback.Comment = &comment
	}
	return feedback, nil
}

func correctionsFromPayload(value *qdrant.Value) ([]domain.TopicCorrectionProjection, error) {
	if value == nil || value.GetListValue() == nil {
		return nil, nil
	}
	values := value.GetListValue().Values
	result := make([]domain.TopicCorrectionProjection, 0, len(values))
	for _, value := range values {
		fields := value.GetStructValue().Fields
		topicID, err := uuid.Parse(fields["topicId"].GetStringValue())
		if err != nil {
			return nil, fmt.Errorf("invalid topic correction ID: %w", err)
		}
		membership := domain.TopicMembership(fields["membership"].GetStringValue())
		if membership != domain.TopicMembershipInclude && membership != domain.TopicMembershipExclude {
			return nil, errors.New("invalid topic correction membership in Qdrant payload")
		}
		result = append(result, domain.TopicCorrectionProjection{
			TopicID: topicID, Membership: membership,
			CorrectionEpoch: fields["correctionEpoch"].GetIntegerValue(),
		})
	}
	return result, nil
}

func publicationDateConditions(filter Filter) []*qdrant.Condition {
	if filter.PublishedAfter != nil || filter.PublishedBefore != nil {
		rangeFilter := &qdrant.DatetimeRange{}
		if filter.PublishedAfter != nil {
			rangeFilter.Gte = timestamppb.New(*filter.PublishedAfter)
		}
		if filter.PublishedBefore != nil {
			rangeFilter.Lte = timestamppb.New(*filter.PublishedBefore)
		}
		dateField := "publishedAt"
		if filter.FeedCandidates {
			dateField = "contentAt"
		}
		return []*qdrant.Condition{qdrant.NewDatetimeRange(dateField, rangeFilter)}
	}
	return nil
}
