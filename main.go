package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/christianscott/go-buildkite/buildkite"

	opentracing "github.com/opentracing/opentracing-go"
	"github.com/uber/jaeger-client-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	"github.com/uber/jaeger-lib/metrics"
)

func main() {
	buildSlug := flag.String("slug", "", "The slug of the build, e.g acme-inc/my-pipeline/123")
	token := flag.String("token", "", "A buildkite API token")
	flag.Parse()

	client, err := createBuildkiteClient(*token)
	if err != nil {
		log.Fatal(err)
	}

	traceBuild(client, *buildSlug, nil)
}

var tracedBuilds map[string]bool = make(map[string]bool)

func traceBuild(client *buildkite.Client, buildSlug string, parentSpan *opentracing.Span) {
	if _, ok := tracedBuilds[buildSlug]; ok {
		log.Printf("already traced %s; skipping", buildSlug)
		return
	}
	tracedBuilds[buildSlug] = true

	org, project, id, err := parseBuildSlug(buildSlug)
	if err != nil {
		log.Fatal(err)
	}

	build, res, err := client.Builds.Get(org, project, id, nil)
	if err != nil {
		log.Fatal(err)
	}
	if res.StatusCode != 200 {
		log.Fatalf("could not find build with slug %s (status code %d)", buildSlug, res.StatusCode)
	}

	pipelineSlug := filepath.Dir(buildSlug)
	tracer, closer, err := createTracer(pipelineSlug)
	if err != nil {
		log.Fatal(err)
	}
	defer closer.Close()

	startSpanOptions := []opentracing.StartSpanOption{
		opentracing.StartTime(build.StartedAt.Time),
		opentracing.Tags{"URL": build.WebURL},
	}
	if parentSpan != nil {
		startSpanOptions = append(startSpanOptions, opentracing.ChildOf((*parentSpan).Context()))
	}

	buildSpan := tracer.StartSpan(
		buildSlug,
		startSpanOptions...,
	)
	defer buildSpan.FinishWithOptions(opentracing.FinishOptions{
		FinishTime: build.FinishedAt.Time,
	})

	// json.NewEncoder(os.Stderr).Encode(build)

	for _, job := range build.Jobs {
		if job.State == nil || (*job.State != "passed" && *job.State != "failed") {
			continue
		}

		startSpanOptions := []opentracing.StartSpanOption{
			opentracing.StartTime(job.StartedAt.Time),
			opentracing.ChildOf(buildSpan.Context()),
			opentracing.Tag{Key: "url", Value: job.WebURL},
		}
		if job.Command != nil {
			startSpanOptions = append(startSpanOptions, opentracing.Tag{Key: "command", Value: *job.Command})
		}

		jobSpan := tracer.StartSpan(
			*job.Name,
			startSpanOptions...,
		)

		if job.TriggeredBuild != nil {
			buildSlug, err := getBuildSlugFromURL(*job.TriggeredBuild.Url)
			if err != nil {
				log.Fatal(err)
			}
			traceBuild(client, buildSlug, &jobSpan)
		}

		jobSpan.FinishWithOptions(opentracing.FinishOptions{
			FinishTime: job.FinishedAt.Time,
		})
	}
}

func parseBuildSlug(buildSlug string) (string, string, string, error) {
	parsed := strings.Split(buildSlug, "/")
	if len(parsed) != 3 {
		return "", "", "", fmt.Errorf("could not parse buildSlug '%s'. expected a string with format $org/$project/$id", buildSlug)
	}
	return parsed[0], parsed[1], parsed[2], nil
}

func getBuildSlugFromURL(u string) (string, error) {
	parsedUrl, err := url.Parse(u)
	if err != nil {
		return "", err
	}

	slug := strings.Replace(parsedUrl.Path, "/v2/organizations/", "", 1)
	slug = strings.Replace(slug, "/pipelines", "", 1)
	return strings.Replace(slug, "/builds", "", 1), nil
}

func createBuildkiteClient(token string) (*buildkite.Client, error) {
	config, err := buildkite.NewTokenConfig(token, false)
	if err != nil {
		return nil, err
	}

	return buildkite.NewClient(config.Client()), nil
}

func createTracer(serviceName string) (opentracing.Tracer, io.Closer, error) {
	cfg := jaegercfg.Configuration{
		ServiceName: serviceName,
		Sampler: &jaegercfg.SamplerConfig{
			Type:  jaeger.SamplerTypeConst,
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			LogSpans: true,
		},
	}

	tracer, closer, err := cfg.NewTracer(
		jaegercfg.Logger(jaeger.StdLogger),
		jaegercfg.Metrics(metrics.NullFactory),
	)
	return tracer, closer, err
}
