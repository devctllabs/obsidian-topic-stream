package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethernity/obsidian-topic-stream/gen/serverhttp"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/require"
)

func TestStrictJSONSerializerRejectsInvalidBodies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		body string
	}{
		{"empty", ""},
		{"malformed", "{"},
		{"unknown field", `{"unknown":true}`},
		{"second object", `{} {}`},
		{"second null", `{} null`},
		{"trailing data", `{} trailing`},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(test.body))
			ctx := echo.New().NewContext(request, httptest.NewRecorder())
			var input serverhttp.FeedRefreshInput

			err := (strictJSONSerializer{}).Deserialize(ctx, &input)

			var failure *bodyDecodeError
			require.ErrorAs(t, err, &failure)
			require.Equal(t, http.StatusBadRequest, failure.status)
			require.Equal(t, problemInvalidFeedRequest, failure.kind)
			require.NotNil(t, failure.Unwrap())
		})
	}
}

func TestStrictJSONSerializerAcceptsTrailingWhitespace(t *testing.T) {
	t.Parallel()
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{\"limit\":3}\n\t "))
	ctx := echo.New().NewContext(request, httptest.NewRecorder())
	var input serverhttp.FeedRefreshInput

	require.NoError(t, (strictJSONSerializer{}).Deserialize(ctx, &input))
	require.NotNil(t, input.Limit)
	require.Equal(t, 3, *input.Limit)
}
