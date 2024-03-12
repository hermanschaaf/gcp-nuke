package gcp

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/arehmandev/gcp-nuke/config"
	"github.com/arehmandev/gcp-nuke/helpers"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/syncmap"
	"google.golang.org/api/compute/v1"
)

// ComputeInstanceGroupsRegion -
type ComputeInstanceGroupsRegion struct {
	serviceClient *compute.Service
	base          ResourceBase
	resourceMap   syncmap.Map
}

func init() {
	computeService, err := compute.NewService(Ctx)
	if err != nil {
		log.Fatal(err)
	}
	computeResource := ComputeInstanceGroupsRegion{
		serviceClient: computeService,
	}
	register(&computeResource)
}

// Name - Name of the resourceLister for ComputeInstanceGroupsRegion
func (c *ComputeInstanceGroupsRegion) Name() string {
	return "ComputeInstanceGroupsRegion"
}

// ToSlice - Name of the resourceLister for ComputeInstanceGroupsRegion
func (c *ComputeInstanceGroupsRegion) ToSlice() (slice []string) {
	return helpers.SortedSyncMapKeys(&c.resourceMap)

}

// Setup - populates the struct
func (c *ComputeInstanceGroupsRegion) Setup(config config.Config) {
	c.base.config = config

}

// List - Returns a list of all ComputeInstanceGroupsRegion
func (c *ComputeInstanceGroupsRegion) List(refreshCache bool) []string {
	if !refreshCache {
		return c.ToSlice()
	}
	// Refresh resource map
	c.resourceMap = sync.Map{}

	for _, region := range c.base.config.Regions {
		instanceListCall := c.serviceClient.RegionInstanceGroupManagers.List(c.base.config.Project, region)
		instanceList, err := instanceListCall.Do()
		if err != nil {
			log.Fatal(err)
		}

		for _, instance := range instanceList.Items {
			instanceResource := DefaultResourceProperties{
				region: region,
			}
			c.resourceMap.Store(instance.Name, instanceResource)
		}
	}
	return c.ToSlice()
}

// Dependencies - Returns a List of resource names to check for
func (c *ComputeInstanceGroupsRegion) Dependencies() []string {
	a := ComputeRegionAutoScalers{}
	return []string{a.Name()}
}

// Remove -
func (c *ComputeInstanceGroupsRegion) Remove() error {

	// Removal logic
	errs, _ := errgroup.WithContext(c.base.config.Ctx)

	c.resourceMap.Range(func(key, value interface{}) bool {
		instanceID := key.(string)
		region := value.(DefaultResourceProperties).region

		// Parallel instance deletion
		errs.Go(func() error {
			deleteCall := c.serviceClient.RegionInstanceGroupManagers.Delete(c.base.config.Project, region, instanceID)
			operation, err := deleteCall.Do()
			if err != nil {
				return err
			}
			var opStatus string
			seconds := 0
			for opStatus != "DONE" {
				log.Printf("[Info] Resource currently being deleted %v [type: %v project: %v region: %v] (%v seconds)", instanceID, c.Name(), c.base.config.Project, region, seconds)
				operationCall := c.serviceClient.RegionOperations.Get(c.base.config.Project, region, operation.Name)
				checkOpp, err := operationCall.Do()
				if err != nil {
					return err
				}
				opStatus = checkOpp.Status

				time.Sleep(time.Duration(c.base.config.PollTime) * time.Second)
				seconds += c.base.config.PollTime
				if seconds > c.base.config.Timeout {
					return fmt.Errorf("[Error] Resource deletion timed out for %v [type: %v project: %v region: %v] (%v seconds)", instanceID, c.Name(), c.base.config.Project, region, c.base.config.Timeout)
				}
			}
			c.resourceMap.Delete(instanceID)

			log.Printf("[Info] Resource deleted %v [type: %v project: %v region: %v] (%v seconds)", instanceID, c.Name(), c.base.config.Project, region, seconds)
			return nil
		})
		return true
	})
	// Wait for all deletions to complete, and return the first non nil error
	err := errs.Wait()
	return err
}
