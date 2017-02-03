package makecoe

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/getcarina/carina/common"
	"github.com/getcarina/libcarina"
	"github.com/pkg/errors"
	"github.com/ryanuber/go-glob"
)

// MakeCOE is an adapter between the cli and Carina (make-coe)
type MakeCOE struct {
	client           *libcarina.CarinaClient
	clusterTypeCache map[int]*libcarina.ClusterType
	Account          *Account
}

func handleNotAcceptable(err libcarina.HTTPErr) error {
	return errors.Wrap(err, "Unable to communicate with the Carina API because the client is out-of-date. Update the carina client to the latest version. See https://getcarina.com/docs/tutorials/carina-cli#update for instructions.")
}

func handleHTTPError(err libcarina.HTTPErr) error {
	switch err.StatusCode {
	default:
		return err
	case 406:
		return handleNotAcceptable(err)
	}
}

func handleLibcarinaError(err error) error {
	cause := errors.Cause(err)
	if httpErr, ok := cause.(libcarina.HTTPErr); ok {
		return handleHTTPError(httpErr)
	}
	return err
}

func (carina *MakeCOE) init() error {
	if carina.client == nil {
		carinaClient, err := carina.Account.Authenticate()
		if err != nil {
			return err
		}
		carina.client = carinaClient
	}
	return nil
}

// GetQuotas retrieves the quotas set for the account
func (carina *MakeCOE) GetQuotas() (common.Quotas, error) {
	return &Quotas{}, nil
}

// CreateCluster creates a new cluster and prints the cluster information
func (carina *MakeCOE) CreateCluster(name string, template string, nodes int) (common.Cluster, error) {
	if template == "" {
		return nil, errors.New("--template is required")
	}

	err := carina.init()
	if err != nil {
		return nil, err
	}

	common.Log.WriteDebug("[make-coe] Looking up a template matching '%s'", template)
	clusterType, err := carina.lookupClusterTypeByName(template)
	if err != nil {
		return nil, err
	}

	common.Log.WriteDebug("[make-coe] Creating a %d-node %s cluster hosted on %s named %s", nodes, clusterType.COE, clusterType.HostType, name)
	createOpts := &libcarina.CreateClusterOpts{
		Name:          name,
		ClusterTypeID: clusterType.ID,
		Nodes:         nodes,
	}

	result, err := carina.client.Create(createOpts)
	if err != nil {
		return nil, handleLibcarinaError(errors.Wrap(err, "[make-coe] Unable to create cluster"))
	}

	cluster := &Cluster{Cluster: result}

	return cluster, nil
}

// GetClusterCredentials retrieves the TLS certificates and configuration scripts for a cluster by its id or name (if unique)
func (carina *MakeCOE) GetClusterCredentials(token string) (*libcarina.CredentialsBundle, error) {
	err := carina.init()
	if err != nil {
		return nil, err
	}

	common.Log.WriteDebug("[make-coe] Retrieving cluster credentials (%s)", token)
	creds, err := carina.client.GetCredentials(token)
	if err != nil {
		return nil, handleLibcarinaError(errors.Wrap(err, "[make-coe] Unable to retrieve the cluster credentials"))
	}

	return creds, nil
}

// ListClusters prints out a list of the user's clusters to the console
func (carina *MakeCOE) ListClusters() ([]common.Cluster, error) {
	var clusters []common.Cluster

	err := carina.init()
	if err != nil {
		return clusters, err
	}

	common.Log.WriteDebug("[make-coe] Listing clusters")
	results, err := carina.client.List()
	if err != nil {
		return clusters, handleLibcarinaError(errors.Wrap(err, "[make-coe] Unable to list clusters"))
	}

	for _, result := range results {
		cluster := &Cluster{Cluster: result}
		clusters = append(clusters, cluster)
	}

	return clusters, err
}

// ListClusterTemplates retrieves available templates for creating a new cluster
func (carina *MakeCOE) ListClusterTemplates() ([]common.ClusterTemplate, error) {
	err := carina.init()
	if err != nil {
		return nil, err
	}

	results, err := carina.listClusterTypes()
	if err != nil {
		return nil, err
	}

	var templates []common.ClusterTemplate
	for _, result := range results {
		template := &ClusterTemplate{ClusterType: result}
		templates = append(templates, template)
	}

	return templates, err
}

// RebuildCluster destroys and recreates the cluster by its id or name (if unique)
func (carina *MakeCOE) RebuildCluster(token string) (common.Cluster, error) {
	return nil, errors.New("[make-coe] Rebuilding clusters from the carina cli is not supported yet")
}

// GetCluster prints out a cluster's information to the console by its id or name (if unique)
func (carina *MakeCOE) GetCluster(token string) (common.Cluster, error) {
	err := carina.init()
	if err != nil {
		return nil, err
	}

	common.Log.WriteDebug("[make-coe] Retrieving cluster (%s)", token)
	result, err := carina.client.Get(token)
	if err != nil {
		return nil, handleLibcarinaError(errors.Wrap(err, fmt.Sprintf("[make-coe] Unable to retrieve cluster (%s)", token)))
	}
	cluster := &Cluster{Cluster: result}

	return cluster, nil
}

// DeleteCluster permanently deletes a cluster by its id or name (if unique)
func (carina *MakeCOE) DeleteCluster(token string) (common.Cluster, error) {
	err := carina.init()
	if err != nil {
		return nil, err
	}

	common.Log.WriteDebug("[make-coe] Deleting cluster (%s)", token)
	result, err := carina.client.Delete(token)
	if err != nil {
		cause := errors.Cause(err)
		if httpErr, ok := cause.(libcarina.HTTPErr); ok {
			if httpErr.StatusCode == http.StatusNotFound {
				common.Log.WriteWarning("Could not find the cluster (%s) to delete", token)
				cluster := newCluster()
				cluster.Status = "deleted"
				return cluster, nil
			}
		}

		return nil, handleLibcarinaError(errors.Wrap(cause, fmt.Sprintf("[make-coe] Unable to delete cluster (%s)", token)))
	}

	cluster := &Cluster{Cluster: result}

	return cluster, nil
}

// GrowCluster adds nodes to a cluster by its id or name (if unique)
func (carina *MakeCOE) GrowCluster(token string, nodes int) (common.Cluster, error) {
	return nil, errors.New("[make-coe] Grow command not supported. Please use 'resize'.")
}

// ResizeCluster resizes the cluster to the specified number of nodes
func (carina *MakeCOE) ResizeCluster(token string, nodes int) (common.Cluster, error) {
	err := carina.init()
	if err != nil {
		return nil, err
	}

	common.Log.WriteDebug("[make-coe] Resizing cluster (%s) to %d nodes", token, nodes)

	result, err := carina.client.Resize(token, nodes)
	if err != nil {
		return nil, handleLibcarinaError(errors.Wrap(err, fmt.Sprintf("[make-coe] Unable to resize cluster (%s)", token)))
	}

	cluster := &Cluster{Cluster: result}

	return cluster, nil
}

// SetAutoScale is not supported
func (carina *MakeCOE) SetAutoScale(token string, value bool) (common.Cluster, error) {
	return nil, errors.New("make-coe does not support autoscaling")
}

// WaitUntilClusterIsActive waits until the prior cluster operation is completed
func (carina *MakeCOE) WaitUntilClusterIsActive(cluster common.Cluster) (common.Cluster, error) {
	isDone := func(cluster common.Cluster) bool {
		status := strings.ToLower(cluster.GetStatus())
		return status == "active" || status == "error"
	}

	if isDone(cluster) {
		return cluster, nil
	}

	pollingInterval := 5 * time.Second
	for {
		cluster, err := carina.GetCluster(cluster.GetID())
		if err != nil {
			return nil, err
		}

		if isDone(cluster) {
			return cluster, nil
		}

		common.Log.WriteDebug("[make-coe] Waiting until cluster (%s) is active, currently in %s", cluster.GetName(), cluster.GetStatus())
		time.Sleep(pollingInterval)
	}
}

// WaitUntilClusterIsDeleted polls the cluster status until either the cluster is gone or an error state is hit
func (carina *MakeCOE) WaitUntilClusterIsDeleted(cluster common.Cluster) error {
	isDone := func(cluster common.Cluster) (bool, error) {
		status := strings.ToLower(cluster.GetStatus())
		if status == "error" {
			return true, errors.New("Unable to delete cluster, an error occured while deleting.")
		}
		return status == "deleted", nil
	}

	if done, err := isDone(cluster); done {
		return err
	}

	pollingInterval := 5 * time.Second
	for {
		cluster, err := carina.GetCluster(cluster.GetID())
		if err != nil {
			cause := errors.Cause(err)

			// Gracefully handle a 404 Not Found when the cluster is deleted quickly
			if httpErr, ok := cause.(libcarina.HTTPErr); ok {
				if httpErr.StatusCode == http.StatusNotFound {
					return nil
				}
			}

			return err
		}

		if done, err := isDone(cluster); done {
			return err
		}

		common.Log.WriteDebug("[make-coe] Waiting until cluster (%s) is deleted, currently in %s", cluster.GetName(), cluster.GetStatus())
		time.Sleep(pollingInterval)
	}
}

func (carina *MakeCOE) listClusterTypes() ([]*libcarina.ClusterType, error) {
	common.Log.WriteDebug("[make-coe] Listing cluster types")
	clusterTypes, err := carina.client.ListClusterTypes()
	if err != nil {
		return nil, handleLibcarinaError(errors.Wrap(err, "[make-coe] Unabe to list cluster types"))
	}

	return clusterTypes, err
}

func (carina *MakeCOE) getClusterTypeCache() (map[int]*libcarina.ClusterType, error) {
	if carina.clusterTypeCache == nil {
		clusterTypes, err := carina.listClusterTypes()
		if err != nil {
			return nil, err
		}

		carina.clusterTypeCache = make(map[int]*libcarina.ClusterType)
		for _, clusterType := range clusterTypes {
			carina.clusterTypeCache[clusterType.ID] = clusterType
		}
	}

	return carina.clusterTypeCache, nil
}

func (carina *MakeCOE) lookupClusterTypeByName(pattern string) (*libcarina.ClusterType, error) {
	cache, err := carina.getClusterTypeCache()
	if err != nil {
		return nil, err
	}

	var clusterType *libcarina.ClusterType
	for _, m := range cache {
		if !glob.GlobI(pattern, m.Name) {
			continue
		}

		common.Log.WriteDebug("Matched template '%s' to pattern '%s'", m.Name, pattern)
		if clusterType == nil {
			clusterType = m
		} else {
			return nil, &common.MultipleMatchingTemplatesError{TemplatePattern: pattern}
		}
	}

	if clusterType == nil {
		return nil, fmt.Errorf("Could not find template named %s", pattern)
	}

	return clusterType, nil
}
