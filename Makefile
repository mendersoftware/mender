GO ?= go
GOFMT ?= gofmt
V ?=
PKGS = $(shell go list ./... | grep -v vendor)
PKGFILES = $(shell find . \( -path ./vendor -o -path ./Godeps \) -prune \
		-o -type f -name '*.go' -print)
PKGFILES_notest = $(shell echo $(PKGFILES) | tr ' ' '\n' | grep -v _test.go)
GOCYCLO ?= 15

CGO_ENABLED=1
export CGO_ENABLED

TOOLS = \
	github.com/fzipp/gocyclo \
	github.com/opennota/check/cmd/varcheck \
	github.com/mendersoftware/deadcode

VERSION = $(shell git describe --tags --always --dirty)

GO_LDFLAGS = \
	-ldflags "-X main.Version=$(VERSION)"

ifeq ($(V),1)
BUILDV = -v
endif

TAGS =
ifeq ($(LOCAL),1)
TAGS += local
endif

ifneq ($(TAGS),)
BUILDTAGS = -tags '$(TAGS)'
endif

build:
	$(GO) build $(GO_LDFLAGS) $(BUILDV) $(BUILDTAGS)

install:
	$(GO) install $(GO_LDFLAGS) $(BUILDV) $(BUILDTAGS)

clean:
	$(GO) clean
	rm -f coverage.out coverage-tmp.out

get-tools:
	for t in $(TOOLS); do \
		echo "-- go getting $$t"; \
		go get -u $$t; \
	done

test:
	$(GO) test -v $(PKGS)

check:
	echo "-- checking if code is gofmt'ed"
	if [ -n "$$($(GOFMT) -d $(PKGFILES))" ]; then \
		echo "-- gofmt check failed"; \
		/bin/false; \
	fi
	echo "-- checking with govet"
	$(GO) tool vet -unsafeptr=false $(PKGFILES_notest)
	echo "-- checking for dead code"
	deadcode -ignore version.go:Version
	echo "-- checking with varcheck"
	varcheck .
	echo "-- checking cyclometric complexity > $(GOCYCLO)"
	gocyclo -over $(GOCYCLO) $(PKGFILES_notest)

cover: coverage
	$(GO) tool cover -func=coverage.out

htmlcover: coverage
	$(GO) tool cover -html=coverage.out

coverage:
	rm -f coverage.out
	echo 'mode: set' > coverage.out
	for p in $(PKGS); do \
		rm -f coverage-tmp.out;  \
		$(GO) test -coverprofile=coverage-tmp.out $$p ; \
		cat coverage-tmp.out |grep -v 'mode:' >> coverage.out; \
	done

.PHONY: build clean get-tools test check \
	cover htmlcover coverage
