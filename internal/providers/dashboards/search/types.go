package search

// wireSearchHit is the JSON representation of a single hit from the Grafana
// dashboard search API (GET /apis/dashboard.grafana.app/v0alpha1/.../search).
// The resource field distinguishes dashboards ("dashboards") from folders
// ("folders"). Note: the server silently ignores any type= parameter, so
// client-side filtering on this field is required.
type wireSearchHit struct {
	Resource string   `json:"resource"` // "dashboards" or "folders"
	Name     string   `json:"name"`     // dashboard UID (metadata.name)
	Title    string   `json:"title"`
	Folder   string   `json:"folder"` // folder UID, empty for root
	Tags     []string `json:"tags"`
}

// wireSearchResponse is the top-level JSON body returned by the search API.
// Wire format: {"hits":[...],"maxScore":...,"queryCost":...,"totalHits":...}.
type wireSearchResponse struct {
	Hits      []wireSearchHit `json:"hits"`
	MaxScore  float64         `json:"maxScore"`
	QueryCost int64           `json:"queryCost"`
	TotalHits int64           `json:"totalHits"`
}

// SearchParams holds the parameters for a dashboard search request.
type SearchParams struct {
	Query   string
	Folders []string
	Tags    []string
	Limit   int
	Sort    string
	Deleted bool
}

// DashboardHitSpec holds the spec fields of a search result item.
type DashboardHitSpec struct {
	Title  string   `json:"title"`
	Folder string   `json:"folder"`
	Tags   []string `json:"tags"`
}

// DashboardHitMeta holds the metadata of a K8s-style DashboardHit item.
type DashboardHitMeta struct {
	Name string `json:"name"`
}

// DashboardHit is a single search result item wrapped in the K8s-style envelope.
type DashboardHit struct {
	Kind       string           `json:"kind"`
	APIVersion string           `json:"apiVersion"`
	Metadata   DashboardHitMeta `json:"metadata"`
	Spec       DashboardHitSpec `json:"spec"`
}

// DashboardSearchResultList is the K8s-style list envelope returned by
// `gcx dashboards search`. It carries kind, apiVersion, and a list of
// DashboardHit items so that `-o yaml` and `-o json` output is machine-readable
// and round-trippable.
type DashboardSearchResultList struct {
	Kind       string         `json:"kind"`
	APIVersion string         `json:"apiVersion"`
	Items      []DashboardHit `json:"items"`
}
