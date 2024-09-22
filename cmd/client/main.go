package main

import (
	"flag"
	"fmt"
)

func main() {
	var (
		localFilePath = flag.String("file", "", "Local file path to upload")
		bucketName    = flag.String("bucket", "", "int flag")
		dest          = flag.String("dest", "", "File path in S3")
	)
	flag.Parse()
	fmt.Printf("param -file : %s\n", *localFilePath)
	fmt.Printf("param -bucket : %s\n", *bucketName)
	fmt.Printf("param -dest : %s\n", *dest)
}
