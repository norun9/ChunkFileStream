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
	"time"
)

func main() {
	// 1. 80番portのLisnterを作成
	port := 80
	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		panic(err)
	}

	// 2. gRPCサーバーを作成
	s := grpc.NewServer(
		grpc.MaxRecvMsgSize(10*1024*1024), // 5MBに設定
		grpc.MaxSendMsgSize(10*1024*1024), // 5MBに設定
	)

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
		partNumber      int32 = 1
		uploadID        string
		multipartUpload *s3.CreateMultipartUploadOutput
		svc             *s3.Client
		bucket          string
		key             string
	)

	// 1. ループ内で最初のリクエストおよびその後のチャンクを処理
	for {
		log.Println("Waiting for chunk...")

		req, err := stream.Recv() // クライアントからのリクエストを受信
		if errors.Is(err, io.EOF) {
			log.Println("All chunks received.")
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to receive chunk")
		}

		// 2. 最初のリクエストでS3のマルチパートアップロードを初期化
		if partNumber == 1 {
			awsConfigReq := req.AwsConfig
			bucket = req.GetBucket()
			key = req.GetKey()
			svc = mys3.NewClient(stream.Context(), mys3.AWSConfig{
				Profile: awsConfigReq.Profile,
				Region:  awsConfigReq.Region,
			})

			// マルチパートアップロードの開始
			multipartUpload, err = svc.CreateMultipartUpload(stream.Context(), &s3.CreateMultipartUploadInput{
				Bucket: aws.String(req.GetBucket()),
				Key:    aws.String(req.GetKey()),
				//ContentType: aws.String(contType),
			})
			if err != nil {
				return errors.Wrap(err, "failed to initiate multipart upload")
			}
			uploadID = *multipartUpload.UploadId
			log.Printf("Multipart upload initiated: UploadID: %s", uploadID)
		}

		log.Printf("Received chunk number %d, size: %d bytes", partNumber, len(req.GetChunkData()))

		// S3にチャンクをアップロード
		uploadResp, err := svc.UploadPart(stream.Context(), &s3.UploadPartInput{
			Bucket:     aws.String(req.GetBucket()),
			Key:        aws.String(req.GetKey()),
			PartNumber: aws.Int32(partNumber),
			UploadId:   aws.String(uploadID),
			Body:       bytes.NewReader(req.GetChunkData()),
		})
		if err != nil {
			// アップロードに失敗した場合はAbortMultipartUploadを呼び出す
			log.Printf("Error uploading part %d: %v", partNumber, err)
			abortErr := abortMultipartUpload(stream.Context(), svc, req.GetBucket(), req.GetKey(), uploadID)
			if abortErr != nil {
				log.Printf("Failed to abort multipart upload: %v", abortErr)
			}
			return errors.Wrap(err, "failed to upload part")
		}

		// アップロードしたパート情報を追跡
		completedParts = append(completedParts, types.CompletedPart{
			ETag:       uploadResp.ETag,
			PartNumber: aws.Int32(partNumber),
		})

		// クライアントに進捗を送信
		err = stream.Send(&pb.MultipleUploadResponse{
			ChunkNumber: partNumber,
			Message:     fmt.Sprintf("Chunk %d uploaded successfully", partNumber),
		})
		if err != nil {
			return errors.Wrap(err, "failed to send response")
		}

		partNumber++
	}

	log.Printf("Completed parts: %+v", completedParts)

	// 3. マルチパートアップロードの完了
	s3Ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	_, err := svc.CompleteMultipartUpload(s3Ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		// 完了に失敗した場合もAbortMultipartUploadを呼び出す
		log.Printf("Failed to complete multipart upload: %v", err)
		abortErr := abortMultipartUpload(stream.Context(), svc, bucket, key, uploadID)
		if abortErr != nil {
			log.Printf("Failed to abort multipart upload: %v", abortErr)
		}
		return errors.Wrap(err, "failed to complete multipart upload")
	}

	log.Println("Multipart upload completed successfully")

	return nil
}

// マルチパートアップロードの中止
func abortMultipartUpload(ctx context.Context, svc *s3.Client, bucket, key, uploadID string) error {
	_, err := svc.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
		Bucket:   aws.String(bucket),
		Key:      aws.String(key),
		UploadId: aws.String(uploadID),
	})
	return err
}
