package store

import (
	"context"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Each failed playback remains diagnosable, independently of deduplicated
// provider incidents. No external message, source identity, key or URL is stored.
func (s *Store) RecordPlaybackFailure(ctx context.Context, service, stage, code string) error {
	if service != "alldebrid" && service != "torbox" {
		return model.ErrInvalid
	}
	operation := map[string]string{
		"request": "contacting the provider", "cleanup": "releasing temporary checks",
		"inventory": "reading the account inventory", "add": "preparing the torrent",
		"file_list": "reading the file list", "file_selection": "selecting the requested video",
		"unlock": "generating the playback link",
	}[stage]
	reason := map[string]string{
		"upstream": "The provider could not complete the request.",
		"timeout":  "The request timed out.", "authentication": "The provider refused the credential.",
		"parse":            "The provider response could not be used.",
		"no_matching_file": "No unique matching video was found.",
	}[code]
	if reason == "" && code != "" && (model.ProviderCall{Phase: "provider_request", ErrorCode: code}).Valid() {
		reason = model.ProviderFailureMessage(code)
	}
	if operation == "" || reason == "" {
		return model.ErrInvalid
	}
	id, err := newRunID()
	if err != nil {
		return err
	}
	message := model.ServiceName(service) + " playback failed while " + operation + ". " + reason
	_, err = s.pool.Exec(ctx, `INSERT INTO ingest.activity_logs(event_id,level,kind,message,service,data)
VALUES($1,'error','debrid.playback_failed',$2,$3,jsonb_build_object('operation',$4::text,'failure_code',$5::text))`, "activity-"+id, message, service, stage, code)
	return databaseError("record playback failure", err)
}
