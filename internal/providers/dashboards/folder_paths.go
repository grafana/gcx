package dashboards

import (
	"context"
	"errors"

	"github.com/grafana/gcx/internal/config"
	"github.com/grafana/gcx/internal/resources"
	"github.com/grafana/gcx/internal/resources/discovery"
	"github.com/grafana/gcx/internal/resources/dynamic"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type folderInfo struct {
	Title     string
	ParentUID string
}

func loadFolderPaths(ctx context.Context, cfg config.NamespacedRESTConfig, client *dynamic.NamespacedClient) (map[string]string, error) {
	desc, err := resolveFoldersDescriptor(ctx, cfg)
	if err != nil {
		return nil, err
	}

	list, err := client.List(ctx, desc, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return buildFolderPathMap(list.Items), nil
}

func resolveFoldersDescriptor(ctx context.Context, cfg config.NamespacedRESTConfig) (resources.Descriptor, error) {
	reg, err := discovery.NewDefaultRegistry(ctx, cfg)
	if err != nil {
		return resources.Descriptor{}, err
	}

	sels, err := resources.ParseSelectors([]string{"folders"})
	if err != nil {
		return resources.Descriptor{}, err
	}

	filters, err := reg.MakeFilters(discovery.MakeFiltersOptions{
		Selectors:            sels,
		PreferredVersionOnly: true,
	})
	if err != nil {
		return resources.Descriptor{}, err
	}
	if len(filters) == 0 {
		return resources.Descriptor{}, errors.New("server does not expose folders resource")
	}

	return filters[0].Descriptor, nil
}

func buildFolderPathMap(folders []unstructured.Unstructured) map[string]string {
	infos := make(map[string]folderInfo, len(folders))
	for _, folder := range folders {
		uid := folder.GetName()
		if uid == "" {
			continue
		}
		title := nestedString(folder.Object, "spec", "title")
		if title == "" {
			title = uid
		}
		infos[uid] = folderInfo{
			Title:     title,
			ParentUID: dashboardFolderUID(folder),
		}
	}

	paths := make(map[string]string, len(infos))
	for uid := range infos {
		paths[uid] = folderPath(uid, infos, paths, make(map[string]bool))
	}
	return paths
}

func folderPath(uid string, infos map[string]folderInfo, cache map[string]string, visiting map[string]bool) string {
	if path := cache[uid]; path != "" {
		return path
	}
	info, ok := infos[uid]
	if !ok {
		return uid
	}
	if visiting[uid] {
		return info.Title
	}

	visiting[uid] = true
	defer delete(visiting, uid)

	if info.ParentUID == "" {
		cache[uid] = info.Title
		return info.Title
	}

	parentPath := folderPath(info.ParentUID, infos, cache, visiting)
	if parentPath == "" || parentPath == info.ParentUID {
		cache[uid] = info.Title
		return info.Title
	}

	cache[uid] = parentPath + "/" + info.Title
	return cache[uid]
}
