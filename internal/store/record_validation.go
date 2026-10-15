package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/theo-bnts/aiosources/internal/model"
)

func recordRepresentationError(err error) bool {
	var dataError *pgconn.PgError
	return errors.As(err, &dataError) && (dataError.Code == "22P02" || dataError.Code == "22P05" || dataError.Code == "22003" || dataError.Code == "22021")
}

// Compact success observations are validated but not archived. Validate a page
// in one savepoint instead of three round trips per record. A data error falls
// back to the original isolated checks, preserving each bad record's diagnostic
// and the page's existing all-or-nothing publication/checkpoint rules.
func validateCompactRecords(ctx context.Context, tx pgx.Tx, items []model.Record, contentType string) (bool, error) {
	if len(items) == 0 {
		return true, nil
	}
	fields, ids, types := make([]string, 0, len(items)), make([]string, 0, len(items)), make([]string, 0, len(items))
	for _, record := range items {
		encoded, err := jsonObject(record.Fields)
		if err != nil {
			return false, nil
		}
		typ := record.ContentType
		if typ == "" {
			typ = contentType
		}
		fields, ids, types = append(fields, string(encoded)), append(ids, record.SourceID), append(types, typ)
	}
	check, err := tx.Begin(ctx)
	if err != nil {
		return false, databaseError("begin page validation", err)
	}
	defer rollback(check)
	var count int64
	var idLength, typeLength int64
	err = check.QueryRow(ctx, `SELECT count(jsonb_typeof(fields::jsonb)),sum(length(source_id)),sum(length(content_type))
 FROM unnest($1::text[],$2::text[],$3::text[]) AS records(fields,source_id,content_type)`, fields, ids, types).Scan(&count, &idLength, &typeLength)
	if recordRepresentationError(err) {
		return false, nil
	}
	if err != nil {
		return false, databaseError("validate compact page", err)
	}
	return true, databaseError("finish page validation", check.Commit(ctx))
}
