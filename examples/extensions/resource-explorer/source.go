package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/grafana/gcx-ext-resource-explorer/browse"
	"github.com/grafana/gcx-ext-resource-explorer/resources"
	sdk "github.com/grafana/gcx/experimental/extensionsdk"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kjson "k8s.io/apimachinery/pkg/util/json"
)

type cliSource struct {
	commands  sdk.Executor
	selectors []string
}

func (s *cliSource) call(ctx context.Context, args ...string) ([]byte, error) {
	result, err := s.commands.Execute(ctx, sdk.CommandRequest{Args: args})
	if err != nil {
		return result.Stdout, err
	}
	return result.Stdout, result.Err()
}

type typeEntry struct {
	Kind, Plural, Singular string
	Schema                 map[string]any
}
type typeResponse map[string]map[string][]typeEntry

func (s *cliSource) discover(ctx context.Context, noSchema bool) (resources.Descriptors, map[string]map[string]any, error) {
	selectors := s.selectors
	if len(selectors) == 0 {
		selectors = []string{""}
	}
	var result resources.Descriptors
	schemas := map[string]map[string]any{}
	seen := map[string]bool{}
	for _, selector := range selectors {
		args := []string{"resources", "list-types", "--output", "json"}
		if noSchema {
			args = append(args, "--no-schema")
		}
		if selector != "" {
			args = append(args, selector)
		}
		data, err := s.call(ctx, args...)
		if err != nil {
			return result, schemas, err
		}
		var groups typeResponse
		if err := json.Unmarshal(data, &groups); err != nil {
			return nil, nil, fmt.Errorf("decoding resource types: %w", err)
		}
		for group, versions := range groups {
			for version, entries := range versions {
				for _, entry := range entries {
					d := resources.Descriptor{GroupVersion: schema.GroupVersion{Group: group, Version: version}, Kind: entry.Kind, Plural: entry.Plural, Singular: entry.Singular}
					key := browse.Selector(d)
					if !seen[key] {
						result = append(result, d)
						seen[key] = true
					}
					if entry.Schema != nil {
						schemas[key] = entry.Schema
					}
				}
			}
		}
	}
	return result.Sorted(), schemas, nil
}

func (s *cliSource) Types(ctx context.Context) (resources.Descriptors, error) {
	d, _, err := s.discover(ctx, true)
	return d, err
}

func (s *cliSource) Schemas(ctx context.Context, _ resources.Descriptors) (map[string]map[string]any, error) {
	_, schemas, err := s.discover(ctx, false)
	return schemas, err
}

func (s *cliSource) List(ctx context.Context, d resources.Descriptor) ([]unstructured.Unstructured, error) {
	data, err := s.call(ctx, "resources", "get", browse.Selector(d), "--output", "json", "--limit", "0")
	var ce *sdk.ExitError
	partial := errors.As(err, &ce) && ce.Code == 4
	if err != nil && !partial {
		return nil, err
	}
	var list struct {
		Items []map[string]any `json:"items"`
	}
	// Some datasource items omit kind. Decode their payload without TypeMeta validation,
	// preserving integer precision for metadata such as generation.
	if decodeErr := kjson.Unmarshal(data, &list); decodeErr != nil {
		return nil, fmt.Errorf("decoding resources: %w", decodeErr)
	}
	items := make([]unstructured.Unstructured, len(list.Items))
	for i, object := range list.Items {
		items[i] = unstructured.Unstructured{Object: object}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].GetName() < items[j].GetName() })
	if partial {
		return items, &browse.PartialError{Err: err}
	}
	return items, nil
}
