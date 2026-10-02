package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// Remote manager RPC paths, relative to the plugin proxy prefix.
// ClusterConnection is the operator channel and is not wrapped here.
const (
	pathListClusters      = "/remotemanager.v1.RemoteManagerService/ListClusters"
	pathCreateCluster     = "/remotemanager.v1.RemoteManagerService/CreateCluster"
	pathUpdateCluster     = "/remotemanager.v1.RemoteManagerService/UpdateCluster"
	pathDeleteCluster     = "/remotemanager.v1.RemoteManagerService/DeleteCluster"
	pathListCollectorCRs  = "/remotemanager.v1.RemoteManagerService/ListCollectorCRs"
	pathCreateCollectorCR = "/remotemanager.v1.RemoteManagerService/CreateCollectorCR"
	pathUpdateCollectorCR = "/remotemanager.v1.RemoteManagerService/UpdateCollectorCR"
	pathDeleteCollectorCR = "/remotemanager.v1.RemoteManagerService/DeleteCollectorCR"
)

// postOK POSTs body and decodes a 200 response into dest.
// dest may be nil when the caller does not need the body.
func (c *Client) postOK(ctx context.Context, path string, body any, dest any) error {
	resp, err := c.doRequest(ctx, path, body)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return httpError(resp, path)
	}
	if dest == nil {
		if _, err := io.Copy(io.Discard, resp.Body); err != nil {
			return fmt.Errorf("drain response: %w", err)
		}
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

// ListClusters returns every cluster for the tenant. The API has no pagination.
// The result is never nil.
func (c *Client) ListClusters(ctx context.Context) ([]Cluster, error) {
	var result struct {
		Clusters []Cluster `json:"clusters"`
	}
	if err := c.postOK(ctx, pathListClusters, map[string]any{}, &result); err != nil {
		return nil, fmt.Errorf("fleet: list clusters: %w", err)
	}
	if result.Clusters == nil {
		result.Clusters = []Cluster{}
	}
	return result.Clusters, nil
}

// CreateCluster stores a new cluster. The caller chooses the id.
// Fails when that id already exists.
func (c *Client) CreateCluster(ctx context.Context, cluster Cluster) (*Cluster, error) {
	var created Cluster
	if err := c.postOK(ctx, pathCreateCluster, clusterWire(cluster), &created); err != nil {
		return nil, fmt.Errorf("fleet: create cluster: %w", err)
	}
	return &created, nil
}

// UpdateCluster replaces an existing cluster. Fields left empty are stored empty.
func (c *Client) UpdateCluster(ctx context.Context, cluster Cluster) (*Cluster, error) {
	var updated Cluster
	if err := c.postOK(ctx, pathUpdateCluster, clusterWire(cluster), &updated); err != nil {
		return nil, fmt.Errorf("fleet: update cluster %s: %w", cluster.ID, err)
	}
	return &updated, nil
}

// DeleteCluster removes a cluster. Fails when the cluster still has Collector CRs.
func (c *Client) DeleteCluster(ctx context.Context, id string) error {
	if err := c.postOK(ctx, pathDeleteCluster, map[string]string{"id": id}, nil); err != nil {
		return fmt.Errorf("fleet: delete cluster %s: %w", id, err)
	}
	return nil
}

// ListCollectorCRs returns desired Collector CRs.
// An empty clusterID lists every Collector CR for the tenant. A non-empty
// clusterID is sent as clusterId and limits the result to that cluster.
// The API has no pagination. The result is never nil.
func (c *Client) ListCollectorCRs(ctx context.Context, clusterID string) ([]CollectorCR, error) {
	body := map[string]any{}
	if clusterID != "" {
		body["clusterId"] = clusterID
	}
	var result struct {
		CollectorCRs []CollectorCR `json:"collectorCrs"`
	}
	if err := c.postOK(ctx, pathListCollectorCRs, body, &result); err != nil {
		return nil, fmt.Errorf("fleet: list collector CRs: %w", err)
	}
	if result.CollectorCRs == nil {
		result.CollectorCRs = []CollectorCR{}
	}
	return result.CollectorCRs, nil
}

// CreateCollectorCR stores a new desired Collector CR.
// The server assigns revision. Applied revision and apply error are not sent.
func (c *Client) CreateCollectorCR(ctx context.Context, cr CollectorCR) (*CollectorCR, error) {
	var created CollectorCR
	if err := c.postOK(ctx, pathCreateCollectorCR, collectorCRWire(cr), &created); err != nil {
		return nil, fmt.Errorf("fleet: create collector CR: %w", err)
	}
	return &created, nil
}

// UpdateCollectorCR replaces an existing desired Collector CR.
// Fields left empty are stored empty. The server assigns a new revision when
// namespace, name, release, or spec changes.
func (c *Client) UpdateCollectorCR(ctx context.Context, cr CollectorCR) (*CollectorCR, error) {
	var updated CollectorCR
	if err := c.postOK(ctx, pathUpdateCollectorCR, collectorCRWire(cr), &updated); err != nil {
		return nil, fmt.Errorf("fleet: update collector CR %s: %w", cr.ID, err)
	}
	return &updated, nil
}

// DeleteCollectorCR removes a desired Collector CR by id.
func (c *Client) DeleteCollectorCR(ctx context.Context, id string) error {
	if err := c.postOK(ctx, pathDeleteCollectorCR, map[string]string{"id": id}, nil); err != nil {
		return fmt.Errorf("fleet: delete collector CR %s: %w", id, err)
	}
	return nil
}

// clusterWire is the create/update body. Empty strings are sent so the server
// stores them empty instead of the encoder dropping them.
func clusterWire(cluster Cluster) map[string]string {
	return map[string]string{
		"id":        cluster.ID,
		"name":      cluster.Name,
		"namespace": cluster.Namespace,
	}
}

// collectorCRWire is the create/update body. Server-owned revision fields are
// omitted; the API ignores them and assigns revision itself.
func collectorCRWire(cr CollectorCR) map[string]string {
	return map[string]string{
		"id":        cr.ID,
		"clusterId": cr.ClusterID,
		"namespace": cr.Namespace,
		"name":      cr.Name,
		"release":   cr.Release,
		"spec":      cr.Spec,
	}
}
