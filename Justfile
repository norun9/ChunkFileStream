minikube-docker-build tag_name="gopipe-server":
    eval $(minikube docker-env) && docker build --no-cache -t {{ tag_name }} . --progress=plain
