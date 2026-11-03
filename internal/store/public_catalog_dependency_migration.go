package store

const publicCatalogDependencySchema = `
UPDATE ingest.aio_public_access SET catalog_enabled=FALSE,revision=revision+1,catalog_generation=catalog_generation+1
WHERE catalog_enabled AND NOT expose_sources;
ALTER TABLE ingest.aio_public_access ADD CONSTRAINT aio_public_catalog_requires_sources CHECK(NOT catalog_enabled OR expose_sources);
`
