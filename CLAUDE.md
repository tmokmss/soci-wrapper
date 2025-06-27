# SOCI-Wrapper Repository Overview

## Repository Purpose

SOCI-Wrapper is a command-line tool that provides a wrapper for the [SOCI (Seekable OCI)](https://github.com/awslabs/soci-snapshotter) snapshotter tool. It helps with managing SOCI indices for container images, specifically with ECR repositories.

## Code Structure

### Main Components

- **main.go**: The entry point of the application.
  - Contains the CLI argument handling logic
  - Defines the main process flow for SOCI index creation
  - Handles temporary directory creation and cleanup

- **utils/**: Utility packages
  - **registry/**: ECR registry interaction (auth, pull, push)
  - **log/**: Logging utilities with context awareness
  - **fs/**: File system utilities

## Key Functions

1. **process()**: Main workflow function that:
   - Initializes registry client
   - Validates image manifest
   - Creates temp directory
   - Pulls the image
   - Builds the SOCI index
   - Pushes the index back to registry

2. **buildIndex()**: Builds SOCI index for an image

3. **Registry operations**:
   - **Init()**: Initialize registry client
   - **Pull()**: Pull images from registry to local storage
   - **Push()**: Push SOCI artifacts to registry
   - **ValidateImageManifest()**: Validate image manifest format

## CLI Usage

The tool accepts the following arguments:

```
soci-wrapper --repo REPOSITORY_NAME --digest IMAGE_DIGEST --region AWS_REGION --account AWS_ACCOUNT
```

Options:
- `--repo`: Repository name (required)
- `--digest`: Image digest (required)
- `--region`: AWS region (required)
- `--account`: AWS account ID (required)

## Dependencies

Major external dependencies:
- `github.com/awslabs/soci-snapshotter/soci`: SOCI core functionality
- `github.com/containerd/containerd`: Container runtime interactions
- `oras.land/oras-go/v2`: OCI Registry As Storage
- `github.com/aws/aws-sdk-go`: AWS SDK for Go
- `github.com/rs/zerolog`: Structured logging

## Testing

The repository contains minimal tests:
- `utils/fs/fs_test.go`: Tests for the file system utility functions