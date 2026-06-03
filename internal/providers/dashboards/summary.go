package dashboards

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type dashboardSummaryList struct {
	Kind       string             `json:"kind"`
	APIVersion string             `json:"apiVersion"`
	Metadata   dashboardListMeta  `json:"metadata"`
	Items      []dashboardSummary `json:"items"`
}

type dashboardListMeta struct {
	Continue           string `json:"continue,omitempty"`
	ResourceVersion    string `json:"resourceVersion,omitempty"`
	RemainingItemCount *int64 `json:"remainingItemCount,omitempty"`
}

type dashboardSummary struct {
	Kind       string               `json:"kind"`
	APIVersion string               `json:"apiVersion"`
	Metadata   dashboardSummaryMeta `json:"metadata"`
	Spec       dashboardSummarySpec `json:"spec"`
}

type dashboardSummaryMeta struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid,omitempty"`
	ResourceVersion   string            `json:"resourceVersion,omitempty"`
	Generation        int64             `json:"generation,omitempty"`
	CreationTimestamp *metav1.Time      `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

type dashboardSummarySpec struct {
	Title      string   `json:"title,omitempty"`
	Folder     string   `json:"folder,omitempty"`
	FolderUID  string   `json:"folderUID,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	PanelCount *int     `json:"panelCount,omitempty"`
}

func dashboardListSummary(list *unstructured.UnstructuredList) *dashboardSummaryList {
	return dashboardListSummaryWithFolderPaths(list, nil)
}

func dashboardListSummaryWithFolderPaths(list *unstructured.UnstructuredList, folderPaths map[string]string) *dashboardSummaryList {
	if list == nil {
		return &dashboardSummaryList{Kind: "DashboardSummaryList", Items: []dashboardSummary{}}
	}

	apiVersion := list.GetAPIVersion()
	if apiVersion == "" && len(list.Items) > 0 {
		apiVersion = list.Items[0].GetAPIVersion()
	}

	out := &dashboardSummaryList{
		Kind:       "DashboardSummaryList",
		APIVersion: apiVersion,
		Metadata: dashboardListMeta{
			Continue:           list.GetContinue(),
			ResourceVersion:    list.GetResourceVersion(),
			RemainingItemCount: list.GetRemainingItemCount(),
		},
		Items: make([]dashboardSummary, 0, len(list.Items)),
	}

	for _, item := range list.Items {
		out.Items = append(out.Items, dashboardItemSummary(item, folderPaths))
	}

	return out
}

func dashboardItemSummary(item unstructured.Unstructured, folderPaths map[string]string) dashboardSummary {
	meta := dashboardSummaryMeta{
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		UID:             string(item.GetUID()),
		ResourceVersion: item.GetResourceVersion(),
		Generation:      item.GetGeneration(),
		Labels:          item.GetLabels(),
		Annotations:     item.GetAnnotations(),
	}
	if ts := item.GetCreationTimestamp(); !ts.IsZero() {
		meta.CreationTimestamp = &ts
	}

	folderUID := dashboardFolderUID(item)

	return dashboardSummary{
		Kind:       "DashboardSummary",
		APIVersion: item.GetAPIVersion(),
		Metadata:   meta,
		Spec: dashboardSummarySpec{
			Title:      nestedString(item.Object, "spec", "title"),
			Folder:     dashboardFolderPath(folderUID, folderPaths),
			FolderUID:  folderUID,
			Tags:       dashboardTagSlice(item),
			PanelCount: dashboardPanelCountValue(item),
		},
	}
}
