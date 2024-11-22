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