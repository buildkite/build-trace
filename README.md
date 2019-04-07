# Buildkite Build Trace

This is a CLI tool for generating OpenTracing traces of a build and the jobs that are in it. It's useful for troubleshooting what is slow in a build.

## Installing

```bash
go get -u github.com/buildkite/build-trace
```

## Running Jaeger

To run Jaeger locally, you can use docker:

```
docker run -d --name jaeger \
  -p 6831:6831/udp -p 16686:16686 jaegertracing/all-in-one:1.11
```

## Running a Trace

```
build-trace --slug "buildkite/agent/2963" --token "$GRAPHQL_TOKEN"
```
