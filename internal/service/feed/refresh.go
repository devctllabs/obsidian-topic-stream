package feed

import (
	"context"
	"fmt"
	"time"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
)

// Refresh admits one bounded batch and returns saved history without waiting for candidate preparation.
func (s Service) Refresh(ctx context.Context, command domain.FeedRefreshCommand) (domain.FeedPage, error) {
	if s.admission == nil || s.settings == nil || s.history == nil || s.topics == nil {
		return domain.FeedPage{}, domain.ErrNotImplemented
	}
	query, err := s.readSelection(ctx, domain.FeedReadQuery{TopicID: command.TopicID, Limit: command.Limit})
	if err != nil {
		return domain.FeedPage{}, err
	}
	settings, err := s.refreshSettings(ctx)
	if err != nil {
		return domain.FeedPage{}, err
	}
	if err := s.admitSelection(ctx, query, settings); err != nil {
		return domain.FeedPage{}, err
	}
	return s.Read(ctx, query)
}

func (s Service) refreshChronological(ctx context.Context, settings domain.WorkspaceSettings, at time.Time) error {
	items, err := s.admission.RecentUnpublishedFeed(ctx, domain.FeedCandidateQuery{WorkspaceID: s.workspaceID, At: at, CandidateLimit: settings.Retrieval.CandidateLimit})
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	generation, _, err := s.admission.GetActive(ctx, s.workspaceID)
	if err != nil {
		return err
	}
	publications := make([]domain.FeedPublication, 0, len(items))
	for _, item := range items {
		publications = append(publications, domain.FeedPublication{ItemID: item.ID, ModelID: generation.ModelID, Score: 1, Pipeline: "shared-feed-chronological-v1", Signals: map[string]float64{"selection.chronological": 1, "settings.version": float64(settings.Version)}})
	}
	return s.publish(ctx, domain.FeedPublishCommand{WorkspaceID: s.workspaceID, PublishedAt: s.now().UTC(), Publications: publications})
}

func (s Service) publish(ctx context.Context, command domain.FeedPublishCommand) error {
	if len(command.Publications) == 0 {
		return nil
	}
	result, err := s.admission.PublishFeed(ctx, command)
	if err != nil {
		return err
	}
	if len(result.Publications) > 0 && s.events != nil {
		s.events.InvalidateFeed(s.workspaceID, result.Publications[0].ItemID)
	}
	return nil
}

func (s Service) refreshSettings(ctx context.Context) (domain.WorkspaceSettings, error) {
	settings, err := s.settings.GetWorkspace(ctx)
	if err != nil {
		return domain.WorkspaceSettings{}, err
	}
	if settings.Retrieval.CandidateLimit < 1 || settings.Retrieval.CandidateLimit > 100 {
		return domain.WorkspaceSettings{}, fmt.Errorf("invalid Feed candidate limit")
	}
	return settings, nil
}
func (s Service) admitSelection(ctx context.Context, query domain.FeedReadQuery, settings domain.WorkspaceSettings) error {
	enabled := query.TopicID != nil
	if !enabled {
		var err error
		enabled, err = s.admission.HasEnabledFeedTopics(ctx, s.workspaceID)
		if err != nil {
			return err
		}
	}
	if !enabled {
		return s.refreshChronological(ctx, settings, s.now().UTC())
	}
	return s.refreshReady(ctx, query, settings)
}

func (s Service) refreshReady(ctx context.Context, query domain.FeedReadQuery, settings domain.WorkspaceSettings) error {
	at := s.now().UTC()
	rows, err := s.admission.ReadFeedCandidates(ctx, domain.FeedCandidateQuery{WorkspaceID: s.workspaceID, At: at, CandidateLimit: settings.Retrieval.CandidateLimit, GateVersion: s.gateVersion})
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	generation, found, err := s.admission.GetActive(ctx, s.workspaceID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	compatible := make([]domain.FeedCandidate, 0, len(rows))
	for _, row := range rows {
		if row.Inputs.ModelID == generation.ModelID {
			compatible = append(compatible, row)
		}
	}
	selection := selectReady(compatible, query, settings.Retrieval, s.selectionStart)
	return s.admitReadySelection(ctx, selection, domain.FeedRankingRequest{WorkspaceID: s.workspaceID, Generation: generation, Settings: settings, Candidates: selection.candidates, At: at}, query)
}
func (s Service) admitReadySelection(ctx context.Context, selection readySelection, request domain.FeedRankingRequest, query domain.FeedReadQuery) error {
	if len(request.Candidates) == 0 {
		return nil
	}
	if s.ranker == nil {
		return domain.ErrNotImplemented
	}
	ranked, err := s.ranker.RankFeed(ctx, request)
	if err != nil {
		return err
	}
	return s.publish(ctx, domain.FeedPublishCommand{WorkspaceID: s.workspaceID, TopicID: query.TopicID, PublishedAt: s.now().UTC(), Publications: selection.publications(ranked, request.Settings.Version, request.Generation.ModelID)})
}
