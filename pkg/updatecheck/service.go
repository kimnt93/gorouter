// Package updatecheck retrieves public release metadata without exposing container control.
package updatecheck

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const Image = "ghcr.io/kimnt93/gorouter"
const releaseURL = "https://api.github.com/repos/kimnt93/gorouter/releases/latest"

var releaseTag = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Version is set by the release image build. Source and custom builds report unknown.
var Version = ""

type Status struct {
	Installed       string    `json:"installed"`
	Latest          string    `json:"latest"`
	UpdateAvailable bool      `json:"update_available"`
	Image           string    `json:"image"`
	ReleaseURL      string    `json:"release_url"`
	PublishedAt     string    `json:"published_at,omitempty"`
	CheckedAt       time.Time `json:"checked_at"`
}

type Service struct {
	Client *http.Client
	URL    string
}

func (s Service) Check(ctx context.Context) (Status, error) {
	current := strings.TrimSpace(Version)
	if !releaseTag.MatchString(current) {
		current = "unknown"
	}
	url := s.URL
	if url == "" {
		url = releaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Status{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gorouter-update-check")
	client := s.Client
	if client == nil {
		client = &http.Client{Timeout: 8 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return Status{}, errors.New("release check unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return Status{}, fmt.Errorf("release check returned HTTP %d", resp.StatusCode)
	}
	var release struct {
		Tag        string `json:"tag_name"`
		URL        string `json:"html_url"`
		Published  string `json:"published_at"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&release) != nil || !releaseTag.MatchString(release.Tag) || release.Draft || release.Prerelease || release.URL != "https://github.com/kimnt93/gorouter/releases/tag/"+release.Tag {
		return Status{}, errors.New("invalid release metadata")
	}
	return Status{Installed: current, Latest: release.Tag, UpdateAvailable: current != "unknown" && compare(release.Tag, current) > 0, Image: Image + ":" + release.Tag, ReleaseURL: release.URL, PublishedAt: release.Published, CheckedAt: time.Now().UTC()}, nil
}
func compare(a, b string) int {
	var x, y [3]uint64
	fmt.Sscanf(a, "v%d.%d.%d", &x[0], &x[1], &x[2])
	fmt.Sscanf(b, "v%d.%d.%d", &y[0], &y[1], &y[2])
	for i := range x {
		if x[i] > y[i] {
			return 1
		}
		if x[i] < y[i] {
			return -1
		}
	}
	return 0
}
