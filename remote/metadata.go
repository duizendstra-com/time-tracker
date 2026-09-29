package remote

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"
)

// ProjectID asks Cloud Run's metadata server for the project id; "" off Cloud Run
// or on any failure.
func ProjectID(ctx context.Context) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://metadata.google.internal/computeMetadata/v1/project/project-id", nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Metadata-Flavor", "Google")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(res.Body, 256))
	if res.StatusCode != http.StatusOK {
		return ""
	}
	return strings.TrimSpace(string(body))
}
