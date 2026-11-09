package store

// Older public AllDebrid probes used administrator retry plans without keeping
// their origin. Retire those mixed plans once, preserving all cache evidence
// and owned temporary-item cleanup. New administrator requests recreate only
// their own retry plans; visitor checks never create one.
const publicVisitorProbesSchema = `DELETE FROM aiosources.aio_resources WHERE service='alldebrid';`
