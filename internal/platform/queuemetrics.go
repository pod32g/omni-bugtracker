package platform

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

// The gauges you would actually page on.
//
// The counters in NewMetrics answer "how much happened", which is useful after the
// fact and useless while something is going wrong: a queue that has stopped draining
// increments nothing, and neither does a connection pool that has run out. Both look
// exactly like an idle system.
//
// These are registered as a custom collector rather than as gauges some caller has to
// remember to set, because a gauge that is only correct when somebody updates it is
// the same trap one level down. Collect runs at scrape time and reads the current
// state.

// RegisterQueueMetrics adds River queue-depth and pgx pool gauges to the registry.
func RegisterQueueMetrics(reg prometheus.Registerer, pool *pgxpool.Pool) {
	reg.MustRegister(&queueCollector{pool: pool})
}

type queueCollector struct {
	pool *pgxpool.Pool
}

var (
	queueDepth = prometheus.NewDesc(
		"bugtracker_job_queue_depth",
		"Jobs waiting to run, by queue and state. `available` growing means the "+
			"workers are not keeping up; `retryable` growing means they are failing.",
		[]string{"queue", "state"}, nil,
	)
	queueOldest = prometheus.NewDesc(
		"bugtracker_job_queue_oldest_seconds",
		"Age of the oldest runnable job, by queue. This is the one to alert on: depth "+
			"can be large and healthy, but latency cannot.",
		[]string{"queue"}, nil,
	)
	poolConns = prometheus.NewDesc(
		"bugtracker_db_pool_connections",
		"pgx pool connections by state. acquired == max means every query is queueing.",
		[]string{"state"}, nil,
	)
)

func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- queueDepth
	ch <- queueOldest
	ch <- poolConns
}

func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	stat := c.pool.Stat()
	for state, v := range map[string]float64{
		"acquired":     float64(stat.AcquiredConns()),
		"idle":         float64(stat.IdleConns()),
		"total":        float64(stat.TotalConns()),
		"max":          float64(stat.MaxConns()),
		"constructing": float64(stat.ConstructingConns()),
	} {
		ch <- prometheus.MustNewConstMetric(poolConns, prometheus.GaugeValue, v, state)
	}

	// A scrape must not hang on a wedged database — that would turn "the queue is
	// stuck" into "monitoring is down", which is strictly less useful.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	rows, err := c.pool.Query(ctx, `
		SELECT queue, state::text, count(*),
		       COALESCE(EXTRACT(EPOCH FROM (now() - min(scheduled_at))), 0)
		  FROM river_job
		 WHERE state IN ('available', 'retryable', 'running')
		 GROUP BY queue, state`)
	if err != nil {
		return // reported as absent series rather than a failed scrape
	}
	defer rows.Close()

	oldest := map[string]float64{}
	for rows.Next() {
		var queue, state string
		var count, age float64
		if err := rows.Scan(&queue, &state, &count, &age); err != nil {
			return
		}
		ch <- prometheus.MustNewConstMetric(queueDepth, prometheus.GaugeValue, count, queue, state)
		// Running jobs are not "waiting", so they do not count towards latency.
		if state != "running" && age > oldest[queue] {
			oldest[queue] = age
		}
	}
	for queue, age := range oldest {
		ch <- prometheus.MustNewConstMetric(queueOldest, prometheus.GaugeValue, age, queue)
	}
}
