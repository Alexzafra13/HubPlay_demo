-- Media stream tracks (video, audio, subtitle) per item.
--
-- Table schema: migrations/sqlite/001_initial_schema.sql (CREATE TABLE media_streams).
-- PK: (item_id, stream_index).

-- name: DeleteMediaStreamsByItem :exec
DELETE FROM media_streams WHERE item_id = $1;

-- name: InsertMediaStream :exec
INSERT INTO media_streams (
    item_id, stream_index, stream_type, codec, profile, bitrate,
    width, height, frame_rate, hdr_type, color_space, channels, sample_rate,
    language, title, is_default, is_forced, is_hearing_impaired
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18);

-- name: ListMediaStreamsByItem :many
SELECT item_id, stream_index, stream_type,
       COALESCE(codec, '') AS codec,
       COALESCE(profile, '') AS profile,
       COALESCE(bitrate, 0) AS bitrate,
       COALESCE(width, 0) AS width,
       COALESCE(height, 0) AS height,
       COALESCE(frame_rate, 0) AS frame_rate,
       COALESCE(hdr_type, '') AS hdr_type,
       COALESCE(color_space, '') AS color_space,
       COALESCE(channels, 0) AS channels,
       COALESCE(sample_rate, 0) AS sample_rate,
       COALESCE(language, '') AS language,
       COALESCE(title, '') AS title,
       COALESCE(is_default, FALSE) AS is_default,
       COALESCE(is_forced, FALSE) AS is_forced,
       COALESCE(is_hearing_impaired, FALSE) AS is_hearing_impaired
FROM media_streams
WHERE item_id = $1
ORDER BY stream_index;
