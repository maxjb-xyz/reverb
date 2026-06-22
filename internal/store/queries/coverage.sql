-- name: GetArtistExternalMap :one
SELECT * FROM artist_external_map WHERE library_artist_id = ? AND source = ?;

-- name: UpsertArtistExternalMap :exec
INSERT INTO artist_external_map (library_artist_id, source, external_artist_id, confidence, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(library_artist_id, source) DO UPDATE SET
  external_artist_id = excluded.external_artist_id, confidence = excluded.confidence;

-- name: GetDiscographyCache :one
SELECT * FROM discography_cache WHERE source = ? AND external_artist_id = ?;

-- name: UpsertDiscographyCache :exec
INSERT INTO discography_cache (source, external_artist_id, albums_json, fetched_at)
VALUES (?, ?, ?, ?)
ON CONFLICT(source, external_artist_id) DO UPDATE SET
  albums_json = excluded.albums_json, fetched_at = excluded.fetched_at;

-- name: GetAlbumExternalMap :one
SELECT * FROM album_external_map WHERE library_album_id = ? AND source = ?;

-- name: UpsertAlbumExternalMap :exec
INSERT INTO album_external_map (library_album_id, source, external_album_id, confidence, created_at)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(library_album_id, source) DO UPDATE SET
  external_album_id = excluded.external_album_id, confidence = excluded.confidence;

-- name: GetAlbumCoverage :one
SELECT * FROM album_coverage WHERE source = ? AND external_album_id = ?;

-- name: UpsertAlbumCoverage :exec
INSERT INTO album_coverage (source, external_album_id, coverage_json, library_album_id, library_version, fetched_at)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(source, external_album_id) DO UPDATE SET
  coverage_json = excluded.coverage_json, library_album_id = excluded.library_album_id,
  library_version = excluded.library_version, fetched_at = excluded.fetched_at;
