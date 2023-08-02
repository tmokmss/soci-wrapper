# soci-wrapper
Build and push a SOCI index in an alternative way.

## Build
To build this project, you must install [all the dependencies](https://github.com/awslabs/soci-snapshotter/blob/main/docs/build.md#dependencies) of soci-snapshotter.

```
go build
```

## Usage
It can only be built on Linux environment. 

```
soci-wrapper REPOSITORY_NAME DIGEST AWS_REGION AWS_ACCOUNT
```

## NOTICE
Most of the code is copied from [cfn-ecr-aws-soci-index-builder](https://github.com/aws-ia/cfn-ecr-aws-soci-index-builder) project.
