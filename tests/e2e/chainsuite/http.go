package chainsuite

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/tidwall/gjson"
)

// Get fetches url and returns its body and status.
func Get(ctx context.Context, url string) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

// GetJSON fetches url and requires a 200 with a JSON body.
func GetJSON(ctx context.Context, url string) (gjson.Result, error) {
	body, status, err := Get(ctx, url)
	if err != nil {
		return gjson.Result{}, err
	}
	if status != http.StatusOK {
		return gjson.Result{}, fmt.Errorf("GET %s: status %d: %s", url, status, body)
	}
	if !gjson.ValidBytes(body) {
		return gjson.Result{}, fmt.Errorf("GET %s: body is not JSON: %s", url, body)
	}
	return gjson.ParseBytes(body), nil
}
