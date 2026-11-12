package search

type Resource struct {
	Group    string `json:"group"`
	Resource string `json:"resource"`
}

type Filter struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

type Query struct {
	APIVersion string     `json:"apiVersion"`
	Kind       string     `json:"kind"`
	Resources  []Resource `json:"resources,omitempty"`
	Query      string     `json:"query"`
	Filters    []Filter   `json:"filters,omitempty"`
	Limit      int        `json:"limit"`
}

type Results struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
	Items      []Item `json:"items" yaml:"items"`
}

type ResourceRef struct {
	Group    string `json:"group" yaml:"group"`
	Resource string `json:"resource" yaml:"resource"`
	Kind     string `json:"kind" yaml:"kind"`
	Name     string `json:"name" yaml:"name"`
}

type ManagedBy struct {
	Kind string `json:"kind" yaml:"kind"`
	ID   string `json:"id" yaml:"id"`
}

type Chunk struct {
	Subresource string `json:"subresource" yaml:"subresource"`
	Content     string `json:"content" yaml:"content"`
}

type Item struct {
	Resource    ResourceRef `json:"resource" yaml:"resource"`
	Score       float64     `json:"score" yaml:"score"`
	Title       string      `json:"title" yaml:"title"`
	Folder      string      `json:"folder" yaml:"folder"`
	FolderTitle string      `json:"folderTitle,omitempty" yaml:"folderTitle,omitempty"`
	ManagedBy   *ManagedBy  `json:"managedBy,omitempty" yaml:"managedBy,omitempty"`
	Chunks      []Chunk     `json:"chunks" yaml:"chunks"`
}
