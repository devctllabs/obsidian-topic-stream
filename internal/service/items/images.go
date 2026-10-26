package items

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/ethernity/obsidian-topic-stream/internal/domain"
	"github.com/google/uuid"
)

const maxImageBytes = 10 << 20

var markdownImage = regexp.MustCompile(`!\[([^\]]*)\]\(\s*(?:<([^>\r\n]+)>|([^\s)\r\n]+))(?:\s+(?:"[^"]*"|'[^']*'|\([^)]*\)))?\s*\)`)

// ImageDescriptor identifies an absolute HTTP(S) image referenced by an Item's canonical Markdown.
type ImageDescriptor struct {
	ID      uuid.UUID
	URL     string
	AltText *string
}

// ImageDownload contains validated image bytes and the response media type.
type ImageDownload struct {
	Bytes       []byte
	ContentType string
}

// ExtractImages exposes only absolute HTTP(S) images referenced by canonical Markdown. IDs are
// deterministic for an Item, image position, and URL so callers cannot turn DownloadImage into an
// arbitrary URL proxy.
func ExtractImages(item domain.Item) []ImageDescriptor {
	matches := markdownImage.FindAllStringSubmatch(item.ContentMarkdown, -1)
	result := make([]ImageDescriptor, 0, len(matches))
	for _, match := range matches {
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if !safeImageURL(value) {
			continue
		}
		var altText *string
		if alt := strings.TrimSpace(match[1]); alt != "" {
			altText = &alt
		}
		result = append(result, ImageDescriptor{
			ID:  uuid.NewSHA1(item.ID, []byte(fmt.Sprintf("%d:%s", len(result), value))),
			URL: value, AltText: altText,
		})
	}
	return result
}

func safeImageURL(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.IsAbs() && parsed.Hostname() != "" && parsed.User == nil &&
		(parsed.Scheme == "http" || parsed.Scheme == "https")
}

// DownloadImage fetches only an image identified in canonical Markdown and validates type and size.
func (s Service) DownloadImage(ctx context.Context, itemID, imageID uuid.UUID) (ImageDownload, error) {
	item, err := s.Get(ctx, itemID)
	if err != nil {
		return ImageDownload{}, err
	}
	descriptor, found := findImage(ExtractImages(item), imageID)
	if !found {
		return ImageDownload{}, ErrImageNotFound
	}
	return s.fetchImage(ctx, descriptor.URL)
}

func (s Service) fetchImage(ctx context.Context, imageURL string) (download ImageDownload, resultErr error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, imageURL, nil)
	if err != nil {
		return ImageDownload{}, fmt.Errorf("create item image request: %w", err)
	}
	response, err := s.imageClient.Do(request)
	if err != nil {
		return ImageDownload{}, fmt.Errorf("download item image: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, response.Body.Close()) }()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
		return ImageDownload{}, fmt.Errorf("download item image: upstream status %d", response.StatusCode)
	}
	contentType, err := imageContentType(response.Header.Get("Content-Type"))
	if err != nil {
		return ImageDownload{}, err
	}
	bytes, err := io.ReadAll(io.LimitReader(response.Body, maxImageBytes+1))
	if err != nil {
		return ImageDownload{}, fmt.Errorf("read item image: %w", err)
	}
	if len(bytes) > maxImageBytes {
		return ImageDownload{}, fmt.Errorf("%w: image exceeds 10 MB", ErrInvalidImage)
	}
	if contentType == "" {
		contentType = http.DetectContentType(bytes)
		if !strings.HasPrefix(contentType, "image/") {
			return ImageDownload{}, fmt.Errorf("%w: payload is not an image", ErrInvalidImage)
		}
	}
	return ImageDownload{Bytes: bytes, ContentType: contentType}, nil
}

func findImage(images []ImageDescriptor, imageID uuid.UUID) (ImageDescriptor, bool) {
	for _, image := range images {
		if image.ID == imageID {
			return image, true
		}
	}
	return ImageDescriptor{}, false
}

func imageContentType(header string) (string, error) {
	if strings.TrimSpace(header) == "" {
		return "", nil
	}
	contentType, _, err := mime.ParseMediaType(header)
	if err != nil || !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("%w: unsupported content type", ErrInvalidImage)
	}
	if contentType == "image/svg+xml" {
		return contentType, nil
	}
	return contentType, nil
}
