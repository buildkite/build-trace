# Buildkite Build Trace

This is a CLI tool for generating OpenTracing traces of a build and the jobs that are in it. It's useful for troubleshooting what is slow in a build.

![](https://lox-screenshots.s3.amazonaws.com/Jaeger_UI_2019-04-08_08-44-34.png)

## Installing

```bash
go get -u github.com/buildkite/build-trace
```

## Running Jaeger

To run [Jaeger](https://www.jaegertracing.io) locally, you can use docker:

```
docker run -d --name jaeger \
  -p 6831:6831/udp -p 16686:16686 jaegertracing/all-in-one:1.11
```

Then you open http://localhost:16686.

## Running a Trace

You will need a GraphQL token from https://buildkite.com/user/api-access-tokens.

```
build-trace --slug "buildkite/agent/2963" --token "$GRAPHQL_TOKEN"
```

Then search for the trace in Jaeger.
