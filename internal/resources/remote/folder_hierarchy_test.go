package remote_test

import (
	"testing"

	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/remote"
	"github.com/stretchr/testify/require"
)

func TestSortFoldersByDependency(t *testing.T) {
	tests := []struct {
		name           string
		folders        []*resources.Resource
		expectError    bool
		expectedLevels int
		validateLevels func(t *testing.T, levels [][]*resources.Resource)
	}{
		{
			name:           "empty list",
			folders:        nil,
			expectError:    false,
			expectedLevels: 0,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				require.Nil(t, levels)
			},
		},
		{
			name: "single folder",
			folders: []*resources.Resource{
				createFolderWithParent("root", ""),
			},
			expectError:    false,
			expectedLevels: 1,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				require.Len(t, levels[0], 1)
				require.Equal(t, "root", levels[0][0].Name())
			},
		},
		{
			name: "flat hierarchy - all folders at root level",
			folders: []*resources.Resource{
				createFolderWithParent("folder-1", ""),
				createFolderWithParent("folder-2", ""),
				createFolderWithParent("folder-3", ""),
			},
			expectError:    false,
			expectedLevels: 1,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				require.Len(t, levels[0], 3)
			},
		},
		{
			name: "two level hierarchy - root -> child-1, child-2",
			folders: []*resources.Resource{
				createFolderWithParent("root", ""),
				createFolderWithParent("child-1", "root"),
				createFolderWithParent("child-2", "root"),
			},
			expectError:    false,
			expectedLevels: 2,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				// Level 0: root folder
				require.Len(t, levels[0], 1)
				require.Equal(t, "root", levels[0][0].Name())

				// Level 1: child folders
				require.Len(t, levels[1], 2)
				childNames := []string{levels[1][0].Name(), levels[1][1].Name()}
				require.Contains(t, childNames, "child-1")
				require.Contains(t, childNames, "child-2")
			},
		},
		{
			name: "three level hierarchy - root -> child -> grandchild",
			folders: []*resources.Resource{
				createFolderWithParent("root", ""),
				createFolderWithParent("child", "root"),
				createFolderWithParent("grandchild", "child"),
			},
			expectError:    false,
			expectedLevels: 3,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				// Level 0: root
				require.Len(t, levels[0], 1)
				require.Equal(t, "root", levels[0][0].Name())

				// Level 1: child
				require.Len(t, levels[1], 1)
				require.Equal(t, "child", levels[1][0].Name())

				// Level 2: grandchild
				require.Len(t, levels[2], 1)
				require.Equal(t, "grandchild", levels[2][0].Name())
			},
		},
		{
			name: "multiple independent trees",
			folders: []*resources.Resource{
				createFolderWithParent("tree-a-root", ""),
				createFolderWithParent("tree-a-child", "tree-a-root"),
				createFolderWithParent("tree-b-root", ""),
				createFolderWithParent("tree-b-child", "tree-b-root"),
			},
			expectError:    false,
			expectedLevels: 2,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				// Level 0: both root folders
				require.Len(t, levels[0], 2)
				rootNames := []string{levels[0][0].Name(), levels[0][1].Name()}
				require.Contains(t, rootNames, "tree-a-root")
				require.Contains(t, rootNames, "tree-b-root")

				// Level 1: both child folders
				require.Len(t, levels[1], 2)
				childNames := []string{levels[1][0].Name(), levels[1][1].Name()}
				require.Contains(t, childNames, "tree-a-child")
				require.Contains(t, childNames, "tree-b-child")
			},
		},
		{
			name: "orphaned folder - references non-existent parent",
			folders: []*resources.Resource{
				createFolderWithParent("orphan", "non-existent-parent"),
			},
			expectError:    false,
			expectedLevels: 1,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				// Orphaned folder should be treated as root
				require.Len(t, levels[0], 1)
				require.Equal(t, "orphan", levels[0][0].Name())
			},
		},
		{
			name: "complex hierarchy with multiple roots and branches",
			folders: []*resources.Resource{
				// root-1
				//   ├─ child-1-1
				//   │   └─ grandchild-1-1-1
				//   └─ child-1-2
				// root-2
				//   └─ child-2-1
				createFolderWithParent("root-1", ""),
				createFolderWithParent("child-1-1", "root-1"),
				createFolderWithParent("child-1-2", "root-1"),
				createFolderWithParent("grandchild-1-1-1", "child-1-1"),
				createFolderWithParent("root-2", ""),
				createFolderWithParent("child-2-1", "root-2"),
			},
			expectError:    false,
			expectedLevels: 3,
			validateLevels: func(t *testing.T, levels [][]*resources.Resource) {
				t.Helper()
				// Level 0: root folders
				require.Len(t, levels[0], 2)
				rootNames := []string{levels[0][0].Name(), levels[0][1].Name()}
				require.Contains(t, rootNames, "root-1")
				require.Contains(t, rootNames, "root-2")

				// Level 1: child folders
				require.Len(t, levels[1], 3)
				childNames := []string{levels[1][0].Name(), levels[1][1].Name(), levels[1][2].Name()}
				require.Contains(t, childNames, "child-1-1")
				require.Contains(t, childNames, "child-1-2")
				require.Contains(t, childNames, "child-2-1")

				// Level 2: grandchild
				require.Len(t, levels[2], 1)
				require.Equal(t, "grandchild-1-1-1", levels[2][0].Name())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			levels, err := remote.SortFoldersByDependency(tt.folders)

			if tt.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Len(t, levels, tt.expectedLevels)

			if tt.validateLevels != nil {
				tt.validateLevels(t, levels)
			}
		})
	}
}
