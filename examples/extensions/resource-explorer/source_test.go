package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/grafana/gcx-ext-resource-explorer/browse"
	sdk "github.com/grafana/gcx/experimental/extensionsdk"
)

func TestFixtureProcess(_ *testing.T) {
	for i, arg := range os.Args {
		if arg == "--" {
			os.Exit(fixture(os.Args[i+1:], os.Stdout, os.Stderr))
		}
	}
}

func TestCLIDataBoundary(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands, err := sdk.New(sdk.Options{Binary: binary, Prefix: []string{"-test.run=TestFixtureProcess", "--"}})
	if err != nil {
		t.Fatal(err)
	}
	src := &cliSource{commands: commands}
	types, err := src.Types(t.Context())
	if err != nil || len(types) != 2 {
		t.Fatalf("types=%v err=%v", types, err)
	}
	schemas, err := src.Schemas(t.Context(), types)
	if err != nil || len(schemas) != 2 {
		t.Fatalf("schemas=%v err=%v", schemas, err)
	}
	for _, d := range types {
		if schemas[browse.Selector(d)] == nil {
			t.Fatalf("missing schema for %v", d)
		}
		items, err := src.List(t.Context(), d)
		if err != nil || len(items) != 2 {
			t.Fatalf("items=%v err=%v", items, err)
		}
		if items[1].GetKind() != "" {
			t.Fatal("fixture must exercise missing kind")
		}
	}
	src.selectors = []string{"folders"}
	types, err = src.Types(t.Context())
	if err != nil || len(types) != 1 || types[0].Kind != "Folder" {
		t.Fatalf("filtered=%v err=%v", types, err)
	}
	src.selectors = []string{"invalid"}
	if _, err := src.Types(t.Context()); err == nil {
		t.Fatal("invalid selector succeeded")
	}
}

type commandFunc func(context.Context, sdk.CommandRequest) (sdk.Result, error)

func (f commandFunc) Execute(ctx context.Context, r sdk.CommandRequest) (sdk.Result, error) {
	return f(ctx, r)
}

func TestPartialMissingKind(t *testing.T) {
	src := &cliSource{commands: commandFunc(func(_ context.Context, r sdk.CommandRequest) (sdk.Result, error) {
		if r.Args[1] == "list-types" {
			return sdk.Result{Stdout: []byte(`{"example.grafana.app":{"v1":[{"kind":"DataSource","plural":"datasources"}]}}`)}, nil
		}
		if r.Args[2] != "datasources.v1.example.grafana.app" {
			t.Fatal(r.Args)
		}
		return sdk.Result{ExitCode: 4, Stderr: []byte("one upstream failed"), Stdout: []byte(`{"items":[{"metadata":{"name":"kept","generation":9007199254740993}}]}`)}, nil
	})}
	types, err := src.Types(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	items, err := src.List(t.Context(), types[0])
	var partial *browse.PartialError
	if !errors.As(err, &partial) || len(items) != 1 || items[0].GetGeneration() != 9007199254740993 {
		t.Fatalf("items=%v err=%v", items, err)
	}
}

func TestGenericSelectorRouting(t *testing.T) {
	src := &cliSource{commands: commandFunc(func(_ context.Context, r sdk.CommandRequest) (sdk.Result, error) {
		if r.Args[1] == "list-types" {
			return sdk.Result{Stdout: []byte(`{"one.grafana.app":{"v1":[{"kind":"Widget","plural":"widgets"},{"kind":"Gadget","plural":"gadgets"}]},"two.grafana.app":{"v1":[{"kind":"Widget","plural":"widgets"}]}}`)}, nil
		}
		return sdk.Result{Stdout: fmt.Appendf(nil, `{"items":[{"metadata":{"name":%q}}]}`, r.Args[2])}, nil
	})}
	types, err := src.Types(t.Context())
	if err != nil || len(types) != 3 {
		t.Fatalf("types=%v err=%v", types, err)
	}
	for _, d := range types {
		items, err := src.List(t.Context(), d)
		if err != nil || len(items) != 1 || items[0].GetName() != browse.Selector(d) {
			t.Fatalf("selector=%v items=%v err=%v", d, items, err)
		}
	}
}
