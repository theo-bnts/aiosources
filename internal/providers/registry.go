// Package providers manages the authoritative, human-editable JSON definitions.
package providers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/theo-bnts/aiosources/internal/model"
)

// Registry keeps a directory descriptor, so renaming the configured directory
// cannot redirect subsequent operations. All cooperating processes serialize
// reads and mutations with an advisory lock in that same directory.
type Registry struct {
	mu       sync.Mutex
	root     *os.Root
	lock     *os.File
	validate func(model.Provider) error
}

type definition struct {
	name     string
	key      string
	document model.ProviderDocument
	readable bool
}

func New(dir string, validate func(model.Provider) error) (*Registry, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("provider directory is required")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("cannot create provider directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("provider directory must be a real directory, not a symlink")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot open provider directory")
	}
	opened, err := root.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		root.Close()
		return nil, fmt.Errorf("provider directory changed while opening it")
	}
	lock, err := openLock(root)
	if err != nil {
		root.Close()
		return nil, err
	}
	return &Registry{root: root, lock: lock, validate: validate}, nil
}

func openLock(root *os.Root) (*os.File, error) {
	const name = ".registry.lock"
	file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if errors.Is(err, os.ErrExist) {
		info, statErr := root.Lstat(name)
		if statErr != nil || !privateRegular(info) {
			return nil, fmt.Errorf("provider registry lock is not a regular unlinked file")
		}
		file, err = root.OpenFile(name, os.O_RDWR|syscall.O_NONBLOCK, 0)
		if err == nil {
			opened, statErr := file.Stat()
			if statErr != nil || !privateRegular(opened) || !os.SameFile(info, opened) {
				file.Close()
				return nil, fmt.Errorf("provider registry lock changed while opening it")
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("cannot open provider registry lock")
	}
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, fmt.Errorf("cannot protect provider registry lock")
	}
	return file, nil
}

// Close releases the pinned directory and lock descriptors. It waits for any
// operation on this Registry; the lock file itself is intentionally retained.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.root == nil {
		return nil
	}
	err := errors.Join(r.lock.Close(), r.root.Close())
	r.root = nil
	r.lock = nil
	return err
}

func (r *Registry) acquire() error {
	r.mu.Lock()
	if r.root == nil {
		r.mu.Unlock()
		return fmt.Errorf("provider registry is closed")
	}
	for {
		err := syscall.Flock(int(r.lock.Fd()), syscall.LOCK_EX)
		if errors.Is(err, syscall.EINTR) {
			continue
		}
		if err != nil {
			r.mu.Unlock()
			return fmt.Errorf("cannot lock provider registry")
		}
		break
	}
	// Replacing the lock inode would split process coordination. Refuse to use
	// a renamed, hard-linked or symlink-replaced lock rather than silently race.
	info, err := r.root.Lstat(".registry.lock")
	opened, openedErr := r.lock.Stat()
	if err != nil || openedErr != nil || !privateRegular(info) || !os.SameFile(info, opened) {
		r.release()
		return fmt.Errorf("provider registry lock changed; reopen the registry")
	}
	return nil
}

func (r *Registry) release() {
	for {
		if err := syscall.Flock(int(r.lock.Fd()), syscall.LOCK_UN); !errors.Is(err, syscall.EINTR) {
			break
		}
	}
	r.mu.Unlock()
}

func (r *Registry) List() ([]model.ProviderSummary, error) {
	if err := r.acquire(); err != nil {
		return nil, err
	}
	defer r.release()
	definitions, err := r.scan()
	if err != nil {
		return nil, err
	}
	items := make([]model.ProviderSummary, 0, len(definitions))
	for _, entry := range definitions {
		p := entry.document.Provider
		items = append(items, model.ProviderSummary{
			RemoteCatalog: p.IsRemoteCatalog(),
			ID:            entry.key, Name: p.Name, Adapter: p.Adapter, Enabled: p.Enabled,
			Revision: entry.document.Revision, Valid: len(entry.document.Issues) == 0,
			Issues: entry.document.Issues, OutputFields: p.Output.Fields,
		})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}

// WithDefinitions holds the definition lock through the callback so schedule
// reconciliation and due-slot commits cannot race a cooperating JSON writer.
// Keys include repair IDs for invalid definitions. The callback must not call
// another Registry method.
func (r *Registry) WithDefinitions(fn func(map[string]model.ProviderDocument) error) error {
	if err := r.acquire(); err != nil {
		return err
	}
	defer r.release()
	definitions, err := r.scan()
	if err != nil {
		return err
	}
	documents := make(map[string]model.ProviderDocument, len(definitions))
	for _, entry := range definitions {
		documents[entry.key] = entry.document
	}
	return fn(documents)
}

func (r *Registry) Get(id string) (model.ProviderDocument, error) {
	if !ValidID(id) {
		return model.ProviderDocument{}, fmt.Errorf("%w: unsafe provider id", model.ErrInvalid)
	}
	if err := r.acquire(); err != nil {
		return model.ProviderDocument{}, err
	}
	defer r.release()
	definitions, err := r.scan()
	if err != nil {
		return model.ProviderDocument{}, err
	}
	for _, entry := range definitions {
		if entry.key != id {
			continue
		}
		if !entry.readable {
			return entry.document, fmt.Errorf("%w: definition is not a readable regular file", model.ErrInvalid)
		}
		return entry.document, nil
	}
	return model.ProviderDocument{}, model.ErrNotFound
}

func (r *Registry) Save(id, raw, expectedRevision string) (model.ProviderDocument, error) {
	if !ValidID(id) {
		return model.ProviderDocument{}, fmt.Errorf("%w: unsafe provider id", model.ErrInvalid)
	}
	validation := r.Validate(raw)
	if !validation.Valid {
		return model.ProviderDocument{JSON: raw, Issues: validation.Issues}, fmt.Errorf("%w: provider validation failed", model.ErrInvalid)
	}
	if err := r.acquire(); err != nil {
		return model.ProviderDocument{}, err
	}
	defer r.release()
	definitions, err := r.scan()
	if err != nil {
		return model.ProviderDocument{}, err
	}
	var existing *definition
	for i := range definitions {
		if definitions[i].key == id {
			existing = &definitions[i]
			break
		}
	}
	if existing == nil {
		if expectedRevision != "" {
			return model.ProviderDocument{}, model.ErrConflict
		}
		if validation.ID != id {
			return model.ProviderDocument{}, fmt.Errorf("%w: JSON id must match the requested provider id", model.ErrInvalid)
		}
	} else {
		if !existing.readable {
			return model.ProviderDocument{}, fmt.Errorf("%w: unsafe definition file cannot be replaced", model.ErrInvalid)
		}
		if expectedRevision == "" || expectedRevision != existing.document.Revision {
			return model.ProviderDocument{}, model.ErrConflict
		}
		if validation.ID != id && len(existing.document.Issues) == 0 {
			return model.ProviderDocument{}, fmt.Errorf("%w: existing provider id cannot be changed", model.ErrInvalid)
		}
	}
	for i := range definitions {
		entry := &definitions[i]
		if existing != nil && entry.name == existing.name {
			continue
		}
		if entry.document.Provider.ID == validation.ID || entry.key == validation.ID {
			return model.ProviderDocument{}, model.ErrConflict
		}
	}
	var name string
	if existing != nil {
		name = existing.name
	} else {
		name, err = r.newFilename(id)
		if err != nil {
			return model.ProviderDocument{}, err
		}
	}
	data := []byte(raw)
	if err := r.atomicWrite(name, data, expectedRevision); err != nil {
		return model.ProviderDocument{}, err
	}
	return model.ProviderDocument{
		Provider: *validation.Provider, JSON: raw, Revision: revision(data), Issues: []string{},
	}, nil
}

// Delete keeps the mutation lock through the optional admission guard and removal.
// The guard runs only for a readable definition with a matching revision and must
// not call another Registry method.
func (r *Registry) Delete(id, expectedRevision string, before func() error) error {
	return r.DeleteWithCleanup(id, expectedRevision, func(remove func() error) error {
		if before != nil {
			if err := before(); err != nil {
				return err
			}
		}
		return remove()
	})
}

// DeleteWithCleanup keeps definition admission locked through data cleanup and
// removal. Cleanup must call remove before committing; a failed cleanup restores
// the original definition, including when removal's directory sync failed.
func (r *Registry) DeleteWithCleanup(id, expectedRevision string, cleanup func(func() error) error) error {
	if !ValidID(id) {
		return fmt.Errorf("%w: unsafe provider id", model.ErrInvalid)
	}
	if err := r.acquire(); err != nil {
		return err
	}
	defer r.release()
	definitions, err := r.scan()
	if err != nil {
		return err
	}
	for _, entry := range definitions {
		if entry.key != id {
			continue
		}
		if !entry.readable {
			return fmt.Errorf("%w: unsafe definition file cannot be deleted", model.ErrInvalid)
		}
		if expectedRevision == "" || expectedRevision != entry.document.Revision {
			return model.ErrConflict
		}
		removed := false
		remove := func() error {
			if removed {
				return model.ErrConflict
			}
			current, err := r.readDefinition(entry.name)
			if err != nil || revision(current) != expectedRevision {
				return model.ErrConflict
			}
			if err := r.root.Remove(entry.name); err != nil {
				return fmt.Errorf("cannot remove provider definition")
			}
			removed = true
			return r.syncDirectory()
		}
		var err error
		if cleanup == nil {
			err = remove()
		} else {
			err = cleanup(remove)
		}
		if err == nil && !removed {
			return fmt.Errorf("provider cleanup did not remove the definition")
		}
		if err != nil && removed {
			if restore := r.atomicWrite(entry.name, []byte(entry.document.JSON), ""); restore != nil {
				return errors.Join(err, fmt.Errorf("cannot restore provider definition after cleanup failure"))
			}
		}
		return err
	}
	return model.ErrNotFound
}

func (r *Registry) scan() ([]definition, error) {
	dir, err := r.root.Open(".")
	if err != nil {
		return nil, fmt.Errorf("cannot read provider directory")
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return nil, fmt.Errorf("cannot list provider directory")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	definitions := make([]definition, 0, len(entries))
	claims := make(map[string]int)
	for _, entry := range entries {
		name := entry.Name()
		if !definitionFilename(name) {
			continue
		}
		item := definition{name: name, document: model.ProviderDocument{Issues: []string{}}}
		raw, err := r.readDefinition(name)
		if err != nil {
			item.document.Issues = append(item.document.Issues, "definition must be a readable regular file without symlinks or hard links")
		} else {
			item.readable = true
			item.document.JSON = string(raw)
			item.document.Revision = revision(raw)
			validation := r.Validate(item.document.JSON)
			item.document.Issues = validation.Issues
			if validation.Provider != nil {
				item.document.Provider = *validation.Provider
			} else {
				item.document.Provider.ID = validation.ID
			}
			if ValidID(validation.ID) {
				item.key = validation.ID
			}
		}
		if item.key == "" {
			stem := strings.TrimSuffix(name, filepath.Ext(name))
			if ValidID(stem) {
				item.key = stem
			} else {
				item.key = repairID(name, 0)
			}
		}
		claims[item.key]++
		definitions = append(definitions, item)
	}
	used := make(map[string]bool, len(definitions))
	for i := range definitions {
		item := &definitions[i]
		if claims[item.key] > 1 {
			item.document.Issues = append(item.document.Issues, "provider id conflicts with another definition; repair the id or delete this definition")
			for attempt := 0; ; attempt++ {
				candidate := repairID(item.name, attempt)
				if claims[candidate] == 0 && !used[candidate] {
					item.key = candidate
					break
				}
			}
		}
		used[item.key] = true
	}
	return definitions, nil
}

func definitionFilename(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".json") && !strings.HasSuffix(lower, ".example.json")
}

func (r *Registry) newFilename(id string) (string, error) {
	// An existing arbitrary filename need not match its JSON id. Do not let it
	// reserve an unrelated id, including on case-insensitive filesystems.
	for attempt := 0; ; attempt++ {
		name := id + ".json"
		if attempt != 0 {
			name = fmt.Sprintf("%s-%d.json", id, attempt)
		}
		_, err := r.root.Lstat(name)
		if errors.Is(err, os.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", fmt.Errorf("cannot choose provider filename")
		}
	}
}

func repairID(name string, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d", name, attempt)))
	return "repair-" + hex.EncodeToString(sum[:16])
}

func revision(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func privateRegular(info os.FileInfo) bool {
	if info == nil || !info.Mode().IsRegular() {
		return false
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}

func (r *Registry) readDefinition(name string) ([]byte, error) {
	info, err := r.root.Lstat(name)
	if err != nil || !privateRegular(info) {
		return nil, fmt.Errorf("unsafe provider file")
	}
	file, err := r.root.OpenFile(name, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil || !privateRegular(opened) || !os.SameFile(info, opened) {
		return nil, fmt.Errorf("provider file changed while opening it")
	}
	// Existing invalid files are returned byte-for-byte for repair, including
	// oversized ones. The 128 KiB submission limit is enforced by Validate.
	raw, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	after, err := r.root.Lstat(name)
	if err != nil || !privateRegular(after) || !os.SameFile(opened, after) || after.Size() != int64(len(raw)) || !after.ModTime().Equal(opened.ModTime()) {
		return nil, fmt.Errorf("provider file changed while reading it")
	}
	return raw, nil
}

func (r *Registry) atomicWrite(name string, raw []byte, expectedRevision string) error {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return fmt.Errorf("cannot generate temporary provider filename")
	}
	temporary := ".provider-" + hex.EncodeToString(random[:]) + ".tmp"
	file, err := r.root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return fmt.Errorf("cannot create temporary provider file")
	}
	defer r.root.Remove(temporary)
	if err = file.Chmod(0600); err == nil {
		_, err = file.Write(raw)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return fmt.Errorf("cannot persist provider definition")
	}
	if expectedRevision != "" {
		current, err := r.readDefinition(name)
		if err != nil || revision(current) != expectedRevision {
			return model.ErrConflict
		}
		if err := r.root.Rename(temporary, name); err != nil {
			return fmt.Errorf("cannot atomically replace provider definition")
		}
	} else {
		// A hard-link install provides atomic create-if-absent without overwriting
		// an unrelated file created after the directory scan.
		if err := r.root.Link(temporary, name); err != nil {
			if errors.Is(err, os.ErrExist) {
				return model.ErrConflict
			}
			return fmt.Errorf("cannot atomically create provider definition")
		}
		if err := r.root.Remove(temporary); err != nil {
			return fmt.Errorf("cannot finalize provider definition")
		}
	}
	return r.syncDirectory()
}

func (r *Registry) syncDirectory() error {
	dir, err := r.root.Open(".")
	if err != nil {
		return fmt.Errorf("cannot open provider directory for synchronization")
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("cannot persist provider directory changes")
	}
	return nil
}
