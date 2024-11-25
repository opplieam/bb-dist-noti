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