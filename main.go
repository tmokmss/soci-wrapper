package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"errors"
	"path"
	"soci-wrapper/utils/fs"
	"soci-wrapper/utils/log"
	registryutils "soci-wrapper/utils/registry"

	"github.com/containerd/containerd/images"
	"oras.land/oras-go/v2/content/oci"

	"github.com/awslabs/soci-snapshotter/soci"
	"github.com/awslabs/soci-snapshotter/soci/store"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/content/local"
	"github.com/containerd/containerd/platforms"

	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
)

const artifactsStoreName = "store"
const artifactsDbName = "artifacts.db"

// Returns ecr registry url from an image action event
func buildEcrRegistryUrl(region string, account string) string {
	var awsDomain = ".amazonaws.com"
	if strings.HasPrefix(region, "cn") {
		awsDomain = ".amazonaws.com.cn"
	}
	return account + ".dkr.ecr." + region + awsDomain
}

// Create a temp directory in /tmp
// The directory is prefixed by the Lambda's request id
func createTempDir(ctx context.Context) (string, error) {
	// free space in bytes
	freeSpace := fs.CalculateFreeSpace("/tmp")
	log.Info(ctx, fmt.Sprintf("There are %d bytes of free space in /tmp directory", freeSpace))
	log.Info(ctx, "Creating a directory to store images and SOCI artifacts")
	tempDir, err := os.MkdirTemp("/tmp", "TODO") // The temp dir name is prefixed by the request id
	return tempDir, err
}

// Clean up the data written by the Lambda
func cleanUp(ctx context.Context, dataDir string) {
	log.Info(ctx, fmt.Sprintf("Removing all files in %s", dataDir))
	if err := os.RemoveAll(dataDir); err != nil {
		log.Error(ctx, "Clean up error", err)
	}
}

// Init containerd store
func initContainerdStore(dataDir string) (content.Store, error) {
	containerdStore, err := local.NewStore(path.Join(dataDir, artifactsStoreName))
	return containerdStore, err
}

// Init SOCI artifact store
func initSociStore(ctx context.Context, dataDir string) (*store.SociStore, error) {
	// Note: We are wrapping an *oci.Store in a store.SociStore because soci.WriteSociIndex
	// expects a store.Store, an interface that extends the oci.Store to provide support
	// for garbage collection.
	ociStore, err := oci.NewWithContext(ctx, path.Join(dataDir, artifactsStoreName))
	return &store.SociStore{ociStore}, err
}

// Init a new instance of SOCI artifacts DB
func initSociArtifactsDb(dataDir string) (*soci.ArtifactsDb, error) {
	artifactsDbPath := path.Join(dataDir, artifactsDbName)
	artifactsDb, err := soci.NewDB(artifactsDbPath)
	if err != nil {
		return nil, err
	}
	return artifactsDb, nil
}

// Build soci index for an image and returns its ocispec.Descriptor
func buildIndex(ctx context.Context, dataDir string, sociStore *store.SociStore, image images.Image, sociIndexVersion string) (*ocispec.Descriptor, error) {
	log.Info(ctx, fmt.Sprintf("Building SOCI index version %s", sociIndexVersion))
	platform := platforms.DefaultSpec()

	artifactsDb, err := initSociArtifactsDb(dataDir)
	if err != nil {
		return nil, err
	}

	containerdStore, err := initContainerdStore(dataDir)
	if err != nil {
		return nil, err
	}

	// Create index builder with the appropriate arguments based on API requirements
	builder, err := soci.NewIndexBuilder(containerdStore, sociStore, soci.WithArtifactsDb(artifactsDb), soci.WithMinLayerSize(0))
	if err != nil {
		return nil, err
	}

	// Branch processing based on SOCI index version
	if sociIndexVersion == "V2" {
		log.Info(ctx, "Building SOCI V2 index using Convert method")
		// Using Convert() method for V2
		indexDescriptor, err := builder.Convert(ctx, image)
		if err != nil {
			return nil, fmt.Errorf("failed to convert image to SOCI V2 index: %w", err)
		}
		return indexDescriptor, nil
	} else {
		// Default to V1
		log.Info(ctx, "Building SOCI V1 index using Build method")
		_, err = builder.Build(ctx, image)
		if err != nil {
			return nil, fmt.Errorf("failed to build SOCI V1 index: %w", err)
		}

		// Get SOCI indices for the image from the OCI store
		indexDescriptorInfos, _, err := soci.GetIndexDescriptorCollection(ctx, containerdStore, artifactsDb, image, []ocispec.Platform{platform})
		if err != nil {
			return nil, err
		}
		if len(indexDescriptorInfos) == 0 {
			return nil, errors.New("no SOCI indices found in OCI store")
		}
		sort.Slice(indexDescriptorInfos, func(i, j int) bool {
			return indexDescriptorInfos[i].CreatedAt.Before(indexDescriptorInfos[j].CreatedAt)
		})

		return &indexDescriptorInfos[len(indexDescriptorInfos)-1].Descriptor, nil
	}
}

// Log and return the lambda handler error
func lambdaError(ctx context.Context, msg string, err error) (string, error) {
	log.Error(ctx, msg, err)
	return msg, err
}

func processV1(ctx context.Context, repo string, digest string, region string, account string) (string, error) {
	registryUrl := buildEcrRegistryUrl(region, account)
	ctx = context.WithValue(ctx, "RegistryURL", registryUrl)

	registry, err := registryutils.Init(ctx, registryUrl, region)
	if err != nil {
		return lambdaError(ctx, "Remote registry initialization error", err)
	}

	err = registry.ValidateImageDigest(ctx, repo, digest, "V1")
	if err != nil {
		log.Warn(ctx, fmt.Sprintf("Image validation error: %v", err))
		return "Exited early due to manifest validation error", nil
	}

	dataDir, err := createTempDir(ctx)
	log.Info(ctx, fmt.Sprintf("The path to the dataDir: %s", dataDir))
	if err != nil {
		return lambdaError(ctx, "Directory create error", err)
	}
	defer cleanUp(ctx, dataDir)

	sociStore, err := initSociStore(ctx, dataDir)
	if err != nil {
		return lambdaError(ctx, "OCI storage initialization error", err)
	}

	desc, err := registry.Pull(ctx, repo, sociStore, digest)
	if err != nil {
		return lambdaError(ctx, "Image pull error", err)
	}

	image := images.Image{
		Name:   repo + "@" + digest,
		Target: *desc,
	}

	indexDescriptor, err := buildIndex(ctx, dataDir, sociStore, image, "V1")
	if err != nil {
		return lambdaError(ctx, "SOCI index build error", err)
	}
	ctx = context.WithValue(ctx, "SOCIIndexDigest", indexDescriptor.Digest.String())

	err = registry.Push(ctx, sociStore, *indexDescriptor, repo, "")
	if err != nil {
		return lambdaError(ctx, "SOCI index push error", err)
	}

	log.Info(ctx, "Successfully built and pushed SOCI index")
	return "Successfully built and pushed SOCI index", nil
}

func processV2(ctx context.Context, repo string, digest string, region string, account string, outputTag string) (string, error) {
	registryUrl := buildEcrRegistryUrl(region, account)
	ctx = context.WithValue(ctx, "RegistryURL", registryUrl)

	registry, err := registryutils.Init(ctx, registryUrl, region)
	if err != nil {
		return lambdaError(ctx, "Remote registry initialization error", err)
	}

	err = registry.ValidateImageDigest(ctx, repo, digest, "V2")
	if err != nil {
		log.Warn(ctx, fmt.Sprintf("Image validation error: %v", err))
		return "Exited early due to manifest validation error", nil
	}

	dataDir, err := createTempDir(ctx)
	log.Info(ctx, fmt.Sprintf("The path to the dataDir: %s", dataDir))
	if err != nil {
		return lambdaError(ctx, "Directory create error", err)
	}
	defer cleanUp(ctx, dataDir)

	sociStore, err := initSociStore(ctx, dataDir)
	if err != nil {
		return lambdaError(ctx, "OCI storage initialization error", err)
	}

	desc, err := registry.Pull(ctx, repo, sociStore, digest)
	if err != nil {
		return lambdaError(ctx, "Image pull error", err)
	}

	image := images.Image{
		Name:   repo + "@" + digest,
		Target: *desc,
	}

	log.Info(ctx, fmt.Sprintf("Using output tag: %s", outputTag))
	indexDescriptor, err := buildIndex(ctx, dataDir, sociStore, image, "V2")
	if err != nil {
		return lambdaError(ctx, "SOCI index build error", err)
	}
	ctx = context.WithValue(ctx, "SOCIIndexDigest", indexDescriptor.Digest.String())

	err = registry.Push(ctx, sociStore, *indexDescriptor, repo, outputTag)
	if err != nil {
		return lambdaError(ctx, "SOCI index push error", err)
	}

	log.Info(ctx, "Successfully built and pushed SOCI index")
	return "Successfully built and pushed SOCI index", nil
}

func main() {
	// Define flags for named arguments
	repoPtr := flag.String("repo", "", "Repository name (required)")
	digestPtr := flag.String("digest", "", "Image digest (required)")
	regionPtr := flag.String("region", "", "AWS region (required)")
	accountPtr := flag.String("account", "", "AWS account ID (required)")
	sociIndexVersionPtr := flag.String("soci-version", "V1", "SOCI index version (V1 or V2, default: V1)")
	outputTagPtr := flag.String("output-tag", "", "Output tag for SOCI index (required for V2 SOCI index)")

	// Define custom usage message
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: soci-wrapper --repo REPOSITORY_NAME --digest IMAGE_DIGEST --region AWS_REGION --account AWS_ACCOUNT [--soci-version SOCI_INDEX_VERSION] [--output-tag OUTPUT_TAG]\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		flag.PrintDefaults()
	}

	// Parse flags
	flag.Parse()

	// Validate required flags
	if *repoPtr == "" || *digestPtr == "" || *regionPtr == "" || *accountPtr == "" {
		fmt.Println("Error: All required arguments must be provided")
		flag.Usage()
		os.Exit(1)
	}

	// Validate SOCI index version
	if *sociIndexVersionPtr != "V1" && *sociIndexVersionPtr != "V2" {
		fmt.Println("Invalid SOCI index version. Must be 'V1' or 'V2'")
		os.Exit(1)
	}

	// For V2, the output tag is required
	if *sociIndexVersionPtr == "V2" && *outputTagPtr == "" {
		fmt.Println("Error: --output-tag is required when using SOCI index version V2")
		flag.Usage()
		os.Exit(1)
	}

	// Call appropriate process function based on version
	var err error
	if *sociIndexVersionPtr == "V2" {
		_, err = processV2(context.TODO(), *repoPtr, *digestPtr, *regionPtr, *accountPtr, *outputTagPtr)
	} else {
		_, err = processV1(context.TODO(), *repoPtr, *digestPtr, *regionPtr, *accountPtr)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
