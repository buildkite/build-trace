package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/buildkite/cli/graphql"
	"github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-lib/metrics"

	opentracing "github.com/opentracing/opentracing-go"
	jaegercfg "github.com/uber/jaeger-client-go/config"
	jaegerlog "github.com/uber/jaeger-client-go/log"
)

func main() {
	buildSlug := flag.String("slug", "", "The slug of the build, e.g acme-inc/my-pipeline/123")
	token := flag.String("token", "", "A graphql token")
	flag.Parse()

	pipelineSlug := filepath.Dir(*buildSlug)
	service := &pipelineSlug

	client, err := graphql.NewClient(*token)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Fetching jobs for %s", *buildSlug)
	t := time.Now()

	build, err := findBuild(client, *buildSlug)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("Found build %s, with %d jobs in %v", build.UUID, len(build.Jobs), time.Since(t))

	buildTracer, buildCloser, err := newTracer(*service)
	if err != nil {
		log.Fatal(err)
	}
	defer buildCloser.Close()

	if build.FinishedAt == nil {
		log.Printf("Not tracing, build not finished")
		os.Exit(1)
	}

	buildSpan := buildTracer.StartSpan(
		fmt.Sprintf("Build %s", build.UUID),
		opentracing.StartTime(*build.ScheduledAt),
		opentracing.Tag{Key: "URL", Value: build.URL},
	)

	for _, job := range build.Jobs {
		if job.WaitJob != nil {
			log.Printf("Found a wait, moving to next group")
		} else if job.CommandJob != nil {
			jobTracer, jobCloser, err := newTracer(job.CommandJob.Label)
			if err != nil {
				log.Fatal(err)
			}

			jobSpan := jobTracer.StartSpan(
				`Execute`,
				opentracing.ChildOf(buildSpan.Context()),
				opentracing.StartTime(*job.CommandJob.StartedAt),
				opentracing.Tag{Key: "UUID", Value: job.UUID},
			)

			jobSpan.FinishWithOptions(opentracing.FinishOptions{
				FinishTime: *job.CommandJob.FinishedAt,
			})

			jobCloser.Close()
		}
	}

	buildSpan.FinishWithOptions(opentracing.FinishOptions{
		FinishTime: *build.FinishedAt,
	})
}

func newTracer(service string) (opentracing.Tracer, io.Closer, error) {
	cfg := jaegercfg.Configuration{
		ServiceName: service,
		Sampler: &jaegercfg.SamplerConfig{
			Type:  jaeger.SamplerTypeConst,
			Param: 1,
		},
		Reporter: &jaegercfg.ReporterConfig{
			LogSpans: true,
		},
	}
	return cfg.NewTracer(
		jaegercfg.Logger(jaegerlog.StdLogger),
		jaegercfg.Metrics(metrics.NullFactory),
	)
}

type build struct {
	UUID        string
	URL         string
	CreatedAt   *time.Time
	ScheduledAt *time.Time
	StartedAt   *time.Time
	FinishedAt  *time.Time
	Jobs        []job
}

func (b *build) UnmarshalJSON(j []byte) error {
	var parsed struct {
		UUID        string        `json:"uuid"`
		URL         string        `json:"url"`
		CreatedAt   *jsonDateTime `json:"createdAt"`
		ScheduledAt *jsonDateTime `json:"scheduledAt"`
		StartedAt   *jsonDateTime `json:"startedAt"`
		FinishedAt  *jsonDateTime `json:"finishedAt"`
		Jobs        struct {
			Edges []struct {
				Node *json.RawMessage `json:"node"`
			} `json:"edges"`
		} `json:"jobs"`
	}

	err := json.Unmarshal(j, &parsed)
	if err != nil {
		return err
	}

	b.UUID = parsed.UUID
	b.URL = parsed.URL
	b.CreatedAt = parsed.CreatedAt.NilableTime()
	b.ScheduledAt = parsed.ScheduledAt.NilableTime()
	b.StartedAt = parsed.StartedAt.NilableTime()
	b.FinishedAt = parsed.FinishedAt.NilableTime()

	jobs := []job{}

	for _, edge := range parsed.Jobs.Edges {
		var typedJob struct {
			UUID string `json:"uuid"`
			Type string `json:"__typename"`
		}
		if err := json.Unmarshal(*edge.Node, &typedJob); err != nil {
			return err
		}

		switch typedJob.Type {
		case `JobTypeCommand`:
			var parsedJob struct {
				Label      string        `json:"label"`
				CreatedAt  *jsonDateTime `json:"createdAt"`
				RunnableAt *jsonDateTime `json:"runnableAt"`
				StartedAt  *jsonDateTime `json:"startedAt"`
				FinishedAt *jsonDateTime `json:"finishedAt"`
				State      string        `json:"state"`
				Command    string        `json:"command"`
			}
			if err := json.Unmarshal(*edge.Node, &parsedJob); err != nil {
				return err
			}

			if (parsedJob.State != "SKIPPED") {
				jobs = append(jobs, job{
					UUID: typedJob.UUID,
					CommandJob: &commandJob{
						CreatedAt:  parsedJob.CreatedAt.NilableTime(),
						RunnableAt: parsedJob.RunnableAt.NilableTime(),
						StartedAt:  parsedJob.StartedAt.NilableTime(),
						FinishedAt: parsedJob.FinishedAt.NilableTime(),
						State:      parsedJob.State,
						Command:    parsedJob.Command,
						Label:      parsedJob.Label,
					},
				})
			}

		case `JobTypeWait`:
			var parsedJob struct {
				State string `json:"state"`
			}
			if err := json.Unmarshal(*edge.Node, &parsedJob); err != nil {
				return err
			}

			b.Jobs = append(b.Jobs, job{
				UUID: typedJob.UUID,
				WaitJob: &waitJob{
					State: parsedJob.State,
				},
			})

		default:
			log.Printf("Unhandled job type %s", typedJob.Type)
		}
	}

	// reverse jobs for chronological order
	for i, j := 0, len(jobs)-1; i < j; i, j = i+1, j-1 {
		jobs[i], jobs[j] = jobs[j], jobs[i]
	}

	b.Jobs = jobs

	return nil
}

type job struct {
	UUID       string
	CommandJob *commandJob
	WaitJob    *waitJob
}

type waitJob struct {
	State string
}

type commandJob struct {
	State      string
	Command    string
	Label      string
	CreatedAt  *time.Time
	RunnableAt *time.Time
	StartedAt  *time.Time
	FinishedAt *time.Time
}

func findBuild(client *graphql.Client, slug string) (build, error) {
	resp, err := client.Do(`
	query JobsForBuild($slug: ID!) {
		build(slug: $slug) {
		  uuid
		  createdAt
		  scheduledAt
		  startedAt
		  finishedAt
		  url
		  jobs(first: 500) {
			edges {
			  node {
				 __typename
				... on JobTypeCommand {
				  uuid
				  command
				  label
				  createdAt
				  scheduledAt
				  runnableAt
				  startedAt
				  finishedAt
				  state
				}
				... on JobTypeWait {
				  uuid
				  state
				}
				... on JobTypeBlock {
				  unblockedAt
				}
				... on JobTypeTrigger {
				  triggered {
					number
				  }
				}
			  }
			}
		  }
		}
	  }
	`, map[string]interface{}{
		`slug`: slug,
	})
	if err != nil {
		return build{}, err
	}

	var parsed struct {
		Data struct {
			Build build `json:"build"`
		} `json:"data"`
	}

	if err = resp.DecodeInto(&parsed); err != nil {
		return build{}, fmt.Errorf("Failed to parse GraphQL response: %v", err)
	}

	return parsed.Data.Build, nil
}

type jsonDateTime time.Time

func (t *jsonDateTime) UnmarshalJSON(j []byte) error {
	var s string

	err := json.Unmarshal(j, &s)
	if err != nil {
		return err
	}

	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return err
	}

	*t = jsonDateTime(ts)
	return nil
}

func (t *jsonDateTime) NilableTime() *time.Time {
	if t == nil {
		return nil
	}
	tt := time.Time(*t)
	return &tt
}
