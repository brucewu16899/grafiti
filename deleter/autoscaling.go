package deleter

import (
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/autoscaling/autoscalingiface"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/coreos/grafiti/arn"
)

// AutoScalingGroupDeleter represents an AWS autoscaling group
type AutoScalingGroupDeleter struct {
	Client        autoscalingiface.AutoScalingAPI
	ResourceType  arn.ResourceType
	ResourceNames arn.ResourceNames
}

func (rd *AutoScalingGroupDeleter) String() string {
	return fmt.Sprintf(`{"Type": "%s", "Names": %v}`, rd.ResourceType, rd.ResourceNames)
}

// GetClient returns an AWS Client, and initalizes one if one has not been
func (rd *AutoScalingGroupDeleter) GetClient() autoscalingiface.AutoScalingAPI {
	if rd.Client == nil {
		rd.Client = autoscaling.New(setUpAWSSession())
	}
	return rd.Client
}

// AddResourceNames adds autoscaling group names to ResourceNames
func (rd *AutoScalingGroupDeleter) AddResourceNames(ns ...arn.ResourceName) {
	rd.ResourceNames = append(rd.ResourceNames, ns...)
}

// DeleteResources deletes autoscaling groups from AWS
func (rd *AutoScalingGroupDeleter) DeleteResources(cfg *DeleteConfig) error {
	if len(rd.ResourceNames) == 0 {
		return nil
	}

	fmtStr := "Deleted AutoScalingGroup"

	var params *autoscaling.DeleteAutoScalingGroupInput
	for _, n := range rd.ResourceNames {
		if cfg.DryRun {
			fmt.Println(drStr, fmtStr, n)
			continue
		}

		params = &autoscaling.DeleteAutoScalingGroupInput{
			AutoScalingGroupName: n.AWSString(),
			ForceDelete:          aws.Bool(true),
		}

		// Prevent throttling
		time.Sleep(cfg.BackoffTime)

		ctx := aws.BackgroundContext()
		_, err := rd.GetClient().DeleteAutoScalingGroupWithContext(ctx, params)
		if err != nil {
			cfg.logDeleteError(arn.AutoScalingGroupRType, n, err)
			if cfg.IgnoreErrors {
				continue
			}
			return err
		}

		fmt.Println(fmtStr, n)
	}

	time.Sleep(time.Duration(30) * time.Second)
	return nil
}

// RequestAutoScalingGroups requests autoscaling groups from the AWS API and returns autoscaling
// groups by names
func (rd *AutoScalingGroupDeleter) RequestAutoScalingGroups() ([]*autoscaling.Group, error) {
	if len(rd.ResourceNames) == 0 {
		return nil, nil
	}

	params := &autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: rd.ResourceNames.AWSStringSlice(),
	}
	asgs := make([]*autoscaling.Group, 0)

	for {
		ctx := aws.BackgroundContext()
		resp, err := rd.GetClient().DescribeAutoScalingGroupsWithContext(ctx, params)
		if err != nil {
			return nil, err
		}

		for _, asg := range resp.AutoScalingGroups {
			asgs = append(asgs, asg)
		}

		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}

		params.NextToken = resp.NextToken
	}

	return asgs, nil
}

// AutoScalingLaunchConfigurationDeleter represents an AWS launch configuration
type AutoScalingLaunchConfigurationDeleter struct {
	Client        autoscalingiface.AutoScalingAPI
	ResourceType  arn.ResourceType
	ResourceNames arn.ResourceNames
}

func (rd *AutoScalingLaunchConfigurationDeleter) String() string {
	return fmt.Sprintf(`{"Type": "%s", "Names": %v}`, rd.ResourceType, rd.ResourceNames)
}

// GetClient returns an AWS Client, and initalizes one if one has not been
func (rd *AutoScalingLaunchConfigurationDeleter) GetClient() autoscalingiface.AutoScalingAPI {
	if rd.Client == nil {
		rd.Client = autoscaling.New(setUpAWSSession())
	}
	return rd.Client
}

// AddResourceNames adds launch configuration names to ResourceNames
func (rd *AutoScalingLaunchConfigurationDeleter) AddResourceNames(ns ...arn.ResourceName) {
	rd.ResourceNames = append(rd.ResourceNames, ns...)
}

// DeleteResources deletes a launch configurations from AWS
func (rd *AutoScalingLaunchConfigurationDeleter) DeleteResources(cfg *DeleteConfig) error {
	if len(rd.ResourceNames) == 0 {
		return nil
	}

	fmtStr := "Deleted LaunchConfiguration"

	var params *autoscaling.DeleteLaunchConfigurationInput
	for _, n := range rd.ResourceNames {
		if cfg.DryRun {
			fmt.Println(drStr, fmtStr, n)
			continue
		}

		params = &autoscaling.DeleteLaunchConfigurationInput{
			LaunchConfigurationName: n.AWSString(),
		}

		// Prevent throttling
		time.Sleep(cfg.BackoffTime)

		ctx := aws.BackgroundContext()
		_, err := rd.GetClient().DeleteLaunchConfigurationWithContext(ctx, params)
		if err != nil {
			cfg.logDeleteError(arn.AutoScalingLaunchConfigurationRType, n, err)
			if cfg.IgnoreErrors {
				continue
			}
			return err
		}

		fmt.Println(fmtStr, n)
	}

	return nil
}

// RequestAutoScalingLaunchConfigurations requests resources from the AWS API and returns launch
// configurations by names
func (rd *AutoScalingLaunchConfigurationDeleter) RequestAutoScalingLaunchConfigurations() ([]*autoscaling.LaunchConfiguration, error) {
	if len(rd.ResourceNames) == 0 {
		return nil, nil
	}

	params := &autoscaling.DescribeLaunchConfigurationsInput{
		LaunchConfigurationNames: rd.ResourceNames.AWSStringSlice(),
	}
	lcs := make([]*autoscaling.LaunchConfiguration, 0)

	for {
		ctx := aws.BackgroundContext()
		resp, err := rd.GetClient().DescribeLaunchConfigurationsWithContext(ctx, params)
		if err != nil {
			return nil, err
		}

		for _, lc := range resp.LaunchConfigurations {
			lcs = append(lcs, lc)
		}

		if resp.NextToken == nil || *resp.NextToken == "" {
			break
		}

		params.NextToken = resp.NextToken
	}

	return lcs, nil
}

// RequestIAMInstanceProfilesFromLaunchConfigurations retrieves instance profiles from
// launch configuration names
func (rd *AutoScalingLaunchConfigurationDeleter) RequestIAMInstanceProfilesFromLaunchConfigurations() ([]*iam.InstanceProfile, error) {
	if len(rd.ResourceNames) == 0 {
		return nil, nil
	}

	lcs, rerr := rd.RequestAutoScalingLaunchConfigurations()
	if rerr != nil {
		return nil, rerr
	}

	// We cannot request instance profiles by their ID's so we must search
	// iteratively with a map
	want := map[string]struct{}{}
	var iprName string
	for _, lc := range lcs {
		if lc.IamInstanceProfile == nil {
			continue
		}

		// The docs say that IAMInstanceProfile can be either an ARN or name; if an
		// ARN, parse out name
		iprName = *lc.IamInstanceProfile
		if strings.HasPrefix(*lc.IamInstanceProfile, "arn:") {
			iprSplit := strings.Split(*lc.IamInstanceProfile, "instance-profile/")
			if len(iprSplit) != 2 || iprSplit[1] == "" {
				continue
			}
			iprName = iprSplit[1]
		}
		if _, ok := want[iprName]; !ok {
			want[iprName] = struct{}{}
		}
	}

	svc := iam.New(setUpAWSSession())

	iprs := make([]*iam.InstanceProfile, 0)
	params := new(iam.ListInstanceProfilesInput)
	for {
		ctx := aws.BackgroundContext()
		resp, err := svc.ListInstanceProfilesWithContext(ctx, params)
		if err != nil {
			return nil, err
		}

		for _, ipr := range resp.InstanceProfiles {
			if _, ok := want[*ipr.InstanceProfileName]; ok {
				iprs = append(iprs, ipr)
			}
		}

		if resp.IsTruncated == nil || !*resp.IsTruncated {
			break
		}

		params.Marker = resp.Marker
	}

	return iprs, nil
}
