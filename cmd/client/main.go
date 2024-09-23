package main

import (
	"context"
	"flag"
	"fmt"
	pb "github.com/norun9/S3CP/pkg/proto"
	mys3 "github.com/norun9/S3CP/pkg/s3"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"io"
	"log"
	"os"
)

var (
	profile       = flag.String("profile", "", "AWS profile")
	region        = flag.String("region", "ap-northeast-1", "AWS region")
	localFilePath = flag.String("file", "", "local file path to upload")
	bucketName    = flag.String("bucket", "", "int flag")
)

func init() {
	flag.Parse()
	fmt.Printf("param -profile(AWS) : %s\n", *profile)
	fmt.Printf("param -region(AWS) : %s\n", *region)
	fmt.Printf("param -file(S3) : %s\n", *localFilePath)
	fmt.Printf("param -bucket(S3) : %s\n", *bucketName)
	if *localFilePath == "" {
		log.Fatalf("Error: Missing required parameter '-file'. Please specify the path to the local file you want to upload.")
	}
	if *bucketName == "" {
		log.Fatalf("Error: Missing required parameter '-bucket'. Please specify the S3 bucket name where the file should be uploaded.")
	}
}

const chunkSize = 5 * 1024 * 1024 // 5MB

func main() {
	file, err := os.Open(*localFilePath)
	if err != nil {
		log.Fatalf("failed to open file %s: %v", *localFilePath, err)
	}
	defer file.Close()

	conn, err := grpc.NewClient("localhost:50051", grpc.WithInsecure())
	if err != nil {
		log.Fatalf("failed to grpc connect: %v", err)
	}
	defer conn.Close()

	client := pb.NewFileUploadServiceClient(conn)

	// Get file size
	fileInfo, err := file.Stat()
	if err != nil {
		log.Fatalf("failed to stat file %s: %v", *localFilePath, err)
	}
	fileSize := fileInfo.Size()

	awsConfig := mys3.AWSConfig{Profile: *profile, Region: *region}

	if fileSize < chunkSize {
		// If file size is smaller than 5MB, use Unary RPC
		if err = singleUpload(client, file, awsConfig); err != nil {
			log.Fatalf("failed to single upload file: %v", err)
		}
	} else {
		// If file size is 5MB or larger, use Bidirectional Streaming
		if err = multipleUpload(client, file, awsConfig); err != nil {
			log.Fatalf("failed to multiple upload file: %v", err)
		}
	}
}

func singleUpload(client pb.FileUploadServiceClient, file *os.File, conf mys3.AWSConfig) (err error) {
	ctx := context.Background()
	buffer := make([]byte, chunkSize)
	n, err := file.Read(buffer)
	if err != nil {
		log.Fatalf("failed to read file: %v", err)
	}
	req := &pb.SingleUploadRequest{
		Filename:  "",
		Data:      buffer[:n],
		AwsConfig: &pb.AWSConfig{Profile: conf.Profile, Region: conf.Region},
	}
	var res *pb.SingleUploadResponse
	if res, err = client.SingleUpload(ctx, req); err != nil {
		return err
	} else {
		log.Printf("single upload result: %v", res)
	}

	return nil
}

func multipleUpload(svc pb.FileUploadServiceClient, file *os.File, conf mys3.AWSConfig) (err error) {
	ctx := context.Background()
	var stream grpc.BidiStreamingClient[pb.MultipleUploadRequest, pb.MultipleUploadResponse]
	if stream, err = svc.MultipleUpload(ctx); err != nil {
		return errors.Wrap(err, "failed to create upload stream")
	}

	buffer := make([]byte, chunkSize)
	var chunkNumber int32 = 1
	for {
		n, err := file.Read(buffer)
		log.Printf("reads up to %d bytes", len(buffer))
		if err == io.EOF {
			// At end of file, Read returns 0, io.EOF
			break
		}
		if err != nil {
			return errors.Wrap(err, "failed to read file")
		}

		// 送信処理
		err = stream.Send(&pb.MultipleUploadRequest{
			Filename:    "",
			ChunkData:   buffer[:n],
			ChunkNumber: chunkNumber,
			AwsConfig:   &pb.AWSConfig{Profile: conf.Profile, Region: conf.Region},
		})
		if err != nil {
			return errors.Wrap(err, "failed to send chunk")
		}

		// 受信処理
		resp, err := stream.Recv()
		if err == io.EOF {
			break // discovered EOF. no more messages from server
		}
		if err != nil {
			return errors.Wrap(err, "failed to receive response")
		}

		// Handle the server's response
		fmt.Println("Received response:", resp.GetMessage())
		chunkNumber++
	}

	// ストリーム終端処理
	if err := stream.CloseSend(); err != nil {
		return errors.Wrap(err, "failed to close")
	}
	return nil
}
