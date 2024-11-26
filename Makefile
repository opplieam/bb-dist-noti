# ------------------------ Proto Start ------------------------
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

# ------------------------ Proto End ------------------------

# ------------------------ TLS Start ------------------------
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
# ------------------------ TLS End --------------------------

# ------------------------ Run Node Locally Start --------------------------

.PHONY: rm-data-dir
rm-data-dir:
	rm -rf ./dev-data

.PHONY: run-node-1
run-node-1:
	go run cmd/noti/main.go \
		--data-dir=./dev-data/node1 \
		--node-name=node1 \
		--serf-addr=127.0.0.1:8401 \
		--rpc-port=8400 \
		--bootstrap=true \
		--start-join-addrs=127.0.0.1:8402

.PHONY: run-node-2
run-node-2:
	go run cmd/noti/main.go \
		--data-dir=./dev-data/node2 \
		--node-name=node2 \
		--serf-addr=127.0.0.1:8501 \
		--rpc-port=8500 \
		--start-join-addrs=127.0.0.1:8401

.PHONY: run-node-3
run-node-3:
	go run cmd/noti/main.go \
		--data-dir=./dev-data/node3 \
		--node-name=node3 \
		--serf-addr=127.0.0.1:8601 \
		--rpc-port=8600 \
		--start-join-addrs=127.0.0.1:8401
# ------------------------ Run Node Locally End ----------------------------