.PHONY: test integration experiment check frontend-check dashboard cli offline

test:
	go test -race -count=1 ./...

check:
	sh -n scripts/experiment.sh scripts/test-wrapper.sh scripts/dashboard.sh
	sh scripts/test-wrapper.sh
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race -count=1 ./...

integration:
	./scripts/experiment.sh test

experiment:
	./scripts/experiment.sh

frontend-check:
	npm --prefix web ci --ignore-scripts
	npm --prefix web run format:check
	npm --prefix web test
	npm --prefix web run build

dashboard:
	./scripts/dashboard.sh

cli:
	mkdir -p artifacts/bin
	CGO_ENABLED=1 go build -trimpath -o artifacts/bin/faultline ./cmd/faultline

offline:
	mkdir -p artifacts/bin
	CGO_ENABLED=0 go build -trimpath -o artifacts/bin/faultline-bundle ./cmd/faultline-bundle
