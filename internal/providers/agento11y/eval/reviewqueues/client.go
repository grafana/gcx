// Package reviewqueues manages human review queues: collections whose
// conversations are scored by reviewers against a rubric of human evaluators.
package reviewqueues

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"

	"github.com/grafana/gcx/internal/providers/agento11y/agento11yhttp"
)

const basePath = "/eval/review-queues"

// truncatedHeader is set by the backend when an export stopped at its row cap.
const truncatedHeader = "X-Review-Export-Truncated"

// ErrNotFound is returned when the server answers 404 for a queue. The backend
// also answers 404 for every review-queue route when human review is not
// enabled for the tenant.
var ErrNotFound = errors.New("review queue not found")

// Client is an HTTP client for the review queue endpoints.
type Client struct {
	base *agento11yhttp.Client
}

// NewClient creates a new review queue client.
func NewClient(base *agento11yhttp.Client) *Client {
	return &Client{base: base}
}

func queuePath(collectionID string) string {
	return basePath + "/" + url.PathEscape(collectionID)
}

// List returns every review queue with its progress and without items.
func (c *Client) List(ctx context.Context) ([]ReviewQueue, error) {
	return agento11yhttp.ListAll[ReviewQueue](ctx, c.base, basePath, nil)
}

// Get returns one queue with its rubric, progress and items.
func (c *Client) Get(ctx context.Context, collectionID string) (*ReviewQueue, error) {
	queue, err := agento11yhttp.DoJSONNotFound[any, ReviewQueue](ctx, c.base, http.MethodGet, queuePath(collectionID), nil,
		fmt.Errorf("%s: %w", collectionID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &queue, nil
}

// GetConversation returns the queue as seen from one conversation.
func (c *Client) GetConversation(ctx context.Context, collectionID, conversationID string) (*ConversationView, error) {
	view, err := agento11yhttp.DoJSONNotFound[any, ConversationView](ctx, c.base, http.MethodGet,
		queuePath(collectionID)+"/conversations/"+url.PathEscape(conversationID), nil,
		fmt.Errorf("%s/%s: %w", collectionID, conversationID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &view, nil
}

// SetRubric replaces the queue's rubric.
func (c *Client) SetRubric(ctx context.Context, collectionID string, request *RubricRequest) (*ReviewQueue, error) {
	queue, err := agento11yhttp.DoJSONNotFound[RubricRequest, ReviewQueue](ctx, c.base, http.MethodPut, queuePath(collectionID), request,
		fmt.Errorf("%s: %w", collectionID, ErrNotFound), http.StatusOK)
	if err != nil {
		return nil, err
	}
	return &queue, nil
}

// SubmitReview stores one reviewer's answers for one conversation, all or none.
func (c *Client) SubmitReview(ctx context.Context, collectionID string, request *ReviewRequest) (*ReviewResult, error) {
	result, err := agento11yhttp.DoJSONNotFound[ReviewRequest, ReviewResult](ctx, c.base, http.MethodPost, queuePath(collectionID)+"/reviews", request,
		fmt.Errorf("%s: %w", collectionID, ErrNotFound), http.StatusOK, http.StatusCreated)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// ExportScores downloads the queue's answers (the scores.csv export) and
// decodes them by header name, so columns the backend adds later are ignored
// rather than misread. truncated reports that the backend stopped at its row
// cap.
func (c *Client) ExportScores(ctx context.Context, collectionID string) ([]ScoreRow, bool, error) {
	resp, err := c.base.DoRequest(ctx, http.MethodGet, queuePath(collectionID)+"/scores.csv", nil)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, false, fmt.Errorf("%s: %w", collectionID, ErrNotFound)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, agento11yhttp.HandleErrorResponse(resp)
	}

	rows, err := decodeScoreRows(resp.Body)
	if err != nil {
		return nil, false, err
	}
	return rows, resp.Header.Get(truncatedHeader) == "true", nil
}

func decodeScoreRows(r io.Reader) ([]ScoreRow, error) {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to read export header: %w", err)
	}
	if !slices.Contains(header, "conversation_id") {
		return nil, errors.New("unexpected export format: no conversation_id column")
	}

	rows := []ScoreRow{}
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read export row: %w", err)
		}

		var row ScoreRow
		for i, name := range header {
			if i >= len(record) {
				break
			}
			switch name {
			case "conversation_id":
				row.ConversationID = record[i]
			case "generation_id":
				row.GenerationID = record[i]
			case "agent_name":
				row.AgentName = record[i]
			case "agent_version":
				row.AgentVersion = record[i]
			case "evaluator_id":
				row.EvaluatorID = record[i]
			case "evaluator_version":
				row.EvaluatorVersion = record[i]
			case "score_key":
				row.ScoreKey = record[i]
			case "value":
				row.Value = record[i]
			case "passed":
				row.Passed = record[i]
			case "reviewer":
				row.Reviewer = record[i]
			case "note":
				row.Note = record[i]
			case "created_at":
				row.CreatedAt = record[i]
			}
		}
		rows = append(rows, row)
	}
}
