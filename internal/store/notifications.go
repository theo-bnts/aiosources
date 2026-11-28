package store

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/theo-bnts/aiosources/internal/model"
)

var ErrDuplicateReleaseAlert = errors.New("an alert already exists for this target")

const notificationChannelColumns = `c.id,c.name,c.type,c.enabled,c.is_default,c.allow_trusted,c.config,c.secret_ref,c.header_secret_ref,
 (SELECT count(*) FROM aiosources.release_alerts a WHERE a.channel_id=c.id),c.revision,c.created_at,c.updated_at`

func scanNotificationChannel(row scanner) (model.NotificationChannel, error) {
	var c model.NotificationChannel
	var cfg []byte
	err := row.Scan(&c.ID, &c.Name, &c.Type, &c.Enabled, &c.IsDefault, &c.AllowTrusted, &cfg, &c.SecretRef, &c.HeaderSecretRef, &c.ActiveAlerts, &c.Revision, &c.CreatedAt, &c.UpdatedAt)
	if err == nil && len(cfg) > 0 && string(cfg) != "null" {
		err = json.Unmarshal(cfg, &c.Config)
	}
	return c, err
}

func (s *Store) NotificationChannels(ctx context.Context) ([]model.NotificationChannel, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+notificationChannelColumns+` FROM aiosources.notification_channels c ORDER BY c.is_default DESC,lower(c.name),c.id`)
	if err != nil {
		return nil, databaseError("list notification channels", err)
	}
	defer rows.Close()
	out := []model.NotificationChannel{}
	for rows.Next() {
		ch, err := scanNotificationChannel(rows)
		if err != nil {
			return nil, databaseError("read notification channel", err)
		}
		out = append(out, ch)
	}
	return out, databaseError("list notification channels", rows.Err())
}

func (s *Store) NotificationChannel(ctx context.Context, id string) (model.NotificationChannel, error) {
	if !model.ValidNotificationID(id) {
		return model.NotificationChannel{}, model.ErrNotFound
	}
	ch, err := scanNotificationChannel(s.pool.QueryRow(ctx, `SELECT `+notificationChannelColumns+` FROM aiosources.notification_channels c WHERE c.id=$1`, id))
	return ch, databaseError("read notification channel", err)
}

func (s *Store) TrustedNotificationChannels(ctx context.Context) ([]model.NotificationChannelSummary, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,name,type,is_default FROM aiosources.notification_channels WHERE enabled AND allow_trusted ORDER BY is_default DESC,lower(name),id`)
	if err != nil {
		return nil, databaseError("list trusted notification channels", err)
	}
	defer rows.Close()
	out := []model.NotificationChannelSummary{}
	for rows.Next() {
		var item model.NotificationChannelSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.Type, &item.IsDefault); err != nil {
			return nil, databaseError("read trusted notification channel", err)
		}
		out = append(out, item)
	}
	return out, databaseError("list trusted notification channels", rows.Err())
}

func (s *Store) SaveNotificationChannel(ctx context.Context, value model.NotificationChannel) (model.NotificationChannel, error) {
	if err := value.NormalizeAndValidate(); err != nil {
		return model.NotificationChannel{}, model.ErrInvalid
	}
	if value.Revision < 0 {
		return model.NotificationChannel{}, model.ErrInvalid
	}
	cfg, err := json.Marshal(value.Config)
	if err != nil {
		return model.NotificationChannel{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.NotificationChannel{}, databaseError("begin notification channel update", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, providerLockKey("notifications.channels")); err != nil {
		return model.NotificationChannel{}, databaseError("lock notification channels", err)
	}
	if err = lockSecretReferences(ctx, tx, value.SecretReferences(), model.ErrInvalid, "lock notification channel secret"); err != nil {
		return model.NotificationChannel{}, err
	}
	creating := value.ID == ""
	if creating {
		if value.Revision != 0 {
			return model.NotificationChannel{}, model.ErrInvalid
		}
		var count int
		if err = tx.QueryRow(ctx, `SELECT count(*) FROM aiosources.notification_channels`).Scan(&count); err != nil {
			return model.NotificationChannel{}, databaseError("count notification channels", err)
		}
		if count >= model.MaxNotificationChannels {
			return model.NotificationChannel{}, model.ErrConfigurationLimit
		}
		// Automatically make the first enabled channel default for convenience.
		if count == 0 && value.Enabled {
			value.IsDefault = true
		}
		value.ID, err = newRunID()
		if err != nil {
			return model.NotificationChannel{}, err
		}
	} else if !model.ValidNotificationID(value.ID) || value.Revision < 1 {
		return model.NotificationChannel{}, model.ErrInvalid
	}
	if value.IsDefault {
		if _, err = tx.Exec(ctx, `UPDATE aiosources.notification_channels SET is_default=false WHERE is_default AND id<>$1`, value.ID); err != nil {
			return model.NotificationChannel{}, databaseError("clear default notification channel", err)
		}
	}
	var saved model.NotificationChannel
	if creating {
		saved, err = scanNotificationChannel(tx.QueryRow(ctx, `INSERT INTO aiosources.notification_channels(id,name,type,enabled,is_default,allow_trusted,config,secret_ref,header_secret_ref)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
 RETURNING id,name,type,enabled,is_default,allow_trusted,config,secret_ref,header_secret_ref,0::bigint,revision,created_at,updated_at`,
			value.ID, value.Name, value.Type, value.Enabled, value.IsDefault, value.AllowTrusted, cfg, value.SecretRef, value.HeaderSecretRef))
	} else {
		saved, err = scanNotificationChannel(tx.QueryRow(ctx, `UPDATE aiosources.notification_channels c
 SET name=$2,type=$3,enabled=$4,is_default=$5,allow_trusted=$6,config=$7,secret_ref=$8,header_secret_ref=$9,revision=revision+1,updated_at=now()
 WHERE c.id=$1 AND c.revision=$10
 RETURNING `+notificationChannelColumns,
			value.ID, value.Name, value.Type, value.Enabled, value.IsDefault, value.AllowTrusted, cfg, value.SecretRef, value.HeaderSecretRef, value.Revision))
		if errors.Is(err, pgx.ErrNoRows) {
			return model.NotificationChannel{}, model.ErrConflict
		}
	}
	if err != nil {
		return model.NotificationChannel{}, databaseError("save notification channel", err)
	}
	if err = recordActivityTx(ctx, tx, "info", "notifications.channel_saved", "", "", "", map[string]any{"channel_id": saved.ID, "name": saved.Name, "type": saved.Type, "enabled": saved.Enabled}); err != nil {
		return model.NotificationChannel{}, err
	}
	return saved, databaseError("commit notification channel", tx.Commit(ctx))
}

func (s *Store) DeleteNotificationChannel(ctx context.Context, id string, revision int64) error {
	if !model.ValidNotificationID(id) || revision < 1 {
		return model.ErrInvalid
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin notification channel deletion", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, providerLockKey("notifications.channels")); err != nil {
		return databaseError("lock notification channels", err)
	}
	var name string
	var wasDefault bool
	err = tx.QueryRow(ctx, `DELETE FROM aiosources.notification_channels WHERE id=$1 AND revision=$2 RETURNING name,is_default`, id, revision).Scan(&name, &wasDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ErrConflict
	}
	if err != nil {
		return databaseError("delete notification channel", err)
	}
	if wasDefault {
		if _, err = tx.Exec(ctx, `UPDATE aiosources.notification_channels SET is_default=true WHERE id=(SELECT id FROM aiosources.notification_channels WHERE enabled ORDER BY created_at,id LIMIT 1)`); err != nil {
			return databaseError("reassign default notification channel", err)
		}
	}
	if err = recordActivityTx(ctx, tx, "info", "notifications.channel_deleted", "", "", "", map[string]any{"channel_id": id, "name": name}); err != nil {
		return err
	}
	return databaseError("commit notification channel deletion", tx.Commit(ctx))
}

func releaseAlertColumns(locale string) string {
	return `a.id,a.owner_type,a.owner_id,COALESCE(u.email,''),a.work_id,a.media_type,` + catalogTitleLabel(locale, "NULL") + `,` + bookWorkYears + `,
 COALESCE(NULLIF(w.localized_posters->>split_part(` + locale + `,'-',1),''),NULLIF(w.localized_posters->>'en',''),w.poster_path),
 COALESCE(` + bookEditionLocalizedCover(locale) + `,NULLIF((w.book_data->>'cover')::bigint,0),0),
 a.season,a.episode,a.channel_id,c.name,c.type,a.filters,a.status,a.attempts,a.next_attempt,a.last_error,a.revision,a.created_at,a.updated_at`
}

func scanReleaseAlert(row scanner) (model.ReleaseAlert, error) {
	var a model.ReleaseAlert
	var poster string
	var cover int64
	var filters []byte
	err := row.Scan(
		&a.ID, &a.OwnerType, &a.OwnerID, &a.OwnerEmail, &a.WorkID, &a.MediaType,
		&a.Title, &a.Years, &poster, &cover,
		&a.Season, &a.Episode, &a.ChannelID, &a.ChannelName, &a.ChannelType,
		&filters, &a.Status, &a.Attempts, &a.NextAttempt, &a.LastError,
		&a.Revision, &a.CreatedAt, &a.UpdatedAt,
	)
	if err == nil && len(filters) > 0 && string(filters) != "null" {
		err = json.Unmarshal(filters, &a.Filters)
	}
	a.Cover = model.CoverURL(a.MediaType, poster, cover)
	return a, err
}

func (s *Store) ListReleaseAlerts(ctx context.Context, ownerType, ownerID, locale string) ([]model.ReleaseAlert, error) {
	if locale == "" {
		locale = "en"
	}
	var rows pgx.Rows
	var err error
	base := `SELECT ` + releaseAlertColumns("$1") + `
 FROM aiosources.release_alerts a
 JOIN aiosources.aio_works w ON w.id=a.work_id
 JOIN aiosources.notification_channels c ON c.id=a.channel_id
 LEFT JOIN aiosources.trusted_users u ON a.owner_type='trusted' AND u.id=a.owner_id`
	if ownerType == "" {
		rows, err = s.pool.Query(ctx, base+` ORDER BY a.created_at DESC,a.id`, locale)
	} else {
		rows, err = s.pool.Query(ctx, base+` WHERE a.owner_type=$2 AND a.owner_id=$3 ORDER BY a.created_at DESC,a.id`, locale, ownerType, ownerID)
	}
	if err != nil {
		return nil, databaseError("list release alerts", err)
	}
	defer rows.Close()
	out := []model.ReleaseAlert{}
	for rows.Next() {
		item, err := scanReleaseAlert(rows)
		if err != nil {
			return nil, databaseError("read release alert", err)
		}
		out = append(out, item)
	}
	return out, databaseError("list release alerts", rows.Err())
}

func (s *Store) ReleaseAlert(ctx context.Context, id, locale string) (model.ReleaseAlert, error) {
	if !model.ValidNotificationID(id) {
		return model.ReleaseAlert{}, model.ErrNotFound
	}
	if locale == "" {
		locale = "en"
	}
	item, err := scanReleaseAlert(s.pool.QueryRow(ctx, `SELECT `+releaseAlertColumns("$2")+`
 FROM aiosources.release_alerts a
 JOIN aiosources.aio_works w ON w.id=a.work_id
 JOIN aiosources.notification_channels c ON c.id=a.channel_id
 LEFT JOIN aiosources.trusted_users u ON a.owner_type='trusted' AND u.id=a.owner_id
 WHERE a.id=$1`, id, locale))
	return item, databaseError("read release alert", err)
}

func (s *Store) CreateReleaseAlert(ctx context.Context, ownerType, ownerID, mediaType string, input model.ReleaseAlertInput, locale string) (model.ReleaseAlert, error) {
	if err := input.Filters.NormalizeAndValidate(); err != nil {
		return model.ReleaseAlert{}, model.ErrInvalid
	}
	if (ownerType != model.AlertOwnerAdmin && ownerType != model.AlertOwnerTrusted) ||
		(ownerType == model.AlertOwnerAdmin && ownerID != "") ||
		(ownerType == model.AlertOwnerTrusted && ownerID == "") ||
		input.WorkID < 1 || !model.ValidNotificationID(input.ChannelID) {
		return model.ReleaseAlert{}, model.ErrInvalid
	}
	if mediaType == "tv" {
		if input.Season == nil || input.Episode == nil || *input.Season < 0 || *input.Season > 999 || *input.Episode < 1 || *input.Episode > 9999 {
			return model.ReleaseAlert{}, model.ErrInvalid
		}
	} else {
		if input.Season != nil || input.Episode != nil {
			return model.ReleaseAlert{}, model.ErrInvalid
		}
	}
	id, err := newRunID()
	if err != nil {
		return model.ReleaseAlert{}, err
	}
	filtersJSON, err := json.Marshal(input.Filters)
	if err != nil {
		return model.ReleaseAlert{}, err
	}
	limit := model.MaxAdminReleaseAlerts
	if ownerType == model.AlertOwnerTrusted {
		limit = model.MaxTrustedReleaseAlerts
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ReleaseAlert{}, databaseError("begin release alert creation", err)
	}
	defer rollback(tx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, providerLockKey("alerts:"+ownerType+":"+ownerID)); err != nil {
		return model.ReleaseAlert{}, databaseError("lock release alerts", err)
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM aiosources.release_alerts WHERE owner_type=$1 AND owner_id=$2`, ownerType, ownerID).Scan(&count); err != nil {
		return model.ReleaseAlert{}, databaseError("count release alerts", err)
	}
	if count >= limit {
		return model.ReleaseAlert{}, model.ErrConfigurationLimit
	}
	var enabled, allowTrusted bool
	err = tx.QueryRow(ctx, `SELECT enabled,allow_trusted FROM aiosources.notification_channels WHERE id=$1 FOR SHARE`, input.ChannelID).Scan(&enabled, &allowTrusted)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReleaseAlert{}, model.ErrNotFound
	}
	if err != nil {
		return model.ReleaseAlert{}, databaseError("verify notification channel", err)
	}
	if !enabled || (ownerType == model.AlertOwnerTrusted && !allowTrusted) {
		return model.ReleaseAlert{}, model.ErrInvalid
	}
	_, err = tx.Exec(ctx, `INSERT INTO aiosources.release_alerts(id,owner_type,owner_id,work_id,media_type,season,episode,channel_id,filters)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		id, ownerType, ownerID, input.WorkID, mediaType, input.Season, input.Episode, input.ChannelID, filtersJSON)
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return model.ReleaseAlert{}, ErrDuplicateReleaseAlert
	}
	if err != nil {
		return model.ReleaseAlert{}, databaseError("insert release alert", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return model.ReleaseAlert{}, databaseError("commit release alert", err)
	}
	return s.ReleaseAlert(ctx, id, locale)
}

func (s *Store) UpdateReleaseAlert(ctx context.Context, id, ownerType, ownerID string, input model.ReleaseAlertInput, locale string) (model.ReleaseAlert, error) {
	if !model.ValidNotificationID(id) || input.Revision < 1 || !model.ValidNotificationID(input.ChannelID) {
		return model.ReleaseAlert{}, model.ErrInvalid
	}
	if err := input.Filters.NormalizeAndValidate(); err != nil {
		return model.ReleaseAlert{}, model.ErrInvalid
	}
	filtersJSON, err := json.Marshal(input.Filters)
	if err != nil {
		return model.ReleaseAlert{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.ReleaseAlert{}, databaseError("begin release alert update", err)
	}
	defer rollback(tx)
	var existingOwnerType, existingOwnerID string
	var revision int64
	err = tx.QueryRow(ctx, `SELECT owner_type,owner_id,revision FROM aiosources.release_alerts WHERE id=$1 FOR UPDATE`, id).Scan(&existingOwnerType, &existingOwnerID, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReleaseAlert{}, model.ErrNotFound
	}
	if err != nil {
		return model.ReleaseAlert{}, databaseError("lock release alert", err)
	}
	if ownerType == model.AlertOwnerTrusted && (existingOwnerType != model.AlertOwnerTrusted || existingOwnerID != ownerID) {
		return model.ReleaseAlert{}, model.ErrNotFound
	}
	if revision != input.Revision {
		return model.ReleaseAlert{}, model.ErrConflict
	}
	var enabled, allowTrusted bool
	err = tx.QueryRow(ctx, `SELECT enabled,allow_trusted FROM aiosources.notification_channels WHERE id=$1 FOR SHARE`, input.ChannelID).Scan(&enabled, &allowTrusted)
	if errors.Is(err, pgx.ErrNoRows) {
		return model.ReleaseAlert{}, model.ErrNotFound
	}
	if err != nil {
		return model.ReleaseAlert{}, databaseError("verify notification channel", err)
	}
	if !enabled || (existingOwnerType == model.AlertOwnerTrusted && !allowTrusted) {
		return model.ReleaseAlert{}, model.ErrInvalid
	}
	_, err = tx.Exec(ctx, `UPDATE aiosources.release_alerts
 SET channel_id=$2,filters=$3,status='watching',attempts=0,next_attempt=now(),last_error='',revision=revision+1,updated_at=now()
 WHERE id=$1`, id, input.ChannelID, filtersJSON)
	if err != nil {
		return model.ReleaseAlert{}, databaseError("update release alert", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return model.ReleaseAlert{}, databaseError("commit release alert update", err)
	}
	return s.ReleaseAlert(ctx, id, locale)
}

func (s *Store) DeleteReleaseAlert(ctx context.Context, id, ownerType, ownerID string, revision int64) error {
	if !model.ValidNotificationID(id) || revision < 1 {
		return model.ErrInvalid
	}
	var tag pgconn.CommandTag
	var err error
	if ownerType == model.AlertOwnerTrusted {
		tag, err = s.pool.Exec(ctx, `DELETE FROM aiosources.release_alerts WHERE id=$1 AND owner_type='trusted' AND owner_id=$2 AND revision=$3`, id, ownerID, revision)
	} else {
		tag, err = s.pool.Exec(ctx, `DELETE FROM aiosources.release_alerts WHERE id=$1 AND revision=$2`, id, revision)
	}
	if err != nil {
		return databaseError("delete release alert", err)
	}
	if tag.RowsAffected() != 1 {
		var exists bool
		if ownerType == model.AlertOwnerTrusted {
			_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.release_alerts WHERE id=$1 AND owner_type='trusted' AND owner_id=$2)`, id, ownerID).Scan(&exists)
		} else {
			_ = s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM aiosources.release_alerts WHERE id=$1)`, id).Scan(&exists)
		}
		if !exists {
			return model.ErrNotFound
		}
		return model.ErrConflict
	}
	return nil
}

// DueReleaseAlerts returns alerts whose watched work currently has at least one
// published matched torrent and whose next_attempt is due. Unmatched works are
// skipped purely inside PostgreSQL via release_alerts_work_idx.
func (s *Store) DueReleaseAlerts(ctx context.Context, now time.Time, limit int) ([]model.ReleaseAlert, error) {
	if limit < 1 || limit > 200 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `SELECT `+releaseAlertColumns("'en'")+`
 FROM aiosources.release_alerts a
 JOIN aiosources.aio_works w ON w.id=a.work_id
 JOIN aiosources.notification_channels c ON c.id=a.channel_id
 LEFT JOIN aiosources.trusted_users u ON a.owner_type='trusted' AND u.id=a.owner_id
 WHERE a.next_attempt<=$1
  AND c.enabled
  AND (a.owner_type='admin' OR (c.allow_trusted AND u.enabled AND COALESCE((u.permissions->>'alerts_enabled')::boolean,false)))
  AND NOT w.adult
  AND EXISTS(SELECT 1`+catalogReleaseSource+`)
 ORDER BY a.next_attempt,a.created_at,a.id
 LIMIT $2`, now, limit)
	if err != nil {
		return nil, databaseError("list due release alerts", err)
	}
	defer rows.Close()
	out := []model.ReleaseAlert{}
	for rows.Next() {
		item, err := scanReleaseAlert(rows)
		if err != nil {
			return nil, databaseError("read due release alert", err)
		}
		out = append(out, item)
	}
	return out, databaseError("list due release alerts", rows.Err())
}

func (s *Store) NextReleaseAlertRetry(ctx context.Context) (*time.Time, error) {
	var next *time.Time
	err := s.pool.QueryRow(ctx, `SELECT min(next_attempt) FROM aiosources.release_alerts WHERE status='retrying'`).Scan(&next)
	return next, databaseError("read next release alert retry", err)
}

func (s *Store) CompleteReleaseAlert(ctx context.Context, id string, revision int64, details map[string]any) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, databaseError("begin release alert completion", err)
	}
	defer rollback(tx)
	tag, err := tx.Exec(ctx, `DELETE FROM aiosources.release_alerts WHERE id=$1 AND revision=$2`, id, revision)
	if err != nil {
		return false, databaseError("delete delivered release alert", err)
	}
	if tag.RowsAffected() != 1 {
		return false, nil
	}
	if err = recordActivityTx(ctx, tx, "info", "notifications.alert_sent", "", "", "", details); err != nil {
		return false, err
	}
	return true, databaseError("commit release alert completion", tx.Commit(ctx))
}

func (s *Store) DeferReleaseAlert(ctx context.Context, id string, revision int64, nextAttempt time.Time, failure string, logDetails map[string]any) error {
	failure = strings.TrimSpace(failure)
	if len(failure) > 500 {
		failure = failure[:500]
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return databaseError("begin release alert retry update", err)
	}
	defer rollback(tx)
	if failure != "" {
		_, err = tx.Exec(ctx, `UPDATE aiosources.release_alerts
 SET status='retrying',attempts=attempts+1,next_attempt=$3,last_error=$4,updated_at=now()
 WHERE id=$1 AND revision=$2`, id, revision, nextAttempt, failure)
	} else {
		_, err = tx.Exec(ctx, `UPDATE aiosources.release_alerts
 SET next_attempt=$3,updated_at=now()
 WHERE id=$1 AND revision=$2`, id, revision, nextAttempt)
	}
	if err != nil {
		return databaseError("update release alert schedule", err)
	}
	if failure != "" && logDetails != nil {
		if err = recordActivityTx(ctx, tx, "warn", "notifications.alert_failed", "", "", "", logDetails); err != nil {
			return err
		}
	}
	return databaseError("commit release alert schedule", tx.Commit(ctx))
}

// WakeReleaseAlertsForWorks resets next_attempt for watching alerts targeting
// any of the supplied works so newly matched releases are evaluated right away.
// Retrying alerts keep their delivery backoff intact.
func (s *Store) WakeReleaseAlertsForWorks(ctx context.Context, workIDs []int64) (bool, error) {
	if len(workIDs) == 0 {
		return false, nil
	}
	var hasAlerts bool
	err := s.pool.QueryRow(ctx, `WITH woke AS (
 UPDATE aiosources.release_alerts
 SET next_attempt=now()
 WHERE work_id=ANY($1::bigint[]) AND status='watching' AND next_attempt>now()
 RETURNING 1
)
SELECT EXISTS(SELECT 1 FROM woke) OR EXISTS(
 SELECT 1 FROM aiosources.release_alerts
 WHERE work_id=ANY($1::bigint[]) AND status='watching' AND next_attempt<=now()
)`, workIDs).Scan(&hasAlerts)
	return hasAlerts, databaseError("wake release alerts", err)
}
