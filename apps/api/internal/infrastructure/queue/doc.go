// Package queue owns the RabbitMQ topology, connection, and publisher
// for the async job lifecycle (spec §11, ADR-0004).
//
// Topology declared by Declare(ch):
//
//	exchange jobs       (topic, durable)
//	exchange jobs.dlx   (topic, durable)
//
//	queue jobs.pending  (durable; DLX -> jobs.dlx / jobs.dead)
//	  binding: jobs --[jobs.pending.*]--> jobs.pending
//
//	queue jobs.retry    (durable; DLX -> jobs / jobs.pending.retry)
//	  no binding — published into via the default exchange with a
//	  per-message TTL by the retry policy. On expiry the message is
//	  dead-lettered back into jobs.pending via the topic binding.
//
//	queue jobs.dead     (durable; terminal)
//	  binding: jobs.dlx --[jobs.dead]--> jobs.dead
//
// Anti-pattern guard from spec §18 ("background job declared but never
// triggered"): topology_test.go runs against a real broker via
// testcontainers — declaration is proven by behaviour, not inspection.
package queue
