PHONY: all build

build:
	GOOS=linux GOARCH=arm go build -o netint_vpu_exporter_arm cmd/main.go
	GOOS=linux GOARCH=amd64 go build -o netint_vpu_exporter_x64 cmd/main.go
	chmod +x netint_vpu_exporter_arm
	chmod +x netint_vpu_exporter_x64

deployt:
	make build
	ssh root@10.10.40.131 -p12500 "rm -rf /srv/production/netint_vpu_exporter/netint_vpu_exporter_x64"
	scp -P 12500 ./netint_vpu_exporter_x64 root@10.10.40.131:/srv/production/netint_vpu_exporter/
	ssh root@10.10.40.131 -p12500 "cd /srv/production/netint_vpu_exporter/ && ./netint_vpu_exporter_x64"
	