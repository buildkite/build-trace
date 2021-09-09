# Deprecated

> Buildkite no longer maintains build-trace. This repository has been deprecated and is no longer maintained.

# Buildkite Build Trace

🦑🧙🏻‍♂️ Generate a waterfall graph of the Jobs in a Build using our [GraphQL API](https://buildkite.com/docs/apis/graphql-api) and [Jaeger](https://www.jaegertracing.io).

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
