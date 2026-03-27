package remote

import (
	"fmt"

	"github.com/grafana/gcx/internal/resources"
)

// Folder Hierarchy Algorithm
//
// This file implements a topological sort algorithm to organize folders by their
// dependency levels, ensuring parent folders are pushed before their children.
//
// ## Algorithm Overview
//
// The algorithm works in three phases:
//
// 1. Build Dependency Graph - Parse parent-child relationships from folder annotations
// 2. Assign Levels - Use depth-first traversal to calculate depth for each folder
// 3. Group by Level - Organize folders into levels for concurrent processing
//
// ## Example Folder Hierarchy
//
// Given this folder structure (arrows indicate parent → child):
//
//	root-1 ─┬─→ child-1-1 ─→ grandchild-1-1-1
//	        └─→ child-1-2
//	root-2 ─→ child-2-1
//
// The algorithm produces these levels:
//
//	Level 0: [root-1, root-2]           # All root folders (no parent)
//	Level 1: [child-1-1, child-1-2, child-2-1]  # Children of level 0
//	Level 2: [grandchild-1-1-1]         # Children of level 1
//
// ## Phase 1: Build Dependency Graph
//
// Creates a graph of folderNode objects with parent-child links:
//
//	┌─────────────┐
//	│   root-1    │ level: 0
//	│  (no parent)│
//	└──────┬──────┘
//	       │
//	   ┌───┴────┬───────────┐
//	   │        │           │
//	   ▼        ▼           ▼
//	┌────────┐ ┌────────┐ ┌─────────┐
//	│child-1-1│ │child-1-2│ │child-2-1│ level: 1
//	│(parent: │ │(parent: │ │(parent: │
//	│ root-1) │ │ root-1) │ │ root-2) │
//	└────┬────┘ └─────────┘ └─────────┘
//	     │
//	     ▼
//	┌──────────────┐
//	│grandchild-1-1│ level: 2
//	│  (parent:    │
//	│  child-1-1)  │
//	└──────────────┘
//
// Orphaned folders (parent doesn't exist in the set) are treated as roots:
//
//	┌─────────────┐
//	│   orphan    │ level: 0
//	│ (parent:    │ ← Parent "missing" not in folder set
//	│  missing)   │ → Treated as root
//	└─────────────┘
//
// ## Phase 2: Assign Levels (Depth-First Traversal)
//
// Starting from each root folder, recursively assign depth levels:
//
//	traverse(root-1, level=0):
//	  ├─ root-1.level = 0
//	  ├─ traverse(child-1-1, level=1):
//	  │    ├─ child-1-1.level = 1
//	  │    └─ traverse(grandchild-1-1-1, level=2):
//	  │         └─ grandchild-1-1-1.level = 2
//	  └─ traverse(child-1-2, level=1):
//	       └─ child-1-2.level = 1
//
//	traverse(root-2, level=0):
//	  ├─ root-2.level = 0
//	  └─ traverse(child-2-1, level=1):
//	       └─ child-2-1.level = 1
//
// ## Phase 3: Group by Level
//
// Collect folders into arrays by their assigned level:
//
//	levels[0] = [root-1, root-2]
//	levels[1] = [child-1-1, child-1-2, child-2-1]
//	levels[2] = [grandchild-1-1-1]
//
// ## Concurrency Strategy
//
// Folders within the same level can be pushed concurrently (no dependencies):
//
//	Level 0: Push root-1 and root-2 in parallel ════════════╗
//	                                                          ║
//	Wait for level 0 to complete ════════════════════════════╝
//	                                                          ║
//	Level 1: Push all 3 children in parallel ════════════════╣
//	                                                          ║
//	Wait for level 1 to complete ════════════════════════════╝
//	                                                          ║
//	Level 2: Push grandchild ═════════════════════════════════╝
//
// This maximizes concurrency while guaranteeing that parents are always
// created before their children.
//
// ## Time Complexity
//
// - Build graph: O(n) where n = number of folders
// - Assign levels: O(n) - each node visited once
// - Group by level: O(n) - single pass through nodes
// - Total: O(n) - linear time complexity

// SortFoldersByDependency groups folders by their dependency level.
// Returns a slice where each element is a slice of folders at the same level.
// All folders at the same level can be pushed concurrently.
func SortFoldersByDependency(folders []*resources.Resource) ([][]*resources.Resource, error) {
	if len(folders) == 0 {
		return nil, nil
	}

	// Build the dependency graph
	nodes, rootUIDs := buildFolderHierarchy(folders)

	// Assign level numbers to all nodes
	assignLevels(nodes, rootUIDs)

	// Find the maximum level
	maxLevel := 0
	for _, node := range nodes {
		if node.level > maxLevel {
			maxLevel = node.level
		}
	}

	// Group folders by level
	levels := make([][]*resources.Resource, maxLevel+1)
	for _, node := range nodes {
		if node.level < 0 {
			return nil, fmt.Errorf("folder %s has invalid level (circular dependency?)", node.resource.Name())
		}
		levels[node.level] = append(levels[node.level], node.resource)
	}

	return levels, nil
}

// folderNode represents a folder in the dependency graph.
type folderNode struct {
	resource *resources.Resource
	children []*folderNode
	level    int // depth level in the hierarchy (0 = root)
}

// buildFolderHierarchy constructs a dependency graph from folder resources.
// Returns a map of folder UIDs to nodes and a list of root folder UIDs.
func buildFolderHierarchy(folders []*resources.Resource) (map[string]*folderNode, []string) {
	// Create a node for each folder
	nodes := make(map[string]*folderNode, len(folders))
	for _, folder := range folders {
		uid := folder.Name()
		nodes[uid] = &folderNode{
			resource: folder,
			children: []*folderNode{},
			level:    -1, // Will be set during level assignment
		}
	}

	// Build parent-child relationships and identify root folders
	var rootUIDs []string
	for uid, node := range nodes {
		parentUID := node.resource.GetFolder()
		if parentUID == "" {
			// This is a root folder
			rootUIDs = append(rootUIDs, uid)
		} else if parentNode, exists := nodes[parentUID]; exists {
			// Parent exists in our set, link them
			parentNode.children = append(parentNode.children, node)
		} else {
			// Parent doesn't exist in our set (orphaned folder)
			// Treat as root folder
			rootUIDs = append(rootUIDs, uid)
		}
	}

	return nodes, rootUIDs
}

// assignLevels performs a depth-first traversal to assign level numbers to folders.
func assignLevels(nodes map[string]*folderNode, rootUIDs []string) {
	var traverse func(uid string, level int)
	traverse = func(uid string, level int) {
		node := nodes[uid]
		node.level = level
		for _, child := range node.children {
			traverse(child.resource.Name(), level+1)
		}
	}

	// Start traversal from each root
	for _, rootUID := range rootUIDs {
		traverse(rootUID, 0)
	}
}
