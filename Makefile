# bag — multicall binary of memory-safe drop-in CLI tools.
#
# All build targets pin CGO_ENABLED=0 so the result is a static binary
# with no native dependencies. -trimpath strips local paths.

GO        ?= go
BINARY    ?= bag
VERSION   ?= dev
LDFLAGS   ?= -s -w -X main.version=$(VERSION)
DISTROS   ?= ubuntu debian alpine fedora

.PHONY: all
all: build

.PHONY: build
build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./

.PHONY: install-symlinks
install-symlinks: build
	@for tool in curl wget; do \
		ln -sf $(BINARY) $$tool; \
		echo "linked $$tool -> $(BINARY)"; \
	done

.PHONY: test
test:
	$(GO) test -count=1 -race ./...

.PHONY: test-short
test-short:
	$(GO) test -count=1 -short ./...

.PHONY: vet
vet:
	$(GO) vet ./...

.PHONY: lint-no-unsafe
lint-no-unsafe:
	@if grep -RIn --include='*.go' -E '"unsafe"|cgo' . | grep -v _test.go | grep -v vendor/ ; then \
		echo "FAIL: unsafe / cgo references found"; exit 1; \
	else \
		echo "OK: no unsafe / cgo"; \
	fi

.PHONY: docker-test
docker-test:
	@for d in $(DISTROS); do \
		echo "=== $$d ==="; \
		docker build -f test/docker/Dockerfile.$$d -t bag-test-$$d . && \
		docker run --rm bag-test-$$d || exit 1; \
	done

.PHONY: clean
clean:
	rm -f $(BINARY) curl wget
	rm -rf dist/
