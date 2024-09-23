package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	pb "github.com/norun9/s3cp/pkg/proto"
	mys3 "github.com/norun9/s3cp/pkg/s3"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
)

func main() {
	port := 80
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}

	s := grpc.NewServer(
		grpc.MaxRecvMsgSize(10*1024*1024),
		grpc.MaxSendMsgSize(10*1024*1024),
	)

	pb.RegisterFileUploadServiceServer(s, &server{})

	reflection.Register(s)

	go func() {
		log.Printf("start gRPC server port: %v", port)
		s.Serve(listener)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("Stopping gRPC server...")
	s.GracefulStop()
}

type server struct {
	pb.UnimplementedFileUploadServiceServer
}

func (*server) SingleUpload(ctx context.Context, req *pb.SingleUploadRequest) (*pb.SingleUploadResponse, error) {
	awsConfigReq := req.GetAwsConfig()
	s3ConfigReq := req.GetS3Config()
	svc := mys3.NewClient(ctx, mys3.AWSConfig{Profile: awsConfigReq.GetProfile(), Region: awsConfigReq.GetRegion()})
	_, err := svc.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s3ConfigReq.GetBucket()),
		Key:         aws.String(s3ConfigReq.GetKey()),
		Body:        bytes.NewReader(req.GetData()),
		ContentType: aws.String(s3ConfigReq.GetContentType()),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to upload file")
	}
	return &pb.SingleUploadResponse{
		Message: fmt.Sprintf("file uploaded successfully. bucket: %s key: %s", s3ConfigReq.GetBucket(), s3ConfigReq.GetKey()),
	}, nil
}

// MultipleUpload handles the bidirectional streaming of file chunks to S3
func (*server) MultipleUpload(stream grpc.BidiStreamingServer[pb.MultipleUploadRequest, pb.MultipleUploadResponse]) error {
	var (
		completedParts  []types.CompletedPart
		partNumber      int32 = 1
		uploadID        string
		multipartUpload *s3.CreateMultipartUploadOutput
		svc             *s3.Client
		bucket          string
		key             string
	)

	for {
		log.Println("waiting for data chunk...")

		// Receive request from Client
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			log.Println("all chunks received!")
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to receive chunk")
		}

		// Initialize multipart upload of s3 in first request
		if partNumber == 1 {
			awsConfigReq := req.GetAwsConfig()
			s3ConfigReq := req.GetS3Config()
			bucket = s3ConfigReq.GetBucket()
			key = s3ConfigReq.GetKey()

			svc = mys3.NewClient(stream.Context(), mys3.AWSConfig{
				Profile: awsConfigReq.Profile,
				Region:  awsConfigReq.Region,
			})

			// Start multipart upload
			multipartUpload, err = svc.CreateMultipartUpload(stream.Context(), &s3.CreateMultipartUploadInput{
				Bucket:      aws.String(bucket),
				Key:         aws.String(key),
				ContentType: aws.String(s3ConfigReq.GetContentType()),
			})
			if err != nil {
				return errors.Wrap(err, "failed to initiate multipart upload")
			}
			uploadID = *multipartUpload.UploadId
		}

		log.Printf("received chunk number %d, size: %d bytes", partNumber, len(req.GetChunkData()))

		// Upload the data chunk to S3
		uploadResp, err := svc.UploadPart(stream.Context(), &s3.UploadPartInput{
			Bucket:     aws.String(bucket),
			Key:        aws.String(key),
			PartNumber: aws.Int32(partNumber),
			UploadId:   aws.String(uploadID),
			Body:       bytes.NewReader(req.GetChunkData()),
		})
		if err != nil {
			abortErr := abortMultipartUpload(stream.Context(), svc, bucket, key, uploadID)
			if abortErr != nil {
				log.Printf("Failed to abort multipart upload: %v", abortErr)
			}
			return errors.Wrap(err, "failed to upload part")
		}

		// Trace the part information uploaded to S3
		completedParts = append(completedParts, types.CompletedPart{
			ETag:       uploadResp.ETag,
			PartNumber: aws.Int32(partNumber),
		})

		// Send the progress to Client
		err = stream.Send(&pb.MultipleUploadResponse{
			ChunkNumber: partNumber,
			Message:     fmt.Sprintf("chunk %d uploaded successfully", partNumber),
		})
		if err != nil {
			return errors.Wrap(err, "failed to send response")
		}

		partNumber++
	}

	// Complete multipart upload to S3
	_, err := svc.CompleteMultipartUpload(context.Background(), &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		abortErr := abortMultipartUpload(stream.Context(), svc, bucket, key, uploadID)
		if abortErr != nil {
			log.Printf(abortErr.Error())
		}
		return errors.Wrap(err, "failed to complete multipart upload")
	}

	log.Println("multipart upload completed successfully!")

	return nil
}

func abortMultipartUpload(ctx context.Context, svc *s3.Client, bucket, key, uploadID string) error {
	_, err := svc.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return errors.Wrap(err, "failed to abort multipart upload")
}
