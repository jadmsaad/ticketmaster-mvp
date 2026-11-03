-- name: ListEvents :many
SELECT id, name, venue, starts_at
FROM events
ORDER BY starts_at, id;

-- name: GetEvent :one
SELECT id, name, venue, starts_at
FROM events
WHERE id = $1;
