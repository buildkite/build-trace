run: build
	./build-trace --slug "$(BUILDKITE_BUILD_SLUG)" --token "$(BUILDKITE_TOKEN)"

build:
	go build
