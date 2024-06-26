package main

import (
	"context"
	"strings"

	"gopkg.in/ini.v1"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/crossplane/crossplane-runtime/pkg/errors"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	fnv1beta1 "github.com/crossplane/function-sdk-go/proto/v1beta1"
	"github.com/crossplane/function-sdk-go/request"
	"github.com/crossplane/function-sdk-go/resource"
	"github.com/crossplane/function-sdk-go/response"
)

// Function returns whatever response you ask it to.
type Function struct {
	fnv1beta1.UnimplementedFunctionRunnerServiceServer

	log           logging.Logger
	instanceTypes []types.InstanceTypeInfo
}

// RunFunction runs the Function.
func (f *Function) RunFunction(_ context.Context, req *fnv1beta1.RunFunctionRequest) (*fnv1beta1.RunFunctionResponse, error) {
	f.log.Info("Running function", "tag", req.GetMeta().GetTag())
	rsp := response.To(req, response.DefaultTTL)

	// if we haven't already retrieved the hundreds of available EC2 instance
	// types, do so now. We only do this operation once since it requires
	// multiple network calls to AWS.
	if len(f.instanceTypes) == 0 {
		// retrieve the credentials passed to this function from the composition
		accessKeyID, secretAccessKey, err := f.getCreds(req, "aws_access_key_id", "aws_secret_access_key")
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "cannot get credentials"))
			return rsp, nil
		}

		// load all the available instance types to reference later
		if err := f.loadInstanceTypes(accessKeyID, secretAccessKey); err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "cannot load instance types"))
			return rsp, nil
		}
	}

	// get all of the desired resources so we can check them all
	desired, err := request.GetDesiredComposedResources(req)
	if err != nil {
		response.Fatal(rsp, errors.Wrapf(err, "cannot get desired composed resources from %T", req))
		return rsp, nil
	}

	// check every desired resource to see if it's using a current gen instance type
	for name, dr := range desired {
		it, err := getInstanceType(dr)
		if err != nil {
			response.Fatal(rsp, errors.Wrapf(err, "cannot get instance type from desired resource %s", name))
			return rsp, nil
		}

		// if the instance type is not current gen, return a fatal response - only current gen instances at our cutting edge company!
		if !f.isCurrentGen(it) {
			response.Fatal(rsp, errors.Errorf("invalid: desired resource %s must use a current generation instance type - (%s is not current gen)", name, it))
			return rsp, nil
		}
	}

	return rsp, nil
}

func (f *Function) loadInstanceTypes(accessKeyID, secretAccessKey string) error {
	// load the AWS config and create an EC2 client
	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("us-west-2"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return err
	}
	client := ec2.NewFromConfig(cfg)

	// describe all available instance types, using pagination to retrieve all of them
	var nextToken *string
	for {
		resp, err := client.DescribeInstanceTypes(
			context.Background(),
			&ec2.DescribeInstanceTypesInput{NextToken: nextToken})
		if err != nil {
			return err
		}
		f.instanceTypes = append(f.instanceTypes, resp.InstanceTypes...)
		if resp.NextToken == nil {
			// no more pages left
			break
		}
		nextToken = resp.NextToken
	}

	return nil
}

func (f *Function) isCurrentGen(instanceType string) bool {
	if instanceType == "" {
		// we don't have an instance type, don't bother checking
		return false
	}

	for _, it := range f.instanceTypes {
		if instanceType == string(it.InstanceType) {
			// found a matching instance type, return whether it's current gen
			return aws.ToBool(it.CurrentGeneration)
		}
	}

	// didn't find the instance type, just say it's not current gen
	return false
}

func getInstanceType(dr *resource.DesiredComposed) (string, error) {
	gvk := dr.Resource.GroupVersionKind()
	if gvk.Group == "ec2.aws.upbound.io" && gvk.Kind == "Instance" {
		return dr.Resource.GetString("spec.forProvider.instanceType")
	}

	return "", nil
}

func (f *Function) getCreds(req *fnv1beta1.RunFunctionRequest, keyName, secretKeyName string) (string, string, error) {
	// use the go function SDK to retrieve the raw credentials secret
	creds, err := request.GetCredentials(req, "aws-creds")
	if err != nil {
		return "", "", err
	}
	rawCreds := string(creds.Data["credentials"])

	// parse the raw credentials secret to find the access key ID and secret access key
	cfg, err := ini.Load(strings.NewReader(rawCreds))
	if err != nil {
		return "", "", err
	}

	accessKeyID := cfg.Section("default").Key(keyName).String()
	secretAccessKey := cfg.Section("default").Key(secretKeyName).String()
	return accessKeyID, secretAccessKey, nil
}
