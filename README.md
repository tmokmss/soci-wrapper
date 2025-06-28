# soci-wrapper

A wrapper for the [SOCI](https://github.com/awslabs/soci-snapshotter) (Seekable OCI) snapshotter tool.

## Usage

```
soci-wrapper --repo REPOSITORY_NAME --digest IMAGE_DIGEST --region AWS_REGION --account AWS_ACCOUNT [--soci-version SOCI_INDEX_VERSION] [--output-tag OUTPUT_TAG]
```

### Options

- `--repo` - Repository name (required)
- `--digest` - Image digest (required)
- `--region` - AWS region (required)
- `--account` - AWS account ID (required)
- `--soci-version` - SOCI index version (V1 or V2, default: V1)
- `--output-tag` - Output tag for SOCI index (required for V2 SOCI index and ignored for V1 index.)

Build and push a SOCI index in an alternative way.

* You do not need any other dependencies (such as containerd or zlib) installed.
* You can run this binary anywhere such as CodeBuild or Lambda.

This CLI is used in [`deploy-time-build`](https://github.com/tmokmss/deploy-time-build?tab=readme-ov-file#build-soci-index-for-a-container-image), a CDK construct to build and deploy a SOCI index on CDK deployment.

### Examples

For SOCI V1 index:
```sh
soci-wrapper --repo my-repo --digest sha256:abc123... --region us-west-2 --account 123456789012
```

For SOCI V2 index:
```sh
soci-wrapper --repo my-repo --digest sha256:abc123... --region us-west-2 --account 123456789012 --soci-version V2 --output-tag my-soci-index
```

## Build
To build this project, you must install [all the dependencies](https://github.com/awslabs/soci-snapshotter/blob/main/docs/build.md#dependencies) of soci-snapshotter.

```sh
go build
```

## NOTICE
Most of the code is copied from [cfn-ecr-aws-soci-index-builder](https://github.com/aws-ia/cfn-ecr-aws-soci-index-builder) project.
