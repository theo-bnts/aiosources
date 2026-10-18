package connectors

// UNIT3D is a source profile, not another transport. Its expanded definition
// stays editable and uses the same HTTP JSON collector and coverage guarantees.
func unit3dTemplate() string {
	return `{
  "version": 1,
  "id": "example_unit3d",
  "name": "UNIT3D tracker",
  "adapter": "http_json",
  "url": "https://tracker.example/api/torrents/filter",
  "enabled": false,
  "auth": {
    "type": "bearer",
    "secret_ref": "example_unit3d_token"
  },
  "request_interval": "3s",
  "page_size": 25,
  "http": {
    "method": "GET",
    "items_path": "/data",
    "headers": {
      "Accept": "application/json"
    },
    "query": {
      "sortField": "created_at",
      "sortDirection": "asc",
      "categories[]": []
    }
  },
  "pagination": {
    "type": "page",
    "in": "query",
    "page_param": "page",
    "size_param": "perPage",
    "start": 1,
    "current_path": "/meta/current_page",
    "next_path": "/links/next"
  },
  "mapping": {
    "id": "/id",
    "fields": {
      "guid": "/id",
      "title": "/attributes/name",
      "size": "/attributes/size",
      "categories": "/attributes/category_id",
      "category_name": "/attributes/category",
      "published_at": "/attributes/created_at",
      "tmdb_id": "/attributes/tmdb_id",
      "imdb_id": "/attributes/imdb_id",
      "tvdb_id": "/attributes/tvdb_id",
      "mal_id": "/attributes/mal_id",
      "igdb_id": "/attributes/igdb_id",
      "info_hash": "/attributes/info_hash"
    }
  },
  "options": {
    "preserve_query_on_next": true
  },
  "schedule": {
    "known_pages": 0
  },
  "output": {
    "fields": [
      "guid",
      "title",
      "size",
      "categories",
      "category_name",
      "published_at",
      "tmdb_id",
      "imdb_id",
      "tvdb_id",
      "mal_id",
      "igdb_id",
      "info_hash"
    ]
  }
}
`
}
