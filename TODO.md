# TODO

## Reference resources

- [x] Add `Reference.Resource.List` with pagination for non-deleted resources.
- [x] Add `Reference.Resource.Detach` and `Reference.Resource.ListItemResources`.
- [x] Include active, non-deleted resources in `User.Get`, `User.Resolve`, and `User.List` item responses without N+1 queries.
- [x] Extend item models and user item-read responses so each item returns its attached resources in the same bulk query path.
- [x] Include linked resources in `Admin.GetItem`.
- [x] Define and add `reference.resource.*` workspace permissions to `control`.
- [x] Add PostgreSQL integration tests for create, update, soft-delete, attach, detach, workspace isolation, and cache invalidation across nodes.
- [x] Add resource read/write benchmarks, including warm L1/L2 cache paths.
- [ ] Replace synchronous Reference export/import with an asynchronous ZIP dump job: prepare the archive incrementally, retain it for 24 hours, then allow download.
- [x] Implement batch garbage collection for unreferenced local/S3 media versions: only after 1 hour, bounded work per run, no immediate delete on update or soft-delete.

## Media runtime

- [x] Do not link native Lottie renderers in backend: admin frontend supplies a PNG/WebP first frame; backend validates original media and derives previews/placeholder from that frame.
- [ ] Add integration fixtures for real WebP, Lottie, TGS and SVG inputs.
https://opncd.ai/share/5xYMy6w6
