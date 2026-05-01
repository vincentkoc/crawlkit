.PHONY: test vet tidy check

test:
	go test ./...

vet:
	go vet ./...

tidy:
	go mod tidy

check: tidy vet test
	git diff --exit-code -- go.mod go.sum

