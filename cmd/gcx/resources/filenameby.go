package resources

import "fmt"

// FilenameByMode selects the source of filenames for pulled resources.
type FilenameByMode string

const (
	// FilenameByName preserves metadata.name as the filename.
	FilenameByName FilenameByMode = "name"
	// FilenameByTitle uses spec.title, falling back to metadata.name.
	FilenameByTitle FilenameByMode = "title"
)

// Validate checks the supported filename sources.
func (m FilenameByMode) Validate() error {
	switch m {
	case FilenameByName, FilenameByTitle:
		return nil
	default:
		return fmt.Errorf("invalid --filename-by %q: must be 'name' or 'title'", m)
	}
}
