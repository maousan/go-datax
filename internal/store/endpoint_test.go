package store

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestEndpointCrudAndNamespaces(t *testing.T) {
	dataStore, err := Open(filepath.Join(t.TempDir(), "admin.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer dataStore.Close()

	ds := &DataSource{Name: "files", Type: "filesystem", JDBCUrl: "", Properties: StringMap{"basePath": t.TempDir()}}
	if err := dataStore.CreateDataSource(ds); err != nil {
		t.Fatal(err)
	}

	source := &Endpoint{
		Kind:         EndpointKindSource,
		Name:         "Orders",
		Connector:    "csv",
		DataSourceID: ds.ID,
		Path:         "orders.csv",
		SchemaJSON:   `[{"name":"id","type":"int64"}]`,
		Options:      StringMap{"header": "true"},
	}
	if err := dataStore.CreateEndpoint(source); err != nil {
		t.Fatal(err)
	}
	duplicate := *source
	duplicate.Name = "orders"
	if err := dataStore.CreateEndpoint(&duplicate); !errors.Is(err, ErrDuplicateEndpointName) {
		t.Fatalf("duplicate source error = %v, want ErrDuplicateEndpointName", err)
	}

	sink := &Endpoint{
		Kind:         EndpointKindSink,
		Name:         "orders",
		Connector:    "csv",
		DataSourceID: ds.ID,
		Path:         "orders-out.csv",
		SchemaJSON:   `[{"name":"id","type":"int64"}]`,
	}
	if err := dataStore.CreateEndpoint(sink); err != nil {
		t.Fatalf("same name in the sink namespace should be allowed: %v", err)
	}

	sources, err := dataStore.ListEndpoints(EndpointKindSource, "ord")
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 || sources[0].Name != "Orders" || sources[0].Options["header"] != "true" {
		t.Fatalf("sources = %+v", sources)
	}
	sinks, err := dataStore.ListEndpoints(EndpointKindSink, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(sinks) != 1 || sinks[0].ID != sink.ID {
		t.Fatalf("sinks = %+v", sinks)
	}

	if _, err := dataStore.GetEndpoint(EndpointKindSink, source.ID); !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("cross-kind get error = %v, want ErrEndpointNotFound", err)
	}

	referencing, err := dataStore.EndpointsUsingDataSource(ds.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(referencing) != 2 {
		t.Fatalf("referencing endpoints = %d, want 2", len(referencing))
	}

	source.Description = "updated"
	if err := dataStore.UpdateEndpoint(source); err != nil {
		t.Fatal(err)
	}
	loaded, err := dataStore.GetEndpoint(EndpointKindSource, source.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Description != "updated" {
		t.Fatalf("description = %q, want updated", loaded.Description)
	}

	if err := dataStore.DeleteEndpoint(EndpointKindSource, source.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := dataStore.GetEndpoint(EndpointKindSource, source.ID); !errors.Is(err, ErrEndpointNotFound) {
		t.Fatalf("get after delete error = %v, want ErrEndpointNotFound", err)
	}
	if _, err := dataStore.GetEndpoint(EndpointKindSink, sink.ID); err != nil {
		t.Fatalf("sink should survive source deletion: %v", err)
	}
}
