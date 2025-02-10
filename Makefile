# ------------------------ Proto ------------------------
GO_MODULE := github.com/opplieam/bb-dist-noti

.PHONY: clean-directory
clean-directory:
ifeq ($(OS), Windows_NT)
	if exist "protogen" rd /s /q protogen
	mkdir protogen
else
	rm -fR ./protogen
	mkdir -p ./protogen
endif

.PHONY: protoc-go
protoc-go:
	protoc --go_opt=module=${GO_MODULE} --go_out=. \
	--go-grpc_opt=module=${GO_MODULE} --go-grpc_out=. \
	./proto/*.proto

.PHONY: build-proto
build-proto: clean-directory protoc-go

# ------------------------ Proto ------------------------

# ------------------------ TLS ------------------------
CONFIG_PATH=${HOME}/.bb-noti/

.PHONY: init-dir
init-dir:
	mkdir -p ${CONFIG_PATH}

.PHONY: gencert
gencert:
	cfssl gencert \
			-initca tls/ca-csr.json | cfssljson -bare ca

	cfssl gencert \
			-ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=tls/ca-config.json \
			-profile=server \
			tls/server-csr.json | cfssljson -bare server

	cfssl gencert \
			-ca=ca.pem \
			-ca-key=ca-key.pem \
			-config=tls/ca-config.json \
			-profile=client \
			tls/client-csr.json | cfssljson -bare client
	mv *.pem *.csr ${CONFIG_PATH}
# ------------------------ TLS --------------------------

# ------------------------ Run Node Locally --------------------------

.PHONY: rm-data-dir
rm-data-dir:
	rm -rf ./dev-data

.PHONY: run-node-1
run-node-1:
	go run cmd/noti/main.go \
		--data-dir=./dev-data/node1 \
		--node-name=node1 \
		--http-addr=":8402" \
		--serf-addr=127.0.0.1:8401 \
		--rpc-port=8400 \
		--bootstrap=true \
		--start-join-addrs=127.0.0.1:8401 \
		--start-join-addrs=127.0.0.1:8501 \
		--start-join-addrs=127.0.0.1:8601

.PHONY: run-node-2
run-node-2:
	go run cmd/noti/main.go \
		--data-dir=./dev-data/node2 \
		--node-name=node2 \
		--http-addr=":8502" \
		--serf-addr=127.0.0.1:8501 \
		--rpc-port=8500 \
		--start-join-addrs=127.0.0.1:8401 \
		--start-join-addrs=127.0.0.1:8501 \
		--start-join-addrs=127.0.0.1:8601

.PHONY: run-node-3
run-node-3:
	go run cmd/noti/main.go \
		--data-dir=./dev-data/node3 \
		--node-name=node3 \
		--http-addr=":8602" \
		--serf-addr=127.0.0.1:8601 \
		--rpc-port=8600 \
		--start-join-addrs=127.0.0.1:8401 \
        --start-join-addrs=127.0.0.1:8501 \
        --start-join-addrs=127.0.0.1:8601

.PHONY: help
help:
	go run cmd/noti/main.go --help
# ------------------------ Run Node Locally ----------------------------

# ------------------------ Run NATs ----------------------------------
.PHONY: run-jet-stream
run-jet-stream:
	docker run --rm -p 4222:4222 nats -js

.PHONY: run-mock-pub
run-mock-pub:
	go run cmd/mockpub/main.go

# ------------------------ Run NATs ----------------------------------

# ------------------------ Build ----------------------------------
BASE_IMAGE_NAME 	:= opplieam
SERVICE_NAME    	:= bb-noti
VERSION         	:= "0.0.1-$(shell git rev-parse --short HEAD)"
VERSION_DEV         := "cluster-dev"
SERVICE_IMAGE   	:= $(BASE_IMAGE_NAME)/$(SERVICE_NAME):$(VERSION)
SERVICE_IMAGE_DEV   := $(BASE_IMAGE_NAME)/$(SERVICE_NAME):$(VERSION_DEV)

.PHONY: docker-build-minikube
docker-build-minikube:
	@eval $$(minikube docker-env);\
	docker build -t $(SERVICE_IMAGE_DEV) .

.PHONY: docker-build-kind
docker-build-kind:
	docker build -t $(SERVICE_IMAGE_DEV) .
	kind load docker-image $(SERVICE_IMAGE_DEV)

.PHONY: docker-build-prod
docker-build-prod:
	docker build -t $(SERVICE_IMAGE) .

.PHONY: docker-push
docker-push:
	docker push $(SERVICE_IMAGE)

.PHONY: docker-build-push
docker-build-push: docker-build-prod docker-push

.PHONY: helm
helm:
	helm install bb-noti deploy/bb-noti

.PHONY: helm-del
helm-del:
	helm uninstall bb-noti

# ------------------------ Build ------------------------------------

# ------------------------ Lint/Test ------------------------------------
.PHONY: test
test:
	go test -count=1 -race ./...

.PHONY: lint
lint:
	golangci-lint run --config .golangci.yml --verbose

# ------------------------ Lint/Test ------------------------------------

# ------------------------ Nginx Controller ------------------------------------
.PHONY: install-nginx
install-nginx:
	helm upgrade --install ingress-nginx ingress-nginx \
  		--repo https://kubernetes.github.io/ingress-nginx \
  		--namespace ingress-nginx --create-namespace

.PHONY: uninstall-nginx
uninstall-nginx:
	helm uninstall ingress-nginx -n ingress-nginx
# ------------------------ Nginx Controller ------------------------------------