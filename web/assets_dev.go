//go:build !production

package webui

import (
	"io/fs"
	"os"
)

// Assets returns the on-disk frontend build during local development.
func Assets() (fs.FS, error) {
	return os.DirFS("web/dist"), nil
}
