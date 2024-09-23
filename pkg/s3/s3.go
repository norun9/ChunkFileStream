package s3

import (
	"context"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"log"
)

type AWSConfig struct {
	Profile string
	Region  string
}

func NewClient(ctx context.Context, awsConfig AWSConfig) *s3.Client {
	sdkConfig, err := config.LoadDefaultConfig(
		ctx,
		config.WithSharedConfigProfile(awsConfig.Profile),
		config.WithRegion(awsConfig.Region),
	)
	if err != nil {
		log.Fatalf("Couldn't load default configuration. Have you set up your AWS account?: %v", err)
	}
	return s3.NewFromConfig(sdkConfig)
}
