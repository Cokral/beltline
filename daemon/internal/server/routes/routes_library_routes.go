// The library repository routes: the Library page's repositories and items, and the
// backup, restore, refresh, install and share commands.
package routes

import (
	"net/http"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/kit/coordinator"
	"github.com/robbertmanders/beltline/daemon/internal/server/api"
)

// libraryQuery is a query parameter's value, or "" when it is absent.
func libraryQuery(query, name string) string {
	if value := ReadRouteSupportQuery(name, query); value != nil {
		return *value
	}
	return ""
}

// AddLibraryRepos is GET api/v1/library/repos: every configured repository as last synced.
// Lock: the handler calls LibraryRepos, which takes the lock.
func AddLibraryRepos(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/library/repos", JSONRoute(c, func(r *http.Request) (any, error) {
		return api.NewLibraryReposResponse(c.LibraryRepos()), nil
	}))
}

// AddLibraryItem is GET api/v1/library/item: an item's files beside this library's copies.
// Lock: the handler calls LibraryItemCompare, which takes the lock.
func AddLibraryItem(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("GET /api/v1/library/item", JSONRoute(c, func(r *http.Request) (any, error) {
		query := r.URL.RawQuery
		origin := core.LibraryOrigin{Repository: libraryQuery(query, "repository"), Path: libraryQuery(query, "path")}
		files, err := c.LibraryItemCompare(origin, libraryQuery(query, "kind"), libraryQuery(query, "id"))
		if err != nil {
			return nil, err
		}
		return api.NewLibraryItemResponse(files), nil
	}))
}

// AddLibraryRefresh is POST api/v1/library/refresh: syncs one repository and answers its state.
// Lock: the handler calls RefreshLibraryRepo, which takes the lock; the fetch runs without it.
func AddLibraryRefresh(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/library/refresh", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.LibraryRepoRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		state, err := c.RefreshLibraryRepo(r.Context(), body.Origin())
		if err != nil {
			return nil, err
		}
		return api.NewLibraryRepoResponse(state), nil
	}))
}

// AddLibraryBackup is POST api/v1/library/backup: backs the library up to a backup repository.
// Lock: the handler calls BackupLibrary, which takes the lock; git runs without it.
func AddLibraryBackup(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/library/backup", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.LibraryRepoRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		result, err := c.BackupLibrary(r.Context(), body.Origin())
		if err != nil {
			return nil, err
		}
		return api.LibraryBackupResponse{Commit: result.Commit, Changed: result.Changed}, nil
	}))
}

// AddLibraryRestore is POST api/v1/library/restore: restores the library from a backup repository.
// Lock: the handler calls RestoreLibrary, which takes the lock; git runs without it.
func AddLibraryRestore(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/library/restore", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.LibraryRepoRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		savedTo, err := c.RestoreLibrary(r.Context(), body.Origin())
		if err != nil {
			return nil, err
		}
		return api.LibraryRestoreResponse{SavedTo: savedTo}, nil
	}))
}

// AddLibraryInstall is POST api/v1/library/install: installs or updates an item from a shared repository.
// Lock: the handler calls InstallLibraryItem, which takes the lock; git runs without it.
func AddLibraryInstall(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/library/install", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.LibraryItemRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		written, err := c.InstallLibraryItem(r.Context(), body.Origin(), body.Kind, body.ID)
		if err != nil {
			return nil, err
		}
		return api.LibraryInstallResponse{Written: append([]string{}, written...)}, nil
	}))
}

// AddLibraryShare is POST api/v1/library/share: shares one of this library's items to a shared repository.
// Lock: the handler calls ShareLibraryItem, which takes the lock; git and gh run without it.
func AddLibraryShare(mux *http.ServeMux, c *coordinator.Coordinator) {
	mux.Handle("POST /api/v1/library/share", JSONRoute(c, func(r *http.Request) (any, error) {
		body, err := DecodeBody[api.LibraryItemRequest](r, DefaultBodyLimit)
		if err != nil {
			return nil, err
		}
		result, err := c.ShareLibraryItem(r.Context(), body.Origin(), body.Kind, body.ID)
		if err != nil {
			return nil, err
		}
		return api.LibraryShareResponse{PullRequest: result.PullRequest, Commit: result.Commit, Changed: result.Changed}, nil
	}))
}
