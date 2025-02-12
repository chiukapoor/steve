package converter

import (
	"strings"

	"github.com/rancher/apiserver/pkg/types"
	"github.com/rancher/steve/pkg/attributes"
	"github.com/rancher/wrangler/v2/pkg/merr"
	"github.com/rancher/wrangler/v2/pkg/schemas"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
)

var (
	preferredGroups = map[string]string{
		"extensions": "apps",
	}
	preferredVersionOverride = map[string]string{
		"autoscaling/v1": "v2beta2",
	}
)

func AddDiscovery(client discovery.DiscoveryInterface, schemasMap map[string]*types.APISchema) error {
	groups, resourceLists, err := client.ServerGroupsAndResources()
	if gd, ok := err.(*discovery.ErrGroupDiscoveryFailed); ok {
		logrus.Errorf("Failed to read API for groups %v", gd.Groups)
	} else if err != nil {
		return err
	}

	versions := indexVersions(groups)

	var errs []error
	for _, resourceList := range resourceLists {
		gv, err := schema.ParseGroupVersion(resourceList.GroupVersion)
		if err != nil {
			errs = append(errs, err)
		}

		if err := refresh(gv, versions, resourceList, schemasMap); err != nil {
			errs = append(errs, err)
		}
	}

	return merr.NewErrors(errs...)
}

func indexVersions(groups []*metav1.APIGroup) map[string]string {
	result := map[string]string{}
	for _, group := range groups {
		result[group.Name] = group.PreferredVersion.Version
		if override, ok := preferredVersionOverride[group.Name+"/"+group.PreferredVersion.Version]; ok {
			for _, version := range group.Versions {
				// ensure override version exists
				if version.Version == override {
					result[group.Name] = override
				}
			}
		}
	}
	return result
}

func refresh(gv schema.GroupVersion, groupToPreferredVersion map[string]string, resources *metav1.APIResourceList, schemasMap map[string]*types.APISchema) error {
	for _, resource := range resources.APIResources {
		if strings.Contains(resource.Name, "/") {
			continue
		}

		gvk := schema.GroupVersionKind{
			Group:   gv.Group,
			Version: gv.Version,
			Kind:    resource.Kind,
		}
		gvr := gvk.GroupVersion().WithResource(resource.Name)

		schema := schemasMap[GVKToVersionedSchemaID(gvk)]
		if schema == nil {
			schema = &types.APISchema{
				Schema: &schemas.Schema{
					ID: GVKToVersionedSchemaID(gvk),
				},
			}
			attributes.SetGVK(schema, gvk)
		}

		schema.PluralName = gvrToPluralName(gvr)
		attributes.SetAPIResource(schema, resource)
		if preferredVersion := groupToPreferredVersion[gv.Group]; preferredVersion != "" && preferredVersion != gv.Version {
			attributes.SetPreferredVersion(schema, preferredVersion)
		}
		if group := preferredGroups[gv.Group]; group != "" {
			attributes.SetPreferredGroup(schema, group)
		}

		schemasMap[schema.ID] = schema
	}

	return nil
}
