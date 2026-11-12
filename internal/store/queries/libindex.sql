-- name: UpsertLibraryTrack :exec
INSERT INTO library_tracks (library_track_id, title, artist, artist_key, artist_id, album, album_id, cover_art_id,
  track_number, disc_number, duration_ms, bit_rate, suffix, content_type, isrc, year, genres, added_at, epoch)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT (library_track_id) DO UPDATE SET
  title = excluded.title, artist = excluded.artist, artist_key = excluded.artist_key, artist_id = excluded.artist_id,
  album = excluded.album, album_id = excluded.album_id, cover_art_id = excluded.cover_art_id,
  track_number = excluded.track_number, disc_number = excluded.disc_number, duration_ms = excluded.duration_ms,
  bit_rate = excluded.bit_rate, suffix = excluded.suffix, content_type = excluded.content_type, isrc = excluded.isrc,
  year = excluded.year, genres = excluded.genres, added_at = excluded.added_at, epoch = excluded.epoch;

-- name: DeleteLibraryTrackGenres :exec
DELETE FROM library_track_genres WHERE library_track_id = ?;

-- name: InsertLibraryTrackGenre :exec
INSERT OR IGNORE INTO library_track_genres (library_track_id, genre_key) VALUES (?, ?);

-- name: DeleteStaleLibraryTracks :execrows
DELETE FROM library_tracks WHERE epoch < ?;

-- name: DeleteOrphanLibraryTrackGenres :exec
DELETE FROM library_track_genres
WHERE library_track_id NOT IN (SELECT library_track_id FROM library_tracks);

-- name: CountLibraryIndex :one
SELECT COUNT(*) AS tracks, COUNT(DISTINCT artist_key) AS artists FROM library_tracks;

-- name: GetLibraryTrack :one
SELECT * FROM library_tracks WHERE library_track_id = ?;

-- name: ListLibraryArtistKeys :many
SELECT artist_key, CAST(MIN(artist) AS TEXT) AS name, COUNT(*) AS tracks
FROM library_tracks WHERE artist_key != ''
GROUP BY artist_key ORDER BY COUNT(*) DESC;

-- name: ListArtistsNeedingSimilar :many
-- Library artists whose external link for a source is missing or older than
-- the cutoff, biggest artists first so the map is useful early.
SELECT t.artist_key, CAST(MIN(t.artist) AS TEXT) AS name, COUNT(*) AS tracks
FROM library_tracks t
LEFT JOIN artist_external e ON e.artist_key = t.artist_key AND e.source = ?
WHERE t.artist_key != '' AND (e.artist_key IS NULL OR e.fetched_at < ?)
GROUP BY t.artist_key
ORDER BY COUNT(*) DESC
LIMIT ?;

-- name: UpsertArtistExternal :exec
INSERT INTO artist_external (artist_key, source, external_id, name, fetched_at) VALUES (?,?,?,?,?)
ON CONFLICT (artist_key, source) DO UPDATE SET
  external_id = excluded.external_id, name = excluded.name, fetched_at = excluded.fetched_at;

-- name: GetArtistExternal :one
SELECT * FROM artist_external WHERE artist_key = ? AND source = ?;

-- name: DeleteArtistSimilar :exec
DELETE FROM artist_similar WHERE artist_key = ? AND source = ?;

-- name: InsertArtistSimilar :exec
INSERT OR REPLACE INTO artist_similar (artist_key, source, similar_key, similar_name, similar_external_id, similar_image, rank, fetched_at)
VALUES (?,?,?,?,?,?,?,?);

-- name: ListSimilarArtists :many
SELECT s.similar_key, s.similar_name, s.similar_external_id, s.similar_image, s.rank, s.source,
       CAST((SELECT COUNT(*) FROM library_tracks t WHERE t.artist_key = s.similar_key) AS INTEGER) AS owned_tracks,
       CAST(COALESCE((SELECT MIN(t.artist_id) FROM library_tracks t WHERE t.artist_key = s.similar_key), '') AS TEXT) AS library_artist_id
FROM artist_similar s
WHERE s.artist_key = ?
ORDER BY s.rank, s.source;

-- name: CountArtistsWithSimilar :one
SELECT COUNT(DISTINCT artist_key) FROM artist_similar;

-- name: ListLibraryTracksByArtistKeys :many
SELECT * FROM library_tracks WHERE artist_key IN (sqlc.slice('keys'));

-- name: ListLibraryTracksByGenre :many
SELECT t.* FROM library_tracks t
JOIN library_track_genres g ON g.library_track_id = t.library_track_id
WHERE g.genre_key = ?
ORDER BY RANDOM()
LIMIT ?;

-- name: ListLibraryTracksByYearRange :many
SELECT * FROM library_tracks WHERE year >= ? AND year <= ? ORDER BY RANDOM() LIMIT ?;

-- name: ListRecentlyAddedLibraryTracks :many
SELECT * FROM library_tracks WHERE added_at > 0 ORDER BY added_at DESC LIMIT ?;

-- name: ListLibraryGenres :many
SELECT genre_key, COUNT(*) AS tracks FROM library_track_genres
GROUP BY genre_key ORDER BY COUNT(*) DESC LIMIT ?;

-- name: ListLibraryTrackGenreKeys :many
SELECT genre_key FROM library_track_genres WHERE library_track_id = ? ORDER BY genre_key;

-- name: ListLibraryTracksByIDs :many
SELECT * FROM library_tracks WHERE library_track_id IN (sqlc.slice('ids'));

-- name: ListRandomLibraryTracks :many
SELECT * FROM library_tracks ORDER BY RANDOM() LIMIT ?;

-- name: ListGenresForArtistKeys :many
-- The genres a set of artists' tracks carry, most common first: how the
-- engine turns a user's favourite artists into genre stations.
SELECT g.genre_key, COUNT(*) AS tracks
FROM library_track_genres g
JOIN library_tracks t ON t.library_track_id = g.library_track_id
WHERE t.artist_key IN (sqlc.slice('keys'))
GROUP BY g.genre_key ORDER BY COUNT(*) DESC LIMIT ?;
