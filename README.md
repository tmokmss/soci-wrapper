# soci-wrapper
Build and push a SOCI index in an alternative way.

* You do not need any other dependencies (such as containerd or zlib) installed.
* You can run this binary anywhere such as CodeBuild or Lambda.

This CLI is used in [`deploy-time-build`](https://github.com/tmokmss/deploy-time-build?tab=readme-ov-file#build-soci-index-for-a-container-image), a CDK construct to build and deploy a SOCI index on CDK deployment.

## Usage
It can only be built on Linux environment. 

```sh
soci-wrapper REPOSITORY_NAME DIGEST AWS_REGION AWS_ACCOUNT
```

Sometimes you will also have to set `AWS_REGION` environment variable:

```sh
export AWS_REGION=us-west-2 # the region your ECR repository is located at
```

## Build
To build this project, you must install [all the dependencies](https://github.com/awslabs/soci-snapshotter/blob/main/docs/build.md#dependencies) of soci-snapshotter.

```sh
go build
```

## NOTICE
Most of the code is copied from [cfn-ecr-aws-soci-index-builder](https://github.com/aws-ia/cfn-ecr-aws-soci-index-builder) project.
