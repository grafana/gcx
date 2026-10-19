package k6

import (
	"context"
	"fmt"
)

// updateLoadTest updates a load test's name and script. It rejects a change of
// project before writing anything, because k6 Cloud's update ignores
// project_id: moving a test between projects is a separate API.
func updateLoadTest(ctx context.Context, client API, id int, lt LoadTest) error {
	if lt.ProjectID != 0 {
		current, err := client.GetLoadTest(ctx, id)
		if err != nil {
			return err
		}
		if current.ProjectID != lt.ProjectID {
			return fmt.Errorf("k6: project_id can't be changed by an update: load test %d belongs to project %d, not %d",
				id, current.ProjectID, lt.ProjectID)
		}
	}
	return client.UpdateLoadTest(ctx, id, lt.Name, lt.Script)
}
