package uploadstatus

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Queryer interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type DB interface {
	Queryer
	Execer
}

type Counts struct {
	Total      int
	Pending    int
	Uploaded   int
	Processing int
	Processed  int
	Failed     int
	Unexpected int
}

func Recalculate(ctx context.Context, db DB, sessionID string) (string, error) {
	counts, err := CountsForSession(ctx, db, sessionID)
	if err != nil {
		return "", err
	}

	status := StatusFromCounts(counts)
	_, err = db.Exec(ctx, `
		UPDATE upload_sessions
		SET status = $2,
			updated_datetime = now(),
			completed_at = CASE
				WHEN $2 IN ('processed', 'completed_with_errors', 'failed') THEN COALESCE(completed_at, now())
				ELSE NULL
			END
		WHERE id = $1
	`, sessionID, status)
	if err != nil {
		return "", err
	}

	return status, nil
}

func CountsForSession(ctx context.Context, db Queryer, sessionID string) (Counts, error) {
	var counts Counts
	err := db.QueryRow(ctx, `
		SELECT
			count(*)::int,
			count(*) FILTER (WHERE status = 'pending')::int,
			count(*) FILTER (WHERE status = 'uploaded')::int,
			count(*) FILTER (WHERE status = 'processing')::int,
			count(*) FILTER (WHERE status = 'processed')::int,
			count(*) FILTER (WHERE status = 'failed')::int,
			count(*) FILTER (WHERE status NOT IN ('pending', 'uploaded', 'processing', 'processed', 'failed'))::int
		FROM image_assets
		WHERE session_id = $1
	`, sessionID).Scan(
		&counts.Total,
		&counts.Pending,
		&counts.Uploaded,
		&counts.Processing,
		&counts.Processed,
		&counts.Failed,
		&counts.Unexpected,
	)
	if err != nil {
		return Counts{}, err
	}

	return counts, nil
}

func StatusFromCounts(counts Counts) string {
	if counts.Total == 0 {
		return "pending"
	}

	if counts.Unexpected > 0 {
		return "processing"
	}

	inFlight := counts.Pending + counts.Uploaded + counts.Processing
	if inFlight > 0 {
		return "processing"
	}

	if counts.Processed == counts.Total {
		return "processed"
	}

	if counts.Failed == counts.Total {
		return "failed"
	}

	if counts.Processed > 0 && counts.Failed > 0 {
		return "completed_with_errors"
	}

	return "processing"
}
