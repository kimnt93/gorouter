package admin

import (
	"bytes"
	"io"
	"net/http"
)

func postJSON(client *http.Client, url string, headers map[string]string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return client.Do(req)
}

func getJSON(client *http.Client, url, bearer string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", bearer)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 0))
	return resp, nil
}
