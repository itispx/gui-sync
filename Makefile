build-image:
	docker build -t gui-sync .

run-container:
	docker run --name gui-sync-container gui-sync

copy-build:
	docker cp gui-sync-container:/build build

clear-container:
	docker rm gui-sync-container

compile: build-image run-container copy-build clear-container
	@echo "Build completed and binaries copied to 'build/'"