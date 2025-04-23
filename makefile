PHONY: all build

build:
	GOOS=linux GOARCH=arm go build -o netint_vpu_exporter_arm cmd/main.go
	GOOS=linux GOARCH=amd64 go build -o netint_vpu_exporter_x64 cmd/main.go
	chmod +x netint_vpu_exporter_arm
	chmod +x netint_vpu_exporter_x64