package main

import (
	"bytes"
	"context"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	pb "github.com/norun9/S3CP/pkg/proto"
	mys3 "github.com/norun9/S3CP/pkg/s3"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"sync"
)

func main() {
	// 1. 80番portのLisnterを作成
	port := 80
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}

	// 2. gRPCサーバーを作成
	s := grpc.NewServer()

	pb.RegisterFileUploadServiceServer(s, &server{})

	reflection.Register(s)

	// 3. 作成したgRPCサーバーを、8080番ポートで稼働させる
	go func() {
		log.Printf("start gRPC server port: %v", port)
		s.Serve(listener)
	}()

	// 4.Ctrl+Cが入力されたらGraceful shutdownされるようにする
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit
	log.Println("stopping gRPC server...")
	s.GracefulStop()
}

type server struct {
	pb.UnimplementedFileUploadServiceServer
}

func (*server) SingleUpload(ctx context.Context, req *pb.SingleUploadRequest) (*pb.SingleUploadResponse, error) {
	awsConfigReq := req.AwsConfig
	svc := mys3.NewClient(ctx, mys3.AWSConfig{Profile: awsConfigReq.GetProfile(), Region: awsConfigReq.GetRegion()})
	_, err := svc.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(req.GetBucket()),
		Key:    aws.String(req.GetKey()),
		Body:   bytes.NewReader(req.GetData()),
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to upload file")
	}
	return &pb.SingleUploadResponse{
		Message: fmt.Sprintf("file uploaded successfully. bucket: %s key: %s", req.GetBucket(), req.GetKey()),
	}, nil
}

// MultipleUpload handles the bidirectional streaming of file chunks to S3
func (*server) MultipleUpload(stream grpc.BidiStreamingServer[pb.MultipleUploadRequest, pb.MultipleUploadResponse]) error {

	var (
		completedParts  []types.CompletedPart
		wg              sync.WaitGroup
		multipartUpload *s3.CreateMultipartUploadOutput
		errChan               = make(chan error, 1)
		partNumber      int32 = 1
	)

	// Initialize multipart upload
	firstReq, err := stream.Recv()
	if err != nil {
		return errors.Wrap(err, "failed to receive initial request")
	}

	awsConfigReq := firstReq.AwsConfig
	svc := mys3.NewClient(stream.Context(), mys3.AWSConfig{Profile: awsConfigReq.Profile, Region: awsConfigReq.Region}) // Initialize S3 client

	multipartUpload, err = svc.CreateMultipartUpload(stream.Context(), &s3.CreateMultipartUploadInput{
		Bucket: aws.String(firstReq.GetBucket()),
		Key:    aws.String(firstReq.GetKey()),
	})
	if err != nil {
		return errors.Wrap(err, "failed to initiate multipart upload")
	}

	uploadID := multipartUpload.UploadId

	// Handle file chunks
	for {
		req, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to receive chunk")
		}

		wg.Add(1)
		go func(partNum int32, chunkData []byte) {
			defer wg.Done()
			uploadResp, err := svc.UploadPart(stream.Context(), &s3.UploadPartInput{
				Bucket:     aws.String(req.GetBucket()),
				Key:        aws.String(req.GetKey()),
				PartNumber: aws.Int32(partNum),
				UploadId:   uploadID,
				Body:       bytes.NewReader(chunkData),
			})
			if err != nil {
				errChan <- err
				return
			}

			// Append the uploaded part to the list of completed parts
			completedParts = append(completedParts, types.CompletedPart{
				ETag:       uploadResp.ETag,
				PartNumber: aws.Int32(partNum),
			})

			// Send a response back to the client
			if err := stream.Send(&pb.MultipleUploadResponse{
				ChunkNumber: partNum,
				Message:     fmt.Sprintf("Chunk %d uploaded successfully", partNum),
				Progress:    float32(partNum) * 100.0 / float32(len(completedParts)), // Optional progress
			}); err != nil {
				log.Printf("Failed to send response for chunk %d: %v", partNum, err)
			}
		}(partNumber, req.GetChunkData())

		partNumber++
	}

	// Wait for all parts to be uploaded
	wg.Wait()
	close(errChan)

	if err := <-errChan; err != nil {
		// If any part fails, abort the multipart upload
		_, abortErr := svc.AbortMultipartUpload(stream.Context(), &s3.AbortMultipartUploadInput{
			Bucket:   aws.String(firstReq.GetBucket()),
			Key:      aws.String(firstReq.GetKey()),
			UploadId: uploadID,
		})
		if abortErr != nil {
			log.Printf("Failed to abort multipart upload: %v", abortErr)
		}
		return errors.Wrap(err, "multipart upload failed")
	}

	// Complete the multipart upload
	_, err = svc.CompleteMultipartUpload(stream.Context(), &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(firstReq.GetBucket()),
		Key:      aws.String(firstReq.GetKey()),
		UploadId: uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return errors.Wrap(err, "failed to complete multipart upload")
	}

	return nil
}
