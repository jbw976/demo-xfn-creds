# demo-xfn-creds

This is a demo function that shows how to use credentials/secrets in your
composition functions. The [composition](./example/composition.yaml) has a
reference to a credentials secret, and the function machinery securely passes
this data to the function. Then the function uses these credentials to connect
to AWS and list all the available EC2 instance types for the region. If the
requested instance type from the composition is not "current generation", then
the function will return an error and the EC2 instance from the composition will
not be created.

General information about credentials/secrets support in Crossplane can be found
in [issue #3718](https://github.com/crossplane/crossplane/issues/3718) and [PR
#5543](https://github.com/crossplane/crossplane/pull/5543).

## Pre-Requisites

1. A Kubernetes cluster with Crossplane [v1.16.0+ installed](https://docs.crossplane.io/latest/software/install/#install-crossplane)

## Set-up

First, `cd` into the [example](./example/) directory:
```
cd example
```

Install all the required functions and providers:
```
kubectl apply -f functions.yaml
kubectl apply -f providers.yaml
```

And make sure they become installed and healthy:
```
kubectl get pkg
```

Create the AWS credentials that will be used by both the function and the AWS provider:
```
AWS_PROFILE=default && echo -e "[default]\naws_access_key_id = $(aws configure get aws_access_key_id --profile $AWS_PROFILE)\naws_secret_access_key = $(aws configure get aws_secret_access_key --profile $AWS_PROFILE)" > aws-creds.txt

kubectl create secret generic aws-creds -n crossplane-system --from-file=credentials=./aws-creds.txt

kubectl apply -f aws-default-provider.yaml
```

Apply the `CompositeResourceDefinition` (XRD) and `Composition`:
```
kubectl apply -f definition.yaml
kubectl apply -f composition.yaml
```

Now we are ready to create a compute instance:
```
kubectl apply -f claim.yaml
```

Since we have provided an old non-current generation instance type, we expect the function to return an error and refuse to create the instance:
```
crossplane beta trace compute.example.crossplane.io/dev-instance -o wide
kubectl describe compute.example.crossplane.io/dev-instance
```

Let's update the instance type to a value that is current generation (`t3.micro`) and try again:
```
# edit claim.yaml to change instanceType to t3.micro, then:
kubectl apply -f claim.yaml
```

Now we should be using a current generation instance type, let's examine the claim again:
```
crossplane beta trace compute.example.crossplane.io/dev-instance -o wide
```

Great, it's creating the EC2 instance now. Let's examine that managed resource too:
```
kubectl get instance.ec2 -l crossplane.io/composite=dev-instance
```

Cool! Our function made authenticated requests to AWS, using the credentials we
supplied from our composition, to check if the requested instance type is
current generation or not, and then only allowed current generation instances to
be created.

Now we have seen how to use credentials/secrets in your composition functions! ✅

## Clean-up

Make sure to clean-up the EC2 instance by deleting the claim:
```
kubectl delete -f claim.yaml
kubectl get instance.ec2
```

## Building and pushing

If you want to build and push this function yourself, you can use commands
similar to the below, taken from the Crossplane docs [guide on building a
function in
Go](https://docs.crossplane.io/v1.16/guides/write-a-composition-function-in-go/#build-and-push-the-function-to-a-package-registry):

Build the runtime images:
```
docker build . --quiet --platform=linux/amd64 --tag runtime-amd64
docker build . --quiet --platform=linux/arm64 --tag runtime-arm64
```

Build the Crossplane packages:
```
crossplane xpkg build \
    --package-root=package \
    --embed-runtime-image=runtime-amd64 \
    --package-file=function-amd64.xpkg
crossplane xpkg build \
    --package-root=package \
    --embed-runtime-image=runtime-arm64 \
    --package-file=function-arm64.xpkg
```

Push to the marketplace registry:
```
crossplane xpkg push \
  --package-files=function-amd64.xpkg,function-arm64.xpkg \
  jaredorg/demo-xfn-creds:v0.0.1
```