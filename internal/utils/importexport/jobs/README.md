# Import/Export Jobs

`jobs.Manager` owns a persistent PostgreSQL queue, status history, workspace
serialization and archive retention. A domain service provides the actual
export serializer and import applier through `Handler` and supplies an
`Archive` adapter for local disk, S3 or MinIO.

Call `Bootstrap` during service bootstrap, create one manager per domain
service, and call `Start` from its lifecycle worker manager. Each job is
scoped by `service` and a canonical workspace UUID. The database allows only
one queued or processing job per pair.

Workers receive a unique default ID and every processing lease is fenced with
a unique token. Lease and cleanup-claim expiry use PostgreSQL's clock. `Start`
is idempotent for a manager instance; archive cleanup claims rows before
deleting and only clears the matching claim.

Export handlers return a ZIP stream. Import handlers receive the previously
stored ZIP stream. Archives are removed after the configured retention period;
the job and transition history remain available.
