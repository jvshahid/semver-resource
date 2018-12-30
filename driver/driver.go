package driver

import (
	"crypto/tls"
	"fmt"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"net/http"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/blang/semver"
	"github.com/concourse/semver-resource/models"
	"github.com/concourse/semver-resource/version"
)

type Driver interface {
	Bump(version.Bump) (semver.Version, error)
	Set(semver.Version) error
	Check(*semver.Version) ([]semver.Version, error)
}

const maxRetries = 12

func FromSource(source models.Source) (Driver, error) {
	var initialVersion semver.Version
	if source.InitialVersion != "" {
		version, err := semver.Parse(source.InitialVersion)
		if err != nil {
			return nil, fmt.Errorf("invalid initial version (%s): %s", source.InitialVersion, err)
		}

		initialVersion = version
	} else {
		initialVersion = semver.Version{Major: 0, Minor: 0, Patch: 0}
	}

	switch source.Driver {
	case models.DriverUnspecified, models.DriverS3:
		regionName := source.RegionName
		if len(regionName) == 0 {
			regionName = "us-east-1"
		}

		var creds *credentials.Credentials
		if source.AccessKeyID == "" && source.SecretAccessKey == "" {
			// If nothing is provided use the default cred chain.
			creds = nil
		} else {
			creds = credentials.NewStaticCredentials(source.AccessKeyID, source.SecretAccessKey, source.SessionToken)
		}

		var httpClient *http.Client
		if source.SkipSSLVerification {
			httpClient = &http.Client{Transport: &http.Transport{
				TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
			}}
		} else {
			httpClient = http.DefaultClient
		}

		awsConfig := aws.NewConfig().WithLogLevel(aws.LogDebugWithHTTPBody).WithRegion(regionName).WithMaxRetries(maxRetries).WithDisableSSL(source.DisableSSL).WithHTTPClient(httpClient).WithS3ForcePathStyle(true)

		if len(source.Endpoint) != 0 {
			awsConfig.Endpoint = aws.String(source.Endpoint)
		}

		sess := session.Must(session.NewSession())
		if source.AccessKeyID != "" && source.SecretAccessKey != "" {
			// If nothing is provided use the default cred chain.
			creds := credentials.NewStaticCredentials(source.AccessKeyID, source.SecretAccessKey, "")
			awsConfig.Credentials = creds
		} else {
			println("Using default credential chain for authentication.")
		}

		svc := s3.New(sess, awsConfig)

		if source.UseV2Signing {
			setv2Handlers(svc)
		}

		return &S3Driver{
			InitialVersion:       initialVersion,
			Svc:                  svc,
			BucketName:           source.Bucket,
			Key:                  source.Key,
			ServerSideEncryption: source.ServerSideEncryption,
		}, nil

	case models.DriverGit:
		return &GitDriver{
			InitialVersion:      initialVersion,
			URI:                 source.URI,
			Branch:              source.Branch,
			PrivateKey:          source.PrivateKey,
			Username:            source.Username,
			Password:            source.Password,
			File:                source.File,
			GitUser:             source.GitUser,
			CommitMessage:       source.CommitMessage,
			SkipSSLVerification: source.SkipSSLVerification,
		}, nil

	case models.DriverSwift:
		return NewSwiftDriver(&source)

	case models.DriverGCS:
		servicer := &GCSIOServicer{
			JSONCredentials: source.JSONKey,
		}

		return &GCSDriver{
			InitialVersion: initialVersion,

			Servicer:   servicer,
			BucketName: source.Bucket,
			Key:        source.Key,
		}, nil

	default:
		return nil, fmt.Errorf("unknown driver: %s", source.Driver)
	}
}
