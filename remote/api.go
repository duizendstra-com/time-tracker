// Package remote holds everything that reaches the network: Calendar, Sheets and
// Drive with the user's token, and Gemini with the service's key. A token or key
// travels in a header only, and no error carries a URL.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// APIError is a Google API's refusal: its status and its own short message.
type APIError struct {
	Status  int
	Reason  string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return fmt.Sprintf("status %d", e.Status)
}

// IsNotFound says whether err is a 404 or a 410 (gone).
func IsNotFound(err error) bool {
	var a *APIError
	return errors.As(err, &a) && (a.Status == http.StatusNotFound || a.Status == http.StatusGone)
}

// IsInsufficientScope says whether err is a refusal for a scope the grant lacks.
func IsInsufficientScope(err error) bool {
	var a *APIError
	return errors.As(err, &a) && a.Status == http.StatusForbidden &&
		(a.Reason == "insufficientPermissions" || a.Reason == "ACCESS_TOKEN_SCOPE_INSUFFICIENT")
}

// client calls one Google API with one set of headers.
type client struct {
	http   *http.Client
	header http.Header
}

func newClient(h *http.Client, header http.Header) client {
	if h == nil {
		h = &http.Client{Timeout: 20 * time.Second}
	}
	return client{http: h, header: header}
}

func bearer(token string) http.Header {
	return http.Header{"Authorization": {"Bearer " + token}}
}

// do sends body (if any) as JSON and decodes the answer into out (if any). Its
// errors name the method and the API's own message, never the URL.
func (c client) do(ctx context.Context, method, url string, body, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return errors.New("bad request")
	}
	for k, v := range c.header {
		req.Header[k] = v
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http.Do(req)
	if err != nil {
		// A *url.Error carries the URL; keep only what went wrong.
		if ctx.Err() != nil {
			return fmt.Errorf("%s: %w", method, ctx.Err())
		}
		return fmt.Errorf("%s: the request did not complete", method)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		return apiError(res.StatusCode, data)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("%s: unreadable answer", method)
	}
	return nil
}

// apiError reads Google's error body: {"error":{"message","status","errors":[{"reason"}],
// "details":[{"reason"}]}}.
func apiError(status int, data []byte) error {
	var body struct {
		Error struct {
			Message string `json:"message"`
			Status  string `json:"status"`
			Errors  []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
			Details []struct {
				Reason string `json:"reason"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(data, &body)
	e := &APIError{Status: status, Message: body.Error.Message}
	switch {
	case len(body.Error.Errors) > 0:
		e.Reason = body.Error.Errors[0].Reason
	case len(body.Error.Details) > 0:
		e.Reason = body.Error.Details[0].Reason
	default:
		e.Reason = body.Error.Status
	}
	return e
}
