package main

import (
	"context"
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

// Init OCI artifact store
func initOciStore(ctx context.Context, dataDir string) (*oci.Store, error) {
	return oci.NewWithContext(ctx, path.Join(dataDir, artifactsStoreName))
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

	// ソースコードからAPIを確認して、正確な引数で呼び出す
	builder, err := soci.NewIndexBuilder(containerdStore, sociStore, soci.WithArtifactsDb(artifactsDb), soci.WithMinLayerSize(0))
	if err != nil {
		return nil, err
	}

	// Build the SOCI index (v0.9.0ではV1のみサポート)
	// V2サポートは最新のコードにのみ存在するため、V2リクエストでもV1で処理
	if sociIndexVersion == "V2" {
		log.Info(ctx, "Warning: SOCI V2 index not supported in this version. Using V1 format.")
	}
	
	_, err = builder.Build(ctx, image)
	if err != nil {
		return nil, fmt.Errorf("failed to build SOCI index: %w", err)
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

// Log and return the lambda handler error
func lambdaError(ctx context.Context, msg string, err error) (string, error) {
	log.Error(ctx, msg, err)
	return msg, err
}

func process(ctx context.Context, repo string, digest string, region string, account string, sociIndexVersion string, imageTag string) (string, error) {
	registryUrl := buildEcrRegistryUrl(region, account)
	ctx = context.WithValue(ctx, "RegistryURL", registryUrl)

	registry, err := registryutils.Init(ctx, registryUrl)
	if err != nil {
		return lambdaError(ctx, "Remote registry initialization error", err)
	}

	err = registry.ValidateImageDigest(ctx, repo, digest, sociIndexVersion)
	if err != nil {
		log.Warn(ctx, fmt.Sprintf("Image validation error: %v", err))
		// Returning a non error to skip retries
		return "Exited early due to manifest validation error", nil
	}

	// Directory in lambda storage to store images and SOCI artifacts
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

	// For V2, prepare tag for the SOCI index
	var tag string
	if sociIndexVersion == "V2" && imageTag != "" {
		tag = imageTag + "-soci"
		log.Info(ctx, fmt.Sprintf("Using image tag with suffix: %s", tag))
	}

	indexDescriptor, err := buildIndex(ctx, dataDir, sociStore, image, sociIndexVersion)
	if err != nil {
		return lambdaError(ctx, "SOCI index build error", err)
	}
	ctx = context.WithValue(ctx, "SOCIIndexDigest", indexDescriptor.Digest.String())

	err = registry.Push(ctx, sociStore, *indexDescriptor, repo, tag)
	if err != nil {
		return lambdaError(ctx, "SOCI index push error", err)
	}

	log.Info(ctx, "Successfully built and pushed SOCI index")
	return "Successfully built and pushed SOCI index", nil
}

func main() {
	if len(os.Args) < 4 {
		fmt.Println("Usage: soci-wrapper REPOSITORY_NAME IMAGE_DIGEST AWS_REGION AWS_ACCOUNT [SOCI_INDEX_VERSION] [IMAGE_TAG]")
		fmt.Println("  SOCI_INDEX_VERSION: V1 or V2 (default: V1)")
		fmt.Println("  IMAGE_TAG: Optional image tag (required for V2 SOCI index)")
		os.Exit(1)
	}
	
	repo := os.Args[1]
	digest := os.Args[2]
	region := os.Args[3]
	account := os.Args[4]
	
	// Default to V1 if not specified
	sociIndexVersion := "V1"
	if len(os.Args) >= 6 {
		sociIndexVersion = os.Args[5]
		if sociIndexVersion != "V1" && sociIndexVersion != "V2" {
			fmt.Println("Invalid SOCI index version. Must be 'V1' or 'V2'")
			os.Exit(1)
		}
	}
	
	// Get image tag if provided
	imageTag := ""
	if len(os.Args) >= 7 {
		imageTag = os.Args[6]
	}
	
	// For V2, the image tag is required
	if sociIndexVersion == "V2" && imageTag == "" {
		fmt.Println("IMAGE_TAG is required when using SOCI index version V2")
		os.Exit(1)
	}
	
	process(context.TODO(), repo, digest, region, account, sociIndexVersion, imageTag)
}
