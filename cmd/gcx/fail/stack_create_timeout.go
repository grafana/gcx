package fail

import (
	"errors"
	"fmt"

	"github.com/grafana/gcx/internal/cloud"
	"github.com/grafana/gcx/internal/gcxerrors"
)

func convertStackCreationTimeout(err error) (*gcxerrors.DetailedError, bool) {
	var timeout *cloud.StackCreationTimeoutError
	if !errors.As(err, &timeout) {
		return nil, false
	}
	return &gcxerrors.DetailedError{
		Summary:  "Network error",
		Details:  fmt.Sprintf("Timed out waiting for Cloud to finish creating stack %q. The stack may already exist or still be provisioning.", timeout.Slug),
		Parent:   err,
		ExitCode: new(gcxerrors.ExitGeneralError),
		Suggestions: []string{
			"Check the stack using the same config/context: gcx cloud stacks get " + timeout.Slug,
			"Check the result before retrying creation; a timeout does not confirm that creation failed",
		},
	}, true
}
