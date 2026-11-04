-- name: LockCatalog :exec
SELECT pg_advisory_xact_lock(hashtextextended('collections:catalog:' || sqlc.arg(workspace_id)::text,0));
-- name: LockCatalogShared :exec
SELECT pg_advisory_xact_lock_shared(hashtextextended('collections:catalog:' || sqlc.arg(workspace_id)::text,0));
-- name: LockOwner :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(owner_scope)::text,0));
-- name: ListCollections :many
SELECT * FROM collections_collection WHERE workspace_id=$1 ORDER BY position,key LIMIT $2 OFFSET $3;
-- name: GetCollection :one
SELECT * FROM collections_collection WHERE workspace_id=$1 AND key=$2;
-- name: ListLocalizations :many
SELECT * FROM collections_localization WHERE workspace_id=$1 AND collection_key=ANY(sqlc.arg(keys)::text[]) ORDER BY collection_key,locale;
-- name: ListEntries :many
SELECT * FROM collections_entry WHERE workspace_id=$1 AND collection_key=ANY(sqlc.arg(keys)::text[]) AND NOT deleted ORDER BY collection_key,position,item_key;
-- name: CreateCollection :execrows
INSERT INTO collections_collection(workspace_id,key,position,active,archived) VALUES($1,$2,$3,$4,$5) ON CONFLICT DO NOTHING;
-- name: SaveCollection :exec
INSERT INTO collections_collection(workspace_id,key,position,active,archived) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(workspace_id,key) DO UPDATE SET position=EXCLUDED.position,active=EXCLUDED.active,archived=EXCLUDED.archived;
-- name: ArchiveCollection :execrows
UPDATE collections_collection SET archived=true WHERE workspace_id=$1 AND key=$2;
-- name: ClearLocalizations :exec
DELETE FROM collections_localization WHERE workspace_id=$1 AND collection_key=$2;
-- name: PutLocalization :exec
INSERT INTO collections_localization(workspace_id,collection_key,locale,title,description) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(workspace_id,collection_key,locale) DO UPDATE SET title=EXCLUDED.title,description=EXCLUDED.description;
-- name: PutEntry :exec
INSERT INTO collections_entry(workspace_id,collection_key,item_key,position,active) VALUES($1,$2,$3,$4,$5)
ON CONFLICT(workspace_id,collection_key,item_key) DO UPDATE SET position=EXCLUDED.position,active=EXCLUDED.active,deleted=false;
-- name: RemoveEntry :execrows
UPDATE collections_entry SET deleted=true WHERE workspace_id=$1 AND collection_key=$2 AND item_key=$3 AND NOT deleted;
-- name: ClearEntries :exec
UPDATE collections_entry SET deleted=true WHERE workspace_id=$1 AND collection_key=$2;
-- name: HasItems :many
SELECT requested.collection_key::text AS collection_key,requested.item_key::text AS item_key,
 COALESCE(o.owned AND c.active AND NOT c.archived AND e.active AND NOT e.deleted,false)::boolean AS owned,
 COALESCE(o.revision,0)::bigint AS revision,o.changed_at
FROM (SELECT unnest(sqlc.arg(collection_keys)::text[]) AS collection_key, unnest(sqlc.arg(item_keys)::text[]) AS item_key) AS requested
LEFT JOIN collections_collection c ON c.workspace_id=sqlc.arg(workspace_id) AND c.key=requested.collection_key
LEFT JOIN collections_entry e ON e.workspace_id=c.workspace_id AND e.collection_key=c.key AND e.item_key=requested.item_key
LEFT JOIN collections_ownership o ON o.workspace_id=c.workspace_id AND o.collection_key=c.key AND o.item_key=requested.item_key
 AND o.app_id=sqlc.arg(app_id) AND o.platform_id=sqlc.arg(platform_id) AND o.platform_user_id=sqlc.arg(platform_user_id);
-- name: ListOwned :many
SELECT o.* FROM collections_ownership o
JOIN collections_collection c ON c.workspace_id=o.workspace_id AND c.key=o.collection_key
JOIN collections_entry e ON e.workspace_id=o.workspace_id AND e.collection_key=o.collection_key AND e.item_key=o.item_key
WHERE o.workspace_id=$1 AND o.app_id=$2 AND o.platform_id=$3 AND o.platform_user_id=$4
 AND o.owned AND c.active AND NOT c.archived AND e.active AND NOT e.deleted
 AND (COALESCE(cardinality(sqlc.arg(keys)::text[]),0)=0 OR o.collection_key=ANY(sqlc.arg(keys)::text[]))
ORDER BY o.collection_key,o.item_key LIMIT $5 OFFSET $6;
-- name: GetReceipt :one
SELECT * FROM collections_operation WHERE workspace_id=$1 AND app_id=$2 AND platform_id=$3 AND platform_user_id=$4 AND source=$5 AND operation_id=$6;
-- name: PutReceipt :exec
INSERT INTO collections_operation(id,workspace_id,app_id,platform_id,platform_user_id,source,operation_id,fingerprint,result,actor_id,reason,created_at)
VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12);
-- name: ListJournal :many
SELECT * FROM collections_operation WHERE workspace_id=$1
 AND (sqlc.arg(app_id)::bigint=0 OR (app_id=sqlc.arg(app_id) AND platform_id=sqlc.arg(platform_id) AND platform_user_id=sqlc.arg(platform_user_id)))
ORDER BY created_at DESC,id LIMIT $2 OFFSET $3;
-- name: CatalogAudit :exec
INSERT INTO collections_catalog_audit(workspace_id,actor_id,method,payload) VALUES($1,$2,$3,$4);
-- name: GetStats :one
SELECT
 (SELECT count(*) FROM collections_ownership s WHERE s.workspace_id=sqlc.arg(workspace_id) AND s.owned AND (sqlc.arg(collection_key)::text='' OR s.collection_key=sqlc.arg(collection_key)))::bigint AS stored_owned,
 (SELECT count(*) FROM collections_ownership o JOIN collections_collection c ON c.workspace_id=o.workspace_id AND c.key=o.collection_key JOIN collections_entry e ON e.workspace_id=o.workspace_id AND e.collection_key=o.collection_key AND e.item_key=o.item_key WHERE o.workspace_id=sqlc.arg(workspace_id) AND o.owned AND c.active AND NOT c.archived AND e.active AND NOT e.deleted AND (sqlc.arg(collection_key)::text='' OR o.collection_key=sqlc.arg(collection_key)))::bigint AS available_owned,
 count(*) FILTER(WHERE ch.after_owned)::bigint AS grants,count(*) FILTER(WHERE NOT ch.after_owned)::bigint AS revokes
FROM collections_change ch WHERE ch.workspace_id=sqlc.arg(workspace_id) AND (sqlc.arg(collection_key)::text='' OR ch.collection_key=sqlc.arg(collection_key));
-- name: RefreshDailyStats :exec
INSERT INTO collections_stats_daily(workspace_id,stats_date,grants,revokes)
SELECT ch.workspace_id,(ch.occurred_at AT TIME ZONE 'UTC')::date,count(*) FILTER(WHERE ch.after_owned),count(*) FILTER(WHERE NOT ch.after_owned)
FROM collections_change ch WHERE ch.workspace_id=$1 AND ch.occurred_at >= $2 AND ch.occurred_at < $3
GROUP BY ch.workspace_id,(ch.occurred_at AT TIME ZONE 'UTC')::date
ON CONFLICT(workspace_id,stats_date) DO UPDATE SET grants=EXCLUDED.grants,revokes=EXCLUDED.revokes;
-- name: ListDailyStats :many
SELECT * FROM collections_stats_daily WHERE workspace_id=$1 AND stats_date >= $2 AND stats_date < $3 ORDER BY stats_date LIMIT $4 OFFSET $5;
-- name: ExportFootprint :one
SELECT count(*)::bigint AS records,COALESCE(sum(bytes),0)::bigint AS bytes FROM (
SELECT octet_length(row_to_json(t)::text) AS bytes FROM collections_collection t WHERE t.workspace_id=$1
UNION ALL SELECT octet_length(row_to_json(t)::text) FROM collections_localization t WHERE t.workspace_id=$1
UNION ALL SELECT octet_length(row_to_json(t)::text) FROM collections_entry t WHERE t.workspace_id=$1
) footprint;

-- name: BatchStates :many
SELECT requested.collection_key::text AS collection_key,requested.item_key::text AS item_key,
 COALESCE(o.owned,false)::boolean AS owned,COALESCE(o.revision,0)::bigint AS revision,o.changed_at,
 COALESCE(c.active AND NOT c.archived AND e.active AND NOT e.deleted,false)::boolean AS available
FROM (SELECT unnest(sqlc.arg(collection_keys)::text[]) AS collection_key,unnest(sqlc.arg(item_keys)::text[]) AS item_key) requested
LEFT JOIN collections_collection c ON c.workspace_id=sqlc.arg(workspace_id) AND c.key=requested.collection_key
LEFT JOIN collections_entry e ON e.workspace_id=c.workspace_id AND e.collection_key=c.key AND e.item_key=requested.item_key
LEFT JOIN collections_ownership o ON o.workspace_id=c.workspace_id AND o.collection_key=c.key AND o.item_key=requested.item_key AND o.app_id=sqlc.arg(app_id) AND o.platform_id=sqlc.arg(platform_id) AND o.platform_user_id=sqlc.arg(platform_user_id);
-- name: ListPublishedCollections :many
SELECT * FROM collections_collection WHERE workspace_id=$1 AND active AND NOT archived ORDER BY position,key LIMIT $2 OFFSET $3;
-- name: ListPublishedEntries :many
SELECT * FROM collections_entry WHERE workspace_id=$1 AND collection_key=$2 AND active AND NOT deleted ORDER BY position,item_key LIMIT $3 OFFSET $4;
-- name: ImportKeys :many
SELECT key FROM collections_collection WHERE workspace_id=$1;
-- name: ClearImportLocalizations :exec
DELETE FROM collections_localization WHERE workspace_id=$1 AND collection_key=ANY(sqlc.arg(keys)::text[]);
-- name: ClearImportEntries :exec
UPDATE collections_entry SET deleted=true WHERE workspace_id=$1 AND collection_key=ANY(sqlc.arg(keys)::text[]);

-- name: SetOwnershipBatch :exec
INSERT INTO collections_ownership(workspace_id,app_id,platform_id,platform_user_id,collection_key,item_key,owned,revision,changed_at)
SELECT sqlc.arg(workspace_id),sqlc.arg(app_id),sqlc.arg(platform_id),sqlc.arg(platform_user_id),
 unnest(sqlc.arg(collection_keys)::text[]),unnest(sqlc.arg(item_keys)::text[]),unnest(sqlc.arg(owned_values)::boolean[]),unnest(sqlc.arg(revisions)::bigint[]),sqlc.arg(changed_at)
ON CONFLICT(workspace_id,app_id,platform_id,platform_user_id,collection_key,item_key)
DO UPDATE SET owned=EXCLUDED.owned,revision=EXCLUDED.revision,changed_at=EXCLUDED.changed_at;
-- name: PutChangeBatch :exec
INSERT INTO collections_change(receipt_id,workspace_id,collection_key,item_key,before_owned,after_owned,revision,occurred_at)
SELECT sqlc.arg(receipt_id),sqlc.arg(workspace_id),unnest(sqlc.arg(collection_keys)::text[]),unnest(sqlc.arg(item_keys)::text[]),unnest(sqlc.arg(before_values)::boolean[]),unnest(sqlc.arg(after_values)::boolean[]),unnest(sqlc.arg(revisions)::bigint[]),sqlc.arg(occurred_at);
-- name: PutCallbackBatch :exec
INSERT INTO collections_clb_event(workspace_id,routing_key,source_service,event_type,event_key,idempotency_key,payload)
SELECT sqlc.arg(workspace_id),sqlc.arg(routing_key),'collections','collections.ownership_changed',r.key,r.key,convert_to(r.payload,'UTF8')
FROM (SELECT unnest(sqlc.arg(event_keys)::text[]) AS key,unnest(sqlc.arg(payloads)::text[]) AS payload) r;
-- name: ListEntryPage :many
SELECT * FROM collections_entry WHERE workspace_id=$1 AND collection_key=$2 AND NOT deleted ORDER BY position,item_key LIMIT $3 OFFSET $4;

-- name: CountEntries :one
SELECT count(*)::bigint FROM collections_entry WHERE workspace_id=$1 AND collection_key=$2 AND NOT deleted;
-- name: ReorderEntries :execrows
UPDATE collections_entry e SET position=(desired.ordinality-1)::integer
FROM unnest(sqlc.arg(item_keys)::text[]) WITH ORDINALITY AS desired(item_key,ordinality)
WHERE e.workspace_id=sqlc.arg(workspace_id) AND e.collection_key=sqlc.arg(collection_key)
 AND e.item_key=desired.item_key AND NOT e.deleted;
-- name: ListUserLocalizations :many
SELECT * FROM collections_localization WHERE workspace_id=$1 AND collection_key=ANY(sqlc.arg(keys)::text[]) AND locale=$2 ORDER BY collection_key;
-- name: CountOtherEntries :one
SELECT count(*)::bigint FROM collections_entry WHERE workspace_id=$1 AND collection_key=$2 AND item_key<>$3 AND NOT deleted;

-- name: GetPublishedCollection :one
SELECT c.key,c.position,COALESCE((SELECT revision FROM collections_catalog_version WHERE workspace_id=c.workspace_id),0)::bigint AS revision
FROM collections_collection c WHERE c.workspace_id=$1 AND c.key=$2 AND c.active AND NOT c.archived;
-- name: BumpCatalogVersion :exec
INSERT INTO collections_catalog_version(workspace_id,revision) VALUES($1,1)
ON CONFLICT(workspace_id) DO UPDATE SET revision=collections_catalog_version.revision+1;
