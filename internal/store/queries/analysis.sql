-- name: ListTracksToAnalyze :many
-- Library songs with no features for the current model, newest first, plus
-- failed ones whose last try is older than retry_before.
SELECT lt.library_track_id, lt.title, lt.artist, lt.suffix
FROM library_tracks lt
LEFT JOIN track_features tf ON tf.library_track_id = lt.library_track_id
WHERE tf.library_track_id IS NULL
   OR (tf.error = '' AND tf.model != sqlc.arg(model))
   OR (tf.error != '' AND tf.analyzed_at < sqlc.arg(retry_before))
ORDER BY lt.added_at DESC, lt.library_track_id
LIMIT sqlc.arg(lim);

-- name: UpsertTrackFeatures :exec
INSERT INTO track_features (library_track_id, model, embedding, tempo, energy, error, analyzed_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(library_track_id) DO UPDATE SET
  model = excluded.model, embedding = excluded.embedding, tempo = excluded.tempo,
  energy = excluded.energy, error = excluded.error, analyzed_at = excluded.analyzed_at;

-- name: ListTrackEmbeddings :many
SELECT tf.library_track_id, tf.embedding, tf.tempo, tf.energy
FROM track_features tf
JOIN library_tracks lt ON lt.library_track_id = tf.library_track_id
WHERE tf.model = ? AND tf.error = '';

-- name: CountTrackFeatures :one
SELECT
  CAST(COALESCE(SUM(CASE WHEN tf.error = '' AND tf.model = sqlc.arg(model) THEN 1 ELSE 0 END), 0) AS INTEGER) AS analyzed,
  CAST(COALESCE(SUM(CASE WHEN tf.error != '' THEN 1 ELSE 0 END), 0) AS INTEGER) AS failed
FROM track_features tf
JOIN library_tracks lt ON lt.library_track_id = tf.library_track_id;

-- name: CountLibraryTracks :one
SELECT COUNT(*) FROM library_tracks;

-- name: DeleteOrphanTrackFeatures :execrows
DELETE FROM track_features
WHERE library_track_id NOT IN (SELECT library_track_id FROM library_tracks);
