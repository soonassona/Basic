package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// JobPublishFailures counts publish-after-commit failures from
// POST /v1/jobs. Labelled by org_id (per-tenant alerting per spec §13)
// and reason for triage. Phase 6 adds the /metrics endpoint that scrapes
// this counter; it is registered against promauto.DefaultRegisterer now
// so no code change is needed at that point.
var JobPublishFailures = promauto.NewCounterVec(
	prometheus.CounterOpts{
		Name: "job_publish_failures_total",
		Help: "Number of POST /v1/jobs publishes that failed after the row was committed.",
	},
	[]string{"org_id", "reason"},
)
