module github.com/theo-bnts/aiosources

go 1.26.11

require (
	github.com/andybalholm/cascadia v1.3.4
	github.com/fsnotify/fsnotify v1.9.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/theo-bnts/aiosources/torznab v0.0.0
	go.yaml.in/yaml/v3 v3.0.5
	golang.org/x/crypto v0.58.0
	golang.org/x/net v0.62.0
	golang.org/x/sync v0.24.0
	golang.org/x/text v0.43.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	golang.org/x/sys v0.49.0 // indirect
)

replace github.com/theo-bnts/aiosources/torznab => ./torznab
