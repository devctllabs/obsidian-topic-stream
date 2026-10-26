package httpapi

import (
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/ethernity/obsidian-topic-stream/internal/service/feed"
	itemservice "github.com/ethernity/obsidian-topic-stream/internal/service/items"
	"github.com/ethernity/obsidian-topic-stream/internal/service/search"
)

func apiFeedback(feedback domain.Feedback) serverhttp.Feedback {
	return serverhttp.Feedback{
		Sentiment: serverhttp.FeedbackSentiment(feedback.Sentiment), Comment: feedback.Comment,
		CreatedAt: feedback.CreatedAt, UpdatedAt: feedback.UpdatedAt,
	}
}

func apiSource(source domain.Source) (serverhttp.Source, error) {
	config, err := apiSourceConfig(source.Config)
	if err != nil {
		return serverhttp.Source{}, err
	}
	return serverhttp.Source{
		Id: source.ID, Name: source.Name, Config: config,
		Enabled: source.Enabled, PollIntervalMinutes: source.PollIntervalMinutes,
		LastSyncedAt: source.LastSyncedAt, CreatedAt: source.CreatedAt, UpdatedAt: source.UpdatedAt,
	}, nil
}

func domainSourceConfig(value serverhttp.SourceConfig) (domain.SourceConfig, error) {
	kind, err := value.Discriminator()
	if err != nil {
		return domain.SourceConfig{}, err
	}
	switch kind {
	case "rss":
		return domainRSSConfig(value)
	case "atom":
		return domainAtomConfig(value)
	case "arxiv":
		return domainArxivConfig(value)
	case "github":
		return domainGitHubConfig(value)
	case "youtube":
		return domainYouTubeConfig(value)
	default:
		return domain.SourceConfig{}, fmt.Errorf("unsupported source config kind %q", kind)
	}
}

func domainRSSConfig(value serverhttp.SourceConfig) (domain.SourceConfig, error) {
	config, err := value.AsRssSourceConfig()
	if err != nil {
		return domain.SourceConfig{}, err
	}
	return domain.SourceConfig{Kind: domain.SourceKindRSS, URL: config.Url, AllowPrivateNetwork: config.AllowPrivateNetwork}, nil
}

func domainAtomConfig(value serverhttp.SourceConfig) (domain.SourceConfig, error) {
	config, err := value.AsAtomSourceConfig()
	if err != nil {
		return domain.SourceConfig{}, err
	}
	return domain.SourceConfig{Kind: domain.SourceKindAtom, URL: config.Url, AllowPrivateNetwork: config.AllowPrivateNetwork}, nil
}

func domainArxivConfig(value serverhttp.SourceConfig) (domain.SourceConfig, error) {
	config, err := value.AsArxivSourceConfig()
	if err != nil {
		return domain.SourceConfig{}, err
	}
	return domain.SourceConfig{Kind: domain.SourceKindArxiv, Query: config.Query}, nil
}

func domainGitHubConfig(value serverhttp.SourceConfig) (domain.SourceConfig, error) {
	config, err := value.AsGithubTrendingSourceConfig()
	if err != nil {
		return domain.SourceConfig{}, err
	}
	return domain.SourceConfig{
		Kind: domain.SourceKindGithub, Period: string(config.Period), Language: string(config.Language),
	}, nil
}

func domainYouTubeConfig(value serverhttp.SourceConfig) (domain.SourceConfig, error) {
	config, err := value.AsYoutubeSourceConfig()
	if err != nil {
		return domain.SourceConfig{}, err
	}
	return domain.SourceConfig{Kind: domain.SourceKindYoutube, ChannelID: config.ChannelId}, nil
}

func apiSourceConfig(value domain.SourceConfig) (serverhttp.SourceConfig, error) {
	var config serverhttp.SourceConfig
	switch value.Kind {
	case domain.SourceKindRSS:
		err := config.FromRssSourceConfig(serverhttp.RssSourceConfig{Kind: "rss", Url: value.URL, AllowPrivateNetwork: value.AllowPrivateNetwork})
		return config, err
	case domain.SourceKindAtom:
		err := config.FromAtomSourceConfig(serverhttp.AtomSourceConfig{Kind: "atom", Url: value.URL, AllowPrivateNetwork: value.AllowPrivateNetwork})
		return config, err
	case domain.SourceKindArxiv:
		err := config.FromArxivSourceConfig(serverhttp.ArxivSourceConfig{Kind: "arxiv", Query: value.Query})
		return config, err
	case domain.SourceKindGithub:
		err := config.FromGithubTrendingSourceConfig(serverhttp.GithubTrendingSourceConfig{Kind: "github", Period: serverhttp.GithubTrendingSourceConfigPeriod(value.Period), Language: serverhttp.GithubTrendingSourceConfigLanguage(value.Language)})
		return config, err
	case domain.SourceKindYoutube:
		err := config.FromYoutubeSourceConfig(serverhttp.YoutubeSourceConfig{Kind: "youtube", ChannelId: value.ChannelID})
		return config, err
	default:
		return config, fmt.Errorf("unsupported source config kind %q", value.Kind)
	}
}

func apiJob(job domain.Job) serverhttp.Job {
	result := serverhttp.Job{
		ModelTransition: apiModelTransition(job.ModelTransition),
		Id:              job.ID, Kind: job.Kind, Queue: serverhttp.JobQueue(job.Queue), Status: serverhttp.JobStatus(job.Status), Attempt: job.Attempt,
		MaxAttempts: job.MaxAttempts, CancellationRequested: job.CancellationRequested, Reason: job.Reason,
		CreatedAt: job.CreatedAt, StartedAt: job.StartedAt, FinishedAt: job.FinishedAt, UpdatedAt: job.UpdatedAt,
	}
	if job.Progress != nil {
		result.Progress = &serverhttp.JobProgress{Phase: serverhttp.JobProgressPhase(job.Progress.Phase), ProcessedItems: job.Progress.ProcessedItems, TotalItems: job.Progress.TotalItems, UpdatedAt: job.Progress.UpdatedAt}
	}
	if job.StartedAt != nil {
		end := job.UpdatedAt
		if job.FinishedAt != nil {
			end = *job.FinishedAt
		}
		duration := end.Sub(*job.StartedAt).Milliseconds()
		if duration < 0 {
			duration = 0
		}
		result.DurationMs = &duration
	}
	return result
}

func feedPage(page feed.Page) (serverhttp.FeedPage, error) {
	items := make([]serverhttp.FeedEntry, len(page.Items))
	for index, entry := range page.Items {
		signals := make(map[string]float32, len(entry.Signals))
		for name, value := range entry.Signals {
			signals[name] = float32(value)
		}
		mappedItem, err := apiItem(entry.Item)
		if err != nil {
			return serverhttp.FeedPage{}, err
		}
		items[index] = serverhttp.FeedEntry{
			Item:        mappedItem,
			Score:       float32(entry.Score),
			Explanation: serverhttp.FeedExplanation{Summary: entry.Summary, Signals: signals, PipelineVersion: entry.Pipeline},
		}
	}
	return serverhttp.FeedPage{Items: items, NextCursor: page.NextCursor, Snapshot: page.Snapshot, GeneratedAt: page.GeneratedAt}, nil
}

func searchPage(page search.Page) (serverhttp.SearchPage, error) {
	items := make([]serverhttp.FeedEntry, len(page.Items))
	for index, entry := range page.Items {
		mappedItem, err := apiItem(entry.Item)
		if err != nil {
			return serverhttp.SearchPage{}, err
		}
		signals := make(map[string]float32, len(entry.Signals))
		for name, value := range entry.Signals {
			signals[name] = float32(value)
		}
		items[index] = serverhttp.FeedEntry{
			Item: mappedItem, Score: float32(entry.Score),
			Explanation: serverhttp.FeedExplanation{Summary: entry.Summary, Signals: signals, PipelineVersion: entry.Pipeline},
		}
	}
	return serverhttp.SearchPage{Items: items, NextCursor: page.NextCursor}, nil
}

func apiItem(item domain.Item) (serverhttp.Item, error) {
	input := domain.SummaryInputForItem(item)
	origin := item.ContentOrigin
	if origin == "" {
		origin = domain.ContentOriginUnknown
	}
	details, err := apiItemDetails(item.Details)
	if err != nil {
		return serverhttp.Item{}, err
	}
	result := serverhttp.Item{
		Id:              item.ID,
		SourceIds:       append([]serverhttp.UUID(nil), item.SourceIDs...),
		Kind:            serverhttp.ItemKind(item.Kind),
		Details:         details,
		Title:           item.Title,
		Url:             item.URL,
		CanonicalUrl:    item.CanonicalURL,
		Excerpt:         item.Excerpt,
		ContentMarkdown: item.ContentMarkdown,
		ContentOrigin:   serverhttp.ContentOrigin(origin), SummarySupported: input.Supported(), SummarySourceVersion: input.SourceVersion,
		Authors:      append([]string(nil), item.Authors...),
		PublishedAt:  item.PublishedAt,
		DiscoveredAt: item.DiscoveredAt,
		TopicIds:     append([]serverhttp.UUID(nil), item.TopicIDs...),
		ReadingState: serverhttp.ReadingState(item.ReadingState),
		Visibility:   serverhttp.ItemVisibility(item.Visibility),
		Images:       apiItemImages(itemservice.ExtractImages(item)),
	}
	if result.Authors == nil {
		result.Authors = []string{}
	}
	if result.TopicIds == nil {
		result.TopicIds = []serverhttp.UUID{}
	}
	if item.Feedback != nil {
		feedback := apiFeedback(*item.Feedback)
		result.Feedback = &feedback
	}
	return result, nil
}

func apiItemImages(images []itemservice.ImageDescriptor) []serverhttp.ItemImage {
	result := make([]serverhttp.ItemImage, len(images))
	for index, image := range images {
		result[index] = serverhttp.ItemImage{Id: image.ID, Url: image.URL, AltText: image.AltText}
	}
	return result
}

func apiItemDetails(value domain.ItemDetails) (serverhttp.ItemDetails, error) {
	var details serverhttp.ItemDetails
	switch value.Type {
	case domain.ItemKindArticle:
		return apiArticleDetails(details, value.Article)
	case domain.ItemKindPaper:
		return apiPaperDetails(details, value.Paper)
	case domain.ItemKindVideo:
		return apiVideoDetails(details, value.Video)
	case domain.ItemKindRepository:
		return apiRepositoryDetails(details, value.Repository)
	default:
		return details, fmt.Errorf("unsupported item details type %q", value.Type)
	}
}

func apiArticleDetails(details serverhttp.ItemDetails, value *domain.ArticleDetails) (serverhttp.ItemDetails, error) {
	if value == nil {
		return details, fmt.Errorf("article details are missing")
	}
	err := details.FromArticleDetails(serverhttp.ArticleDetails{
		Type: serverhttp.ArticleDetailsTypeArticle, Description: value.Description,
	})
	return details, err
}

func apiPaperDetails(details serverhttp.ItemDetails, value *domain.PaperDetails) (serverhttp.ItemDetails, error) {
	if value == nil {
		return details, fmt.Errorf("paper details are missing")
	}
	categories := append([]string(nil), value.Categories...)
	if categories == nil {
		categories = []string{}
	}
	err := details.FromPaperDetails(serverhttp.PaperDetails{
		Type: serverhttp.PaperDetailsTypePaper, Abstract: value.Abstract, Categories: categories, Doi: value.DOI,
	})
	return details, err
}

func apiVideoDetails(details serverhttp.ItemDetails, value *domain.VideoDetails) (serverhttp.ItemDetails, error) {
	if value == nil {
		return details, fmt.Errorf("video details are missing")
	}
	err := details.FromVideoDetails(serverhttp.VideoDetails{
		Type: serverhttp.VideoDetailsTypeVideo, Description: value.Description,
		ChannelId: value.ChannelID, VideoId: value.VideoID,
	})
	return details, err
}

func apiRepositoryDetails(
	details serverhttp.ItemDetails,
	value *domain.RepositoryDetails,
) (serverhttp.ItemDetails, error) {
	if value == nil {
		return details, fmt.Errorf("repository details are missing")
	}
	topics := append([]string(nil), value.Topics...)
	if topics == nil {
		topics = []string{}
	}
	err := details.FromRepositoryDetails(serverhttp.RepositoryDetails{
		Type: serverhttp.RepositoryDetailsTypeRepository, Description: value.Description,
		Homepage: value.Homepage, ProgrammingLanguage: value.ProgrammingLanguage,
		Stars: value.Stars, Forks: value.Forks, OpenIssues: value.OpenIssues,
		Topics: topics, DefaultBranch: value.DefaultBranch,
		StarsInPeriod: value.StarsInPeriod, Rank: value.Rank,
		Period: serverhttp.RepositoryDetailsPeriod(value.Period), ObservedAt: value.ObservedAt,
		SnapshotStatus: serverhttp.RepositoryDetailsSnapshotStatus(value.SnapshotStatus),
	})
	return details, err
}

func apiWorkspaceSettings(value domain.WorkspaceSettings) serverhttp.WorkspaceSettings {
	return serverhttp.WorkspaceSettings{
		Version: value.Version, EmbeddingModelId: value.EmbeddingModelID, Generation: apiGeneration(value.Generation),
		Summary: apiSummary(value.Summary), Instructions: apiInstructions(value.Instructions),
		Retention: serverhttp.RetentionSettings{FullContentDays: value.Retention.FullContentDays},
		Retrieval: apiRetrieval(value.Retrieval), UpdatedAt: value.UpdatedAt,
	}
}

func apiPreferenceProfile(value domain.PreferenceProfile) serverhttp.PreferenceProfile {
	return serverhttp.PreferenceProfile{Prefer: value.Prefer, Avoid: value.Avoid, Revision: value.Revision, Version: value.Version, UpdatedAt: value.UpdatedAt}
}

func apiGeneration(value domain.GenerationSettings) serverhttp.GenerationSettings {
	return serverhttp.GenerationSettings{Enabled: value.Enabled, Endpoint: value.Endpoint, SummaryModel: value.SummaryModel, RerankModel: value.RerankModel}
}

func domainGeneration(value serverhttp.GenerationSettings) domain.GenerationSettings {
	return domain.GenerationSettings{Enabled: value.Enabled, Endpoint: value.Endpoint, SummaryModel: value.SummaryModel, RerankModel: value.RerankModel}
}

func apiSummary(value domain.SummarySettings) serverhttp.SummarySettings {
	return serverhttp.SummarySettings{Language: value.Language, MaxInputChars: value.MaxInputChars, MaxSummaryCharacters: value.MaxSummaryCharacters}
}

func domainSummary(value serverhttp.SummarySettings) domain.SummarySettings {
	return domain.SummarySettings{Language: value.Language, MaxInputChars: value.MaxInputChars, MaxSummaryCharacters: value.MaxSummaryCharacters}
}

func apiInstructions(value domain.TaskInstructions) serverhttp.TaskInstructions {
	return serverhttp.TaskInstructions{SummaryChunk: value.SummaryChunk, SummaryReduce: value.SummaryReduce, SummaryFinal: value.SummaryFinal, DigestRerank: value.DigestRerank}
}

func domainInstructions(value serverhttp.TaskInstructions) domain.TaskInstructions {
	return domain.TaskInstructions{SummaryChunk: value.SummaryChunk, SummaryReduce: value.SummaryReduce, SummaryFinal: value.SummaryFinal, DigestRerank: value.DigestRerank}
}

func apiRetrieval(value domain.RetrievalSettings) serverhttp.RetrievalSettings {
	return serverhttp.RetrievalSettings{
		CandidateLimit: value.CandidateLimit, RerankLimit: value.RerankLimit, RrfK: value.RRFK,
		RecencyHalfLifeDays: float32(value.RecencyHalfLifeDays), RecencyMaxBoost: float32(value.RecencyMaxBoost),
		FeedSourceCap: value.FeedSourceCap, DigestSourceCap: value.DigestSourceCap,
		DigestMmrLambda: float32(value.DigestMMRLambda), LlmRerankEnabled: value.LLMRerankEnabled,
		DigestExplorationEnabled: value.DigestExplorationEnabled,
	}
}

func domainRetrieval(value serverhttp.RetrievalSettings) domain.RetrievalSettings {
	return domain.RetrievalSettings{
		CandidateLimit: value.CandidateLimit, RerankLimit: value.RerankLimit, RRFK: value.RrfK,
		RecencyHalfLifeDays: float64(value.RecencyHalfLifeDays), RecencyMaxBoost: float64(value.RecencyMaxBoost),
		FeedSourceCap: value.FeedSourceCap, DigestSourceCap: value.DigestSourceCap,
		DigestMMRLambda: float64(value.DigestMmrLambda), LLMRerankEnabled: value.LlmRerankEnabled,
		DigestExplorationEnabled: value.DigestExplorationEnabled,
	}
}
