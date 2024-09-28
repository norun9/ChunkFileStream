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
https://minikube.sigs.k8s.io/docs/commands/start/

https://stackoverflow.com/questions/71560578/minikube-clusterip-not-reachable
https://stackoverflow.com/questions/62108070/grpc-nodejs-dns-resolution-failed/62136381#62136381
https://stackoverflow.com/questions/64971822/unable-to-connect-with-grpc-when-deployed-with-kubernetes
https://github.com/kubernetes/minikube/issues/11193

https://stackoverflow.com/questions/66676139/how-to-connect-to-the-grpc-service-inside-k8s-cluster-from-outside-grpc-client

https://outcrawl.com/getting-started-microservices-go-grpc-kubernetes

https://developers.freee.co.jp/entry/kubernetes-ingress-controller

自己署名証明書の作成手順
https://qiita.com/rumrais1n/items/8e6e62f4626c56ebfd12

minikube start --addons=ingress --ports=80:80,443:443
https://github.com/kubernetes/minikube/issues/17313

kubectl run grpcurl-pod --image=fullstorydev/grpcurl:latest -it --rm --restart=Never -- -insecure grpc-greeter.example.com:443 list

ログ確認
kubectl logs -n ingress-nginx ingress-nginx-controller-bc57996ff-8q4r8

2024/09/28 12:13:30 [error] 1003#1003: *408497 upstream prematurely closed connection while reading response header from upstream, client: 10.244.0.1, server: grpc-greeter.example.com, request: "POST /grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo HTTP/2.0", upstream: "grpc://10.99.39.149:50051", host: "grpc-greeter.example.com:443"
10.244.0.1 - - [28/Sep/2024:12:13:30 +0000] "POST /grpc.reflection.v1alpha.ServerReflection/ServerReflectionInfo HTTP/2.0" 502 150 "-" "grpcurl/v1.9.1 grpc-go/1.61.0" 16 0.001 [grpc-dispatcher-server-50051] [] 10.99.39.149:50051 0 0.001 502 7cfea64f87bb2d7662279267083fc89b

kubectl get svc -n grpc                                               
NAME                TYPE        CLUSTER-IP     EXTERNAL-IP   PORT(S)     AGE
dispatcher-server   ClusterIP   10.99.39.149   <none>        50051/TCP   3h23m

ClusterIP/Portにアクセスしていることがわかる

minikube tunnel

kubectl run grpcurl-pod --namespace=grpc  --image=fullstorydev/grpcurl:latest -it --rm --restart=Never -- grpc-greeter.example.com:443 list

Failed to dial target host "grpc-greeter.example.com:443": tls: failed to verify certificate: x509: certificate signed by unknown authority
pod "grpcurl-pod" deleted
pod grpc/grpcurl-pod terminated (Error)

curl -v telnet://grpc-greeter.example.com:443
* Host grpc-greeter.example.com:443 was resolved.
* IPv6: (none)
* IPv4: 192.168.64.4
*   Trying 192.168.64.4:443...
* Connected to grpc-greeter.example.com (192.168.64.4) port 443

driverをhyperkitにすると
grpcurl -insecure grpc-greeter.example.com:443 list         
fileupload.FileUploadService
grpc.reflection.v1.ServerReflection
grpc.reflection.v1alpha.ServerReflection

go run main.go -file_path ~/test.txt -bucket noruntestbucket -profile tomoya-ueno-iam -region ap-northeast-1 -dest_key test
param -profile(AWS) : tomoya-ueno-iam
param -region(AWS) : ap-northeast-1
param -localFilePath(S3) : /Users/tomoyaueno/test.txt
param -s3DestKey(S3) : test
param -bucket(S3) : noruntestbucket
2024/09/29 04:46:37 failed to single upload file: rpc error: code = Unavailable desc = connection error: desc = "error reading server preface: http2: frame too large"
exit status 1

いけたっぽい

## TODO

- gRPCサーバーのKubernetesデプロイ
- gRPC通信のTLS対応
