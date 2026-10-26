package httpapi

import (
	"fmt"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

func apiWorkspaceArchive(value domain.WorkspaceArchive) (serverhttp.ExportBundle, error) {
	sources := make([]serverhttp.Source, len(value.Data.Sources))
	for index, source := range value.Data.Sources {
		mapped, err := apiSource(source)
		if err != nil {
			return serverhttp.ExportBundle{}, fmt.Errorf("map archive source: %w", err)
		}
		sources[index] = mapped
	}
	digests, err := apiArchiveDigests(value.Data.Digests)
	if err != nil {
		return serverhttp.ExportBundle{}, err
	}
	return serverhttp.ExportBundle{
		FormatVersion: serverhttp.ExportBundleFormatVersion(value.FormatVersion),
		ExportedAt:    value.ExportedAt, WorkspaceId: value.Data.WorkspaceID,
		Settings: apiWorkspaceSettings(value.Data.Settings), EmbeddingModel: apiEmbeddingModelDescriptor(value.Data.EmbeddingModel), PreferenceProfile: apiPreferenceProfile(value.Data.Profile),
		Sources: sources, Topics: apiArchiveTopics(value.Data.Topics),
		Feedback: apiPortableFeedback(value.Data.Feedback), Digests: digests,
	}, nil
}

func apiArchiveTopics(values []domain.Topic) []serverhttp.Topic {
	result := make([]serverhttp.Topic, len(values))
	for index, value := range values {
		result[index] = apiTopic(value)
	}
	return result
}

func apiArchiveDigests(values []domain.DigestDefinition) ([]serverhttp.DigestDefinition, error) {
	result := make([]serverhttp.DigestDefinition, len(values))
	for index, value := range values {
		mapped, err := apiDigestDefinition(value)
		if err != nil {
			return nil, fmt.Errorf("map archive digest: %w", err)
		}
		result[index] = mapped
	}
	return result, nil
}

func apiPortableFeedback(values []domain.PortableFeedback) []serverhttp.PortableFeedback {
	result := make([]serverhttp.PortableFeedback, len(values))
	for index, value := range values {
		result[index] = serverhttp.PortableFeedback{
			CanonicalUrl: value.CanonicalURL, Feedback: apiFeedback(value.Feedback),
		}
	}
	return result
}

func domainWorkspaceArchive(value serverhttp.ExportBundle) (domain.WorkspaceArchive, error) {
	sources, err := domainArchiveSources(value.Sources, value.WorkspaceId)
	if err != nil {
		return domain.WorkspaceArchive{}, err
	}
	digests, err := domainArchiveDigests(value.Digests, value.WorkspaceId)
	if err != nil {
		return domain.WorkspaceArchive{}, err
	}
	return domain.WorkspaceArchive{
		FormatVersion: string(value.FormatVersion), ExportedAt: value.ExportedAt,
		Data: domain.WorkspaceArchiveData{
			WorkspaceID: value.WorkspaceId, Settings: domainWorkspaceSettings(value.Settings),
			EmbeddingModel: domainEmbeddingModelDescriptor(value.EmbeddingModel),
			Profile:        domainPreferenceProfile(value.PreferenceProfile), Sources: sources,
			Topics:   domainArchiveTopics(value.Topics, value.WorkspaceId),
			Feedback: domainPortableFeedback(value.Feedback), Digests: digests,
		},
	}, nil
}

func apiEmbeddingModelDescriptor(value domain.EmbeddingModelDescriptor) serverhttp.EmbeddingModelDescriptor {
	return serverhttp.EmbeddingModelDescriptor{
		Name: value.Name, Endpoint: value.Endpoint, ProviderModel: value.ProviderModel, Dimensions: value.Dimensions,
	}
}

func domainEmbeddingModelDescriptor(value serverhttp.EmbeddingModelDescriptor) domain.EmbeddingModelDescriptor {
	return domain.EmbeddingModelDescriptor{
		Name: value.Name, Endpoint: value.Endpoint, ProviderModel: value.ProviderModel, Dimensions: value.Dimensions,
	}
}

func domainArchiveSources(values []serverhttp.Source, workspaceID serverhttp.UUID) ([]domain.Source, error) {
	result := make([]domain.Source, len(values))
	for index, value := range values {
		config, err := domainSourceConfig(value.Config)
		if err != nil {
			return nil, fmt.Errorf("map archive source: %w", err)
		}
		result[index] = domain.Source{
			ID: value.Id, WorkspaceID: workspaceID, Name: value.Name, Config: config,
			Enabled: value.Enabled, PollIntervalMinutes: value.PollIntervalMinutes,
			LastSyncedAt: value.LastSyncedAt, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		}
	}
	return result, nil
}

func domainArchiveTopics(values []serverhttp.Topic, workspaceID serverhttp.UUID) []domain.Topic {
	result := make([]domain.Topic, len(values))
	for index, value := range values {
		result[index] = domain.Topic{
			ID: value.Id, WorkspaceID: workspaceID, Name: value.Name,
			SourceIDs: append([]serverhttp.UUID(nil), value.SourceIds...), ItemKinds: domainItemKinds(value.ItemKinds),
			Include: value.Include, Exclude: value.Exclude, Mode: domain.TopicMode(value.Mode),
			Enabled: value.Enabled, Revision: value.Revision, CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		}
	}
	return result
}

func domainArchiveDigests(values []serverhttp.DigestDefinition, workspaceID serverhttp.UUID) ([]domain.DigestDefinition, error) {
	result := make([]domain.DigestDefinition, len(values))
	for index, value := range values {
		recurrence, err := domainRecurrence(value.Recurrence)
		if err != nil {
			return nil, fmt.Errorf("map archive digest: %w", err)
		}
		result[index] = domain.DigestDefinition{
			ID: value.Id, WorkspaceID: workspaceID, Name: value.Name, Topics: domainDigestTopics(value.Topics),
			Timezone: value.Timezone, Recurrence: recurrence, Enabled: value.Enabled, Version: value.Version,
			NextRunAt: value.NextRunAt, LastSuccessfulAt: value.LastSuccessfulAt,
			CreatedAt: value.CreatedAt, UpdatedAt: value.UpdatedAt,
		}
	}
	return result, nil
}

func domainWorkspaceSettings(value serverhttp.WorkspaceSettings) domain.WorkspaceSettings {
	return domain.WorkspaceSettings{
		Version: value.Version, EmbeddingModelID: value.EmbeddingModelId, Generation: domainGeneration(value.Generation),
		Summary: domainSummary(value.Summary), Instructions: domainInstructions(value.Instructions),
		Retention: domain.RetentionSettings{FullContentDays: value.Retention.FullContentDays},
		Retrieval: domainRetrieval(value.Retrieval), UpdatedAt: value.UpdatedAt,
	}
}

func domainPreferenceProfile(value serverhttp.PreferenceProfile) domain.PreferenceProfile {
	return domain.PreferenceProfile{
		Prefer: value.Prefer, Avoid: value.Avoid, Revision: value.Revision,
		Version: value.Version, UpdatedAt: value.UpdatedAt,
	}
}

func domainPortableFeedback(values []serverhttp.PortableFeedback) []domain.PortableFeedback {
	result := make([]domain.PortableFeedback, len(values))
	for index, value := range values {
		result[index] = domain.PortableFeedback{
			CanonicalURL: value.CanonicalUrl,
			Feedback: domain.Feedback{
				Sentiment: string(value.Feedback.Sentiment), Comment: value.Feedback.Comment,
				CreatedAt: value.Feedback.CreatedAt, UpdatedAt: value.Feedback.UpdatedAt,
			},
		}
	}
	return result
}
