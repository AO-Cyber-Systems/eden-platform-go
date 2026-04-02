package aosentry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/aocybersystems/eden-platform-go/platform/skills"
)

// CatalogClient fetches and caches the skill catalog from AOSentry.
type CatalogClient struct {
	client  *Client
	mu      sync.RWMutex
	skills  []skills.Skill
	lastFetch time.Time
	ttl     time.Duration
}

// NewCatalogClient creates a catalog client with the given cache TTL.
func NewCatalogClient(client *Client, ttl time.Duration) *CatalogClient {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &CatalogClient{client: client, ttl: ttl}
}

// Skills returns the cached skill catalog, refreshing if stale.
func (c *CatalogClient) Skills(ctx context.Context) []skills.Skill {
	c.mu.RLock()
	if time.Since(c.lastFetch) < c.ttl && c.skills != nil {
		defer c.mu.RUnlock()
		return c.skills
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(c.lastFetch) < c.ttl && c.skills != nil {
		return c.skills
	}

	fetched, err := c.fetchSkills(ctx)
	if err != nil {
		slog.Warn("aosentry: catalog fetch failed, using cached", "error", err)
		return c.skills // Return stale cache on error
	}

	c.skills = fetched
	c.lastFetch = time.Now()
	return c.skills
}

// FindByTaskType returns skills matching a task type.
func (c *CatalogClient) FindByTaskType(ctx context.Context, taskType string) []skills.Skill {
	all := c.Skills(ctx)
	var matched []skills.Skill
	for _, s := range all {
		for _, t := range s.TaskTypes {
			if t == taskType {
				matched = append(matched, s)
				break
			}
		}
	}
	return matched
}

func (c *CatalogClient) fetchSkills(ctx context.Context) ([]skills.Skill, error) {
	resp, err := c.client.do(ctx, http.MethodGet, "/v1/skills", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, MapHTTPError(resp.StatusCode, body)
	}

	var result struct {
		Skills []skills.Skill `json:"skills"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("aosentry: decode skills catalog: %w", err)
	}
	return result.Skills, nil
}
