package miniflux

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
)

// DeleteFeed unsubscribes a feed (204). A feed that is already gone (404) is
// not an error, so a retried Delete of a Newsletter Source moves on.
func (c *Client) DeleteFeed(ctx context.Context, id int64) error {
	path := "/v1/feeds/" + strconv.FormatInt(id, 10)
	request, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	request.Header.Set("X-Auth-Token", c.apiKey)
	request.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(request)
	if err != nil {
		return fmt.Errorf("miniflux: %s %s: %w", http.MethodDelete, path, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK, http.StatusNotFound:
		return nil
	}
	return statusError(http.MethodDelete, path, resp)
}
