# Gopipe

Gopipe は、ローカルのファイルを Amazon S3 バケットにコピーするためのツールです。

- 5MB以上のファイルの場合、S3のMultipart Uploadを使用し、ファイルをチャンクに分割して効率的にアップロードします。(Bidirectional Streaming RPCを使用)
- 5MB未満のファイルの場合は、通常のアップロードを行います。(Unary RPCを使用)

```bash
# Test Command
go run main.go -file_path ~/test.txt -bucket bucket -profile your-profile-name -dest_key test.txt
```

# minikubeを用いたローカル開発時

https://stackoverflow.com/questions/63138358/kubernetes-ingress-with-grpc
https://ucwork.hatenablog.com/entry/2019/03/28/000133
https://docs.aws.amazon.com/ja_jp/prescriptive-guidance/latest/patterns/configure-mutual-tls-authentication-for-applications-running-on-amazon-eks.html
https://www.alibabacloud.com/help/en/ack/ack-managed-and-ack-dedicated/user-guide/use-an-ingress-controller-to-access-grpc-services

## TODO

- gRPCサーバーのKubernetesデプロイ
- gRPC通信のTLS対応
