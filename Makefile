.PHONY: test integration experiment check

test:
	go test -race -count=1 ./...

check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race -count=1 ./...

integration:
	./scripts/experiment.sh test

experiment:
	./scripts/experiment.sh
